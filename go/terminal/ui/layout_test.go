package ui

import (
	"fmt"
	"time"

	"testing"

	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/text"
)

// Layout math is pure: a Ui with a fixed-advance fake measure (7px per rune,
// ascent 10, descent 3) exercises everything - anchors, docks, stacks,
// height-for-width - with no GL, no fonts, no window.
func fakeUi() *Ui {
	u := New()
	u.Measure = func(_ gfx.Font, s string) *text.Run {
		runes := []rune(s)
		glyphs := make([]text.Placed, len(runes))
		for i := range runes {
			glyphs[i] = text.Placed{GID: 1, X: float32(i) * 7, Cluster: i}
		}
		return &text.Run{Glyphs: glyphs, Width: float32(len(runes)) * 7, Ascent: 10, Descent: 3}
	}
	return u
}

func layout(u *Ui, root Widget, w, h float32) {
	root.Base().UI = u
	root.Base().Bounds = gfx.R(0, 0, w, h)
	root.LayoutChildren()
}

func wantBounds(t *testing.T, w Widget, x, y, wd, ht float32) {
	t.Helper()
	b := w.Base().Bounds
	if b.X != x || b.Y != y || b.W != wd || b.H != ht {
		t.Fatalf("bounds = (%g,%g %gx%g); want (%g,%g %gx%g)", b.X, b.Y, b.W, b.H, x, y, wd, ht)
	}
}

func TestAnchorsPinBothEdgesStretch(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	c := NewPanel()
	root.Add(c)
	c.Anchors = &Anchors{Left: f32p(10), Right: f32p(20), Top: f32p(5), Bottom: f32p(5)}
	layout(u, root, 200, 100)
	wantBounds(t, c, 10, 5, 170, 90)
}

func TestAnchorsOneSidedPinKeepsSizeAndClamps(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	c := NewPanel()
	root.Add(c)
	c.Width, c.Height = f32p(50), f32p(20)
	c.Anchors = &Anchors{Right: f32p(10), Bottom: f32p(10)}
	layout(u, root, 200, 100)
	wantBounds(t, c, 140, 70, 50, 20)

	// A child wider than its parent clamps instead of overflowing.
	c.Width = f32p(500)
	layout(u, root, 200, 100)
	wantBounds(t, c, 0, 70, 190, 20)
}

func TestAnchorsCenter(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	c := NewPanel()
	root.Add(c)
	c.Width, c.Height = f32p(40), f32p(20)
	c.Anchors = &Anchors{CenterX: f32p(0), CenterY: f32p(-10)}
	layout(u, root, 200, 100)
	wantBounds(t, c, 80, 30, 40, 20)
}

func TestDockConsumesInDeclarationOrder(t *testing.T) {
	u := fakeUi()
	d := NewDock()
	top := NewPanel()
	top.Height = f32p(30)
	top.Dock = "top"
	left := NewPanel()
	left.Width = f32p(50)
	left.Dock = "left"
	fill := NewPanel()
	fill.Dock = "fill"
	d.Add(top)
	d.Add(left)
	d.Add(fill)
	layout(u, d, 200, 100)
	wantBounds(t, top, 0, 0, 200, 30)
	wantBounds(t, left, 0, 30, 50, 70)
	wantBounds(t, fill, 50, 30, 150, 70)
}

func TestStackFixedAndFillWeights(t *testing.T) {
	u := fakeUi()
	s := NewVStack()
	s.Spacing = 0
	s.Align = "stretch"
	a := NewPanel()
	a.StackSize = StackSize{Kind: "fixed", Px: 20}
	b := NewPanel()
	b.StackSize = StackSize{Kind: "fill", Weight: 1}
	c := NewPanel()
	c.StackSize = StackSize{Kind: "fill", Weight: 3}
	s.Add(a)
	s.Add(b)
	s.Add(c)
	layout(u, s, 100, 100)
	wantBounds(t, a, 0, 0, 100, 20)
	wantBounds(t, b, 0, 20, 100, 20)
	wantBounds(t, c, 0, 40, 100, 60)
}

// The wrapped-label fixture: "aaaa " x10 at 7px/rune, maxW 100 -> 14-rune
// lines ("aaaa aaaa aaaa"), 4 lines. lineH = 13, advance = 16 -> height 61.
func wrapFixtureLabel() *Label {
	textStr := "aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa aaaa"
	l := NewLabel(textStr, gfx.NewFont(13, gfx.FontOpts{}), nil)
	l.Wrap = true
	return l
}

