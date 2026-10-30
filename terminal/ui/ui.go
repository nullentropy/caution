package ui

import (
	"math"
	"runtime/debug"
	"strings"
	"time"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/text"

	"github.com/rs/zerolog/log"
)

// drag selection over a text widget's boundaries (glyph indices for a
// single-line label, source byte offsets for wrapped text)
// Start may exceed End while dragging backwards
type TextSelection struct {
	Owner      selectionOwner
	Start, End int
}

type selectionOwner interface {
	Widget
	// text between two selection boundaries, in either order
	GlyphText(start, end int) string
}

// client runtime shell: owns the root widget and all generic interaction
// (pointer capture, hover, cursor, selection, clipboard, focus). the GLFW layer
// feeds it events, the protocol session feeds it trees. nothing app-specific
type UI struct {
	Root Widget
	// client-local layer (popovers, menus), painted above the tree and hit first
	Overlay Widget
	// client-owned status layer (connection state), painted above everything
	// against the viewport and never hit-tested, so input passes through to the tree
	Banner Widget
	// in-window menu bar for platforms without a system bar (or -menubar).
	// reserves MenubarH() at the top and pushes the tree down. nil = no menus
	Menubar *MenuBar

	// shapes text at the surface's current DPR, set by the shell
	Measure func(f gfx.Font, s string) *text.Run
	// set by the shell
	OnInvalidate func()
	// "pointer", "text", "col-resize", "row-resize", "" = default. set by the shell
	SetCursor func(name string)
	// set by the shell
	WriteClipboard func(s string)
	ReadClipboard  func() string
	// the shell owns text editing
	HostText bool

	hovered   Widget
	active    Widget
	focused   Widget
	focusRing bool
	// no key focus, so the focused editor shows no caret and asks for no blink ticks
	windowInactive bool
	sel            *TextSelection
	// glass that captured the in-flight right-button gesture
	// nil means right-click keeps its context-menu meaning
	rightGlass *Glass

	// pending/visible tooltip, driven by hover idle. the shell wakes for the show
	// moment via TickIn (no timer thread)
	tipW       Widget
	tipX, tipY float32
	tipAt      time.Time

	// session-registered shortcuts (canonical combo -> id) and the upstream callback
	KeyCombos  map[string]int
	OnKeyCombo func(id int)

	OnAux func(button int)

	// gesture token -> source (SetSounds), PlaySound is the audio store
	Sounds    map[string]string
	PlaySound func(src string)

	// active theme tween, nil when idle. advanced each BuildFrame, kept alive by TickIn
	tween *themeTween

	// geometric animation (see animGeometry): structural is armed by the session
	// for frames that applied insert/remove/move ops, animating while any slide or
	// fade is mid-flight, frameAt is the pass clock
	structural bool
	animating  bool
	frameAt    time.Time
	inNew      bool // animWalk is inside a subtree first seen this pass
	inNoFade   bool // animWalk is inside a NoFade subtree

	// see Motion. systemReduced is the OS preference, the shell reports it
	motion        Motion
	systemReduced bool

	// multi-click detection, GLFW only reports individual presses
	lastDown struct {
		t      time.Time
		x, y   float32
		count  int
		target Widget
	}

	lastW, lastH float32

	// -- per-subtree retained display lists (see Core.PaintTree) --
	// prev is last BuildFrame's list. reuse is prev when this frame may splice
	// from it, nil when everything must be rebuilt. full is set by the untargeted
	// Invalidate and the frame after it takes no shortcuts
	prev, reuse *gfx.DisplayList
	full        bool
	// commands copied instead of rebuilt this frame (probes and tests assert on it)
	reused int

	// turns off per-subtree layout skipping for frames the dirty flags don't
	// describe: a resize, an untargeted invalidation, or a geometric animation
	// (offsets bounds after layout, so all of them need re-deriving next frame).
	// see layoutSubtree
	layoutAll bool

	// dpr the shell last measured text at, measureEpoch its change counter.
	// widget text caches (truncation, wrapped lines, natural size) key on it: the
	// same string measures a hair differently at another DPR since advances are in
	// device pixels
	dpr          float32
	measureEpoch int

	semSeq int
}

// focused text editor as the host's input funnel needs it
type TextState struct {
	Value, Placeholder string
	SelStart, SelEnd   int
	Multiline          bool
	Bounds             gfx.Rect
}

