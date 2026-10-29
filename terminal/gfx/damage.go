package gfx

// one BeginLayer..EndLayer range, by command index
type LayerSpan struct {
	// index of the matching CmdLayerEnd
	End int
	// nothing strictly inside animates, so the texture is identical on every
	// replay and partial frames only re-run the composite
	Cacheable bool
}

type DamagePlan struct {
	// merged damage, each Animated rect promoted to its outermost layer since
	// a composite may map any texel anywhere in its output
	// empty means nothing self-animates and the list can't drive partial frames
	Rects []Rect
	// keyed by CmdLayerBegin index
	Layers map[int]LayerSpan
}

// scissor passes per partial frame, past it the rects collapse into one
// bounding box (more area, fewer passes)
const maxDamageRects = 12

// single pass, view clamps damage to the viewport
func PlanDamage(dl *DisplayList, view Rect) DamagePlan {
	plan := DamagePlan{Layers: make(map[int]LayerSpan)}
	var open []int // begin indexes of the currently open layers
	cacheable := make(map[int]bool)
	var rects []Rect

	// promoted to the outermost enclosing layer when nested
	damage := func(r Rect) {
		if len(open) > 0 {
			r = dl.Cmds[open[0]].Rect
		}
		r = Intersect(r, view)
		if r.W > 0 && r.H > 0 {
			rects = append(rects, r)
		}
	}
	// animating inside a layer changes its texture every frame, so it and
	// every enclosing layer stop being cacheable
	poison := func() {
		for _, b := range open {
			cacheable[b] = false
		}
	}

	for i := range dl.Cmds {
		c := &dl.Cmds[i]
		switch c.Kind {
		case CmdLayerBegin:
			open = append(open, i)
			cacheable[i] = true
		case CmdLayerEnd:
			if len(open) == 0 {
				continue // unbalanced, PaintTree can't produce this
			}
			begin := open[len(open)-1]
			open = open[:len(open)-1]
			plan.Layers[begin] = LayerSpan{End: i, Cacheable: cacheable[begin]}
			if c.Animated {
				poison()
				damage(c.Rect)
			}
		case CmdShaderQuad:
			if c.Animated {
				poison()
				damage(c.Rect)
			}
		}
	}

	plan.Rects = mergeRects(rects)
	return plan
}

// union overlapping rects to a fixpoint so nothing replays twice, then
// apply the pass cap
func mergeRects(rs []Rect) []Rect {
	for {
		merged := false
		for i := 0; i < len(rs) && !merged; i++ {
			for j := i + 1; j < len(rs); j++ {
				if Overlaps(rs[i], rs[j]) {
					rs[i] = Union(rs[i], rs[j])
					rs = append(rs[:j], rs[j+1:]...)
					merged = true
					break
				}
			}
		}
		if !merged {
			break
		}
	}
	if len(rs) > maxDamageRects {
		rs = []Rect{boundingBox(rs)}
	}
	return rs
}

func boundingBox(rs []Rect) Rect {
	box := rs[0]
	for _, r := range rs[1:] {
		box = Union(box, r)
	}
	return box
}

// slack around a measured line box for ink outside it: marks above the
// ascent, side bearings, italic overhang
func TextInk(f Font) float32 { return max32(2, f.Size) }

// conservative bound on the pixels a command can write. the vertex shader
// inflates rect quads by 1px for AA and shadow quads by blur*2.5+1 (see
// render/shaders.go), and the fragment clip has ~1px of AA slack
// text is its line box plus TextInk, or its clip alone when built without
// Measure, which for unclipped text matches every damage rect
func PaintedBounds(c *Cmd) Rect {
	switch c.Kind {
	case CmdText:
		if c.Rect.W > 0 && c.Rect.H > 0 {
			return Intersect(Inflate(c.Rect, TextInk(c.Font)), Inflate(c.Clip, 1))
		}
		return Inflate(c.Clip, 1)
	case CmdShadow:
		return Intersect(Inflate(c.Rect, c.Blur*2.5+1), Inflate(c.Clip, 1))
	case CmdShaderQuad, CmdLayerBegin, CmdLayerEnd:
		// these record no clip. shader quad rects are clip-intersected at record
		// time, layer rects are read by the replayer directly. intersecting with
		// the zero Clip would give empty bounds
		return c.Rect
	default:
		return Intersect(Inflate(c.Rect, 1), Inflate(c.Clip, 1))
	}
}

// -- real damage -------------------------------------------------------------

// caps the diff accumulator. a frame that changed everything (theme tween)
// must not pay a quadratic merge, so past this it collapses to its bounding
// box and the area guard decides if scissoring still pays
const maxDiffRects = 64

// fraction of the view past which scissor passes and their clears cost
// more than one clean sweep
const diffAreaLimit = 0.6