func TestWrappedLabelHeightForWidth(t *testing.T) {
	u := fakeUi()
	l := wrapFixtureLabel()
	l.UI = u
	h, ok := l.HeightForWidth(100)
	if !ok {
		t.Fatal("wrap label must report height-for-width")
	}
	if h != 61 {
		t.Fatalf("heightForWidth(100) = %g; want 61", h)
	}
}

func TestAnchoredWrappedLabelTakesItsTrueHeight(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	l := wrapFixtureLabel()
	root.Add(l)
	l.Anchors = &Anchors{Left: f32p(0), Right: f32p(0), Top: f32p(0)}
	layout(u, root, 100, 400)
	wantBounds(t, l, 0, 0, 100, 61)
}

func TestDockedWrappedLabelSizesTheDockSlot(t *testing.T) {
	u := fakeUi()
	d := NewDock()
	l := wrapFixtureLabel()
	l.Dock = "top"
	fill := NewPanel()
	fill.Dock = "fill"
	d.Add(l)
	d.Add(fill)
	layout(u, d, 100, 400)
	wantBounds(t, l, 0, 0, 100, 61)
	wantBounds(t, fill, 0, 61, 100, 339)
}

func TestTabTrapsInsideTopmostDialog(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	bg := NewButton("background")
	root.Add(bg)
	dlg := NewDialog()
	inA := NewButton("in-a")
	inB := NewButton("in-b")
	dlg.Add(inA)
	dlg.Add(inB)
	root.Add(dlg)
	u.SetRoot(root)
	layout(u, root, 400, 300)

	u.moveFocus(1)
	if u.focused != Widget(inA) {
		t.Fatalf("first Tab focused %T; want the dialog's first button", u.focused)
	}
	u.moveFocus(1)
	if u.focused != Widget(inB) {
		t.Fatalf("second Tab focused %T; want the dialog's second button", u.focused)
	}
	u.moveFocus(1)
	if u.focused != Widget(inA) {
		t.Fatal("third Tab must wrap inside the dialog, not reach the background")
	}
}

func TestTabScrollsFocusIntoView(t *testing.T) {
	u := fakeUi()
	sv := NewScrollView()
	one := NewButton("one")
	one.Anchors = &Anchors{Left: f32p(0), Top: f32p(0)}
	two := NewButton("two")
	two.Anchors = &Anchors{Left: f32p(0), Top: f32p(500)}
	sv.Add(one)
	sv.Add(two)
	u.SetRoot(sv)
	layout(u, sv, 200, 100)

	u.moveFocus(1) // "one" is already visible
	if sv.ScrollY != 0 {
		t.Fatalf("focusing a visible widget scrolled to %g", sv.ScrollY)
	}
	u.moveFocus(1) // "two" sits at y=500 in a 100-tall viewport
	// The reveal asks for an 8px margin past the button, but the scroller
	// clamps to its content end: 500+32 content - 100 viewport = 432.
	want := float32(432)
	if sv.ScrollY != want {
		t.Fatalf("scrollY = %g; want %g", sv.ScrollY, want)
	}
}

func TestThemeTweenFadesToTarget(t *testing.T) {
	u := fakeUi()
	orig := *Theme["accent"]
	defer func() { *Theme["accent"] = orig }()
	*Theme["accent"] = gfx.Hex("#000000")

	u.AnimateTheme(map[string]string{"accent": "#ffffff"})
	var dl gfx.DisplayList
	u.BuildFrame(&dl, 100, 100)
	if Theme["accent"].R > 0.5 {
		t.Fatalf("tween jumped straight to the target: %v", *Theme["accent"])
	}
	if u.TickIn() == 0 {
		t.Fatal("an active tween must keep the shell awake")
	}

	time.Sleep(220 * time.Millisecond) // past the 180ms tween
	u.BuildFrame(&dl, 100, 100)
	if c := *Theme["accent"]; c.R != 1 || c.G != 1 || c.B != 1 {
		t.Fatalf("tween did not land on target: %v", c)
	}
	if u.tween != nil {
		t.Fatal("a finished tween must clear")
	}
}