func (u *UI) FocusedText() (TextState, bool) {
	var t *TextField
	multiline := false
	switch f := u.focused.(type) {
	case *TextField:
		t = f
	case *TextArea:
		t = &f.TextField
		multiline = true
	default:
		return TextState{}, false
	}
	return TextState{
		Value: t.Value, Placeholder: t.Placeholder,
		SelStart: t.selStart, SelEnd: t.selEnd,
		Multiline: multiline, Bounds: u.focused.Base().Bounds,
	}, true
}

// host reporting the funnel's value and selection after an edit or caret move
func (u *UI) SyncText(value string, selStart, selEnd int, typed bool) {
	switch f := u.focused.(type) {
	case *TextField:
		f.SyncFromHost(value, selStart, selEnd, typed)
	case *TextArea:
		f.SyncFromHost(value, selStart, selEnd, typed)
	}
}

// changes whenever text starts measuring differently. measurement caches key on it
func (u *UI) MeasureEpoch() int { return u.measureEpoch }

// a change drops every cached measurement and rebuilds the frame
func (u *UI) NoteDPR(dpr float32) {
	if dpr > 0 && dpr != u.dpr {
		u.dpr = dpr
		u.measureEpoch++
		u.full = true
	}
}

// commands the last BuildFrame copied from the previous list instead of rebuilding
func (u *UI) Reused() int { return u.reused }

func New() *UI { return &UI{} }

// frame that rebuilds the whole display list. for paint changes no widget owns:
// theme colors (mutated in place behind every widget), server patches that move
// the tree, resize, remount. widget-local changes use Core.Invalidate
func (u *UI) Invalidate() {
	u.full = true
	u.wake()
}

// targeted Invalidate: only w (and whatever else is marked) repaints
// the session uses it for a `set` op whose node it can name
func (u *UI) Damage(w Widget) {
	if w != nil {
		b := w.Base()
		b.dirty = true
		b.InvalidateLayout()
	}
	u.wake()
}

func (u *UI) wake() {
	if u.OnInvalidate != nil {
		u.OnInvalidate()
	}
}

const themeTweenDur = 180 * time.Millisecond

type themeTween struct {
	start    time.Time
	targets  []*gfx.Color
	from, to []gfx.Color
}

// like ApplyTheme but fades existing tokens over ~180ms on the damage clock
// (BuildFrame advances it, TickIn keeps the shell awake). a new theme
// mid-tween restarts from the current colors
func (u *UI) AnimateTheme(tokens map[string]string) {
	if u.reducedMotion() {
		u.tween = nil
		ApplyTheme(tokens)
		u.Invalidate()
		return
	}
	tw := &themeTween{start: time.Now()}
	for k, v := range tokens {
		c := gfx.Hex(v)
		if existing := Theme[k]; existing != nil {
			tw.targets = append(tw.targets, existing)
			tw.from = append(tw.from, *existing)
			tw.to = append(tw.to, c)
		} else {
			Theme[k] = &c // brand-new tokens have nothing to fade from
		}
	}
	if len(tw.targets) == 0 {
		u.tween = nil
		return
	}
	u.tween = tw
	u.Invalidate()
}

// mutates the metric tokens and invalidates everything derived from them
func (u *UI) ApplyMetrics(tokens map[string]float64) {
	applyMetricTokens(tokens)
	u.measureEpoch++
	u.Invalidate()
}

// next frame applied structural server ops (insert/remove/move)
func (u *UI) NoteStructural() { u.structural = true }

const (
	slideDur = 160 * time.Millisecond
	fadeDur  = 200 * time.Millisecond
)

// how much decorative motion plays: the insert fade, the reflow slide and the
// theme crossfade. app-driven animation (shaders, effects) always runs
type Motion uint8

const (
	MotionSystem  Motion = iota // full unless the OS asks for reduced motion
	MotionFull                  // ignores the OS preference
	MotionReduced               // no decorative motion
)

// the session's choice, see Motion. a change applies to motion that starts
// after it, tweens mid-flight finish
func (u *UI) SetMotion(m Motion) { u.motion = m }

// the OS reduce-motion preference, the shell reports it at startup and
// whenever it may have changed
func (u *UI) SetSystemReducedMotion(on bool) { u.systemReduced = on }

func (u *UI) reducedMotion() bool {
	return u.motion == MotionReduced || u.motion == MotionSystem && u.systemReduced
}

func easeOut(t float32) float32 { return t * (2 - t) }

