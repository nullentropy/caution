package ui

import "github.com/nullentropy/caution/go/terminal/gfx"

// Visual gutter between panes; the grab target extends splitSlop px past it.
const (
	splitDivider = 6
	splitSlop    = 3
)

// SplitView is a two-pane split with a draggable divider (nest for more
// panes). Dragging is client-local; the final position commits upstream as
// `split-resize` on release. Pos is the first pane's main-axis size in px,
// clamped by MinA/MinB every layout, so window resizes keep both panes sane.
type SplitView struct {
	Core
	Axis     string // "h" | "v"
	Pos      float32
	MinA     float32
	MinB     float32
	OnResize func(pos float32)

	hoverDivider bool
	grab         *float32
}

func NewSplitView() *SplitView {
	s := &SplitView{Axis: "h", Pos: 300, MinA: 80, MinB: 80}
	s.self = s
	return s
}

func (s *SplitView) Interactive() bool { return true } // only the divider hits (see HitTest)

func (s *SplitView) LayoutChildren() {
	s.adopt()
	b := s.Bounds
	main := b.W
	if s.Axis == "v" {
		main = b.H
	}
	maxPos := max(s.MinA, main-splitDivider-s.MinB)
	s.Pos = min(max(s.Pos, s.MinA), maxPos)

	if len(s.Kids) > 0 {
		first := s.Kids[0]
		if s.Axis == "h" {
			first.Base().Bounds = gfx.R(b.X, b.Y, s.Pos, b.H)
		} else {
			first.Base().Bounds = gfx.R(b.X, b.Y, b.W, s.Pos)
		}
		layoutSubtree(first)
	}
	if len(s.Kids) > 1 {
		second := s.Kids[1]
		start := s.Pos + splitDivider
		if s.Axis == "h" {
			second.Base().Bounds = gfx.R(b.X+start, b.Y, max(0, b.W-start), b.H)
		} else {
			second.Base().Bounds = gfx.R(b.X, b.Y+start, b.W, max(0, b.H-start))
		}
		layoutSubtree(second)
	}
	for _, extra := range s.Kids[min(2, len(s.Kids)):] {
		extra.Base().Bounds = gfx.Rect{}
	}
}

func (s *SplitView) dividerRect() gfx.Rect {
	b := s.Bounds
	if s.Axis == "h" {
		return gfx.R(b.X+s.Pos-splitSlop, b.Y, splitDivider+splitSlop*2, b.H)
	}
	return gfx.R(b.X, b.Y+s.Pos-splitSlop, b.W, splitDivider+splitSlop*2)
}

func (s *SplitView) PaintOverlay(dl *gfx.DisplayList) {
	// The divider is invisible at rest - it reveals itself on hover (with the
	// resize cursor) and stays visible while dragging.
	if !s.hoverDivider && s.grab == nil {
		return
	}
	b := s.Bounds
	c := s.Pos + splitDivider/2
	var line gfx.Rect
	if s.Axis == "h" {
		line = gfx.R(b.X+c-1.5, b.Y, 3, b.H)
	} else {
		line = gfx.R(b.X, b.Y+c-1.5, b.W, 3)
	}
	dl.Fill(line, gfx.WithAlpha(*Theme["accent"], 0.7), gfx.CornerRadius(1.5))
}

func (s *SplitView) HitTest(x, y float32) Widget {
	if !Contains(s.Bounds, x, y) {
		return nil
	}
	if Contains(s.dividerRect(), x, y) {
		return s // divider beats pane edges
	}
	for i := len(s.Kids) - 1; i >= 0; i-- {
		if hit := s.Kids[i].HitTest(x, y); hit != nil {
			return hit
		}
	}
	return nil
}

func (s *SplitView) Cursor() string {
	if s.Axis == "h" {
		return "col-resize"
	}
	return "row-resize"
}

func (s *SplitView) OnPointerDown(x, y float32, _ int) {
	v := x
	if s.Axis == "v" {
		v = y
	}
	grab := v - s.Pos
	s.grab = &grab
	s.Invalidate()
}

func (s *SplitView) OnPointerDrag(x, y float32) {
	if s.grab == nil {
		return
	}
	v := x
	if s.Axis == "v" {
		v = y
	}
	s.Pos = v - *s.grab // clamped in layout
	s.Invalidate()
}

func (s *SplitView) OnPointerUp(_, _ float32) {
	if s.grab == nil {
		return
	}
	s.grab = nil
	s.Invalidate()
	if s.OnResize != nil {
		s.OnResize(float32(int(s.Pos + 0.5))) // commit once, on release
	}
}

func (s *SplitView) OnHoverChange(h bool) {
	s.hoverDivider = h
	s.Invalidate()
}
