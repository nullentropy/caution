package ui

import (
	"github.com/nullentropy/caution/go/terminal/gfx"
)

type Size struct{ W, H float32 }

// Anchors is springs-and-struts pinning against the parent's bounds (classic
// Cocoa autoresizing / WinForms anchors). Pin one edge and the widget keeps
// its intrinsic/explicit size. Pin both opposing edges and it stretches.
// Fields are pointers because "unset" is meaningful.
type Anchors struct {
	Left, Right, Top, Bottom *float32
	// Offset from the parent's center.
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

// Key is a normalized keyboard event (the GLFW shell translates).
type Key struct {
	Name                   string // "Enter", "Backspace", "Left", " ", "a", ...
	Meta, Ctrl, Shift, Alt bool
}

func Contains(r gfx.Rect, x, y float32) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Widget is the retained tree node contract. Base implements every method
// with the default behavior. Concrete widgets embed Base and override.
type Widget interface {
	Base() *Core
	IntrinsicSize() Size
	// HeightForWidth is the widget's natural height at a given laid-out
	// width, for widgets whose height depends on it (wrapped labels).
	// ok=false means height doesn't depend on width.
	HeightForWidth(w float32) (h float32, ok bool)
	LayoutChildren()
	PaintSelf(dl *gfx.DisplayList)
	PaintOverlay(dl *gfx.DisplayList)
	// PaintTree paints this subtree. oldBase is where the subtree's commands
	// started in the previous frame's list, or -1 when there is nothing to
	// reuse (see Core.PaintTree).
	PaintTree(dl *gfx.DisplayList, oldBase int)
	Interactive() bool
	Focusable() bool
	OnKey(k Key) bool
	// OnChar receives printable text input (the char callback), while shortcuts
	// and control keys arrive via OnKey.
	OnChar(r rune) bool
	OnFocusChange(focused bool)
	Activate()
	Cursor() string
	HitTest(x, y float32) Widget
	OnPointerDown(x, y float32, detail int)
	OnPointerDrag(x, y float32)
	OnPointerUp(x, y float32)
	OnHoverChange(hovered bool)
	OnPointerHover(x, y float32)
}

// Base carries what every widget shares. Layout inputs
// (Frame/Anchors/Dock/StackSize) are interpreted by the *parent* container;
// Bounds is the absolute result. The base layout is Interface Builder-style:
// anchors if present, else frame, else intrinsic size at the parent origin.
type Core struct {
	// self is the outer widget, standing in for virtual dispatch in the
	// recursive tree walks. Constructors set it and Add double-checks.
	self Widget

	Parent Widget
	Kids   []Widget
	UI     *Ui

	Frame     *gfx.Rect
	Anchors   *Anchors
	Dock      DockSide
	StackSize StackSize
	GridSpan  int
	Width     *float32
	Height    *float32

	// Clips confines painting and hit-testing of children to Bounds.
	Clips bool

	// -- geometric animation (server-driven reflows only; see Ui.animGeometry) --
	// prevX/prevY track the laid-out position at last frame. When a
	// structural frame (insert/remove/move ops) moves a widget, its paint
	// position decays from the old spot to the new one, and fadeR composites a
	// freshly inserted subtree through a fade-in layer. Zero values mean
	// idle, so widget literals never animate.
	prevX, prevY   float32
	hadFrame       bool
	animDx, animDy float32
	animR          float32 // slide remaining: 1 -> 0, 0 = idle
	fadeR          float32 // fade-in remaining: 1 -> 0, 0 = opaque
	// Effect renders the subtree to an offscreen texture and composites it
	// back through app GLSL. Visual only: hit-testing sees the geometry.
	Effect *Effect
	// WindowDrag marks this subtree as a window-move region (the app's own
	// titlebar under a hidden system one): a press here that no interactive
	// widget claims moves the WINDOW. See Ui.WindowDragAt.
	WindowDrag bool
	// Outline is the tooling adorner (designer selection ring).
	Outline bool
	// Tip is tooltip text (`tip` prop): client-local, shown by the Ui after
	// a short hover idle. A tip makes an otherwise inert widget
	// hover-targetable.
	Tip string
	// SoundToken (`sound` prop) replaces the token this widget's gestures
	// fire. "" keeps each gesture's own, "none" silences the widget.
	SoundToken string
	// ContextItems is the right-click menu (`context` prop): opened
	// client-locally, and the deepest carrier under the pointer wins. Picks flow
	// through OnContextPick with the item's server-assigned id.
	ContextItems  []ContextItem
	OnContextPick func(id int)

	// Bounds is absolute, in logical px, valid after layout.
	Bounds gfx.Rect

	// -- per-subtree retained display list (see Core.PaintTree) --
	// dirty means this widget's own painted appearance may have changed;
	// Invalidate sets it, the marking pass clears it. needsPaint is that
	// aggregated over the subtree. False means the whole subtree can splice
	// the commands it produced last frame instead of being walked.
	dirty      bool
	needsPaint bool
	// cacheOff is where this subtree's commands began in the last frame's list,
	// relative to the parent's own start (absolute for the Ui's roots), and
	// cacheLen how many there were. Relative offsets survive a parent splicing
	// its whole range verbatim, which keeps a deep tree's caches valid frame
	// after frame. The parent records both.
	cacheOff, cacheLen int32
	cacheOK            bool
	// cacheBounds and cacheClip are the geometry and ambient clip the cached
	// commands were recorded under: different either one, and they are stale.
	cacheBounds, cacheClip gfx.Rect

	// -- layout skipping (see layoutSubtree) --
	// layoutDirty means something in this subtree changed what layout would
	// produce. Invalidate raises it here and on every ancestor, so a container
	// can tell a subtree that still fits its old geometry from one that has to
	// be walked again. laidOutAt is the bounds the subtree was last laid out
	// for, and laidOut says there is one.
	layoutDirty bool
	laidOut     bool
	laidOutAt   gfx.Rect

	// FocusSeq/RevealSeq are the last-seen values of the server's universal
	// one-shot command props. The requests park until the next paint (a
	// widget built by the same patch that commands it has no UI yet).
	FocusSeq, RevealSeq float32
	wantsFocus          bool
	wantsReveal         bool
}

// RequestFocus is the server-driven focus command (`Focus()` in the SDK);
// consumed at the next paint.
func (b *Core) RequestFocus() {
	b.wantsFocus = true
	b.Invalidate()
}

// RequestReveal is the server-driven scroll-into-view command (`Reveal()`);
// consumed at the next paint, when bounds are fresh.
func (b *Core) RequestReveal() {
	b.wantsReveal = true
	b.Invalidate()
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

// LayoutInto is frame/anchors placement against an arbitrary parent rect
// (ScrollView passes a virtual one, Dialog its card content area).
func (b *Core) LayoutInto(p gfx.Rect) {
	for _, c := range b.Kids {
		cb := c.Base()
		sz := sizeOf(c)
		w, h := sz.W, sz.H
		var x, y float32
		if a := cb.Anchors; a != nil {
			// One-sided pins clamp to the parent's opposite edge, so an anchored
			// child never lays out wider or taller than its parent and labels
			// truncate at the boundary instead of overflowing. Use an explicit
			// frame for overflow.
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
			// Height-for-width: once the width is settled, widgets whose
			// height depends on it (wrapped labels) restate their height.
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

// PaintTree paints this widget and its subtree in painter's order. When
// nothing inside can have changed (no dirty widget, no moved bounds, the same
// ambient clip), the commands the subtree produced last frame are copied
// wholesale instead of rebuilt. The copy is byte-for-byte what the walk would
// have produced (TestPaintReuseMatchesFullWalk covers this), so everything
// downstream including the damage diff is unaffected. oldBase is where those
// commands begin in the previous list, or -1 when there is nothing to reuse.
func (b *Core) PaintTree(dl *gfx.DisplayList, oldBase int) {
	if u := b.UI; u != nil && u.reuse != nil && oldBase >= 0 && !b.needsPaint &&
		b.cacheClip == dl.Clip() && oldBase+int(b.cacheLen) <= len(u.reuse.Cmds) {
		dl.Cmds = append(dl.Cmds, u.reuse.Cmds[oldBase:oldBase+int(b.cacheLen)]...)
		u.reused += int(b.cacheLen)
		return
	}
	start := len(dl.Cmds)
	b.cacheBounds, b.cacheClip = b.Bounds, dl.Clip()
	if b.UI != nil {
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
	// A fading (freshly inserted) subtree composites through a fade layer,
	// outside any app effect layer. The rect is inflated so drop shadows
	// fade with their widget instead of popping in at the end.
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
	// The parent records each child's range, since it is the only participant
	// that knows both where its own range started and where the child's did.
	for _, c := range b.Kids {
		cb := c.Base()
		at := len(dl.Cmds)
		oldChild := -1
		if oldBase >= 0 && cb.cacheOK {
			oldChild = oldBase + int(cb.cacheOff)
		}
		c.PaintTree(dl, oldChild)
		cb.cacheOff = int32(at - start)
		cb.cacheLen = int32(len(dl.Cmds) - at)
		cb.cacheOK = true
	}
	b.self.PaintOverlay(dl)
	if b.Clips {
		dl.PopClip()
	}
	if b.Outline {
		accent := gfx.WithAlpha(*Theme["accent"], 0.9)
		dl.Rect(gfx.Inflate(b.Bounds, 1), gfx.Transparent, gfx.RectOpts{
			Radius: gfx.CornerRadius(4), BorderWidth: 2, BorderColor: &accent,
		})
	}
	if fx != nil && fx.Frag != "" {
		dl.EndLayer(fx.Frag, fx.Uniforms, fx.Animate)
	}
	if fading {
		t := 1 - b.fadeR
		dl.EndLayer(fadeFrag, map[string]float32{"u_fade": t * (2 - t)}, false)
	}
}

// fadeFrag composites a fading subtree. The pipeline is premultiplied, so
// scaling the whole sample scales coverage and color together.
const fadeFrag = `vec4 effect(vec2 uv) { return src(uv) * u_fade; }`

func (b *Core) Interactive() bool                 { return false }
func (b *Core) Focusable() bool                   { return false }
func (b *Core) OnKey(_ Key) bool                  { return false }
func (b *Core) OnChar(_ rune) bool                { return false }
func (b *Core) OnFocusChange(_ bool)              {}
func (b *Core) Activate()                         {}
func (b *Core) Cursor() string                    { return "" }
func (b *Core) OnPointerDown(_, _ float32, _ int) {}
func (b *Core) OnPointerDrag(_, _ float32)        {}
func (b *Core) OnPointerUp(_, _ float32)          {}
func (b *Core) OnHoverChange(_ bool)              {}
func (b *Core) OnPointerHover(_, _ float32)       {}

// HitTest finds the topmost interactive (or tipped) widget at (x, y), in
// paint order.
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

// selfNeedsPaint is the per-widget half of the marking pass (see
// Ui.markPaint): its own state changed, the layout moved or resized it since
// its commands were recorded, it has never been painted, or a geometric tween
// is moving it this frame.
func (b *Core) selfNeedsPaint() bool {
	return b.dirty || !b.cacheOK || b.Bounds != b.cacheBounds || b.animR > 0 || b.fadeR > 0
}

// Invalidate marks this widget's own painting stale and asks for a frame. It is
// targeted: the next frame repaints this widget and its ancestors' own
// commands, and splices every clean subtree around it. Anything that changes
// what a widget paints without going through here must use Ui.Invalidate
// instead, which rebuilds the whole list.
func (b *Core) Invalidate() {
	b.dirty = true
	b.InvalidateLayout()
	if b.UI != nil {
		b.UI.wake()
	}
}

// InvalidateLayout marks this subtree's geometry stale, and every ancestor's
// with it, since a child's size feeds every container above it. Invalidate
// already does this. Call it directly only for a change that moves geometry
// without changing what this widget paints.
func (b *Core) InvalidateLayout() {
	for w := b; w != nil && !w.layoutDirty; {
		w.layoutDirty = true
		if w.Parent == nil {
			return
		}
		w = w.Parent.Base()
	}
}

// layoutSubtree lays out c unless its geometry is already settled: nothing in
// the subtree has invalidated layout since the last pass, and the parent just
// assigned it the bounds it was laid out for. Containers call this instead of
// LayoutChildren, so a frame that changed one label walks that label's spine
// and nothing else.
//
// Ui.layoutAll turns the skip off for a frame the flags cannot be trusted to
// describe: a resize, an untargeted invalidation, or a geometric animation,
// which moves bounds after layout and needs them re-derived every frame.
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
