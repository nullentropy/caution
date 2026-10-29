package ui

import (
	"github.com/nullentropy/caution/terminal/gfx"
)

type Size struct{ W, H float32 }

// springs-and-struts pinning against the parent's bounds, like cocoa autoresizing
// pin one edge and the widget keeps its size, pin both opposing edges and it stretches
// pointers because unset is meaningful
type Anchors struct {
	Left, Right, Top, Bottom *float32
	// offset from the parent's center
	CenterX, CenterY *float32
}

type DockSide = string // "top" | "bottom" | "left" | "right" | "fill" ("" = fill)

type StackSize struct {
	Kind   string // "content" (default) | "fixed" | "fill"
	Px     float32
	Weight float32
}

type Effect struct {
	Frag     string
	Uniforms map[string]float32
	Animate  bool
}

type Key struct {
	Name                   string // "Enter", "Backspace", "Left", " ", "a", ...
	Meta, Ctrl, Shift, Alt bool
}

func Contains(r gfx.Rect, x, y float32) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

type Widget interface {
	Base() *Core
	IntrinsicSize() Size
	// ok=false means height doesn't depend on width (only wrapped labels say true)
	HeightForWidth(w float32) (h float32, ok bool)
	LayoutChildren()
	PaintSelf(dl *gfx.DisplayList)
	PaintOverlay(dl *gfx.DisplayList)
	// oldBase is where the subtree's commands started in the previous list, -1 when
	// there is nothing to reuse (see Core.PaintTree)
	PaintTree(dl *gfx.DisplayList, oldBase int)
	Interactive() bool
	Focusable() bool
	OnKey(k Key) bool
	OnChar(r rune) bool
	OnFocusChange(focused bool)
	Activate()
	Semantics() *Semantic
	Cursor() string
	HitTest(x, y float32) Widget
	OnPointerDown(x, y float32, detail int)
	OnPointerDrag(x, y float32)
	OnPointerUp(x, y float32)
	OnHoverChange(hovered bool)
	OnPointerHover(x, y float32)
}

// layout inputs (Frame/Anchors/Dock/StackSize) are read by the parent container,
// Bounds is the absolute result
// base layout: anchors if present, else frame, else intrinsic size at the parent origin
type Core struct {
	// outer widget, our virtual dispatch for the tree walks. constructors set it
	self Widget

	Parent Widget
	Kids   []Widget
	UI     *UI

	Frame     *gfx.Rect
	Anchors   *Anchors
	Dock      DockSide
	StackSize StackSize
	GridSpan  int
	Width     *float32
	Height    *float32

	Clips bool

	// -- geometric animation (server-driven reflows only; see UI.animGeometry) --
	// prevX/prevY are last frame's laid-out position. a structural frame
	// (insert/remove/move) that moves a widget decays its paint position from the
	// old spot to the new one, and fadeR fades in a freshly inserted subtree.
	// zero means idle so widget literals never animate
	prevX, prevY   float32
	hadFrame       bool
	animDx, animDy float32
	animR          float32 // slide remaining: 1 -> 0, 0 = idle
	fadeR          float32 // fade-in remaining: 1 -> 0, 0 = opaque
	// renders the subtree offscreen and composites it back through app GLSL
	// visual only, hit-testing sees the real geometry
	Effect *Effect
	// window-move region (the app's own titlebar): a press here no interactive
	// widget claims moves the window. see UI.WindowDragAt
	WindowDrag bool
	// tooltip (`tip` prop), client-local
	// a tip makes an otherwise inert widget hover-targetable
	Tip string
	// `sound` prop: "" keeps each gesture's own token, "none" silences the widget
	SoundToken string
	// right-click menu (`context` prop), deepest carrier under the pointer wins
	// picks come back through OnContextPick with the server-assigned id
	ContextItems  []ContextItem
	OnContextPick func(id int)

	// absolute, logical px, valid after layout
	Bounds gfx.Rect

	// -- per-subtree retained display list (see Core.PaintTree) --
	// dirty: this widget's own paint may have changed. Invalidate sets it, the
	// marking pass clears it. needsPaint is dirty over the whole subtree, false
	// means the subtree splices last frame's commands instead of being walked
	dirty      bool
	needsPaint bool
	// cacheOff is where this subtree's commands began in last frame's list,
	// relative to the parent's start (absolute for the UI's roots). relative so
	// they survive a parent splicing its whole range verbatim, which keeps deep
	// caches valid frame after frame. the parent records both
	cacheOff, cacheLen int32
	cacheOK            bool
	// geometry and ambient clip the cached commands were recorded under
	// either one differs and the cache is stale
	cacheBounds, cacheClip gfx.Rect

	// -- layout skipping (see layoutSubtree) --
	// layoutDirty: something in this subtree changed what layout would produce.
	// Invalidate raises it here and on every ancestor, so a container can tell a
	// subtree that still fits its old geometry from one that must be walked again.
	// laidOutAt is the bounds it was last laid out for, laidOut says there is one
	layoutDirty bool
	laidOut     bool
	laidOutAt   gfx.Rect

	// server focus and reveal park until the next frame's marking walk (a widget
	// built by the same patch that commands it has no UI yet)
	wantsFocus  bool
	wantsReveal bool
	// outside its clipping parent last paint, so it recorded no commands
	culled bool

	// assigned on first semantics visit
	semID int
}