func TestRowCacheEvictsFarRows(t *testing.T) {
	u := fakeUi()
	tv := NewTableView()
	tv.RowCount = 10000
	tv.UI = u
	tv.Bounds = gfx.R(0, 0, 400, 300)
	// Scroll deep, then feed windows all along the way, as a long scroll
	// session would.
	tv.ScrollY = 5000 * tv.RowHeight
	for start := 0; start < 6000; start += 100 {
		rows := make([][]string, 100)
		for i := range rows {
			rows[i] = []string{"cell"}
		}
		tv.ApplyRows(start, rows, nil, false)
	}
	if len(tv.cache) > 2100 {
		t.Fatalf("cache holds %d rows; want it bounded near 2000", len(tv.cache))
	}
	// The viewport's rows must survive eviction.
	if _, ok := tv.cache[5000]; !ok {
		t.Fatal("eviction dropped a row inside the viewport")
	}
}

func TestRequestRevealScrollsAtNextPaint(t *testing.T) {
	u := fakeUi()
	sv := NewScrollView()
	far := NewButton("far")
	far.Anchors = &Anchors{Left: f32p(0), Top: f32p(900)}
	sv.Add(far)
	u.SetRoot(sv)
	layout(u, sv, 200, 100)

	far.RequestReveal()
	var dl gfx.DisplayList
	u.BuildFrame(&dl, 200, 100)
	if sv.ScrollY == 0 {
		t.Fatal("RequestReveal did not scroll the enclosing scroller at paint")
	}
}

func TestRequestFocusLandsAtNextPaint(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	b := NewButton("go")
	root.Add(b)
	u.SetRoot(root)
	layout(u, root, 200, 100)

	b.RequestFocus()
	var dl gfx.DisplayList
	u.BuildFrame(&dl, 200, 100)
	if u.focused != Widget(b) {
		t.Fatalf("RequestFocus focused %T; want the button", u.focused)
	}
}

func TestVStackWrappedLabelMainAxisFollowsWrap(t *testing.T) {
	u := fakeUi()
	s := NewVStack()
	s.Spacing = 0
	s.Align = "stretch"
	l := wrapFixtureLabel()
	b := NewPanel()
	b.StackSize = StackSize{Kind: "fixed", Px: 10}
	s.Add(l)
	s.Add(b)
	layout(u, s, 100, 400)
	wantBounds(t, l, 0, 0, 100, 61)
	wantBounds(t, b, 0, 61, 100, 10)
}

func TestGeometricAnimationSlidesOnStructuralReflow(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	c := NewPanel()
	root.Add(c)
	c.Frame = &gfx.Rect{X: 0, Y: 0, W: 100, H: 30}

	// Frame 1: first sight - records prev, starts nothing.
	layout(u, root, 200, 400)
	u.animWalk(root, 0.016)
	wantBounds(t, c, 0, 0, 100, 30)

	// A structural frame moves it: the painted position starts near the old
	// spot (ease-out travels ~19% in the first 16ms step) and converges.
	c.Frame = &gfx.Rect{X: 0, Y: 100, W: 100, H: 30}
	layout(u, root, 200, 400)
	u.structural = true
	u.animWalk(root, 0.016)
	u.structural = false
	if y := c.Base().Bounds.Y; y < 0 || y >= 100 {
		t.Fatalf("mid-slide y = %g; want in [0, 100)", y)
	}

	prev := c.Base().Bounds.Y
	for i := 0; i < 20; i++ {
		layout(u, root, 200, 400)
		u.animWalk(root, 0.02)
		y := c.Base().Bounds.Y
		if y < prev {
			t.Fatalf("slide reversed: %g after %g", y, prev)
		}
		prev = y
	}
	wantBounds(t, c, 0, 100, 100, 30)

	// A non-structural move (scroll, splitter drag, resize) never animates.
	c.Frame = &gfx.Rect{X: 50, Y: 100, W: 100, H: 30}
	layout(u, root, 200, 400)
	u.animWalk(root, 0.016)
	wantBounds(t, c, 50, 100, 100, 30)
}

