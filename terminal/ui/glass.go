package ui

import (
	"time"

	"github.com/nullentropy/caution/terminal/gfx"
)

// transparent pointer-capture surface for custom interaction. paints nothing,
// swallows presses over its bounds and reports them upstream: `pick` with the
// glass-relative position and the id of the deepest server node under the
// point, coalesced `drag` positions, `drop` on release
type Glass struct {
	Core
	// target is 0 when nothing resolvable is under the point
	// wired by the inflater when the node subscribes to "pick"
	OnPick func(x, y float32, target int)
	// drags coalesce to ~30/s
	OnDragTo func(x, y float32)
	OnDropAt func(x, y float32)
	// coalesced like drags, but deltas accumulate between sends since dropping a
	// wheel event would lose distance
	OnWheelAt func(x, y, dx, dy float32)
	// right-button gestures. while a glass subscribes to rpick, a right-press over
	// it starts a captured right-drag instead of a context menu, so custom
	// interaction (orbit, measure, lasso) gets the second button
	OnRPick   func(x, y float32)
	OnRDragTo func(x, y float32)
	OnRDropAt func(x, y float32)
	// server node id (0 = none), injected by the inflater which owns the id map
	IdOf func(w Widget) int

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

// coalesced on the drag clock
func (g *Glass) RDragAt(x, y float32) {
	if g.OnRDragTo == nil || time.Since(g.rSent) < dragCoalesce {
		return
	}
	g.rSent = time.Now()
	g.OnRDragTo(x-g.Bounds.X, y-g.Bounds.Y)
}

// always the true endpoint, like drop
func (g *Glass) RDropAt(x, y float32) {
	if g.OnRDropAt != nil {
		g.OnRDropAt(x-g.Bounds.X, y-g.Bounds.Y)
	}
}

// a sub-window remainder rides the next event, at gesture end that's at most
// one tick's worth
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

// pointer moves arrive at display rate and the session's event bucket (120/s
// sustained) must absorb long drags without dropping anything
const dragCoalesce = 33 * time.Millisecond

func NewGlass() *Glass {
	g := &Glass{}
	g.self = g
	return g
}

// only when someone listens, so an unsubscribed glass never steals input from
// what's beneath it
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

// deepest server node under an absolute point: HitTest without its
// interactivity filter, excluding the glass itself. a client-internal widget
// answers with its nearest server-known ancestor
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

// invisible by contract
func (g *Glass) PaintSelf(_ *gfx.DisplayList) {}