// server-driven `Focus()`, consumed at the next frame
func (b *Core) RequestFocus() {
	b.wantsFocus = true
	b.Invalidate()
}

// server-driven `Reveal()`, consumed at the next frame when bounds are fresh
func (b *Core) RequestReveal() {
	b.wantsReveal = true
	b.Invalidate()
}

// runs from the marking walks rather than paint, which skips culled widgets
// and a reveal mostly targets exactly those
func (b *Core) takeRequests() {
	if b.UI == nil {
		return
	}
	if b.wantsFocus {
		b.wantsFocus = false
		if b.self.Focusable() {
			b.UI.FocusWidget(b.self)
		}
		b.UI.RevealWidget(b.self) // server focus also brings it into view
	}
	if b.wantsReveal {
		b.wantsReveal = false
		b.UI.RevealWidget(b.self)
	}
}

func (b *Core) Base() *Core { return b }

func (b *Core) sound(token string) {
	if b.UI == nil {
		return
	}
	switch b.SoundToken {
	case "none":
		return
	case "":
	default:
		token = b.SoundToken
	}
	b.UI.Sound(token)
}

func (b *Core) Add(c Widget) {
	c.Base().Parent = b.self
	b.Kids = append(b.Kids, c)
	b.InvalidateLayout()
}

func (b *Core) IntrinsicSize() Size {
	var s Size
	if b.Width != nil {
		s.W = *b.Width
	}
	if b.Height != nil {
		s.H = *b.Height
	}
	return s
}

func (b *Core) HeightForWidth(_ float32) (float32, bool) { return 0, false }

func (b *Core) adopt() {
	for _, c := range b.Kids {
		c.Base().UI = b.UI
	}
}

func sizeOf(c Widget) Size {
	cb := c.Base()
	if cb.Width != nil && cb.Height != nil {
		return Size{*cb.Width, *cb.Height}
	}
	s := c.IntrinsicSize()
	if cb.Width != nil {
		s.W = *cb.Width
	}
	if cb.Height != nil {
		s.H = *cb.Height
	}
	return s
}

func (b *Core) LayoutChildren() {
	b.adopt()
	b.LayoutInto(b.Bounds)
}