func TestGeometricAnimationFadesStructuralInsertsOnly(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	a := NewPanel()
	root.Add(a)
	layout(u, root, 200, 100)
	u.animWalk(root, 0.016) // mount frame is not structural - no fade
	if fr := a.Base().fadeR; fr != 0 {
		t.Fatalf("mount-frame widget got fadeR %g; want 0", fr)
	}

	inserted := NewPanel()
	root.Add(inserted)
	layout(u, root, 200, 100)
	u.structural = true
	u.animWalk(root, 0.016)
	u.structural = false
	if fr := inserted.Base().fadeR; fr <= 0 || fr >= 1 {
		t.Fatalf("inserted widget fadeR = %g; want mid-fade in (0,1)", fr)
	}
	for i := 0; i < 20; i++ {
		layout(u, root, 200, 100)
		u.animWalk(root, 0.02)
	}
	if fr := inserted.Base().fadeR; fr != 0 {
		t.Fatalf("fade never completed: fadeR = %g", fr)
	}
}

func TestTreeKeyboardTogglesAndJumpsToParent(t *testing.T) {
	tv := NewTableView()
	tv.Tree = true
	tv.RowCount = 3
	tv.UI = fakeUi()
	tv.Bounds = gfx.R(0, 0, 300, 200)
	tv.ApplyRows(0, [][]string{{"a"}, {"a1"}, {"b"}}, []RowMeta{
		{Key: "a", Depth: 0, Expandable: true, Expanded: true},
		{Key: "a1", Depth: 1},
		{Key: "b", Depth: 0},
	}, false)
	var toggled []string
	tv.OnToggle = func(_ int, key string) { toggled = append(toggled, key) }

	// Left on a child jumps to its parent without toggling anything.
	tv.cursorRow = 1
	if !tv.OnKey(Key{Name: "Left"}) {
		t.Fatal("Left on a child not handled")
	}
	if tv.cursorRow != 0 || len(toggled) != 0 {
		t.Fatalf("cursor %d toggled %v; want parent 0, no toggles", tv.cursorRow, toggled)
	}

	// Left on the open parent collapses it (server round-trips the rest).
	if !tv.OnKey(Key{Name: "Left"}) {
		t.Fatal("Left on open parent not handled")
	}
	if len(toggled) != 1 || toggled[0] != "a" {
		t.Fatalf("toggled = %v; want [a]", toggled)
	}

	// Right on a collapsed expandable expands it.
	tv.meta[0] = RowMeta{Key: "a", Depth: 0, Expandable: true, Expanded: false}
	if !tv.OnKey(Key{Name: "Right"}) {
		t.Fatal("Right on collapsed parent not handled")
	}
	if len(toggled) != 2 || toggled[1] != "a" {
		t.Fatalf("toggled = %v; want [a a]", toggled)
	}
}

func TestCellWidgetColumnsActivateInsteadOfSelecting(t *testing.T) {
	tv := NewTableView()
	tv.UI = fakeUi()
	tv.RowCount = 2
	tv.Bounds = gfx.R(0, 0, 300, 200)
	tv.Columns = []TableColumn{
		{Key: "name", Title: "N", Weight: 1},
		{Key: "ok", Title: "", Width: 60, Kind: "checkbox"},
		{Key: "act", Title: "", Width: 80, Kind: "button"},
	}
	tv.ApplyRows(0, [][]string{{"a", "true", "Go"}, {"b", "false", "Go"}}, nil, false)
	var got []string
	tv.OnCellActivate = func(row int, _, col, value string) {
		got = append(got, fmt.Sprintf("%d/%s/%s", row, col, value))
	}
	tv.OnRowSelect = func(row int, _ string) { t.Fatalf("cell click selected row %d", row) }

	// Fixed cols take 140, the weighted name column gets 160: checkbox cells
	// span x 160..220, button cells 220..300. Row 0 is y 32..60.
	tv.OnPointerDown(180, 46, 1) // checkbox: flips true -> false, echoes locally
	tv.OnPointerDown(250, 46, 1) // button
	want := []string{"0/ok/false", "0/act/"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("cell activations = %v; want %v", got, want)
	}
	if cell := tv.cache[0][1]; cell != "false" {
		t.Fatalf("checkbox local echo = %q; want false", cell)
	}

	// A text-column click still selects.
	selected := -1
	tv.OnRowSelect = func(row int, _ string) { selected = row }
	tv.OnPointerDown(80, 46, 1)
	if selected != 0 {
		t.Fatalf("text-cell click selected %d; want 0", selected)
	}
}
