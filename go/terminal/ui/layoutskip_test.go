package ui

import (
	"fmt"
	"testing"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

// The layout pass skips subtrees whose geometry cannot have moved (see
// layoutSubtree). The failure mode is stale bounds behind pixels that still
// look right, so the tests here drive the same scene twice, once with the skip
// and once with every frame laid out in full, and compare every widget's
// bounds after each step.

// parityScene is one of every container the layout pass walks, plus a wrapped
// label whose height depends on the width it is handed.
type parityScene struct {
	root    *Panel
	dock    *Dock
	split   *SplitView
	scroll  *ScrollView
	vstack  *Stack
	grid    *Grid
	table   *TableView
	dialog  *Dialog
	wrapped *Label
	cell    *Label
	pinned  *Panel
}

func newParityScene() *parityScene {
	fill := func() *Anchors {
		return &Anchors{Left: f32p(0), Right: f32p(0), Top: f32p(0), Bottom: f32p(0)}
	}
	font := gfx.NewFont(13, gfx.FontOpts{})
	s := &parityScene{}

	s.root = NewPanel()

	s.dock = NewDock()
	s.dock.Padding = 6
	s.dock.Gap = 4
	s.dock.Anchors = fill()
	s.root.Add(s.dock)

	header := NewHStack()
	header.Dock = "top"
	header.Height = f32p(30)
	s.dock.Add(header)
	s.pinned = NewPanel()
	s.pinned.Width, s.pinned.Height = f32p(80), f32p(24)
	header.Add(s.pinned)
	header.Add(NewLabel("header", font, nil))
	stretchy := NewLabel("stretchy", font, nil)
	stretchy.StackSize = StackSize{Kind: "fill", Weight: 1}
	header.Add(stretchy)

	s.split = NewSplitView()
	s.split.Axis, s.split.Pos, s.split.MinA, s.split.MinB = "h", 220, 80, 80
	s.split.Dock = "fill"
	s.dock.Add(s.split)

	s.grid = NewGrid()
	s.grid.Columns = []GridTrack{
		{Kind: "content", Align: "end"},
		{Kind: "fill", Weight: 1, Align: "stretch"},
	}
	s.split.Add(s.grid)
	for i := 0; i < 4; i++ {
		s.grid.Add(NewLabel(fmt.Sprintf("field %d", i), font, nil))
		s.grid.Add(NewTextField())
	}
	wide := NewPanel()
	wide.GridSpan = 2
	wide.Height = f32p(18)
	s.grid.Add(wide)

	right := NewPanel()
	s.split.Add(right)

	s.scroll = NewScrollView()
	s.scroll.Anchors = &Anchors{Left: f32p(0), Right: f32p(0), Top: f32p(0), Bottom: f32p(120)}
	right.Add(s.scroll)
	s.vstack = NewVStack()
	s.vstack.Anchors = &Anchors{Left: f32p(0), Right: f32p(0), Top: f32p(0)}
	s.vstack.Padding = 8
	s.scroll.Add(s.vstack)
	for i := 0; i < 12; i++ {
		row := NewHStack()
		row.StackSize = StackSize{Kind: "fixed", Px: 28}
		s.vstack.Add(row)
		l := NewLabel(fmt.Sprintf("row %d", i), font, nil)
		row.Add(l)
		row.Add(NewButton(fmt.Sprintf("go %d", i)))
		if i == 5 {
			s.cell = l
		}
	}
	s.wrapped = NewLabel("a wrapped label whose height follows the width it is given", font, nil)
	s.wrapped.Wrap = true
	s.wrapped.Anchors = &Anchors{Left: f32p(8), Right: f32p(8), Bottom: f32p(8)}
	right.Add(s.wrapped)

	s.table = NewTableView()
	s.table.Columns = []TableColumn{{Key: "a", Title: "A", Weight: 1}, {Key: "b", Title: "B", Width: 90}}
	s.table.RowCount = 400
	s.table.Anchors = &Anchors{Left: f32p(8), Right: f32p(8), Bottom: f32p(8)}
	s.table.Height = f32p(100)
	right.Add(s.table)

	s.dialog = NewDialog()
	s.dialog.Title, s.dialog.CardW, s.dialog.CardH = "parity", 300, 180
	s.dialog.Anchors = fill()
	s.root.Add(s.dialog)
	body := NewLabel("dialog body", font, nil)
	body.Anchors = &Anchors{Left: f32p(16), Top: f32p(16), Right: f32p(16)}
	s.dialog.Add(body)
	s.dialog.Add(NewCheckbox("agree"))

	return s
}

type parityStep struct {
	name   string
	w, h   float32
	mutate func(u *Ui, s *parityScene)
}

// paritySteps is the script both trees run. Every mutation invalidates the way
// the protocol layer or a widget's own interaction would.
var paritySteps = []parityStep{
	{"initial", 900, 600, nil},
	{"label text", 900, 600, func(_ *Ui, s *parityScene) {
		s.cell.Text = "a considerably longer cell label"
		s.cell.Invalidate()
	}},
	{"narrower viewport", 700, 600, nil},
	{"scroll", 700, 600, func(_ *Ui, s *parityScene) { s.scroll.ScrollBy(120) }},
	{"divider drag", 700, 600, func(_ *Ui, s *parityScene) {
		s.split.Pos = 320
		s.split.Invalidate()
	}},
	{"explicit width", 700, 600, func(_ *Ui, s *parityScene) {
		s.pinned.Width = f32p(160)
		s.pinned.Invalidate()
	}},
	{"append a row", 700, 600, func(_ *Ui, s *parityScene) {
		row := NewHStack()
		row.StackSize = StackSize{Kind: "fixed", Px: 28}
		s.vstack.Add(row)
		row.Add(NewLabel("appended", gfx.NewFont(13, gfx.FontOpts{}), nil))
	}},
	{"drop a row", 700, 600, func(_ *Ui, s *parityScene) {
		s.vstack.Kids = append(s.vstack.Kids[:2], s.vstack.Kids[3:]...)
		s.vstack.InvalidateLayout()
	}},
	{"wrapped text", 700, 600, func(_ *Ui, s *parityScene) {
		s.wrapped.Text = "a much longer wrapped label, long enough that the line " +
			"count changes with the width it is handed and the height follows"
		s.wrapped.Invalidate()
	}},
	{"row count", 700, 600, func(_ *Ui, s *parityScene) {
		s.table.RowCount = 5000
		s.table.Invalidate()
	}},
	{"grid column", 700, 600, func(_ *Ui, s *parityScene) {
		s.grid.Columns[0] = GridTrack{Kind: "fixed", Px: 140, Align: "end"}
		s.grid.Invalidate()
	}},
	{"stack spacing", 700, 600, func(_ *Ui, s *parityScene) {
		s.vstack.Spacing = 20
		s.vstack.Invalidate()
	}},
	{"anchors", 700, 600, func(_ *Ui, s *parityScene) {
		s.pinned.Anchors = &Anchors{Left: f32p(12), Top: f32p(3)}
		s.pinned.Invalidate()
	}},
	{"card size", 700, 600, func(_ *Ui, s *parityScene) {
		s.dialog.CardW, s.dialog.CardH = 420, 240
		s.dialog.Invalidate()
	}},
	{"metrics", 700, 600, func(u *Ui, _ *parityScene) {
		u.ApplyMetrics(map[string]float64{"row.height": 40, "control.height": 44, "space.pad": 14})
	}},
	{"wider viewport", 1100, 640, nil},
	{"quiet frame", 1100, 640, nil},
}

func TestLayoutSkipMatchesFullLayout(t *testing.T) {
	defer func() { Metrics = defaultMetrics() }()

	skipUI, skipScene := fakeUi(), newParityScene()
	skipUI.Root = skipScene.root
	fullUI, fullScene := fakeUi(), newParityScene()
	fullUI.Root = fullScene.root

	for i, step := range paritySteps {
		if step.mutate != nil {
			step.mutate(skipUI, skipScene)
			step.mutate(fullUI, fullScene)
		}
		skipUI.BuildFrame(&gfx.DisplayList{}, step.w, step.h)
		// The reference lays out in full: an untargeted invalidation turns the
		// skip off for the frame.
		fullUI.Invalidate()
		fullUI.BuildFrame(&gfx.DisplayList{}, step.w, step.h)
		compareBounds(t, fmt.Sprintf("step %d (%s)", i, step.name), "root",
			skipScene.root, fullScene.root)
		if t.Failed() {
			return
		}
	}
}

func compareBounds(t *testing.T, step, path string, got, want Widget) {
	t.Helper()
	gb, wb := got.Base(), want.Base()
	if gb.Bounds != wb.Bounds {
		t.Fatalf("%s: %s bounds = %v; full layout gives %v", step, path, gb.Bounds, wb.Bounds)
	}
	if len(gb.Kids) != len(wb.Kids) {
		t.Fatalf("%s: %s has %d children; full layout has %d", step, path, len(gb.Kids), len(wb.Kids))
	}
	for i := range gb.Kids {
		compareBounds(t, step, fmt.Sprintf("%s/%d:%T", path, i, gb.Kids[i]), gb.Kids[i], wb.Kids[i])
	}
}

// TestLayoutSkipActuallySkips keeps the parity test honest: it would pass just
// as well if nothing were ever skipped. Corrupting a child's bounds and finding
// them still corrupt after a quiet frame proves the parent never recursed.
func TestLayoutSkipActuallySkips(t *testing.T) {
	u, s := fakeUi(), newParityScene()
	u.Root = s.root
	u.BuildFrame(&gfx.DisplayList{}, 900, 600)
	u.BuildFrame(&gfx.DisplayList{}, 900, 600)

	junk := gfx.R(-1, -2, -3, -4)
	s.cell.Bounds = junk
	u.BuildFrame(&gfx.DisplayList{}, 900, 600)
	if s.cell.Bounds != junk {
		t.Fatalf("a quiet frame re-laid-out a clean subtree: bounds = %v", s.cell.Bounds)
	}

	// And the skip has to end the moment anything in the subtree invalidates.
	s.cell.Invalidate()
	u.BuildFrame(&gfx.DisplayList{}, 900, 600)
	if s.cell.Bounds == junk {
		t.Fatal("an invalidated widget kept its stale bounds")
	}
}

// TestGeometricAnimationLaysOutInFull pins the one case the dirty flags cannot
// describe: animGeometry offsets bounds after layout, so while a tween is in
// flight every frame has to re-derive them or the offsets compound.
func TestGeometricAnimationLaysOutInFull(t *testing.T) {
	u, s := fakeUi(), newParityScene()
	u.Root = s.root
	u.BuildFrame(&gfx.DisplayList{}, 900, 600)

	u.NoteStructural()
	row := NewHStack()
	row.StackSize = StackSize{Kind: "fixed", Px: 28}
	s.vstack.Kids = append([]Widget{row}, s.vstack.Kids...)
	row.Parent = s.vstack
	s.vstack.InvalidateLayout()

	u.BuildFrame(&gfx.DisplayList{}, 900, 600)
	if !u.animating {
		t.Fatal("a structural frame that moved rows started no tween")
	}
	u.BuildFrame(&gfx.DisplayList{}, 900, 600)
	if !u.layoutAll {
		t.Fatal("a frame with a tween in flight skipped layout")
	}
}