// frame/anchors placement against any parent rect (ScrollView passes a
// virtual one, Dialog its card content area)
func (b *Core) LayoutInto(p gfx.Rect) {
	for _, c := range b.Kids {
		cb := c.Base()
		sz := sizeOf(c)
		w, h := sz.W, sz.H
		var x, y float32
		if a := cb.Anchors; a != nil {
			// one-sided pins clamp to the parent's opposite edge so an anchored child
			// never outgrows its parent and labels truncate instead of overflowing
			// use an explicit frame for overflow
			switch {
			case a.Left != nil && a.Right != nil:
				x = p.X + *a.Left
				w = max(0, p.W-*a.Left-*a.Right)
			case a.Left != nil:
				x = p.X + *a.Left
				w = max(0, min(w, p.W-*a.Left))
			case a.Right != nil:
				w = max(0, min(w, p.W-*a.Right))
				x = p.X + p.W - *a.Right - w
			case a.CenterX != nil:
				x = p.X + (p.W-w)/2 + *a.CenterX
			default:
				x = p.X
			}
			// height-for-width: once width is settled, wrapped labels restate their height
			if cb.Height == nil {
				if hh, ok := c.HeightForWidth(w); ok {
					h = hh
				}
			}
			switch {
			case a.Top != nil && a.Bottom != nil:
				y = p.Y + *a.Top
				h = max(0, p.H-*a.Top-*a.Bottom)
			case a.Top != nil:
				y = p.Y + *a.Top
				h = max(0, min(h, p.H-*a.Top))
			case a.Bottom != nil:
				h = max(0, min(h, p.H-*a.Bottom))
				y = p.Y + p.H - *a.Bottom - h
			case a.CenterY != nil:
				y = p.Y + (p.H-h)/2 + *a.CenterY
			default:
				y = p.Y
			}
		} else if f := cb.Frame; f != nil {
			x, y, w, h = p.X+f.X, p.Y+f.Y, f.W, f.H
		} else {
			x, y = p.X, p.Y
		}
		cb.Bounds = gfx.R(x, y, w, h)
		layoutSubtree(c)
	}
}

func (b *Core) PaintSelf(_ *gfx.DisplayList)    {}
func (b *Core) PaintOverlay(_ *gfx.DisplayList) {}

// when nothing inside can have changed (no dirty widget, no moved bounds,
// same ambient clip) last frame's commands for the subtree are copied
// wholesale instead of rebuilt. the copy is byte-for-byte what the walk
// would produce (TestPaintReuseMatchesFullWalk) so the damage diff and
// everything downstream is unaffected
func (b *Core) PaintTree(dl *gfx.DisplayList, oldBase int) {
	if u := b.UI; u != nil && u.reuse != nil && oldBase >= 0 && !b.needsPaint &&
		b.cacheClip == dl.Clip() && oldBase+int(b.cacheLen) <= len(u.reuse.Cmds) {
		dl.Cmds = append(dl.Cmds, u.reuse.Cmds[oldBase:oldBase+int(b.cacheLen)]...)
		u.reused += int(b.cacheLen)
		return
	}
	start := len(dl.Cmds)
	b.cacheBounds, b.cacheClip = b.Bounds, dl.Clip()
	// a freshly inserted subtree fades through its own layer, outside any app
	// effect layer. rect is inflated so drop shadows fade with the widget instead
	// of popping in at the end
	fading := b.fadeR > 0
	if fading {
		dl.BeginLayer(gfx.Inflate(b.Bounds, 24))
	}
	fx := b.Effect
	if fx != nil && fx.Frag != "" {
		dl.BeginLayer(b.Bounds)
	}
	b.self.PaintSelf(dl)
	if b.Clips {
		dl.PushClip(b.Bounds)
	}
	// the parent records each child's range, it is the only one that knows where
	// both its own range and the child's started
	for _, c := range b.Kids {
		cb := c.Base()
		at := len(dl.Cmds)
		if b.Clips && !gfx.Overlaps(gfx.Inflate(cb.Bounds, cullMargin), dl.Clip()) {
			// an empty range. culling depends only on bounds and clip, the same
			// pair the splice check compares, so it stays valid until one changes
			cb.cacheBounds, cb.cacheClip, cb.culled = cb.Bounds, dl.Clip(), true
		} else {
			oldChild := -1
			// a culled child's descendants hold offsets into a range it never wrote
			if oldBase >= 0 && cb.cacheOK && !cb.culled {
				oldChild = oldBase + int(cb.cacheOff)
			}
			cb.culled = false
			c.PaintTree(dl, oldChild)
		}
		cb.cacheOff = int32(at - start)
		cb.cacheLen = int32(len(dl.Cmds) - at)
		cb.cacheOK = true
	}
	b.self.PaintOverlay(dl)
	if b.Clips {
		dl.PopClip()
	}
	if fx != nil && fx.Frag != "" {
		dl.EndLayer(fx.Frag, fx.Uniforms, fx.Animate)
	}
	if fading {
		t := 1 - b.fadeR
		dl.EndLayer(fadeFrag, map[string]float32{"u_fade": easeOut(t)}, false)
	}
}

