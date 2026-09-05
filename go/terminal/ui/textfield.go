package ui

import (
	"strings"
	"time"
	"unicode"

	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/text"
)

type TextField struct {
	Core
	Value       string
	Placeholder string
	// Radius overrides the radius.control metric for this field (nil = the
	// token): pill search boxes and other chrome-strip shapes.
	Radius *float32
	// FontSize in logical px (`size` prop) and family (`mono` prop).
	FontSize float32
	Mono     bool
	// Focused mirrors Ui focus so the protocol layer can honor local echo
	// (a focused field ignores server value sets).
	Focused bool

	// OnInput fires on every edit (wire-level debouncing happens in the
	// protocol layer), and OnCommit on Enter/blur, only when changed.
	OnInput  func(v string)
	OnCommit func(v string)

	// Last-seen values of the server's one-shot command props (FocusSeq
	// lives on Core, since it's universal).
	ResetSeq    float32
	OverrideSeq float32

	// selEnd is the caret, and selStart pins the other end of the selection.
	selStart, selEnd int
	anchor           int
	scrollX          float32
	lastCommitted    string
	// lastActivity anchors the blink phase: the caret is always solid right
	// after it moves, which needs no per-field timer.
	lastActivity time.Time

	// Snapshot undo: each entry is the state before a mutating edit. Runs of
	// single-rune typing coalesce into one entry, like every editor.
	undo, redo []tfSnapshot
	lastTyping time.Time
}

type tfSnapshot struct {
	value            string
	selStart, selEnd int
}

const tfUndoCap = 200

const tfBlink = 530 * time.Millisecond

// tfPadX is the field's inner text inset: the space.pad token.
func tfPadX() float32 { return Metrics.SpacePad }

func NewTextField() *TextField {
	t := &TextField{FontSize: 14}
	t.self = t
	return t
}

func (t *TextField) font() gfx.Font {
	return gfx.NewFont(t.FontSize, gfx.FontOpts{Mono: t.Mono})
}

func (t *TextField) IntrinsicSize() Size {
	h := max(Metrics.ControlHeight, float32(int(t.FontSize*1.7+0.5)))
	return Size{220, h}
}

func (t *TextField) Interactive() bool { return true }
func (t *TextField) Focusable() bool   { return true }
func (t *TextField) Cursor() string    { return "text" }

func (t *TextField) touch() { t.lastActivity = time.Now() }

func (t *TextField) selRange() (int, int) {
	if t.selStart <= t.selEnd {
		return t.selStart, t.selEnd
	}
	return t.selEnd, t.selStart
}

// Commit fires OnCommit if the value changed since focus/last commit.
func (t *TextField) Commit() {
	if t.Value == t.lastCommitted {
		return
	}
	t.lastCommitted = t.Value
	if t.OnCommit != nil {
		t.OnCommit(t.Value)
	}
}

// ForceClear is the server-forced clear (`ClearValue` in the SDK). Unlike a
// plain value set it applies even while focused, for submit-and-keep-typing
// flows.
func (t *TextField) ForceClear() {
	t.Value = ""
	t.lastCommitted = ""
	t.selStart, t.selEnd, t.anchor = 0, 0, 0
	t.scrollX = 0
	t.undo, t.redo = nil, nil // a server-forced transition starts fresh history
	t.lastTyping = time.Time{}
	t.touch()
	t.Invalidate()
}

// ForceValue is the server-forced value (`SetValueNow` in the SDK). It applies
// even while focused, for validation and formatting. The pushed value becomes
// the new baseline, so Escape reverts to it and no commit fires unless the user
// edits again. History restarts from it.
func (t *TextField) ForceValue(v string) {
	t.Value = v
	t.lastCommitted = v
	n := len([]rune(v))
	t.selStart, t.selEnd, t.anchor = n, n, n
	t.undo, t.redo = nil, nil
	t.lastTyping = time.Time{}
	t.touch()
	t.Invalidate()
}

func (t *TextField) SelectAll() {
	t.selStart, t.anchor = 0, 0
	t.selEnd = len([]rune(t.Value))
	t.touch()
	t.Invalidate()
}

func (t *TextField) OnFocusChange(focused bool) {
	t.Focused = focused
	if focused {
		t.lastCommitted = t.Value
		t.touch()
	} else {
		t.Commit()
	}
	t.Invalidate()
}

// -- editing operations ---------------------------------------------------------

