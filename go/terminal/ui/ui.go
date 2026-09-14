package ui

import (
	"math"
	"runtime/debug"
	"strings"
	"time"

	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/text"

	"github.com/rs/zerolog/log"
)

// TextSelection is a drag selection over a Label's glyph boundaries. Start
// may exceed End while dragging backwards.
type TextSelection struct {
	Label      *Label
	Start, End int
}

// Ui is the client runtime shell: owns the root widget and all generic
// interaction: pointer routing with capture, hover tracking, cursor, text
// selection, clipboard copy, focus. The GLFW layer feeds it events and the
// protocol session feeds it trees. Nothing here is app-specific.
type Ui struct {
	Root Widget
	// Overlay is the client-local layer (popovers, menus): painted above the
	// tree, hit first.
	Overlay Widget
	// Banner is the client-owned status layer (connection state): painted above
	// everything, laid out against the viewport, and never hit-tested, so input
	// passes through to the tree beneath and it stays locally interactive.
	Banner Widget
	// Menubar is the in-window menu bar (platforms without a system bar, or
	// -menubar): client-local like the banner. It reserves MenubarH() at the top
	// and pushes the tree down. nil = no menus. Set via SetMenubar.
	Menubar *MenuBar

	// Measure shapes text at the surface's current DPR (set by the shell).
	Measure func(f gfx.Font, s string) *text.Run
	// OnInvalidate marks the frame dirty (set by the shell).
	OnInvalidate func()
	// SetCursor maps a cursor name ("pointer", "text", "col-resize",
	// "row-resize", "" = default) onto the window (set by the shell).
	SetCursor func(name string)
	// WriteClipboard copies text and ReadClipboard pastes (set by the shell).
	WriteClipboard func(s string)
	ReadClipboard  func() string

	hovered   Widget
	active    Widget
	focused   Widget
	focusRing bool
	sel       *TextSelection
	// rightGlass is the glass that captured the in-flight right-button
	// gesture. When nil, right-click keeps its context-menu meaning.
	rightGlass *Glass

	// Pending/visible tooltip, client-local, driven by hover idle. The shell
	// wakes for the show moment via TickIn (no timer thread).
	tipW       Widget
	tipX, tipY float32
	tipAt      time.Time

	// Session-registered shortcuts (canonical combo string -> id) and the
	// upstream callback, set by the protocol session.
	KeyCombos  map[string]int
	OnKeyCombo func(id int)

	OnAux func(button int)

	// Sounds maps gesture tokens to sources (SetSounds) and PlaySound is the
	// audio store
	Sounds    map[string]string
	PlaySound func(src string)

	// Active theme tween (nil when idle), advanced at each BuildFrame and
	// kept alive by TickIn.
	tween *themeTween

	// Geometric animation (see animGeometry): structural is armed by the
	// session for frames that applied insert/remove/move ops, animating is
	// true while any slide or fade is mid-flight, frameAt is the pass clock.
	structural bool
	animating  bool
	frameAt    time.Time

	// Multi-click detection. GLFW reports individual presses only.
	lastDown struct {
		t      time.Time
		x, y   float32
		count  int
		target Widget
	}

	lastW, lastH float32

	// -- per-subtree retained display lists (see Core.PaintTree) --
	// prev is the list the last BuildFrame produced. reuse is prev when this
	// frame is allowed to splice from it, nil when everything must be
	// rebuilt. full is set by the untargeted Invalidate, and the frame after it
	// takes no shortcuts.
	prev, reuse *gfx.DisplayList
	full        bool
	// reused counts commands copied instead of rebuilt this frame (probes and
	// tests assert on it).
	reused int

	// layoutAll turns off per-subtree layout skipping for this frame, for the
	// cases the dirty flags do not describe: a resize, an untargeted
	// invalidation, or a geometric animation, which offsets bounds after
	// layout and needs every one of them re-derived next frame. See
	// layoutSubtree.
	layoutAll bool

	// dpr is the device pixel ratio the shell last measured text at, and
	// measureEpoch its change counter. Widget-level text caches (a label's
	// truncation, its wrapped lines, its natural size) are keyed by it: the
	// same string at the same font measures a hair differently at a different
	// DPR, because advances are computed in device pixels.
	dpr          float32
	measureEpoch int
}

// MeasureEpoch changes whenever text starts measuring differently under the
// widgets. Anything caching a measurement keys on it.
func (u *Ui) MeasureEpoch() int { return u.measureEpoch }

