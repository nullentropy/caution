package ui

import (
	"github.com/nullentropy/caution/terminal/gfx"
)

type Size struct{ W, H float32 }

type Anchors struct {
	Left, Right, Top, Bottom *float32
	CenterX, CenterY         *float32
}

type DockSide = string

type SizeKind string

const (
	SizeContent SizeKind = "content"
	SizeFixed   SizeKind = "fixed"
	SizeFill    SizeKind = "fill"
)

type StackSize struct {
	Kind   SizeKind
	Px     float32
	Weight float32
}

type Effect struct {
	Frag     string
	Uniforms map[string]float32
	Animate  bool
}

type Key struct {
	Name                   string
	Meta, Ctrl, Shift, Alt bool
	Text                   string
}

func Contains(r gfx.Rect, x, y float32) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

type Widget interface {
	Base() *Core
	IntrinsicSize() Size
	HeightForWidth(w float32) (h float32, ok bool)
	LayoutChildren()
	PaintSelf(dl *gfx.DisplayList)
	PaintOverlay(dl *gfx.DisplayList)
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

type Core struct {
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

	Clips  bool
	NoFade bool

	prevX, prevY    float32
	hadFrame        bool
	animDx, animDy  float32
	animR           float32
	fadeR           float32
	Effect          *Effect
	WindowDrag      bool
	Tip             string
	SoundToken      string
	ContextItems    []ContextItem
	OnContextPick   func(id int)
	SelectionItems  []ContextItem
	OnSelectionPick func(id, start, end int)

	Bounds gfx.Rect

	dirty                  bool
	needsPaint             bool
	cacheOff, cacheLen     int32
	cacheOK                bool
	cacheBounds, cacheClip gfx.Rect

	layoutDirty bool
	laidOut     bool
	laidOutAt   gfx.Rect

	wantsFocus  bool
	wantsReveal bool
	culled      bool

	semID int
}

func (b *Core) RequestFocus() {
	b.wantsFocus = true
	b.InvalidateWidget()
}

func (b *Core) RequestReveal() {
	b.wantsReveal = true
	b.InvalidateWidget()
}

func (b *Core) takeRequests() {
	if b.UI == nil {
		return
	}
	if b.wantsFocus {
		b.wantsFocus = false
		if b.self.Focusable() {
			b.UI.FocusWidget(b.self)
		}
		b.UI.RevealWidget(b.self)
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

func (b *Core) LayoutInto(p gfx.Rect) {
	for _, c := range b.Kids {
		cb := c.Base()
		sz := sizeOf(c)
		w, h := sz.W, sz.H
		var x, y float32
		if a := cb.Anchors; a != nil {
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

func (b *Core) PaintTree(dl *gfx.DisplayList, oldBase int) {
	if u := b.UI; u != nil && u.reuse != nil && oldBase >= 0 && !b.needsPaint &&
		b.cacheClip == dl.Clip() && oldBase+int(b.cacheLen) <= len(u.reuse.Cmds) {
		dl.Cmds = append(dl.Cmds, u.reuse.Cmds[oldBase:oldBase+int(b.cacheLen)]...)
		u.reused += int(b.cacheLen)
		return
	}
	start := len(dl.Cmds)
	b.cacheBounds, b.cacheClip = b.Bounds, dl.Clip()
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
	for _, c := range b.Kids {
		cb := c.Base()
		at := len(dl.Cmds)
		if b.Clips && !gfx.Overlaps(gfx.Inflate(cb.Bounds, cullMargin), dl.Clip()) {
			cb.cacheBounds, cb.cacheClip, cb.culled = cb.Bounds, dl.Clip(), true
		} else {
			oldChild := -1
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

const cullMargin = 64

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
	if inside && (b.self.Interactive() || b.Tip != "" || len(b.ContextItems) > 0 || len(b.SelectionItems) > 0) {
		return b.self
	}
	return nil
}

func (b *Core) selfNeedsPaint() bool {
	return b.dirty || !b.cacheOK || b.Bounds != b.cacheBounds || b.animR > 0 || b.fadeR > 0
}

func (b *Core) InvalidateWidget() {
	b.markDirty()
	if b.UI != nil {
		b.UI.wake()
	}
}

func (b *Core) markDirty() {
	b.dirty = true
	b.InvalidateLayout()
}

func (b *Core) InvalidateLayout() {
	for w := b; w != nil && !w.layoutDirty; {
		w.layoutDirty = true
		if w.Parent == nil {
			return
		}
		w = w.Parent.Base()
	}
}

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
