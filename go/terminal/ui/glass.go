package ui

import (
	"time"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

// Glass is a transparent pointer-capture surface for custom interaction. It
// paints nothing, swallows presses over its bounds, and reports them upstream:
// a press as `pick` with the position (glass-relative, logical px) and the id
// of the deepest server node under the point, drags as coalesced `drag`
// positions, release as `drop`.
//
// Mirrored in the browser terminal (src/ui/glass.ts).
type Glass struct {
	Core
	// OnPick fires on press: glass-relative position + the target node id
	// (0 when nothing resolvable is under the point). Wired by the inflater
	// when the node subscribes to "pick".
	OnPick func(x, y float32, target int)
	// OnDragTo fires while dragging, coalesced to ~30/s. OnDropAt fires on release.
	OnDragTo func(x, y float32)
	OnDropAt func(x, y float32)
	// OnWheelAt fires for wheel over the glass, when subscribed:
	// glass-relative position plus deltas. Coalesced like drags, but deltas
	// ACCUMULATE between sends, because dropping a wheel event would lose distance.
	OnWheelAt func(x, y, dx, dy float32)
	// Right-button gestures, when subscribed: press, drag and release with
	// glass-relative positions. While a glass subscribes to rpick, a right-press
	// over it starts a captured right-drag instead of a context menu, giving the
	// second mouse button to custom interaction like orbit, measure or lasso.
	OnRPick   func(x, y float32)
	OnRDragTo func(x, y float32)
	OnRDropAt func(x, y float32)
	// IdOf resolves a widget to its server node id (0 = none), injected by
	// the inflater, which owns the id map.
	IdOf func(w Widget) int

	dragging  bool
	lastSent  time.Time
	wheelDx   float32
	wheelDy   float32
	wheelSent time.Time
	rSent     time.Time
}

// RPickAt is routed by the UI when a right-press happens on this glass.
func (g *Glass) RPickAt(x, y float32) {
	if g.OnRPick != nil {
		g.OnRPick(x-g.Bounds.X, y-g.Bounds.Y)
	}
	g.rSent = time.Now()
}

// RDragAt delivers right-drag positions, coalesced on the drag clock.
func (g *Glass) RDragAt(x, y float32) {
	if g.OnRDragTo == nil || time.Since(g.rSent) < dragCoalesce {
		return
	}
	g.rSent = time.Now()
	g.OnRDragTo(x-g.Bounds.X, y-g.Bounds.Y)
}

// RDropAt is the right release, always the true endpoint, like drop.
func (g *Glass) RDropAt(x, y float32) {
	if g.OnRDropAt != nil {
		g.OnRDropAt(x-g.Bounds.X, y-g.Bounds.Y)
	}
}

// WheelAt is routed by the Ui's wheel dispatch when this glass is the hit
// target. A sub-window remainder rides the next event: at gesture end that is
// at most one tick's worth.
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

// dragCoalesce is the minimum gap between drag events: pointer moves arrive
// at display rate, and the session's event bucket (120/s sustained) must
// absorb long drags without dropping anything.
const dragCoalesce = 33 * time.Millisecond

func NewGlass() *Glass {
	g := &Glass{}
	g.self = g
	return g
}

// Interactive only when someone listens, so an unsubscribed glass never steals
// input from what is beneath it.
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

// targetAt resolves the deepest server node under an absolute point: the widget
// tree's paint-order-last bounds hit (the HitTest walk without its
// interactivity filter), excluding the glass itself, mapped to an id. When the
// deepest widget is client-internal, the nearest server-known ancestor answers
// instead.
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

// PaintSelf paints nothing. The glass is invisible by contract.
func (g *Glass) PaintSelf(_ *gfx.DisplayList) {}
