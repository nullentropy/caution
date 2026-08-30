package ui

import (
	"github.com/nullentropy/caution/go/terminal/gfx"
)

// heightAt is the height a child wants at width w: explicit > height-for-
// width > fallback.
func heightAt(c Widget, w, fallback float32) float32 {
	cb := c.Base()
	if cb.Height != nil {
		return *cb.Height
	}
	if h, ok := c.HeightForWidth(max(0, w)); ok {
		return h
	}
	return fallback
}

// Dock is a WinForms/WPF-style DockPanel: children consume space from the
// remaining rect in declaration order according to their `dock` side; a
// 'fill' child takes whatever is left.
type Dock struct {
	Core
	Padding float32
	Gap     float32
}

func NewDock() *Dock { d := &Dock{}; d.self = d; return d }

func (d *Dock) LayoutChildren() {
	d.adopt()
	rem := gfx.Inset(d.Bounds, d.Padding)
	for _, c := range d.Kids {
		cb := c.Base()
		sz := sizeOf(c)
		var b gfx.Rect
		switch cb.Dock {
		case "top":
			h := min(heightAt(c, rem.W, sz.H), rem.H)
			b = gfx.R(rem.X, rem.Y, rem.W, h)
			rem.Y += h + d.Gap
			rem.H = max(0, rem.H-h-d.Gap)
		case "bottom":
			h := min(heightAt(c, rem.W, sz.H), rem.H)
			b = gfx.R(rem.X, rem.Y+rem.H-h, rem.W, h)
			rem.H = max(0, rem.H-h-d.Gap)
		case "left":
			w := min(sz.W, rem.W)
			b = gfx.R(rem.X, rem.Y, w, rem.H)
			rem.X += w + d.Gap
			rem.W = max(0, rem.W-w-d.Gap)
		case "right":
			w := min(sz.W, rem.W)
			b = gfx.R(rem.X+rem.W-w, rem.Y, w, rem.H)
			rem.W = max(0, rem.W-w-d.Gap)
		default: // fill
			b = rem
		}
		cb.Bounds = b
		layoutSubtree(c)
	}
}

type StackAlign = string // "start" (default) | "center" | "end" | "stretch"

// Stack is an NSStackView-style run of children along one axis. Main-axis
// size per child comes from its StackSize (content | fixed | fill-by-weight);
// the cross axis follows Align. No wrapping, no flex algebra.
type Stack struct {
	Core
	Axis    string // "h" | "v"
	Padding float32
	Spacing float32
	Align   StackAlign
}

func NewHStack() *Stack { s := &Stack{Axis: "h", Spacing: 8}; s.self = s; return s }
func NewVStack() *Stack { s := &Stack{Axis: "v", Spacing: 8}; s.self = s; return s }

func (s *Stack) IntrinsicSize() Size {
	s.adopt()
	main := s.Spacing * float32(max(0, len(s.Kids)-1))
	var cross float32
	for _, c := range s.Kids {
		sz := sizeOf(c)
		ss := c.Base().StackSize
		if ss.Kind == "fixed" {
			main += ss.Px
		} else if s.Axis == "h" {
			main += sz.W
		} else {
			main += sz.H
		}
		if s.Axis == "h" {
			cross = max(cross, sz.H)
		} else {
			cross = max(cross, sz.W)
		}
	}
	pad := s.Padding * 2
	if s.Axis == "h" {
		return Size{main + pad, cross + pad}
	}
	return Size{cross + pad, main + pad}
}

