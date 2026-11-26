package ui

import "github.com/nullentropy/caution/terminal/gfx"

const (
	splitDivider = 6
	splitSlop    = 3
)

type SplitView struct {
	Core
	Axis     Axis
	Pos      float32
	PinEnd   bool
	MinA     float32
	MinB     float32
	OnResize func(pos float32)

	first        float32
	hoverDivider bool
	grab         *float32
}

func NewSplitView() *SplitView {
	s := &SplitView{Axis: AxisH, Pos: 300, MinA: 80, MinB: 80}
	s.self = s
	return s
}

func (s *SplitView) Interactive() bool { return true }

func (s *SplitView) mainSize() float32 {
	if s.Axis == AxisV {
		return s.Bounds.H
	}
	return s.Bounds.W
}

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
	if s.Axis == AxisH {
		first.Base().Bounds = gfx.R(b.X, b.Y, s.first, b.H)
	} else {
		first.Base().Bounds = gfx.R(b.X, b.Y, b.W, s.first)
	}
	layoutSubtree(first)
	second := s.Kids[1]
	start := s.first + splitDivider
	if s.Axis == AxisH {
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
	if s.Axis == AxisH {
		return gfx.R(b.X+s.first-splitSlop, b.Y, splitDivider+splitSlop*2, b.H)
	}
	return gfx.R(b.X, b.Y+s.first-splitSlop, b.W, splitDivider+splitSlop*2)
}

func (s *SplitView) PaintOverlay(dl *gfx.DisplayList) {
	if !s.split() || !s.hoverDivider && s.grab == nil {
		return
	}
	b := s.Bounds
	c := s.first + splitDivider/2
	var line gfx.Rect
	if s.Axis == AxisH {
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
		return s
	}
	for i := len(s.Kids) - 1; i >= 0; i-- {
		if hit := s.Kids[i].HitTest(x, y); hit != nil {
			return hit
		}
	}
	return nil
}

func (s *SplitView) Cursor() string {
	if s.Axis == AxisH {
		return "col-resize"
	}
	return "row-resize"
}

func (s *SplitView) OnPointerDown(x, y float32, _ int) {
	v := x
	if s.Axis == AxisV {
		v = y
	}
	grab := v - s.first
	s.grab = &grab
	s.InvalidateWidget()
}

func (s *SplitView) OnPointerDrag(x, y float32) {
	if s.grab == nil {
		return
	}
	v := x
	if s.Axis == AxisV {
		v = y
	}
	s.setFirst(v - *s.grab)
	s.InvalidateWidget()
}

func (s *SplitView) OnPointerUp(_, _ float32) {
	if s.grab == nil {
		return
	}
	s.grab = nil
	s.InvalidateWidget()
	if s.OnResize != nil {
		s.OnResize(float32(int(s.Pos + 0.5)))
	}
}

func (s *SplitView) OnHoverChange(h bool) {
	s.hoverDivider = h
	s.InvalidateWidget()
}