// runs between layout and paint. on a structural frame it starts a decaying
// paint offset for every widget the reflow moved and a fade-in for every new
// widget. every frame it advances the tweens and applies the offsets.
// layout re-derives bounds next frame so the offset never leaks into real
// geometry, and since prevX/prevY track every frame, scroll movement is already
// absorbed by the time a structural frame compares against them
func (u *UI) animGeometry(dl *gfx.DisplayList) {
	now := time.Now()
	dt := float32(now.Sub(u.frameAt).Seconds())
	if dt <= 0 || dt > 0.05 {
		dt = 0.016 // first frame or waking from idle: one nominal step
	}
	u.frameAt = now
	u.animating = false
	// a structural frame under reduced motion snaps, prevX/prevY still track
	if u.structural && u.reducedMotion() {
		u.structural = false
	}
	u.animWalk(u.Root, dt)
	u.structural = false
	if u.animating {
		// geometric tweens move bounds every frame so the shell must rebuild, never
		// replay a retained list (GeomAnimation gates RenderPartial and implies
		// WantsAnimation)
		dl.GeomAnimation = true
	}
}

// aggregates needsPaint bottom-up and consumes the dirty bits. false means the
// whole subtree splices last frame's commands. every root is marked before it
// paints, and a true widget makes every ancestor true, so the walk that clears
// a dirty widget always reaches it
func (u *UI) markPaint(w Widget) bool {
	b := w.Base()
	b.takeRequests()
	need := b.selfNeedsPaint()
	b.dirty = false
	for _, c := range b.Kids {
		if u.markPaint(c) {
			need = true
		}
	}
	b.needsPaint = need
	return need
}

// paints one of the four roots and records its command range for next frame's
// splice. the UI stands in for the parent with its start at 0, so a root's
// cacheOff is absolute
func (u *UI) paintRoot(dl *gfx.DisplayList, w Widget) {
	b := w.Base()
	at := len(dl.Cmds)
	oldBase := -1
	if u.reuse != nil && b.cacheOK {
		oldBase = int(b.cacheOff)
	}
	w.PaintTree(dl, oldBase)
	b.cacheOff, b.cacheLen, b.cacheOK = int32(at), int32(len(dl.Cmds)-at), true
}

func (u *UI) animWalk(w Widget, dt float32) {
	b := w.Base()
	bb := &b.Bounds
	isNew := !b.hadFrame
	if isNew {
		b.hadFrame = true
		// only the top of a newly inserted subtree fades, descendants ride along in
		// that layer
		if u.structural && !u.inNew && !u.inNoFade && !b.NoFade {
			b.fadeR = 1
		}
	} else if u.structural && (bb.X != b.prevX || bb.Y != b.prevY) {
		// retarget from the current visual position so a mid-slide widget moved again
		// glides on without snapping
		k := 1 - easeOut(1-b.animR)
		b.animDx = b.prevX + b.animDx*k - bb.X
		b.animDy = b.prevY + b.animDy*k - bb.Y
		b.animR = 1
	}
	b.prevX, b.prevY = bb.X, bb.Y
	if b.animR > 0 {
		if b.animR -= dt / float32(slideDur.Seconds()); b.animR < 0 {
			b.animR = 0
		}
		k := 1 - easeOut(1-b.animR)
		bb.X += b.animDx * k
		bb.Y += b.animDy * k
		if b.animR > 0 {
			u.animating = true
		}
	}
	if b.fadeR > 0 {
		if b.fadeR -= dt / float32(fadeDur.Seconds()); b.fadeR < 0 {
			b.fadeR = 0
		}
		if b.fadeR > 0 {
			u.animating = true
		}
	}
	// marking rides along with the pass that already visits every widget, after
	// the animation bookkeeping settles the bounds a splice compares against
	b.takeRequests()
	need := b.selfNeedsPaint()
	b.dirty = false
	outer, outerNoFade := u.inNew, u.inNoFade
	u.inNew = outer || isNew
	u.inNoFade = outerNoFade || b.NoFade
	for _, c := range b.Kids {
		u.animWalk(c, dt)
		if c.Base().needsPaint {
			need = true
		}
	}
	u.inNew, u.inNoFade = outer, outerNoFade
	b.needsPaint = need
}

func (u *UI) advanceTween() {
	tw := u.tween
	if tw == nil {
		return
	}
	t := float32(time.Since(tw.start)) / float32(themeTweenDur)
	if t >= 1 {
		for i, c := range tw.targets {
			*c = tw.to[i]
		}
		u.tween = nil
		return
	}
	e := easeOut(t)
	for i, c := range tw.targets {
		*c = gfx.Mix(tw.from[i], tw.to[i], e)
	}
}

// whether f has a real glyph for the first rune of s, so widgets can fall back
// to drawn shapes for symbols the embedded fonts lack
func (u *UI) GlyphAvailable(f gfx.Font, s string) bool {
	run := u.Measure(f, s)
	return len(run.Glyphs) > 0 && run.Glyphs[0].GID != 0
}