// how far outside a clipping parent a child may sit and still be painted, for
// drop shadows and focus rings that reach past its bounds
const cullMargin = 64

// pipeline is premultiplied so scaling the sample scales coverage and color together
const fadeFrag = `vec4 effect(vec2 uv) { return src(uv) * u_fade; }`

func (b *Core) Interactive() bool                 { return false }
func (b *Core) Focusable() bool                   { return false }
func (b *Core) OnKey(_ Key) bool                  { return false }
func (b *Core) OnChar(_ rune) bool                { return false }
func (b *Core) OnFocusChange(_ bool)              {}
func (b *Core) Activate()                         {}
func (b *Core) Semantics() *Semantic              { return nil }
func (b *Core) Cursor() string                    { return "" }
func (b *Core) OnPointerDown(_, _ float32, _ int) {}
func (b *Core) OnPointerDrag(_, _ float32)        {}
func (b *Core) OnPointerUp(_, _ float32)          {}
func (b *Core) OnHoverChange(_ bool)              {}
func (b *Core) OnPointerHover(_, _ float32)       {}

// topmost interactive (or tipped) widget at (x, y), in paint order
func (b *Core) HitTest(x, y float32) Widget {
	inside := Contains(b.Bounds, x, y)
	if b.Clips && !inside {
		return nil
	}
	for i := len(b.Kids) - 1; i >= 0; i-- {
		if hit := b.Kids[i].HitTest(x, y); hit != nil {
			return hit
		}
	}
	if inside && (b.self.Interactive() || b.Tip != "" || len(b.ContextItems) > 0) {
		return b.self
	}
	return nil
}

// per-widget half of UI.markPaint: own state changed, layout moved or
// resized it since its commands were recorded, never painted, or a geometric
// tween is moving it this frame
func (b *Core) selfNeedsPaint() bool {
	return b.dirty || !b.cacheOK || b.Bounds != b.cacheBounds || b.animR > 0 || b.fadeR > 0
}

// targeted: next frame repaints this widget and its ancestors' own commands
// and splices every clean subtree around it. anything that changes what a
// widget paints without coming through here must use UI.Invalidate, which
// rebuilds the whole list
func (b *Core) Invalidate() {
	b.markDirty()
	if b.UI != nil {
		b.UI.wake()
	}
}

// Invalidate without the wake, for marks made during the shell's own frame step
func (b *Core) markDirty() {
	b.dirty = true
	b.InvalidateLayout()
}

// marks every ancestor too, a child's size feeds every container above it
// Invalidate already does this, call it directly only for a geometry change
// that doesn't change what this widget paints
func (b *Core) InvalidateLayout() {
	for w := b; w != nil && !w.layoutDirty; {
		w.layoutDirty = true
		if w.Parent == nil {
			return
		}
		w = w.Parent.Base()
	}
}

// skips c when its geometry is settled: nothing in the subtree invalidated
// layout since the last pass and the parent assigned the same bounds it was
// laid out for. containers call this instead of LayoutChildren, so a frame
// that changed one label walks that label's spine and nothing else
//
// UI.layoutAll turns the skip off when the flags can't be trusted: a resize,
// an untargeted invalidation, or a geometric animation (moves bounds after
// layout, so they need re-deriving every frame)
func layoutSubtree(c Widget) {
	b := c.Base()
	if u := b.UI; u != nil && !u.layoutAll &&
		b.laidOut && !b.layoutDirty && b.laidOutAt == b.Bounds {
		return
	}
	c.LayoutChildren()
	b.laidOut, b.laidOutAt, b.layoutDirty = true, b.Bounds, false
}

func f32p(v float32) *float32 { return &v }