func (s *Stack) LayoutChildren() {
	s.adopt()
	if len(s.Kids) == 0 {
		return
	}
	r := gfx.Inset(s.Bounds, s.Padding)
	mainAvail, crossAvail := r.W, r.H
	if s.Axis == "v" {
		mainAvail, crossAvail = r.H, r.W
	}

	used := s.Spacing * float32(len(s.Kids)-1)
	var weightSum float32
	mains := make([]float32, len(s.Kids))
	for i, c := range s.Kids {
		ss := c.Base().StackSize
		if ss.Kind == "fill" {
			weightSum += ss.Weight
			continue
		}
		sz := sizeOf(c)
		m := sz.W
		if s.Axis == "v" {
			// A v-stack knows each child's eventual width, so wrapped labels
			// can restate their height before the main-axis pass.
			cw := min(sz.W, crossAvail)
			if s.Align == "stretch" {
				cw = crossAvail
			}
			m = heightAt(c, cw, sz.H)
		}
		if ss.Kind == "fixed" {
			m = ss.Px
		}
		mains[i] = m
		used += m
	}
	leftover := max(0, mainAvail-used)

	pos := r.X
	if s.Axis == "v" {
		pos = r.Y
	}
	for i, c := range s.Kids {
		ss := c.Base().StackSize
		main := mains[i]
		if ss.Kind == "fill" && weightSum > 0 {
			main = leftover * ss.Weight / weightSum
		}
		sz := sizeOf(c)
		// Cross-axis children never exceed the stack's extent (labels
		// truncate at the boundary instead of overflowing).
		crossNat := sz.H
		if s.Axis == "v" {
			crossNat = sz.W
		}
		cross := min(crossNat, crossAvail)
		if s.Align == "stretch" {
			cross = crossAvail
		}
		crossPos := r.Y
		if s.Axis == "v" {
			crossPos = r.X
		}
		switch s.Align {
		case "center":
			crossPos += (crossAvail - cross) / 2
		case "end":
			crossPos += crossAvail - cross
		}
		if s.Axis == "h" {
			c.Base().Bounds = gfx.R(pos, crossPos, main, cross)
		} else {
			c.Base().Bounds = gfx.R(crossPos, pos, cross, main)
		}
		pos += main + s.Spacing
		layoutSubtree(c)
	}
}

// Scroller is the base for anything with a vertical scroll offset: clamping,
// wheel deltas (routed here by the Ui shell), and the draggable overlay
// thumb. Embedders define the scrolled viewport via viewportFn and set
// ContentH during layout.
type Scroller struct {
	Core
	ScrollY  float32
	ScrollX  float32
	ContentH float32
	ContentW float32

	dragGrab   *float32
	dragGrabX  *float32
	viewportFn func() gfx.Rect
}

// ScrollTarget is what the Ui shell's wheel routing looks for.
type ScrollTarget interface {
	Widget
	ScrollBy(dy float32)
	Scrollable() bool
}

// HScrollTarget is the horizontal half; Scroller implements both.
type HScrollTarget interface {
	ScrollByX(dx float32)
	ScrollableX() bool
}

func (s *Scroller) viewport() gfx.Rect {
	if s.viewportFn != nil {
		return s.viewportFn()
	}
	return s.Bounds
}

func (s *Scroller) maxScroll() float32 {
	return max(0, s.ContentH-s.viewport().H)
}

func (s *Scroller) maxScrollX() float32 {
	return max(0, s.ContentW-s.viewport().W)
}

func (s *Scroller) Scrollable() bool  { return s.maxScroll() > 0.5 }
func (s *Scroller) ScrollableX() bool { return s.maxScrollX() > 0.5 }

func (s *Scroller) clampScroll() {
	s.ScrollY = min(max(s.ScrollY, 0), s.maxScroll())
	s.ScrollX = min(max(s.ScrollX, 0), s.maxScrollX())
}

func (s *Scroller) ScrollBy(dy float32) {
	next := min(max(s.ScrollY+dy, 0), s.maxScroll())
	if next != s.ScrollY {
		s.ScrollY = next
		s.Invalidate()
	}
}

func (s *Scroller) ScrollByX(dx float32) {
	next := min(max(s.ScrollX+dx, 0), s.maxScrollX())
	if next != s.ScrollX {
		s.ScrollX = next
		s.Invalidate()
	}
}

func (s *Scroller) thumbRect() (gfx.Rect, bool) {
	if !s.Scrollable() {
		return gfx.Rect{}, false
	}
	v := s.viewport()
	track := v.H - 8
	h := max(28, track*(v.H/s.ContentH))
	y := v.Y + 4 + (s.ScrollY/s.maxScroll())*(track-h)
	return gfx.R(v.X+v.W-10, y, 6, h), true
}

func (s *Scroller) hThumbRect() (gfx.Rect, bool) {
	if !s.ScrollableX() {
		return gfx.Rect{}, false
	}
	v := s.viewport()
	track := v.W - 8
	w := max(28, track*(v.W/s.ContentW))
	x := v.X + 4 + (s.ScrollX/s.maxScrollX())*(track-w)
	return gfx.R(x, v.Y+v.H-10, w, 6), true
}

func (s *Scroller) PaintOverlay(dl *gfx.DisplayList) {
	if t, ok := s.thumbRect(); ok {
		alpha := float32(0.25)
		if s.dragGrab != nil {
			alpha = 0.5
		}
		dl.Fill(t, gfx.WithAlpha(*Theme["ink"], alpha), gfx.CornerRadius(3))
	}
	if t, ok := s.hThumbRect(); ok {
		alpha := float32(0.25)
		if s.dragGrabX != nil {
			alpha = 0.5
		}
		dl.Fill(t, gfx.WithAlpha(*Theme["ink"], alpha), gfx.CornerRadius(3))
	}
}