// logical viewport height, for popover placement
func (u *UI) ViewH() float32 { return u.lastH }

func (u *UI) BuildFrame(dl *gfx.DisplayList, w, h float32) {
	if dl.Measure == nil {
		// text commands record their line box so a partial frame can tell which
		// text a damage rect touches
		dl.Measure = func(f gfx.Font, s string) (float32, float32, float32) {
			r := u.Measure(f, s)
			return r.Width, r.Ascent, r.Descent
		}
	}

	tweening := u.tween != nil
	u.advanceTween()
	resized := w != u.lastW || h != u.lastH
	if resized {
		u.lastW, u.lastH = w, h
		u.Overlay = nil // anchored popovers don't survive a resize
		u.clearTip()
	}

	u.reuse = u.prev
	if u.full || resized || tweening || u.structural {
		u.reuse = nil
	}
	u.layoutAll = u.full || resized || u.structural || u.animating
	u.full = false
	u.reused = 0
	if p := u.prev; p != nil && cap(dl.Cmds) < len(p.Cmds) {
		dl.Cmds = make([]gfx.Cmd, 0, len(p.Cmds)+len(p.Cmds)/8+16)
	}
	if u.Root == nil {
		u.prev = dl
		return
	}
	barH := float32(0)
	if u.Menubar != nil {
		barH = MenubarH()
	}
	u.Root.Base().UI = u
	u.Root.Base().Bounds = gfx.R(0, barH, w, h-barH)
	layoutSubtree(u.Root)
	u.animGeometry(dl) // lays down the marking too
	u.paintRoot(dl, u.Root)
	if u.Menubar != nil {
		u.Menubar.Base().UI = u
		u.Menubar.Base().Bounds = gfx.R(0, 0, w, barH)
		u.markPaint(u.Menubar)
		u.paintRoot(dl, u.Menubar)
	}
	if u.Overlay != nil {
		u.Overlay.Base().UI = u
		layoutSubtree(u.Overlay)
		u.markPaint(u.Overlay)
		u.paintRoot(dl, u.Overlay)
	}
	u.paintTip(dl, w, h)
	if u.Banner != nil {
		u.Banner.Base().UI = u
		u.Banner.Base().Bounds = gfx.R(0, 0, w, h)
		layoutSubtree(u.Banner)
		u.markPaint(u.Banner)
		u.paintRoot(dl, u.Banner)
	}
	u.prev = dl
}

// mount/remount, drops every stateful reference into the old tree
func (u *UI) SetRoot(w Widget) {
	u.setFocus(nil, false)
	u.clearSelection()
	u.clearTip()
	u.hovered = nil
	u.active = nil
	u.Overlay = nil
	u.Root = w
	w.Base().UI = u
	u.Invalidate()
}

func (u *UI) OpenOverlay(w Widget) {
	w.Base().UI = u
	u.Overlay = w
	u.Invalidate()
}

func (u *UI) CloseOverlay() {
	if u.Overlay != nil {
		u.Sound("close")
		u.dismissOverlay()
	}
}

func (u *UI) dismissOverlay() {
	if u.Overlay != nil {
		u.Overlay = nil
		u.Invalidate()
	}
}

// -- pointer routing ---------------------------------------------------------------

// recover barrier around one input dispatch: a panicking widget logs a stack
// and drops that event instead of taking the terminal down. input-side twin of
// the server SDK's handler barrier
func guardInput(what string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Msgf("caution terminal: panic in %s input: %v\n%s", what, r, debug.Stack())
		}
	}()
	fn()
}

func (u *UI) PointerDown(x, y float32) {
	guardInput("pointer-down", func() { u.pointerDown(x, y) })
}

func (u *UI) PointerMove(x, y float32) {
	guardInput("pointer-move", func() { u.pointerMove(x, y) })
}

func (u *UI) PointerUp(x, y float32) {
	guardInput("pointer-up", func() { u.pointerUp(x, y) })
}

func (u *UI) Wheel(x, y, dx, dy float32) {
	guardInput("wheel", func() { u.wheel(x, y, dx, dy) })
}

func (u *UI) CharInput(r rune) {
	guardInput("char", func() { u.charInput(r) })
}

func (u *UI) KeyDown(k Key) bool {
	handled := false
	guardInput("key", func() { handled = u.keyDown(k) })
	return handled
}

func (u *UI) ContextClick(x, y float32) {
	guardInput("context-click", func() { u.contextClick(x, y) })
}