// edit replaces rune range [s, e) with insert and puts the caret after it.
func (t *TextField) edit(s, e int, insert string) {
	t.editKind(s, e, insert, false)
}

// editKind is edit with a coalescing hint: consecutive typing edits share
// one undo entry.
func (t *TextField) editKind(s, e int, insert string, typing bool) {
	rs := []rune(t.Value)
	s = clampInt(s, 0, len(rs))
	e = clampInt(e, s, len(rs))
	next := string(rs[:s]) + insert + string(rs[e:])
	caret := s + len([]rune(insert))
	changed := next != t.Value
	if changed {
		t.pushUndo(typing)
	}
	t.Value = next
	t.selStart, t.selEnd, t.anchor = caret, caret, caret
	t.touch()
	if changed && t.OnInput != nil {
		t.OnInput(next)
	}
	t.Invalidate()
}

// -- undo ---------------------------------------------------------------------

func (t *TextField) pushUndo(typing bool) {
	now := time.Now()
	coalesce := typing && !t.lastTyping.IsZero() && now.Sub(t.lastTyping) < time.Second && len(t.undo) > 0
	if typing {
		t.lastTyping = now
	} else {
		t.lastTyping = time.Time{}
	}
	t.redo = nil // a fresh edit invalidates the redo branch
	if coalesce {
		return // the run's first snapshot already holds the pre-typing state
	}
	t.undo = append(t.undo, tfSnapshot{t.Value, t.selStart, t.selEnd})
	if len(t.undo) > tfUndoCap {
		t.undo = t.undo[len(t.undo)-tfUndoCap:]
	}
}

func (t *TextField) restore(s tfSnapshot) {
	changed := s.value != t.Value
	t.Value = s.value
	t.selStart, t.selEnd, t.anchor = s.selStart, s.selEnd, s.selEnd
	t.lastTyping = time.Time{}
	t.touch()
	if changed && t.OnInput != nil {
		t.OnInput(t.Value)
	}
	t.Invalidate()
}

func (t *TextField) undoEdit() {
	if len(t.undo) == 0 {
		return
	}
	top := t.undo[len(t.undo)-1]
	t.undo = t.undo[:len(t.undo)-1]
	t.redo = append(t.redo, tfSnapshot{t.Value, t.selStart, t.selEnd})
	t.restore(top)
}

func (t *TextField) redoEdit() {
	if len(t.redo) == 0 {
		return
	}
	top := t.redo[len(t.redo)-1]
	t.redo = t.redo[:len(t.redo)-1]
	t.undo = append(t.undo, tfSnapshot{t.Value, t.selStart, t.selEnd})
	t.restore(top)
}

// revert is escape-to-revert: back to the value the field had at focus (or
// last commit). Reported so the caller can decide whether Escape was
// consumed. A clean field lets it bubble (dialogs, selection clearing).
func (t *TextField) revert() bool {
	if t.Value == t.lastCommitted {
		return false
	}
	t.pushUndo(false) // revert itself is undoable
	t.Value = t.lastCommitted
	n := len([]rune(t.Value))
	t.selStart, t.selEnd, t.anchor = n, n, n
	t.touch()
	if t.OnInput != nil {
		t.OnInput(t.Value)
	}
	t.Invalidate()
	return true
}

func (t *TextField) moveCaret(to int, extend bool) {
	to = clampInt(to, 0, len([]rune(t.Value)))
	if extend {
		t.selEnd = to
	} else {
		t.selStart, t.selEnd, t.anchor = to, to, to
	}
	t.touch()
	t.Invalidate()
}

func isWordRuneTF(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_'
}

func (t *TextField) wordLeft(from int) int {
	rs := []rune(t.Value)
	i := clampInt(from, 0, len(rs))
	for i > 0 && !isWordRuneTF(rs[i-1]) {
		i--
	}
	for i > 0 && isWordRuneTF(rs[i-1]) {
		i--
	}
	return i
}

func (t *TextField) wordRight(from int) int {
	rs := []rune(t.Value)
	i := clampInt(from, 0, len(rs))
	for i < len(rs) && !isWordRuneTF(rs[i]) {
		i++
	}
	for i < len(rs) && isWordRuneTF(rs[i]) {
		i++
	}
	return i
}

