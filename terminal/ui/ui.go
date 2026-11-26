package ui

import (
	"math"
	"runtime/debug"
	"strings"
	"time"
	"unicode"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/text"

	"github.com/rs/zerolog/log"
)

type TextSelection struct {
	Owner      selectionOwner
	Start, End int
}

type selectionOwner interface {
	Widget
	GlyphText(start, end int) string
}

type UI struct {
	Root    Widget
	Overlay Widget
	Banner  Widget
	Menubar *MenuBar

	Measure        func(f gfx.Font, s string) *text.Run
	OnInvalidate   func()
	SetCursor      func(name string)
	WriteClipboard func(s string)
	SaveFile       func(name string, data []byte)
	ReadClipboard  func() string
	PasteImage     func(x, y float32) bool
	HostText       bool

	hovered        Widget
	active         Widget
	focused        Widget
	focusRing      bool
	windowInactive bool
	sel            *TextSelection
	edgeScroll     *ScrollView
	dragX, dragY   float32
	rightGlass     *Glass
	buttonDown     Widget

	tipW       Widget
	tipX, tipY float32
	tipAt      time.Time

	KeyCombos  map[string]int
	OnKeyCombo func(id int)

	OnAux func(button int)

	Sounds    map[string]string
	PlaySound func(src string)

	tween *themeTween

	structural bool
	animating  bool
	frameAt    time.Time
	inNew      bool
	inNoFade   bool

	motion        Motion
	systemReduced bool

	// GLFW only reports individual presses
	lastDown struct {
		t      time.Time
		x, y   float32
		count  int
		target Widget
	}

	lastW, lastH float32

	prev, reuse *gfx.DisplayList
	full        bool
	reused      int

	layoutAll bool

	dpr          float32
	measureEpoch int

	semSeq int
}

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

func (u *UI) SyncText(value string, selStart, selEnd int, typed bool) {
	switch f := u.focused.(type) {
	case *TextField:
		f.SyncFromHost(value, selStart, selEnd, typed)
	case *TextArea:
		f.SyncFromHost(value, selStart, selEnd, typed)
	}
}

func (u *UI) MeasureEpoch() int { return u.measureEpoch }

func (u *UI) NoteDPR(dpr float32) {
	if dpr > 0 && dpr != u.dpr {
		u.dpr = dpr
		u.measureEpoch++
		u.full = true
	}
}

func (u *UI) Reused() int { return u.reused }

func New() *UI { return &UI{} }

func (u *UI) InvalidateAll() {
	u.full = true
	u.wake()
}

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

func (u *UI) AnimateTheme(tokens map[string]string) {
	if u.reducedMotion() {
		u.tween = nil
		ApplyTheme(tokens)
		u.InvalidateAll()
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
			Theme[k] = &c
		}
	}
	if len(tw.targets) == 0 {
		u.tween = nil
		return
	}
	u.tween = tw
	u.InvalidateAll()
}

func (u *UI) ApplyMetrics(tokens map[string]float64) {
	applyMetricTokens(tokens)
	u.Remeasure()
}

func (u *UI) Remeasure() {
	u.measureEpoch++
	u.InvalidateAll()
}

func (u *UI) NoteStructural() { u.structural = true }

const (
	slideDur = 160 * time.Millisecond
	fadeDur  = 200 * time.Millisecond
)

type Motion uint8

const (
	MotionSystem Motion = iota
	MotionFull
	MotionReduced
)

func (u *UI) SetMotion(m Motion) { u.motion = m }

func (u *UI) SetSystemReducedMotion(on bool) { u.systemReduced = on }

func (u *UI) reducedMotion() bool {
	return u.motion == MotionReduced || u.motion == MotionSystem && u.systemReduced
}

func easeOut(t float32) float32 { return t * (2 - t) }