// true when a right-gesture glass under the pointer captured it, and then the
// caller must NOT open a context menu. false keeps the context-menu meaning
func (u *UI) RightDown(x, y float32) bool {
	captured := false
	guardInput("right-down", func() {
		if g := u.glassRightAt(u.Root, x, y); g != nil {
			u.rightGlass = g
			g.RPickAt(x, y)
			captured = true
		}
	})
	return captured
}

func (u *UI) AuxDown(button int) {
	u.clearTip()
	if u.Overlay != nil {
		u.CloseOverlay()
		return
	}
	if u.OnAux != nil {
		guardInput("aux-down", func() { u.OnAux(button) })
	}
}

func (u *UI) SetSounds(tokens map[string]string) { u.Sounds = tokens }

func (u *UI) Sound(token string) {
	if u.PlaySound == nil || token == "" {
		return
	}
	if src := u.Sounds[token]; src != "" {
		u.PlaySound(src)
	}
}

func (u *UI) RightUp(x, y float32) {
	guardInput("right-up", func() {
		if u.rightGlass != nil {
			u.rightGlass.RDropAt(x, y)
			u.rightGlass = nil
		}
	})
}

// deepest right-gesture glass under the pointer. same shape as glassWheelAt,
// gated on rpick so inert glass never eats the context menu
func (u *UI) glassRightAt(w Widget, x, y float32) *Glass {
	if w == nil {
		return nil
	}
	b := w.Base()
	if b.Clips && !Contains(b.Bounds, x, y) {
		return nil
	}
	for i := len(b.Kids) - 1; i >= 0; i-- {
		if found := u.glassRightAt(b.Kids[i], x, y); found != nil {
			return found
		}
	}
	if g, ok := w.(*Glass); ok && g.OnRPick != nil && Contains(b.Bounds, x, y) {
		return g
	}
	return nil
}

func (u *UI) pointerDown(x, y float32) {
	// any press starts a new selection (selectable targets re-set it in
	// OnPointerDown) or clears the old one
	u.clearSelection()
	u.clearTip()
	if u.Overlay != nil {
		hit := u.Overlay.HitTest(x, y)
		if hit == nil {
			u.CloseOverlay() // click-away closes and swallows the press
			// except the menu bar, where a press opens the next menu in the same gesture
			if u.Menubar != nil {
				if bar := u.Menubar.HitTest(x, y); bar != nil {
					bar.OnPointerDown(x, y, 1)
				}
			}
			return
		}
		u.active = hit
		hit.OnPointerDown(x, y, 1)
		return
	}
	var hit Widget
	if u.Menubar != nil {
		hit = u.Menubar.HitTest(x, y)
	}
	if hit == nil && u.Root != nil {
		hit = u.Root.HitTest(x, y)
	}
	d := &u.lastDown
	now := time.Now()
	if now.Sub(d.t) < 450*time.Millisecond && hypot32(x-d.x, y-d.y) < 6 && hit == d.target {
		d.count++
	} else {
		d.count = 1
	}
	d.t, d.x, d.y, d.target = now, x, y, hit
	u.active = hit
	var focusTarget Widget
	if hit != nil && hit.Focusable() {
		focusTarget = hit
	}
	u.setFocus(focusTarget, false)
	if hit != nil {
		hit.OnPointerDown(x, y, d.count)
	}
}

func (u *UI) pointerMove(x, y float32) {
	if u.rightGlass != nil {
		u.rightGlass.RDragAt(x, y)
		return
	}
	if u.active != nil {
		u.active.OnPointerDrag(x, y)
		return
	}
	var hit Widget
	if u.Overlay != nil {
		hit = u.Overlay.HitTest(x, y)
	}
	if hit == nil && u.Menubar != nil {
		hit = u.Menubar.HitTest(x, y)
	}
	if hit == nil && u.Root != nil {
		hit = u.Root.HitTest(x, y)
	}
	if hit != u.hovered {
		if u.hovered != nil {
			u.hovered.OnHoverChange(false)
		}
		u.hovered = hit
		if hit != nil {
			hit.OnHoverChange(true)
		}
	}
	u.tipCandidate(hit, x, y)
	// hover before cursor: positional cursors (table column dividers) update
	// their state in OnPointerHover
	if hit != nil {
		hit.OnPointerHover(x, y)
	}
	if u.SetCursor != nil {
		cur := ""
		if hit != nil {
			cur = hit.Cursor()
		}
		u.SetCursor(cur)
	}
}

func (u *UI) pointerUp(x, y float32) {
	if u.active != nil {
		u.active.OnPointerUp(x, y)
		u.active = nil
	}
}