func (t *TextField) wordRangeAt(i int) (int, int) {
	rs := []rune(t.Value)
	if len(rs) == 0 {
		return 0, 0
	}
	i = clampInt(i, 0, len(rs)-1)
	target := isWordRuneTF(rs[i])
	s, e := i, i+1
	for s > 0 && isWordRuneTF(rs[s-1]) == target {
		s--
	}
	for e < len(rs) && isWordRuneTF(rs[e]) == target {
		e++
	}
	return s, e
}

// OnChar inserts printable input (the shell routes GLFW's char callback
// here). Control runes are dropped, and shortcuts arrive via OnKey instead.
func (t *TextField) OnChar(r rune) bool {
	if r < 0x20 || r == 0x7f {
		return false
	}
	t.sound("type")
	s, e := t.selRange()
	t.editKind(s, e, string(r), true)
	return true
}

func (t *TextField) OnKey(k Key) bool {
	n := len([]rune(t.Value))
	s, e := t.selRange()
	cmdish := k.Meta || k.Ctrl
	switch k.Name {
	case "Enter":
		t.Commit()
		return true
	case "Backspace":
		switch {
		case s != e:
			t.edit(s, e, "")
		case k.Meta:
			t.edit(0, t.selEnd, "") // Cmd+Backspace deletes to line start
		case k.Alt:
			t.edit(t.wordLeft(t.selEnd), t.selEnd, "") // Alt+Backspace deletes a word
		case t.selEnd > 0:
			t.edit(t.selEnd-1, t.selEnd, "")
		}
		return true
	case "Delete":
		if s != e {
			t.edit(s, e, "")
		} else if t.selEnd < n {
			t.edit(t.selEnd, t.selEnd+1, "")
		}
		return true
	case "Left":
		to := t.selEnd - 1
		switch {
		case k.Meta:
			to = 0
		case k.Alt:
			to = t.wordLeft(t.selEnd)
		case s != e && !k.Shift:
			to = s // a plain arrow collapses the selection to its edge
		}
		t.moveCaret(to, k.Shift)
		return true
	case "Right":
		to := t.selEnd + 1
		switch {
		case k.Meta:
			to = n
		case k.Alt:
			to = t.wordRight(t.selEnd)
		case s != e && !k.Shift:
			to = e
		}
		t.moveCaret(to, k.Shift)
		return true
	case "Up", "Home":
		t.moveCaret(0, k.Shift)
		return true
	case "Down", "End":
		t.moveCaret(n, k.Shift)
		return true
	case "a":
		if cmdish {
			t.SelectAll()
			return true
		}
	case "c":
		if cmdish && s != e {
			t.writeClipboard(s, e)
			return true
		}
	case "x":
		if cmdish {
			if s != e {
				t.writeClipboard(s, e)
				t.edit(s, e, "")
			}
			return true
		}
	case "v":
		if cmdish {
			t.paste()
			return true
		}
	case "z":
		if cmdish {
			if k.Shift {
				t.redoEdit()
			} else {
				t.undoEdit()
			}
			return true
		}
	case "Escape":
		return t.revert() // clean field: let Escape bubble (dialogs, selection)
	}
	return false
}

func (t *TextField) writeClipboard(s, e int) {
	if t.UI.WriteClipboard == nil {
		return
	}
	rs := []rune(t.Value)
	t.UI.WriteClipboard(string(rs[s:e]))
}

func (t *TextField) paste() {
	if t.UI.ReadClipboard == nil {
		return
	}
	// Single-line field: newlines flatten to spaces, other control runes drop.
	clip := strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, t.UI.ReadClipboard())
	if clip == "" {
		return
	}
	s, e := t.selRange()
	t.edit(s, e, clip)
}

// -- geometry ---------------------------------------------------------------------

// runeBoundaryXs maps every rune boundary of a shaped run (runeCount runes)
// to an x offset from the run origin, via shaping clusters (runes swallowed
// into a cluster sit at its start).
func runeBoundaryXs(run *text.Run, runeCount int) []float32 {
	xs := make([]float32, runeCount+1)
	seen := make([]bool, runeCount+1)
	for _, g := range run.Glyphs {
		if g.Cluster >= 0 && g.Cluster <= runeCount && !seen[g.Cluster] {
			xs[g.Cluster] = g.X
			seen[g.Cluster] = true
		}
	}
	xs[runeCount] = run.Width
	for i := 1; i < runeCount; i++ {
		if !seen[i] {
			xs[i] = xs[i-1]
		}
	}
	return xs
}