func (u *UI) animGeometry(dl *gfx.DisplayList) {
	now := time.Now()
	dt := float32(now.Sub(u.frameAt).Seconds())
	if dt <= 0 || dt > 0.05 {
		dt = 0.016
	}
	u.frameAt = now
	u.animating = false
	if u.structural && u.reducedMotion() {
		u.structural = false
	}
	u.animWalk(u.Root, dt)
	u.structural = false
	if u.animating {
		dl.GeomAnimation = true
	}
}

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
		if u.structural && !u.inNew && !u.inNoFade && !b.NoFade {
			b.fadeR = 1
		}
	} else if u.structural && (bb.X != b.prevX || bb.Y != b.prevY) {
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

func (u *UI) GlyphAvailable(f gfx.Font, s string) bool {
	run := u.Measure(f, s)
	return len(run.Glyphs) > 0 && run.Glyphs[0].GID != 0
}

func (u *UI) ViewH() float32 { return u.lastH }

func (u *UI) BuildFrame(dl *gfx.DisplayList, w, h float32) {
	if dl.Measure == nil {
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
		u.Overlay = nil
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
	u.animGeometry(dl)
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

func (u *UI) SetRoot(w Widget) {
	u.setFocus(nil, false)
	u.clearSelection()
	u.clearTip()
	u.hovered = nil
	u.active = nil
	u.Overlay = nil
	u.Root = w
	w.Base().UI = u
	u.InvalidateAll()
}

func (u *UI) OpenOverlay(w Widget) {
	w.Base().UI = u
	u.Overlay = w
	u.InvalidateAll()
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
		u.InvalidateAll()
	}
}

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

type buttonTaker interface {
	OnButton(button int, down bool, x, y float32, k Key) bool
}

func (u *UI) Button(button int, down bool, x, y float32, k Key) bool {
	handled := false
	guardInput("button", func() { handled = u.button(button, down, x, y, k) })
	return handled
}

func (u *UI) button(button int, down bool, x, y float32, k Key) bool {
	if !down {
		w := u.buttonDown
		u.buttonDown = nil
		if bt, ok := w.(buttonTaker); ok {
			bt.OnButton(button, false, x, y, k)
			return true
		}
		return false
	}
	if u.Overlay != nil || lastDialog(u.Root) != nil || u.Root == nil {
		return false
	}
	hit := u.Root.HitTest(x, y)
	bt, ok := hit.(buttonTaker)
	if !ok || !bt.OnButton(button, true, x, y, k) {
		return false
	}
	u.clearTip()
	u.buttonDown = hit
	if hit.Focusable() {
		u.setFocus(hit, false)
	}
	return true
}

func (u *UI) RightUp(x, y float32) {
	guardInput("right-up", func() {
		if u.rightGlass != nil {
			u.rightGlass.RDropAt(x, y)
			u.rightGlass = nil
		}
	})
}

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
	u.clearSelection()
	u.clearTip()
	u.edgeScroll = nil
	if u.Overlay != nil {
		hit := u.Overlay.HitTest(x, y)
		if hit == nil {
			u.CloseOverlay()
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
	if u.buttonDown != nil {
		u.buttonDown.OnPointerDrag(x, y)
		return
	}
	if u.active != nil {
		u.active.OnPointerDrag(x, y)
		u.dragX, u.dragY = x, y
		u.edgeScroll = u.selectionEdgeScroll()
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
	u.edgeScroll = nil
	if u.active != nil {
		u.active.OnPointerUp(x, y)
		u.active = nil
	}
}

func (u *UI) wheel(x, y, dx, dy float32) {
	u.clearTip()
	var scope Widget = u.Root
	if d := lastDialog(u.Root); d != nil {
		scope = d
	}
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

const tipDelay = 600 * time.Millisecond

func (u *UI) tipCandidate(hit Widget, x, y float32) {
	if hit == nil || hit.Base().Tip == "" {
		u.clearTip()
		return
	}
	if u.tipW == hit {
		if !u.tipShown() {
			u.tipX, u.tipY = x, y
		}
		return
	}
	u.clearTip()
	u.tipW = hit
	u.tipX, u.tipY = x, y
	u.tipAt = time.Now().Add(tipDelay)
	u.InvalidateAll()
}

func (u *UI) clearTip() {
	if u.tipW != nil {
		shown := u.tipShown()
		u.tipW = nil
		if shown {
			u.InvalidateAll()
		}
	}
}

func (u *UI) tipShown() bool {
	return u.tipW != nil && !time.Now().Before(u.tipAt)
}

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

func (u *UI) charInput(r rune) {
	if u.OnKeyCombo != nil && u.focusedText() == nil && !u.rawFocused() && u.Overlay == nil && lastDialog(u.Root) == nil {
		combo := string(r)
		if unicode.IsUpper(r) {
			combo = "shift+" + string(unicode.ToLower(r))
		}
		if id, ok := u.KeyCombos[combo]; ok {
			u.OnKeyCombo(id)
			return
		}
	}
	if u.focused != nil {
		u.focused.OnChar(r)
	}
}

func (u *UI) FocusWidget(w Widget) {
	u.setFocus(w, false)
}

func (u *UI) TickIn() time.Duration {
	var tick time.Duration
	if tf := u.focusedText(); tf != nil && tf.Focused && tf.selStart == tf.selEnd {
		tick = tf.blinkTick()
	}
	if tg, ok := u.focused.(*TermGrid); ok {
		if d := tg.blinkTick(); d > 0 && (tick == 0 || d < tick) {
			tick = d
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
	if u.edgeScroll != nil && (tick == 0 || edgeScrollTick < tick) {
		tick = edgeScrollTick
	}
	return tick
}

func (u *UI) NoteTick() {
	if tf := u.focusedText(); tf != nil {
		tf.markDirty()
	}
	if tg, ok := u.focused.(*TermGrid); ok && tg.blinks() {
		tg.markDirty()
	}
	u.stepEdgeScroll()
}

func (u *UI) SetWindowActive(active bool) {
	if u.windowInactive != active {
		return
	}
	u.windowInactive = !active
	if tf := u.focusedText(); tf != nil {
		tf.touch()
		tf.InvalidateWidget()
	}
	if tg, ok := u.focused.(*TermGrid); ok {
		tg.Touch()
		tg.InvalidateWidget()
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

type rawKeyer interface{ RawKeys() bool }

func (u *UI) rawFocused() bool {
	r, ok := u.focused.(rawKeyer)
	return ok && r.RawKeys() && u.Overlay == nil
}

func (u *UI) keyDown(k Key) bool {
	if u.rawFocused() && u.focused.OnKey(k) {
		return true
	}
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
	if tf, ok := u.focused.(*TextField); ok && tf.OnNav != nil && !k.Meta && !k.Ctrl && !k.Alt && !k.Shift {
		switch k.Name {
		case "Up":
			tf.OnNav(-1)
			return true
		case "Down":
			tf.OnNav(1)
			return true
		}
	}
	if u.focused != nil && u.focused.OnKey(k) {
		return true
	}
	// with the macOS menu bar, AppKit handles these key combinations before they reach the window
	if u.Menubar != nil && (k.Meta || k.Ctrl || k.Alt) {
		if id, ok := u.Menubar.Combos()[comboString(k)]; ok && u.Menubar.Perform(id) {
			return true
		}
	}
	bare := !k.Meta && !k.Ctrl && !k.Alt
	if u.OnKeyCombo != nil && (!bare || u.focusedText() == nil && u.Overlay == nil && lastDialog(u.Root) == nil) {
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
		if u.focusedText() != nil {
			u.setFocus(nil, false)
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
	u.InvalidateAll()
}

func (u *UI) IsFocused(w Widget) bool { return u.focused == w }

func (u *UI) ShowFocusRing(w Widget) bool { return u.focused == w && u.focusRing }

func (u *UI) moveFocus(dir int) { u.moveFocusFrom(u.focused, dir) }

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
	switch tf := next.(type) {
	case *TextField:
		tf.SelectAll()
	case *TextArea:
		tf.SelectAll()
	}
}

func (u *UI) RevealWidget(w Widget) {
	for p := w.Base().Parent; p != nil; p = p.Base().Parent {
		st, ok := p.(ScrollTarget)
		if !ok || !st.Scrollable() {
			continue
		}
		v := p.Base().Bounds
		b := w.Base().Bounds
		if b.Y < v.Y {
			st.ScrollBy(b.Y - v.Y - 8)
		} else if b.Y+b.H > v.Y+v.H {
			st.ScrollBy(b.Y + b.H - (v.Y + v.H) + 8)
		}
	}
}

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

// trackpad inertia arrives baked into wheel events
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

func (u *UI) selectionFor(w selectionOwner) *TextSelection {
	if u.sel != nil && u.sel.Owner == w {
		return u.sel
	}
	return nil
}

func (u *UI) setSelection(w selectionOwner, start, end int) {
	u.sel = &TextSelection{Owner: w, Start: start, End: end}
	u.InvalidateAll()
}

func (u *UI) moveSelectionEnd(w selectionOwner, end int) {
	if u.sel != nil && u.sel.Owner == w && u.sel.End != end {
		u.sel.End = end
		u.InvalidateAll()
	}
}

const edgeScrollTick = 16 * time.Millisecond

func (u *UI) selectionEdgeScroll() *ScrollView {
	if u.sel == nil || u.sel.Owner != u.active {
		return nil
	}
	for w := u.active.Base().Parent; w != nil; w = w.Base().Parent {
		sv, ok := w.(*ScrollView)
		if !ok || !sv.Scrollable() {
			continue
		}
		if b := sv.Bounds; u.dragY < b.Y || u.dragY > b.Y+b.H {
			return sv
		}
		return nil
	}
	return nil
}

func (u *UI) stepEdgeScroll() {
	sv := u.edgeScroll
	if sv == nil || u.active == nil {
		return
	}
	past := u.dragY - (sv.Bounds.Y + sv.Bounds.H)
	if u.dragY < sv.Bounds.Y {
		past = u.dragY - sv.Bounds.Y
	}
	step := min(max(abs32(past), 8), 80) / 4
	if past < 0 {
		step = -step
	}
	before := sv.ScrollY
	sv.ScrollBy(step)
	if moved := sv.ScrollY - before; moved != 0 {
		u.active.OnPointerDrag(u.dragX, u.dragY+moved)
	}
}

func (u *UI) clearSelection() {
	if u.sel != nil {
		u.sel = nil
		u.InvalidateAll()
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