func (u *UI) wheel(x, y, dx, dy float32) {
	u.clearTip()
	// a modal dialog confines the wheel like it does clicks, scrolling the scrim
	// or anything outside the dialog is swallowed
	var scope Widget = u.Root
	if d := lastDialog(u.Root); d != nil {
		scope = d
	}
	// a subscribed glass under the point owns the wheel, so a camera or zoomable
	// canvas outranks a scroll view beneath the pane
	if g := u.glassWheelAt(scope, x, y); g != nil {
		g.WheelAt(x, y, dx, dy)
		return
	}
	sv := u.scrollTargetAt(scope, x, y)
	if sv == nil {
		return
	}
	if dy != 0 {
		sv.ScrollBy(dy)
	}
	if dx != 0 {
		if h, ok := sv.(HScrollTarget); ok {
			h.ScrollByX(dx)
		}
	}
}

// -- tooltips -----------------------------------------------------------------

const tipDelay = 600 * time.Millisecond

// tracks the hover target, a widget with a tip shows it after ~600ms idle
func (u *UI) tipCandidate(hit Widget, x, y float32) {
	if hit == nil || hit.Base().Tip == "" {
		u.clearTip()
		return
	}
	if u.tipW == hit {
		if !u.tipShown() {
			u.tipX, u.tipY = x, y // follow the pointer until shown, then hold
		}
		return
	}
	u.clearTip()
	u.tipW = hit
	u.tipX, u.tipY = x, y
	u.tipAt = time.Now().Add(tipDelay)
	u.Invalidate() // start the TickIn countdown
}

func (u *UI) clearTip() {
	if u.tipW != nil {
		shown := u.tipShown()
		u.tipW = nil
		if shown {
			u.Invalidate()
		}
	}
}

func (u *UI) tipShown() bool {
	return u.tipW != nil && !time.Now().Before(u.tipAt)
}

// above the tree and overlay, below the connection banner
func (u *UI) paintTip(dl *gfx.DisplayList, vw, vh float32) {
	if !u.tipShown() {
		return
	}
	text := u.tipW.Base().Tip
	f := gfx.NewFont(12, gfx.FontOpts{})
	m := u.Measure(f, text)
	const padX, padY = 8, 5
	w := ceil32(m.Width) + padX*2
	h := ceil32(m.Ascent+m.Descent) + padY*2
	x := max(4, min(u.tipX+12, vw-w-4))
	y := max(4, min(u.tipY+18, vh-h-4))
	box := gfx.R(x, y, w, h)
	dl.Shadow(box, gfx.WithAlpha(gfx.Black, 0.35), 10, gfx.CornerRadius(Metrics.RadiusControl), 0, 2)
	edge := Theme["edgeSoft"]
	dl.Rect(box, *Theme["panelAlt"], gfx.RectOpts{Radius: gfx.CornerRadius(Metrics.RadiusControl), BorderWidth: Metrics.BorderWidth, BorderColor: edge})
	dl.Text(text, x+padX, y+padY, f, *Theme["ink"])
}

// -- keyboard ------------------------------------------------------------------------

func (u *UI) charInput(r rune) {
	if u.focused != nil {
		u.focused.OnChar(r)
	}
}

// pointer-style focus with no keyboard ring, for server-driven focus
func (u *UI) FocusWidget(w Widget) {
	u.setFocus(w, false)
}

// how long until the shell should wake for a time-driven repaint (caret blink,
// tooltip show). zero means no tick
func (u *UI) TickIn() time.Duration {
	var tick time.Duration
	if tf := u.focusedText(); tf != nil && tf.Focused && tf.selStart == tf.selEnd {
		tick = tf.blinkTick()
	}
	if u.tipW != nil && !u.tipShown() {
		if d := max(time.Until(u.tipAt), time.Millisecond); tick == 0 || d < tick {
			tick = d
		}
	}
	if u.tween != nil {
		const frame = 8 * time.Millisecond
		if tick == 0 || frame < tick {
			tick = frame
		}
	}
	return tick
}

// the TickIn wake came due. the caret's blink phase is the one thing a widget
// paints from the clock rather than its own state
func (u *UI) NoteTick() {
	if tf := u.focusedText(); tf != nil {
		tf.markDirty()
	}
}

func (u *UI) SetWindowActive(active bool) {
	if u.windowInactive != active {
		return
	}
	u.windowInactive = !active
	if tf := u.focusedText(); tf != nil {
		tf.touch()
		tf.Invalidate()
	}
}