func (s *Scroller) thumbDown(x, y float32) bool {
	if t, ok := s.thumbRect(); ok && Contains(t, x, y) {
		grab := y - t.Y
		s.dragGrab = &grab
		s.Invalidate()
		return true
	}
	if t, ok := s.hThumbRect(); ok && Contains(t, x, y) {
		grab := x - t.X
		s.dragGrabX = &grab
		s.Invalidate()
		return true
	}
	return false
}

func (s *Scroller) thumbDrag(x, y float32) bool {
	if s.dragGrab != nil {
		v := s.viewport()
		track := v.H - 8
		h := max(28, track*(v.H/s.ContentH))
		rng := track - h
		if rng > 0 {
			ratio := (y - *s.dragGrab - (v.Y + 4)) / rng
			next := min(max(ratio*s.maxScroll(), 0), s.maxScroll())
			if next != s.ScrollY {
				s.ScrollY = next
				s.Invalidate()
			}
		}
		return true
	}
	if s.dragGrabX != nil {
		v := s.viewport()
		track := v.W - 8
		w := max(28, track*(v.W/s.ContentW))
		rng := track - w
		if rng > 0 {
			ratio := (x - *s.dragGrabX - (v.X + 4)) / rng
			next := min(max(ratio*s.maxScrollX(), 0), s.maxScrollX())
			if next != s.ScrollX {
				s.ScrollX = next
				s.Invalidate()
			}
		}
		return true
	}
	return false
}

func (s *Scroller) thumbUp() {
	if s.dragGrab != nil || s.dragGrabX != nil {
		s.dragGrab = nil
		s.dragGrabX = nil
		s.Invalidate()
	}
}

// ScrollView is a vertically scrollable viewport for ordinary widget
// children. Children lay out (frame/anchors) against a virtual content rect
// whose height is their combined extent; the scroll offset just shifts it.
type ScrollView struct {
	Scroller
	// ContentInsetBottom is extra space below the last child.
	ContentInsetBottom float32
}

func NewScrollView() *ScrollView {
	sv := &ScrollView{}
	sv.self = sv
	sv.Clips = true
	return sv
}

func (sv *ScrollView) LayoutChildren() {
	sv.adopt()
	var extent, extentW float32
	for _, c := range sv.Kids {
		cb := c.Base()
		sz := sizeOf(c)
		var top, h float32
		if cb.Frame != nil {
			top, h = cb.Frame.Y, cb.Frame.H
		} else {
			wCtx := sz.W
			if a := cb.Anchors; a != nil {
				if a.Top != nil {
					top = *a.Top
				}
				if a.Left != nil && a.Right != nil {
					wCtx = sv.Bounds.W - *a.Left - *a.Right
				}
			}
			h = heightAt(c, wCtx, sz.H)
		}
		extent = max(extent, top+h)
		// Width extent from left-pinned/framed children only - right-anchored
		// children track the viewport, not the content.
		var left, w float32
		if cb.Frame != nil {
			left, w = cb.Frame.X, cb.Frame.W
		} else {
			w = sz.W
			if a := cb.Anchors; a != nil {
				if a.Right != nil {
					w = 0 // tracks the viewport
				}
				if a.Left != nil {
					left = *a.Left
				}
			}
		}
		extentW = max(extentW, left+w)
	}
	sv.ContentH = extent + sv.ContentInsetBottom
	sv.ContentW = extentW
	sv.clampScroll()
	sv.LayoutInto(gfx.R(sv.Bounds.X-sv.ScrollX, sv.Bounds.Y-sv.ScrollY,
		max(sv.ContentW, sv.Bounds.W), max(sv.ContentH, sv.Bounds.H)))
}

func (sv *ScrollView) HitTest(x, y float32) Widget {
	if !Contains(sv.Bounds, x, y) {
		return nil
	}
	if t, ok := sv.thumbRect(); ok && Contains(t, x, y) {
		return sv // thumbs paint on top, so they hit first
	}
	if t, ok := sv.hThumbRect(); ok && Contains(t, x, y) {
		return sv
	}
	for i := len(sv.Kids) - 1; i >= 0; i-- {
		if hit := sv.Kids[i].HitTest(x, y); hit != nil {
			return hit
		}
	}
	return nil
}

func (sv *ScrollView) OnPointerDown(x, y float32, _ int) { sv.thumbDown(x, y) }
func (sv *ScrollView) OnPointerDrag(x, y float32)        { sv.thumbDrag(x, y) }
func (sv *ScrollView) OnPointerUp(_, _ float32)          { sv.thumbUp() }
