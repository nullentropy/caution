package ui

import "github.com/nullentropy/caution/terminal/gfx"

// visual gutter between panes, the grab target extends splitSlop px past it
const (
	splitDivider = 6
	splitSlop    = 3
)

// two panes with a draggable divider (nest for more). dragging is client-local
// and the final position commits upstream as `split-resize` on release.
// Pos is the first pane's main-axis size in px (the second's with PinEnd),
// clamped by MinA/MinB every layout so window resizes keep both panes sane and
// change the unpinned one. with one child it fills the split, no divider
type SplitView struct {
	Core
	Axis     string // "h" | "v"
	Pos      float32
	PinEnd   bool
	MinA     float32
	MinB     float32
	OnResize func(pos float32)

	first        float32 // first pane's laid-out size
	hoverDivider bool
	grab         *float32
}

func NewSplitView() *SplitView {
	s := &SplitView{Axis: "h", Pos: 300, MinA: 80, MinB: 80}
	s.self = s
	return s
}

func (s *SplitView) Interactive() bool { return true } // only the divider hits (see HitTest)

func (s *SplitView) mainSize() float32 {
	if s.Axis == "v" {
		return s.Bounds.H
	}
	return s.Bounds.W
}

// sizes the first pane within MinA/MinB and keeps Pos in step
func (s *SplitView) setFirst(a float32) {
	main := s.mainSize()
	s.first = min(max(a, s.MinA), max(s.MinA, main-splitDivider-s.MinB))
	s.Pos = s.first
	if s.PinEnd {
		s.Pos = main - splitDivider - s.first
	}
}

func (s *SplitView) split() bool { return len(s.Kids) > 1 }

func (s *SplitView) LayoutChildren() {
	s.adopt()
	b := s.Bounds
	if !s.split() {
		if len(s.Kids) == 1 {
			s.Kids[0].Base().Bounds = b
			layoutSubtree(s.Kids[0])
		}
		return
	}
	if s.PinEnd {
		s.setFirst(s.mainSize() - splitDivider - s.Pos)
	} else {
		s.setFirst(s.Pos)
	}

	first := s.Kids[0]
	if s.Axis == "h" {
		first.Base().Bounds = gfx.R(b.X, b.Y, s.first, b.H)
	} else {
		first.Base().Bounds = gfx.R(b.X, b.Y, b.W, s.first)
	}
	layoutSubtree(first)
	second := s.Kids[1]
	start := s.first + splitDivider
	if s.Axis == "h" {
		second.Base().Bounds = gfx.R(b.X+start, b.Y, max(0, b.W-start), b.H)
	} else {
		second.Base().Bounds = gfx.R(b.X, b.Y+start, b.W, max(0, b.H-start))
	}
	layoutSubtree(second)
	for _, extra := range s.Kids[min(2, len(s.Kids)):] {
		extra.Base().Bounds = gfx.Rect{}
	}
}

func (s *SplitView) dividerRect() gfx.Rect {
	b := s.Bounds
	if s.Axis == "h" {
		return gfx.R(b.X+s.first-splitSlop, b.Y, splitDivider+splitSlop*2, b.H)
	}
	return gfx.R(b.X, b.Y+s.first-splitSlop, b.W, splitDivider+splitSlop*2)
}

func (s *SplitView) PaintOverlay(dl *gfx.DisplayList) {
	// divider is invisible at rest, shows on hover (with the resize cursor) and
	// while dragging
	if !s.split() || !s.hoverDivider && s.grab == nil {
		return
	}
	b := s.Bounds
	c := s.first + splitDivider/2
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
	if s.split() && Contains(s.dividerRect(), x, y) {
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
	grab := v - s.first
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
	s.setFirst(v - *s.grab)
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