func (u *UI) focusedText() *TextField {
	switch w := u.focused.(type) {
	case *TextField:
		return w
	case *TextArea:
		return &w.TextField
	}
	return nil
}

func (u *UI) keyDown(k Key) bool {
	if k.Name == "Tab" {
		if tf := u.focusedText(); tf != nil && tf.OnTab != nil && !k.Meta && !k.Ctrl && !k.Alt {
			tf.OnTab(k.Shift, tf.Value, tf.selEnd)
			return true
		}
		dir := 1
		if k.Shift {
			dir = -1
		}
		u.moveFocus(dir)
		return true
	}
	if u.focused != nil && u.focused.OnKey(k) {
		return true
	}
	// in-window menu key equivalents fire once the focused widget declines and
	// before app combos, so text editing keeps its keys. under NSMenu these chords
	// never reach here
	if u.Menubar != nil && (k.Meta || k.Ctrl || k.Alt) {
		if id, ok := u.Menubar.Combos()[comboString(k)]; ok && u.Menubar.Perform(id) {
			return true
		}
	}
	// app combos next, before shell built-ins (an app may deliberately claim Cmd+C)
	if u.OnKeyCombo != nil && (k.Meta || k.Ctrl || k.Alt) {
		if id, ok := u.KeyCombos[comboString(k)]; ok {
			u.OnKeyCombo(id)
			return true
		}
	}
	if (k.Meta || k.Ctrl) && k.Name == "c" {
		if s := u.SelectedText(); s != "" && u.WriteClipboard != nil {
			u.WriteClipboard(s)
			return true
		}
		return false
	}
	if k.Name == "Escape" {
		if u.Overlay != nil {
			u.CloseOverlay()
			return true
		}
		if d := lastDialog(u.Root); d != nil && d.OnDismiss != nil {
			d.sound("close")
			d.OnDismiss()
			return true
		}
		u.clearSelection()
		return true
	}
	return false
}

func lastDialog(w Widget) *Dialog {
	if w == nil {
		return nil
	}
	var found *Dialog
	if d, ok := w.(*Dialog); ok {
		found = d
	}
	for _, c := range w.Base().Kids {
		if d := lastDialog(c); d != nil {
			found = d
		}
	}
	return found
}

// -- focus ------------------------------------------------------------------

func (u *UI) setFocus(w Widget, viaKeyboard bool) {
	if u.focused == w && u.focusRing == viaKeyboard {
		return
	}
	old := u.focused
	u.focused = w
	u.focusRing = viaKeyboard
	if old != w {
		if old != nil {
			old.OnFocusChange(false)
		}
		if w != nil {
			w.OnFocusChange(true)
		}
	}
	u.Invalidate()
}

func (u *UI) IsFocused(w Widget) bool { return u.focused == w }

// keyboard-driven focus only, like :focus-visible
func (u *UI) ShowFocusRing(w Widget) bool { return u.focused == w && u.focusRing }

func (u *UI) moveFocus(dir int) { u.moveFocusFrom(u.focused, dir) }

// as Tab would from w. dropped once w lost focus, the server answered late
func (u *UI) TabOut(w Widget, dir int) {
	if u.focused == w {
		u.moveFocusFrom(w, dir)
	}
}

func (u *UI) moveFocusFrom(from Widget, dir int) {
	var order []Widget
	var collect func(w Widget)
	collect = func(w Widget) {
		if w.Focusable() {
			order = append(order, w)
		}
		for _, c := range w.Base().Kids {
			collect(c)
		}
	}
	// focus is trapped in the topmost dialog, Tab must not wander into the inert
	// background
	scope := u.Root
	if d := lastDialog(u.Root); d != nil {
		scope = d
	}
	if scope != nil {
		collect(scope)
	}
	if len(order) == 0 {
		return
	}
	idx := -1
	for i, w := range order {
		if w == from {
			idx = i
			break
		}
	}
	next := order[((idx+dir)%len(order)+len(order))%len(order)]
	u.setFocus(next, true)
	u.RevealWidget(next)
	// tabbing into a text field selects its content
	switch tf := next.(type) {
	case *TextField:
		tf.SelectAll()
	case *TextArea:
		tf.SelectAll()
	}
}

