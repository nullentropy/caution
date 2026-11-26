package gfx

type LayerSpan struct {
	End       int
	Cacheable bool
}

type DamagePlan struct {
	Rects  []Rect
	Layers map[int]LayerSpan
}

const maxDamageRects = 12

func PlanDamage(dl *DisplayList, view Rect) DamagePlan {
	plan := DamagePlan{Layers: make(map[int]LayerSpan)}
	var open []int
	cacheable := make(map[int]bool)
	var rects []Rect

	damage := func(r Rect) {
		if len(open) > 0 {
			r = dl.Cmds[open[0]].Rect
		}
		r = Intersect(r, view)
		if r.W > 0 && r.H > 0 {
			rects = append(rects, r)
		}
	}
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
				continue
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

func TextInk(f Font) float32 { return max32(2, f.Size) }

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
		return c.Rect
	default:
		return Intersect(Inflate(c.Rect, 1), Inflate(c.Clip, 1))
	}
}

const maxDiffRects = 64

const diffAreaLimit = 0.6

func DiffDamage(prev, next *DisplayList, view Rect, animated []Rect) ([]Rect, bool) {
	if prev == nil || next == nil {
		return nil, false
	}
	pc, nc := prev.Cmds, next.Cmds
	rects := append([]Rect(nil), animated...)
	if len(pc) == len(nc) {
		var pOpen, nOpen []int
		for i := range pc {
			if !CmdEqual(&pc[i], &nc[i]) {
				if isLayerCmd(pc[i].Kind) != isLayerCmd(nc[i].Kind) {
					return nil, false
				}
				rects = addDamage(rects, pc, pOpen, i, view)
				rects = addDamage(rects, nc, nOpen, i, view)
			}
			if isLayerCmd(pc[i].Kind) {
				pOpen = trackLayers(pOpen, pc, i)
			}
			if isLayerCmd(nc[i].Kind) {
				nOpen = trackLayers(nOpen, nc, i)
			}
		}
	} else {
		p, s := commonEnds(pc, nc)
		if hasLayerCmd(pc[p:len(pc)-s]) || hasLayerCmd(nc[p:len(nc)-s]) {
			return nil, false
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