// boundaryXs maps every rune boundary of the value to an x offset from the
// text origin.
func (t *TextField) boundaryXs() []float32 {
	run := t.UI.Measure(t.font(), t.Value)
	return runeBoundaryXs(run, len([]rune(t.Value)))
}

func (t *TextField) xAt(xs []float32, i int) float32 {
	return xs[clampInt(i, 0, len(xs)-1)]
}

// runeAtX is the nearest rune boundary to an x offset from the text origin.
func (t *TextField) runeAtX(xs []float32, x float32) int {
	best, bestD := 0, float32(1e30)
	for i, bx := range xs {
		if d := abs32(x - bx); d < bestD {
			bestD, best = d, i
		}
	}
	return best
}

// -- painting -----------------------------------------------------------------------

func (t *TextField) PaintSelf(dl *gfx.DisplayList) {
	b := t.Bounds
	if t.UI.ShowFocusRing(t) {
		PaintFocusRing(dl, b, radiusOr(t.Radius, Metrics.RadiusControl))
	}
	border := Theme["controlEdge"]
	bw := Metrics.BorderWidth
	if t.Focused {
		border = Theme["accent"]
		bw = Metrics.BorderWidth * 1.5
	}
	dl.Rect(b, *Theme["panelInset"], gfx.RectOpts{
		Radius: gfx.CornerRadius(radiusOr(t.Radius, Metrics.RadiusControl)), BorderWidth: bw, BorderColor: border,
	})

	run := t.UI.Measure(t.font(), t.Value)
	xs := t.boundaryXs()
	innerW := b.W - tfPadX()*2
	textH := run.Ascent + run.Descent
	textY := b.Y + (b.H-textH)/2

	// Keep the caret inside the viewport by sliding the text.
	if t.Focused {
		caretX := t.xAt(xs, t.selEnd)
		if caretX-t.scrollX > innerW {
			t.scrollX = caretX - innerW
		}
		if caretX-t.scrollX < 0 {
			t.scrollX = caretX
		}
	}
	textX := b.X + tfPadX() - t.scrollX

	dl.PushClip(gfx.Inset(b, 1.5))
	if t.Value == "" && t.Placeholder != "" {
		dl.Text(t.Placeholder, b.X+tfPadX(), textY, t.font(), *Theme["inkFaint"])
	}
	s, e := t.selRange()
	if t.Focused && s != e {
		x0 := t.xAt(xs, s)
		x1 := t.xAt(xs, e)
		dl.Fill(gfx.R(textX+x0-1, textY-1, x1-x0+2, textH+2),
			gfx.WithAlpha(*Theme["accent"], 0.35), gfx.CornerRadius(2))
	}
	dl.Text(t.Value, textX, textY, t.font(), *Theme["ink"])
	if t.Focused && s == e && t.blinkOn() {
		dl.Fill(gfx.R(textX+t.xAt(xs, t.selEnd), textY-1, 1.5, textH+2),
			*Theme["accent"], gfx.Corners{})
	}
	dl.PopClip()
}

func (t *TextField) blinkOn() bool {
	return (time.Since(t.lastActivity)/tfBlink)%2 == 0
}

// blinkTick is how long until the caret phase next flips (the shell's wait
// timeout while a collapsed-caret field is focused).
func (t *TextField) blinkTick() time.Duration {
	return tfBlink - time.Since(t.lastActivity)%tfBlink
}

// -- mouse --------------------------------------------------------------------------

func (t *TextField) OnPointerDown(x, _ float32, detail int) {
	xs := t.boundaryXs()
	i := t.runeAtX(xs, x-(t.Bounds.X+tfPadX())+t.scrollX)
	switch {
	case detail >= 3:
		t.SelectAll()
	case detail == 2:
		s, e := t.wordRangeAt(min(i, max(0, len([]rune(t.Value))-1)))
		t.selStart, t.selEnd, t.anchor = s, e, s
		t.touch()
		t.Invalidate()
	default:
		t.selStart, t.selEnd, t.anchor = i, i, i
		t.touch()
		t.Invalidate()
	}
}

func (t *TextField) OnPointerDrag(x, _ float32) {
	xs := t.boundaryXs()
	i := t.runeAtX(xs, x-(t.Bounds.X+tfPadX())+t.scrollX)
	if t.selStart != t.anchor || t.selEnd != i {
		t.selStart, t.selEnd = t.anchor, i
		t.touch()
		t.Invalidate()
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
