package gfx

// LayerSpan describes one BeginLayer..EndLayer range by command index.
type LayerSpan struct {
	// End is the index of the matching CmdLayerEnd.
	End int
	// Cacheable means nothing strictly inside the range animates: the
	// layer's texture is identical on every replay of this list, so partial
	// frames keep it and re-run only the composite
	Cacheable bool
}

// DamagePlan is what partial invalidation knows about a retained display
// list
type DamagePlan struct {
	// Rects are the merged damage regions: the rects of Animated commands,
	// each promoted to its outermost enclosing layer (a composite shader may
	// map any texel anywhere in its output, so damage inside a layer dirties
	// the whole composite footprint). Empty means the list has no
	// self-animating content and cannot drive partial frames.
	Rects []Rect
	// Layers maps each CmdLayerBegin index to its span.
	Layers map[int]LayerSpan
}

// maxDamageRects caps the scissor passes per partial frame. Beyond it the
// rects collapse into one bounding box (more area, fewer passes).
const maxDamageRects = 12

// PlanDamage scans a display list once. view clamps damage to the viewport.
func PlanDamage(dl *DisplayList, view Rect) DamagePlan {
	plan := DamagePlan{Layers: make(map[int]LayerSpan)}
	var open []int // begin indexes of the currently open layers
	cacheable := make(map[int]bool)
	var rects []Rect

	// damage records an animated command's footprint, promoted to the
	// outermost enclosing layer when nested.
	damage := func(r Rect) {
		if len(open) > 0 {
			r = dl.Cmds[open[0]].Rect
		}
		r = Intersect(r, view)
		if r.W > 0 && r.H > 0 {
			rects = append(rects, r)
		}
	}
	// An animated command inside a layer means that layer's texture changes
	// every frame, so it and every enclosing layer stop being cacheable.
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
				continue // unbalanced; PaintTree can't produce this
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

// mergeRects unions overlapping rects to a fixpoint, so a rect is never
// replayed twice, then applies the pass cap.
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

// TextInk is the slack around a measured line box that covers ink the box
// does not: accents and marks that reach above the face's ascent, left and
// right side bearings, italic overhang.
func TextInk(f Font) float32 { return max32(2, f.Size) }

// PaintedBounds is a conservative bound on the pixels a command can write:
// the vertex shader inflates rect-mode quads by 1px for AA and shadow quads
// by blur*2.5+1 (see render/shaders.go), and the fragment clip has ~1px of
// AA slack. Text is its measured line box plus TextInk, or, when the list
// was built without a Measure hook, its clip alone, which for unclipped text
// matches every damage rect.
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
		// These record no clip: a shader quad's rect is clip-intersected at
		// record time and painted. Layer rects are consulted by the
		// replayer directly. Intersecting with their zero-value Clip would
		// wrongly produce empty bounds.
		return c.Rect
	default:
		return Intersect(Inflate(c.Rect, 1), Inflate(c.Clip, 1))
	}
}

// -- real damage -------------------------------------------------------------

// maxDiffRects bounds the accumulator while diffing. A frame that changed
// everything (a theme tween) must not pay a quadratic merge, so past this
// many regions the accumulator collapses to its bounding box and the area
// guard below decides whether scissoring is still worth it.
const maxDiffRects = 64

// diffAreaLimit is the fraction of the view past which a partial frame stops
// paying: the scissor passes and their clears cost more than one clean sweep.
const diffAreaLimit = 0.6

// DiffDamage compares the list about to be rendered against the one whose
// pixels the scene framebuffer already holds, and returns the regions where
// the two can differ: the painted bounds of every command that changed, taken
// from *both* lists (a command that moved dirties where it was and where it
// went) and promoted to the outermost enclosing layer, since a composite
// shader may map any texel anywhere in its output.
//
// Outside those rects, every command that touches a pixel is byte-identical and
// in the same painter's order, so the scene already holds the right pixels. That
// is what lets an op touching one label repaint one label instead of the world.
//
// animated seeds the result with the new list's animation damage: those
// commands are identical frame to frame, but their u_time is not.
//
// ok=false means "render the whole frame instead": nothing retained, layer
// structure moved (the one case the index-aligned walk cannot reason about),
// or the damage covers so much of the view that scissoring it is a loss.
func DiffDamage(prev, next *DisplayList, view Rect, animated []Rect) ([]Rect, bool) {
	if prev == nil || next == nil {
		return nil, false
	}
	pc, nc := prev.Cmds, next.Cmds
	rects := append([]Rect(nil), animated...)
	if len(pc) == len(nc) {
		// The common case: a widget repainting itself produces the same
		// shape of list, so the two are index-aligned and every difference
		// is exact.
		var pOpen, nOpen []int
		for i := range pc {
			if !CmdEqual(&pc[i], &nc[i]) {
				if isLayerCmd(pc[i].Kind) != isLayerCmd(nc[i].Kind) {
					// Layer nesting diverges from here on: commands after it
					// may paint into a different target than they did, which
					// this walk cannot bound. Take the full frame.
					return nil, false
				}
				rects = addDamage(rects, pc, pOpen, i, view)
				rects = addDamage(rects, nc, nOpen, i, view)
			}
			// Only layer commands move the stack, and they are a rounding
			// error in a real list, so skip the call for the other 99%.
			if isLayerCmd(pc[i].Kind) {
				pOpen = trackLayers(pOpen, pc, i)
			}
			if isLayerCmd(nc[i].Kind) {
				nOpen = trackLayers(nOpen, nc, i)
			}
		}
	} else {
		// A command appeared or vanished, so indexes past it shifted: trim
		// the common prefix and suffix and damage everything between, in
		// both lists.
		p, s := commonEnds(pc, nc)
		if hasLayerCmd(pc[p:len(pc)-s]) || hasLayerCmd(nc[p:len(nc)-s]) {
			return nil, false // as above: the target stack itself moved
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

// trackLayers maintains the open-layer stack across a single-pass walk. A
// CmdLayerEnd closes at its own index, so a walker damages *before* calling
// this.
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

// addRange damages cmds[lo:hi). The layer stack has to be re-derived from the
// start of the list, because a range's enclosing layers are whatever is open when the
// walk reaches it.
func addRange(out []Rect, cmds []Cmd, lo, hi int, view Rect) []Rect {
	var open []int
	for i := 0; i < hi; i++ {
		if i >= lo {
			out = addDamage(out, cmds, open, i, view)
		}
		open = trackLayers(open, cmds, i)
	}
	return out
}

// commonEnds is the length of the identical prefix and, past it, the identical
// suffix of two lists.
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

// CmdEqual reports whether two commands paint identically. Every field
// participates: a difference this misses is a stale pixel, so
// TestCmdEqualSeesEveryField walks Cmd reflectively and fails if a new field
// is not compared here.
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