// prev is whatever the scene framebuffer already holds. result is the
// painted bounds of every changed command from *both* lists (a move dirties
// where it was and where it went), promoted to the outermost layer since a
// composite may map any texel anywhere
//
// outside those rects every command touching a pixel is byte-identical and in
// the same order, so an op touching one label repaints one label, not the world
//
// animated seeds the result: those commands are identical frame to frame but
// their u_time isn't
//
// ok=false means render the whole frame: nothing retained, layer structure
// moved (the index-aligned walk can't reason about it), or the damage covers
// so much of the view that scissoring loses
func DiffDamage(prev, next *DisplayList, view Rect, animated []Rect) ([]Rect, bool) {
	if prev == nil || next == nil {
		return nil, false
	}
	pc, nc := prev.Cmds, next.Cmds
	rects := append([]Rect(nil), animated...)
	if len(pc) == len(nc) {
		// common case: a widget repainting itself keeps the list shape, so
		// the two are index-aligned and every difference is exact
		var pOpen, nOpen []int
		for i := range pc {
			if !CmdEqual(&pc[i], &nc[i]) {
				if isLayerCmd(pc[i].Kind) != isLayerCmd(nc[i].Kind) {
					// layer nesting diverges here, later commands may paint into
					// a different target and this walk can't bound that
					return nil, false
				}
				rects = addDamage(rects, pc, pOpen, i, view)
				rects = addDamage(rects, nc, nOpen, i, view)
			}
			// only layer commands move the stack and they're rare, skip the
			// call for the other 99%
			if isLayerCmd(pc[i].Kind) {
				pOpen = trackLayers(pOpen, pc, i)
			}
			if isLayerCmd(nc[i].Kind) {
				nOpen = trackLayers(nOpen, nc, i)
			}
		}
	} else {
		// a command appeared or vanished so indexes shifted. trim the common
		// prefix and suffix and damage everything between, in both lists
		p, s := commonEnds(pc, nc)
		if hasLayerCmd(pc[p:len(pc)-s]) || hasLayerCmd(nc[p:len(nc)-s]) {
			return nil, false // as above, the target stack moved
		}
		rects = addRange(rects, pc, p, len(pc)-s, view)
		rects = addRange(rects, nc, p, len(nc)-s, view)
	}
	rects = mergeRects(rects)
	var area float32
	for _, r := range rects {
		area += r.W * r.H
	}
	if area > diffAreaLimit*view.W*view.H {
		return nil, false
	}
	return rects, true
}

func isLayerCmd(k CmdKind) bool { return k == CmdLayerBegin || k == CmdLayerEnd }

func hasLayerCmd(cmds []Cmd) bool {
	for i := range cmds {
		if isLayerCmd(cmds[i].Kind) {
			return true
		}
	}
	return false
}

// CmdLayerEnd closes at its own index, so damage *before* calling this
func trackLayers(open []int, cmds []Cmd, i int) []int {
	switch cmds[i].Kind {
	case CmdLayerBegin:
		return append(open, i)
	case CmdLayerEnd:
		if len(open) > 0 {
			return open[:len(open)-1]
		}
	}
	return open
}

func addDamage(out []Rect, cmds []Cmd, open []int, i int, view Rect) []Rect {
	r := PaintedBounds(&cmds[i])
	if len(open) > 0 {
		r = cmds[open[0]].Rect
	}
	r = Intersect(r, view)
	if r.W <= 0 || r.H <= 0 {
		return out
	}
	out = append(out, r)
	if len(out) > maxDiffRects {
		out = []Rect{boundingBox(out)}
	}
	return out
}

// damages cmds[lo:hi). the layer stack is re-derived from the start since a
// range's enclosing layers are whatever is open when the walk reaches it
func addRange(out []Rect, cmds []Cmd, lo, hi int, view Rect) []Rect {
	var open []int
	for i := range hi {
		if i >= lo {
			out = addDamage(out, cmds, open, i, view)
		}
		open = trackLayers(open, cmds, i)
	}
	return out
}

// identical prefix length and, past it, identical suffix length
func commonEnds(a, b []Cmd) (prefix, suffix int) {
	n := min(len(a), len(b))
	for prefix < n && CmdEqual(&a[prefix], &b[prefix]) {
		prefix++
	}
	for suffix < n-prefix && CmdEqual(&a[len(a)-1-suffix], &b[len(b)-1-suffix]) {
		suffix++
	}
	return prefix, suffix
}

// every field participates, a miss is a stale pixel
// TestCmdEqualSeesEveryField fails when a new Cmd field isn't compared here
func CmdEqual(a, b *Cmd) bool {
	if a.Kind != b.Kind || a.Rect != b.Rect || a.Radii != b.Radii || a.Clip != b.Clip ||
		a.Animated != b.Animated || a.Color != b.Color || a.Border != b.Border ||
		a.BorderWidth != b.BorderWidth || a.Blur != b.Blur ||
		a.X != b.X || a.Y != b.Y || a.Text != b.Text || a.Font != b.Font ||
		a.Src != b.Src || a.Fit != b.Fit || a.Frag != b.Frag || a.UV != b.UV {
		return false
	}
	if len(a.Uniforms) != len(b.Uniforms) {
		return false
	}
	for k, v := range a.Uniforms {
		if bv, ok := b.Uniforms[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