// NoteDPR tells the Ui what the shell is measuring text at. A change drops
// every cached measurement and rebuilds the frame.
func (u *Ui) NoteDPR(dpr float32) {
	if dpr > 0 && dpr != u.dpr {
		u.dpr = dpr
		u.measureEpoch++
		u.full = true
	}
}

// Reused is how many commands the last BuildFrame copied from the previous
// frame's list instead of rebuilding.
func (u *Ui) Reused() int { return u.reused }

func New() *Ui { return &Ui{} }

// Invalidate asks for a frame that rebuilds the whole display list. Use it for
// anything that changes painting without being attributable to a widget: theme
// colors (mutated in place behind every widget's back), server patches that
// move the tree around, resize, remount. Widget-local changes go through
// Core.Invalidate instead.
func (u *Ui) Invalidate() {
	u.full = true
	u.wake()
}

// Damage asks for a frame in which only w (and whatever else is marked)
// repaints: the targeted counterpart of Invalidate, used by the protocol
// session for a `set` op whose node it can name.
func (u *Ui) Damage(w Widget) {
	if w != nil {
		b := w.Base()
		b.dirty = true
		b.InvalidateLayout()
	}
	u.wake()
}

func (u *Ui) wake() {
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

// AnimateTheme retargets the theme like ApplyTheme, but fades existing tokens
// over ~180ms. Driven by the damage clock: BuildFrame advances it and TickIn
// keeps the shell awake. A new theme landing mid-tween restarts from the
// current colors.
func (u *Ui) AnimateTheme(tokens map[string]string) {
	tw := &themeTween{start: time.Now()}
	for k, v := range tokens {
		c := gfx.Hex(v)
		if existing := Theme[k]; existing != nil {
			tw.targets = append(tw.targets, existing)
			tw.from = append(tw.from, *existing)
			tw.to = append(tw.to, c)
		} else {
			cc := c
			Theme[k] = &cc // brand-new tokens have nothing to fade from
		}
	}
	if len(tw.targets) == 0 {
		u.tween = nil
		return
	}
	u.tween = tw
	u.Invalidate()
}

// ApplyMetrics mutates the metric tokens (see Metrics) and invalidates
// everything derived from them.
func (u *Ui) ApplyMetrics(tokens map[string]float64) {
	applyMetricTokens(tokens)
	u.measureEpoch++
	u.Invalidate()
}

// NoteStructural marks the next frame as one that applied structural server
// ops (insert/remove/move)
func (u *Ui) NoteStructural() { u.structural = true }

const (
	slideDur = 160 * time.Millisecond
	fadeDur  = 200 * time.Millisecond
)

func easeOut(t float32) float32 { return t * (2 - t) }

// animGeometry runs between layout and paint. On a structural frame it starts a
// decaying paint offset for every widget the reflow moved and a fade-in for
// every widget it introduced. On every frame it advances the tweens and applies
// the offsets. Bounds mutated here are re-derived by layout next frame, so the
// offset never contaminates real geometry, and since prevX/prevY track every
// frame, scroll-induced movement is already absorbed by the time a structural
// frame compares against them.
func (u *Ui) animGeometry(dl *gfx.DisplayList) {
	now := time.Now()
	dt := float32(now.Sub(u.frameAt).Seconds())
	if dt <= 0 || dt > 0.05 {
		dt = 0.016 // first frame, or waking from idle: one nominal step
	}
	u.frameAt = now
	u.animating = false
	u.animWalk(u.Root, dt)
	u.structural = false
	if u.animating {
		// Geometric tweens move bounds every frame, so the shell must rebuild,
		// never replay a retained list (GeomAnimation gates RenderPartial, and
		// implies WantsAnimation).
		dl.GeomAnimation = true
	}
}

// markPaint aggregates needsPaint bottom-up and consumes the dirty bits. A
// widget it leaves false is one whose whole subtree can splice last frame's
// commands. Every root gets marked before it is painted, and a widget marked true
// makes every ancestor true, so a dirty widget is always reached by the walk
// that clears it.
func (u *Ui) markPaint(w Widget) bool {
	b := w.Base()
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

// paintRoot paints one of the Ui's four roots and records the command range it
// produced, so the next frame can splice it. The Ui stands in for the parent,
// with its own start at 0, so a root's cacheOff is absolute.
func (u *Ui) paintRoot(dl *gfx.DisplayList, w Widget) {
	b := w.Base()
	at := len(dl.Cmds)
	oldBase := -1
	if u.reuse != nil && b.cacheOK {
		oldBase = int(b.cacheOff)
	}
	w.PaintTree(dl, oldBase)
	b.cacheOff, b.cacheLen, b.cacheOK = int32(at), int32(len(dl.Cmds)-at), true
}

func (u *Ui) animWalk(w Widget, dt float32) {
	b := w.Base()
	bb := &b.Bounds
	if !b.hadFrame {
		b.hadFrame = true
		if u.structural {
			b.fadeR = 1 // introduced by an op - fade in
		}
	} else if u.structural && (bb.X != b.prevX || bb.Y != b.prevY) {
		// Retarget from the current *visual* position, so a mid-slide
		// widget moved again glides on without snapping.
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
	// Marking rides along with the pass that already visits every widget. The
	// animation bookkeeping above settles the bounds a splice is compared
	// against.
	need := b.selfNeedsPaint()
	b.dirty = false
	for _, c := range b.Kids {
		u.animWalk(c, dt)
		if c.Base().needsPaint {
			need = true
		}
	}
	b.needsPaint = need
}

// advanceTween moves the active theme tween to now
func (u *Ui) advanceTween() {
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
	e := t * (2 - t) // ease-out
	for i, c := range tw.targets {
		*c = gfx.Mix(tw.from[i], tw.to[i], e)
	}
}

// GlyphAvailable reports whether the face behind f has a real glyph for the
// first rune of s. It lets widgets fall back to drawn shapes for symbols the
// embedded fonts lack.
func (u *Ui) GlyphAvailable(f gfx.Font, s string) bool {
	run := u.Measure(f, s)
	return len(run.Glyphs) > 0 && run.Glyphs[0].GID != 0
}

// ViewH is the current logical viewport height (popover placement).
func (u *Ui) ViewH() float32 { return u.lastH }

// BuildFrame lays out and paints one frame at the given logical size.
func (u *Ui) BuildFrame(dl *gfx.DisplayList, w, h float32) {
	if dl.Measure == nil {
		// Text commands record the line box they will paint into, so a
		// partial frame can tell which text a damage rect actually touches.
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

// SetRoot swaps the whole tree (mount/remount) and drops every stateful
// reference into the old one.
func (u *Ui) SetRoot(w Widget) {
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

func (u *Ui) OpenOverlay(w Widget) {
	w.Base().UI = u
	u.Overlay = w
	u.Invalidate()
}

func (u *Ui) CloseOverlay() {
	if u.Overlay != nil {
		u.Sound("close")
		u.dismissOverlay()
	}
}

func (u *Ui) dismissOverlay() {
	if u.Overlay != nil {
		u.Overlay = nil
		u.Invalidate()
	}
}

// -- pointer routing ---------------------------------------------------------------

// guardInput runs one input dispatch behind a recover barrier: a panicking
// widget logs a stack trace and drops that one event instead of taking the
// terminal down. It is the input-side twin of the server SDK's handler
// barrier.
func guardInput(what string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Msgf("caution terminal: panic in %s input: %v\n%s", what, r, debug.Stack())
		}
	}()
	fn()
}

func (u *Ui) PointerDown(x, y float32) {
	guardInput("pointer-down", func() { u.pointerDown(x, y) })
}

func (u *Ui) PointerMove(x, y float32) {
	guardInput("pointer-move", func() { u.pointerMove(x, y) })
}

func (u *Ui) PointerUp(x, y float32) {
	guardInput("pointer-up", func() { u.pointerUp(x, y) })
}

func (u *Ui) Wheel(x, y, dx, dy float32) {
	guardInput("wheel", func() { u.wheel(x, y, dx, dy) })
}

func (u *Ui) CharInput(r rune) {
	guardInput("char", func() { u.charInput(r) })
}

func (u *Ui) KeyDown(k Key) bool {
	handled := false
	guardInput("key", func() { handled = u.keyDown(k) })
	return handled
}

func (u *Ui) ContextClick(x, y float32) {
	guardInput("context-click", func() { u.contextClick(x, y) })
}

// RightDown starts a right-button gesture: a glass subscribed to right
// gestures under the pointer captures it, reported as true, and the caller
// must then NOT open a context menu. Otherwise false, and right-click keeps its
// context-menu meaning.
func (u *Ui) RightDown(x, y float32) bool {
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

func (u *Ui) AuxDown(button int) {
	u.clearTip()
	if u.Overlay != nil {
		u.CloseOverlay()
		return
	}
	if u.OnAux != nil {
		guardInput("aux-down", func() { u.OnAux(button) })
	}
}

// SetSounds replaces the gesture -> source table.
func (u *Ui) SetSounds(tokens map[string]string) { u.Sounds = tokens }

// Sound plays whatever the table maps token to.
func (u *Ui) Sound(token string) {
	if u.PlaySound == nil || token == "" {
		return
	}
	if src := u.Sounds[token]; src != "" {
		u.PlaySound(src)
	}
}

// RightUp ends a captured right-button gesture.
func (u *Ui) RightUp(x, y float32) {
	guardInput("right-up", func() {
		if u.rightGlass != nil {
			u.rightGlass.RDropAt(x, y)
			u.rightGlass = nil
		}
	})
}

// glassRightAt finds the deepest right-gesture-subscribed glass under the
// pointer. Same shape as glassWheelAt, gated on rpick so inert glass never
// eats the context menu.
func (u *Ui) glassRightAt(w Widget, x, y float32) *Glass {
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

func (u *Ui) pointerDown(x, y float32) {
	// Any press either starts a new selection (selectable targets re-set it in
	// OnPointerDown) or clears the old one.
	u.clearSelection()
	u.clearTip()
	if u.Overlay != nil {
		hit := u.Overlay.HitTest(x, y)
		if hit == nil {
			u.CloseOverlay() // click-away closes and swallows the press
			// The menu bar is the exception: a press there opens the next menu
			// in the same gesture.
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

func (u *Ui) pointerMove(x, y float32) {
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
	// Hover first, cursor second: positional cursors (a table's column
	// dividers) update their state in OnPointerHover.
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

func (u *Ui) pointerUp(x, y float32) {
	if u.active != nil {
		u.active.OnPointerUp(x, y)
		u.active = nil
	}
}

func (u *Ui) wheel(x, y, dx, dy float32) {
	u.clearTip()
	// A modal dialog confines the wheel to itself, just as it does clicks:
	// scrolling the scrim (or anywhere outside the dialog's own scroll) is
	// swallowed rather than leaking to the background.
	var scope Widget = u.Root
	if d := lastDialog(u.Root); d != nil {
		scope = d
	}
	// A subscribed glass under the point owns the wheel, so a camera or zoomable
	// canvas outranks whatever scroll view sits beneath the pane.
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

// tipCandidate tracks the hover target. A widget with a tip shows it after
// ~600ms idle.
func (u *Ui) tipCandidate(hit Widget, x, y float32) {
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

func (u *Ui) clearTip() {
	if u.tipW != nil {
		shown := u.tipShown()
		u.tipW = nil
		if shown {
			u.Invalidate()
		}
	}
}

func (u *Ui) tipShown() bool {
	return u.tipW != nil && !time.Now().Before(u.tipAt)
}

// paintTip paints above the tree and overlay, below the connection banner.
func (u *Ui) paintTip(dl *gfx.DisplayList, vw, vh float32) {
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

// CharInput routes printable text input to the focused widget.
func (u *Ui) charInput(r rune) {
	if u.focused != nil {
		u.focused.OnChar(r)
	}
}

// FocusWidget gives a widget pointer-style focus (no keyboard ring): the
// server-driven focus path.
func (u *Ui) FocusWidget(w Widget) {
	u.setFocus(w, false)
}

// TickIn is how long until the shell should wake for a time-driven repaint:
// the focused text editor's caret blink, or a pending tooltip's show moment.
// Zero means no tick.
func (u *Ui) TickIn() time.Duration {
	var tick time.Duration
	switch tf := u.focused.(type) {
	case *TextField:
		if tf.Focused && tf.selStart == tf.selEnd {
			tick = tf.blinkTick()
		}
	case *TextArea:
		if tf.Focused && tf.selStart == tf.selEnd {
			tick = tf.blinkTick()
		}
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

// NoteTick tells the Ui that the time-driven repaint TickIn asked for has come
// due. The caret's blink phase is the one thing a widget paints from the clock
// rather than from its own state, so no dirty bit sees it coming and its owner
// is marked here. The tooltip and the theme tween need nothing: the tip is
// rebuilt every frame, and a running tween rebuilds the whole list.
func (u *Ui) NoteTick() {
	switch tf := u.focused.(type) {
	case *TextField:
		tf.Invalidate()
	case *TextArea:
		tf.Invalidate()
	}
}

// KeyDown routes a key and reports whether it was consumed.
func (u *Ui) keyDown(k Key) bool {
	if k.Name == "Tab" {
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
	// In-window menu key equivalents fire once the focused widget declines, and
	// before app combos, so text editing keeps its keys. Under NSMenu these
	// chords never reach here.
	if u.Menubar != nil && (k.Meta || k.Ctrl || k.Alt) {
		if id, ok := u.Menubar.Combos()[comboString(k)]; ok && u.Menubar.Perform(id) {
			return true
		}
	}
	// App-registered combos fire next, before the shell built-ins (an app
	// may deliberately claim Cmd+C).
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

func (u *Ui) setFocus(w Widget, viaKeyboard bool) {
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

func (u *Ui) IsFocused(w Widget) bool { return u.focused == w }

// ShowFocusRing is true only for keyboard-driven focus (":focus-visible").
func (u *Ui) ShowFocusRing(w Widget) bool { return u.focused == w && u.focusRing }

func (u *Ui) moveFocus(dir int) {
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
	// Focus is trapped inside the topmost dialog while one is open, so Tab
	// must not wander into the inert background.
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
		if w == u.focused {
			idx = i
			break
		}
	}
	next := order[((idx+dir)%len(order)+len(order))%len(order)]
	u.setFocus(next, true)
	u.RevealWidget(next)
	// Tabbing into a text field selects its content.
	switch tf := next.(type) {
	case *TextField:
		tf.SelectAll()
	case *TextArea:
		tf.SelectAll()
	}
}

// RevealWidget scrolls the enclosing scrollers to make a widget visible
// (Tab focus, and the server's revealSeq command).
func (u *Ui) RevealWidget(w Widget) {
	for p := w.Base().Parent; p != nil; p = p.Base().Parent {
		st, ok := p.(ScrollTarget)
		if !ok || !st.Scrollable() {
			continue
		}
		// Bounds are from the last layout, so adjusting an outer scroller after
		// an inner one uses slightly stale offsets. Fine for focus reveal.
		v := p.Base().Bounds
		b := w.Base().Bounds
		if b.Y < v.Y {
			st.ScrollBy(b.Y - v.Y - 8)
		} else if b.Y+b.H > v.Y+v.H {
			st.ScrollBy(b.Y + b.H - (v.Y + v.H) + 8)
		}
	}
}

// WindowDragAt reports whether a press at (x, y) should move the window rather
// than dispatch as input: the point lies in a windowDrag-marked subtree (the
// app's own titlebar under Options.CustomTitlebar), no interactive widget
// claims it, and no overlay or menubar is in play. The shell owns the actual
// move; this is only the routing decision.
func (u *Ui) WindowDragAt(x, y float32) bool {
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

// dragMarkAt: any windowDrag-marked widget whose bounds contain the point
// counts, so unmarked non-interactive content inside a marked panel drags
// through.
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

// glassWheelAt finds the deepest wheel-subscribed glass under the pointer.
// It cannot ride HitTest: glass hit-interactivity is gated on OnPick so an
// unsubscribed pane never steals presses, and a wheel-only glass (a
// zoomable canvas with no pick) must own the wheel without starting to
// swallow clicks.
func (u *Ui) glassWheelAt(w Widget, x, y float32) *Glass {
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

// scrollTargetAt finds the deepest scrollable under the pointer. Wheel
// events route to it directly (trackpad inertia arrives baked in).
func (u *Ui) scrollTargetAt(w Widget, x, y float32) ScrollTarget {
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

func (u *Ui) selectionFor(l *Label) *TextSelection {
	if u.sel != nil && u.sel.Label == l {
		return u.sel
	}
	return nil
}

func (u *Ui) setSelection(l *Label, start, end int) {
	u.sel = &TextSelection{Label: l, Start: start, End: end}
	u.Invalidate()
}

func (u *Ui) moveSelectionEnd(l *Label, end int) {
	if u.sel != nil && u.sel.Label == l && u.sel.End != end {
		u.sel.End = end
		u.Invalidate()
	}
}

func (u *Ui) clearSelection() {
	if u.sel != nil {
		u.sel = nil
		u.Invalidate()
	}
}

func (u *Ui) SelectedText() string {
	if u.sel == nil {
		return ""
	}
	return u.sel.Label.GlyphText(u.sel.Start, u.sel.End)
}

func hypot32(a, b float32) float32 {
	return float32(math.Hypot(float64(a), float64(b)))
}

// comboString is the canonical combo for a key, cmd+ctrl+alt+shift+<key>,
// matching the server SDK's normalization ("space" for " ", lowercase names).
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