// scrolls enclosing scrollers to show w (Tab focus, server reveal)
func (u *UI) RevealWidget(w Widget) {
	for p := w.Base().Parent; p != nil; p = p.Base().Parent {
		st, ok := p.(ScrollTarget)
		if !ok || !st.Scrollable() {
			continue
		}
		// bounds are from the last layout, so an outer scroller adjusted after an
		// inner one uses slightly stale offsets. fine for focus reveal
		v := p.Base().Bounds
		b := w.Base().Bounds
		if b.Y < v.Y {
			st.ScrollBy(b.Y - v.Y - 8)
		} else if b.Y+b.H > v.Y+v.H {
			st.ScrollBy(b.Y + b.H - (v.Y + v.H) + 8)
		}
	}
}

// whether a press should move the window instead of dispatching: the point is
// in a windowDrag subtree (app titlebar under Options.CustomTitlebar), no
// interactive widget claims it, and no overlay or menubar is up. the shell
// does the actual move
func (u *UI) WindowDragAt(x, y float32) bool {
	if u.Root == nil || u.Overlay != nil {
		return false
	}
	if u.Menubar != nil && y < MenubarH() {
		return false
	}
	if u.Root.HitTest(x, y) != nil {
		return false
	}
	return dragMarkAt(u.Root, x, y)
}

// any windowDrag widget containing the point counts, so unmarked inert content
// inside a marked panel drags through
func dragMarkAt(w Widget, x, y float32) bool {
	b := w.Base()
	if b.Clips && !Contains(b.Bounds, x, y) {
		return false
	}
	if b.WindowDrag && Contains(b.Bounds, x, y) {
		return true
	}
	for i := len(b.Kids) - 1; i >= 0; i-- {
		if dragMarkAt(b.Kids[i], x, y) {
			return true
		}
	}
	return false
}

// deepest wheel-subscribed glass under the pointer. can't use HitTest: glass
// hit-interactivity is gated on OnPick so an unsubscribed pane never steals
// presses, and a wheel-only glass (zoomable canvas, no pick) must own the wheel
// without swallowing clicks
func (u *UI) glassWheelAt(w Widget, x, y float32) *Glass {
	if w == nil {
		return nil
	}
	b := w.Base()
	if b.Clips && !Contains(b.Bounds, x, y) {
		return nil
	}
	for i := len(b.Kids) - 1; i >= 0; i-- {
		if found := u.glassWheelAt(b.Kids[i], x, y); found != nil {
			return found
		}
	}
	if g, ok := w.(*Glass); ok && g.OnWheelAt != nil && Contains(b.Bounds, x, y) {
		return g
	}
	return nil
}

// deepest scrollable under the pointer, wheel events route straight to it
// (trackpad inertia arrives baked in)
func (u *UI) scrollTargetAt(w Widget, x, y float32) ScrollTarget {
	if w == nil {
		return nil
	}
	b := w.Base()
	if b.Clips && !Contains(b.Bounds, x, y) {
		return nil
	}
	for i := len(b.Kids) - 1; i >= 0; i-- {
		if found := u.scrollTargetAt(b.Kids[i], x, y); found != nil {
			return found
		}
	}
	if st, ok := w.(ScrollTarget); ok && Contains(b.Bounds, x, y) {
		h, hok := w.(HScrollTarget)
		if st.Scrollable() || (hok && h.ScrollableX()) {
			return st
		}
	}
	return nil
}

// -- selection --------------------------------------------------------------

func (u *UI) selectionFor(w selectionOwner) *TextSelection {
	if u.sel != nil && u.sel.Owner == w {
		return u.sel
	}
	return nil
}

func (u *UI) setSelection(w selectionOwner, start, end int) {
	u.sel = &TextSelection{Owner: w, Start: start, End: end}
	u.Invalidate()
}

func (u *UI) moveSelectionEnd(w selectionOwner, end int) {
	if u.sel != nil && u.sel.Owner == w && u.sel.End != end {
		u.sel.End = end
		u.Invalidate()
	}
}

func (u *UI) clearSelection() {
	if u.sel != nil {
		u.sel = nil
		u.Invalidate()
	}
}

func (u *UI) SelectedText() string {
	if u.sel == nil {
		return ""
	}
	return u.sel.Owner.GlyphText(u.sel.Start, u.sel.End)
}

func hypot32(a, b float32) float32 {
	return float32(math.Hypot(float64(a), float64(b)))
}

// cmd+ctrl+alt+shift+<key>, matching the server SDK's normalization ("space"
// for " ", lowercase names)
func comboString(k Key) string {
	out := ""
	if k.Meta {
		out += "cmd+"
	}
	if k.Ctrl {
		out += "ctrl+"
	}
	if k.Alt {
		out += "alt+"
	}
	if k.Shift {
		out += "shift+"
	}
	name := strings.ToLower(k.Name)
	if name == " " {
		name = "space"
	}
	return out + name
}
