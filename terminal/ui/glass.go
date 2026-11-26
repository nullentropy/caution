package ui

import (
	"time"

	"github.com/nullentropy/caution/terminal/gfx"
)

type Glass struct {
	Core
	OnPick    func(x, y float32, target int)
	OnDragTo  func(x, y float32)
	OnDropAt  func(x, y float32)
	OnWheelAt func(x, y, dx, dy float32)
	OnRPick   func(x, y float32)
	OnRDragTo func(x, y float32)
	OnRDropAt func(x, y float32)
	IdOf      func(w Widget) int
	HoverFill *gfx.Color
	hovered   bool

	dragging  bool
	lastSent  time.Time
	wheelDx   float32
	wheelDy   float32
	wheelSent time.Time
	rSent     time.Time
}

func (g *Glass) RPickAt(x, y float32) {
	if g.OnRPick != nil {
		g.OnRPick(x-g.Bounds.X, y-g.Bounds.Y)
	}
	g.rSent = time.Now()
}

func (g *Glass) RDragAt(x, y float32) {
	if g.OnRDragTo == nil || time.Since(g.rSent) < dragCoalesce {
		return
	}
	g.rSent = time.Now()
	g.OnRDragTo(x-g.Bounds.X, y-g.Bounds.Y)
}

func (g *Glass) RDropAt(x, y float32) {
	if g.OnRDropAt != nil {
		g.OnRDropAt(x-g.Bounds.X, y-g.Bounds.Y)
	}
}

func (g *Glass) WheelAt(x, y, dx, dy float32) {
	if g.OnWheelAt == nil {
		return
	}
	g.wheelDx += dx
	g.wheelDy += dy
	if time.Since(g.wheelSent) < dragCoalesce {
		return
	}
	g.wheelSent = time.Now()
	g.OnWheelAt(x-g.Bounds.X, y-g.Bounds.Y, g.wheelDx, g.wheelDy)
	g.wheelDx, g.wheelDy = 0, 0
}

const dragCoalesce = 33 * time.Millisecond

func NewGlass() *Glass {
	g := &Glass{}
	g.self = g
	return g
}

func (g *Glass) Interactive() bool { return g.OnPick != nil }

func (g *Glass) OnPointerDown(x, y float32, _ int) {
	if g.OnPick == nil {
		return
	}
	g.OnPick(x-g.Bounds.X, y-g.Bounds.Y, g.targetAt(x, y))
	g.dragging = g.OnDragTo != nil || g.OnDropAt != nil
	g.lastSent = time.Now()
}

func (g *Glass) OnPointerDrag(x, y float32) {
	if !g.dragging || g.OnDragTo == nil {
		return
	}
	if time.Since(g.lastSent) < dragCoalesce {
		return
	}
	g.lastSent = time.Now()
	g.OnDragTo(x-g.Bounds.X, y-g.Bounds.Y)
}

func (g *Glass) OnPointerUp(x, y float32) {
	if !g.dragging {
		return
	}
	g.dragging = false
	if g.OnDropAt != nil {
		g.OnDropAt(x-g.Bounds.X, y-g.Bounds.Y)
	}
}

func (g *Glass) targetAt(x, y float32) int {
	if g.IdOf == nil {
		return 0
	}
	root := Widget(g)
	for root.Base().Parent != nil {
		root = root.Base().Parent
	}
	hit := g.deepestAt(root, x, y)
	for w := hit; w != nil; w = w.Base().Parent {
		if id := g.IdOf(w); id != 0 {
			return id
		}
	}
	return 0
}

func (g *Glass) deepestAt(w Widget, x, y float32) Widget {
	if w == Widget(g) {
		return nil
	}
	b := w.Base()
	inside := Contains(b.Bounds, x, y)
	if b.Clips && !inside {
		return nil
	}
	for i := len(b.Kids) - 1; i >= 0; i-- {
		if hit := g.deepestAt(b.Kids[i], x, y); hit != nil {
			return hit
		}
	}
	if inside {
		return w
	}
	return nil
}

func (g *Glass) PaintSelf(dl *gfx.DisplayList) {
	if g.hovered && g.HoverFill != nil {
		dl.Fill(g.Bounds, *g.HoverFill, gfx.Corners{})
	}
}

func (g *Glass) OnHoverChange(h bool) {
	if g.HoverFill != nil && g.hovered != h {
		g.hovered = h
		g.InvalidateWidget()
	}
}
