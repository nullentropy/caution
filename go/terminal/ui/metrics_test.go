package ui

import (
	"testing"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

// TestMetricsPatchRebuildsTheWholeFrame pins the invalidation contract:
// retained display lists record resolved numbers, so a frame after a metrics
// change must splice nothing. A reused subtree would carry the old radii even
// though every widget reads the new table.
func TestMetricsPatchRebuildsTheWholeFrame(t *testing.T) {
	defer func() { Metrics = defaultMetrics() }()
	u := fakeUi()
	root := NewPanel()
	btn := NewButton("Save")
	btn.Frame = &gfx.Rect{X: 10, Y: 10, W: 96, H: 32}
	root.Add(btn)
	lbl := NewLabel("quiet", gfx.NewFont(13, gfx.FontOpts{}), nil)
	lbl.Frame = &gfx.Rect{X: 10, Y: 60, W: 120, H: 18}
	root.Add(lbl)
	u.Root = root

	u.BuildFrame(&gfx.DisplayList{}, 400, 300)
	second := &gfx.DisplayList{}
	u.BuildFrame(second, 400, 300)
	if u.Reused() == 0 {
		t.Fatal("the quiet frame spliced nothing, so this test proves nothing")
	}

	u.ApplyMetrics(map[string]float64{"radius.control": 0})
	third := &gfx.DisplayList{}
	u.BuildFrame(third, 400, 300)
	if u.Reused() != 0 {
		t.Fatalf("frame after a metrics patch spliced %d retained commands - they carry the old radii", u.Reused())
	}
	// The rebuild must actually resolve the new table: the button's rect
	// (the only rounded command in this tree) re-records square.
	var rounded, square int
	for i := range third.Cmds {
		c := &third.Cmds[i]
		if c.Kind != gfx.CmdRect && c.Kind != gfx.CmdShadow {
			continue
		}
		if c.Radii.TL > 0 {
			rounded++
		} else {
			square++
		}
	}
	if rounded != 0 || square == 0 {
		t.Fatalf("after radius.control=0: %d rounded / %d square commands; want 0 rounded", rounded, square)
	}
}

// TestMetricsChangeRelayouts pins the layout half of the contract: metric
// tokens change measurement, so a patch must move real geometry on the very
// next frame. Memoized intrinsics recompute on the epoch bump, the menubar slot
// re-reserves, and a table's scroll extent follows its rows. A per-node
// rowHeight prop keeps overriding the token.
func TestMetricsChangeRelayouts(t *testing.T) {
	defer func() { Metrics = defaultMetrics() }()
	u := fakeUi()
	root := NewPanel()
	btn := NewButton("Go") // 2 runes x 7px fake advance = 14px of label
	root.Add(btn)
	table := NewTableView()
	table.RowCount = 100
	table.Frame = &gfx.Rect{X: 0, Y: 40, W: 300, H: 200}
	root.Add(table)
	fixed := NewTableView()
	fixed.RowCount = 100
	fixed.RowHeight = 40 // explicit prop: the token must not touch it
	fixed.Frame = &gfx.Rect{X: 0, Y: 260, W: 300, H: 100}
	root.Add(fixed)
	u.Root = root
	u.SetMenubar([]MenuSpec{{Title: "File", Items: []MenuItemSpec{{ID: 1, Title: "New"}}}}, func(int) {})

	u.BuildFrame(&gfx.DisplayList{}, 400, 400)
	if got := btn.Bounds; got.H != 32 || got.W != 14+2*16 {
		t.Fatalf("default button = %gx%g; want %gx%g", got.W, got.H, float32(14+2*16), float32(32))
	}
	if root.Bounds.Y != 28 {
		t.Fatalf("default menubar slot = %g; want 28 (row.height)", root.Bounds.Y)
	}
	if table.ContentH != 100*28 {
		t.Fatalf("default table extent = %g; want %d", table.ContentH, 100*28)
	}

	u.ApplyMetrics(map[string]float64{
		"control.height": 26, "control.padX": 10, "row.height": 22,
	})
	u.BuildFrame(&gfx.DisplayList{}, 400, 400)
	if got := btn.Bounds; got.H != 26 || got.W != 14+2*10 {
		t.Fatalf("compact button = %gx%g; want %gx%g (stale size memo?)",
			got.W, got.H, float32(14+2*10), float32(26))
	}
	if root.Bounds.Y != 22 {
		t.Fatalf("compact menubar slot = %g; want 22", root.Bounds.Y)
	}
	if table.ContentH != 100*22 {
		t.Fatalf("compact table extent = %g; want %d", table.ContentH, 100*22)
	}
	if fixed.ContentH != 100*40 {
		t.Fatalf("rowHeight-prop table extent = %g; want %d - the prop must override the token",
			fixed.ContentH, 100*40)
	}
}

// TestApplyMetricsBumpsMeasureEpoch: widget size memos (a button's intrinsic
// size, a label's fit) key on MeasureEpoch, and layout metrics change what
// those memos hold - a metrics patch that left the epoch alone would leave
// stale sizes behind a rebuilt frame.
func TestApplyMetricsBumpsMeasureEpoch(t *testing.T) {
	defer func() { Metrics = defaultMetrics() }()
	u := fakeUi()
	before := u.MeasureEpoch()
	u.ApplyMetrics(map[string]float64{"border.width": 2})
	if u.MeasureEpoch() == before {
		t.Fatal("ApplyMetrics left MeasureEpoch unchanged; size memos would go stale")
	}
}

// TestApplyMetricTokensIgnoresUnknownNames: a newer server against an older
// terminal degrades to the defaults the terminal ships.
func TestApplyMetricTokensIgnoresUnknownNames(t *testing.T) {
	defer func() { Metrics = defaultMetrics() }()
	applyMetricTokens(map[string]float64{"radius.control": 3, "no.such.token": 99})
	if Metrics.RadiusControl != 3 {
		t.Fatalf("radius.control = %g; want 3", Metrics.RadiusControl)
	}
	if Metrics == (MetricSet{RadiusControl: 3}) {
		t.Fatal("unknown token wiped the rest of the table")
	}
}

func TestCheckboxRadiusKeepsTheMarkSquare(t *testing.T) {
	defer func() { Metrics = defaultMetrics() }()
	// The default pair (18, 6) sits at the size/3 cap: stock look
	// is unchanged by the cap's existence.
	if got := Metrics.CheckboxRadius(); got != Metrics.RadiusControl {
		t.Fatalf("default checkbox radius = %g; want radius.control %g", got, Metrics.RadiusControl)
	}
	// A smaller box keeps the default's proportion instead of growing
	// rounder: 6px of radius on a 14px box is 43% round and reads as a
	// radio, and circle-vs-square is how the two marks are told apart.
	applyMetricTokens(map[string]float64{"checkbox.size": 14})
	want := float32(14) / 3
	if got := Metrics.CheckboxRadius(); got != want {
		t.Fatalf("checkbox radius at size 14 = %g; want %g", got, want)
	}
	// Squarer themes pass through - the cap never rounds a corner up.
	applyMetricTokens(map[string]float64{"radius.control": 0})
	if got := Metrics.CheckboxRadius(); got != 0 {
		t.Fatalf("checkbox radius under a square theme = %g; want 0", got)
	}
	// And the painter uses it: the 14px box paints the capped radii, in
	// both the unchecked (bordered) and checked (filled) states.
	applyMetricTokens(map[string]float64{"radius.control": 6})
	u := fakeUi()
	c := NewCheckbox("x")
	c.Base().UI = u
	c.Bounds = gfx.R(0, 0, 120, 24)
	for _, checked := range []bool{false, true} {
		c.Checked = checked
		dl := &gfx.DisplayList{}
		c.PaintSelf(dl)
		found := false
		for _, cmd := range dl.Cmds {
			if cmd.Rect.W == 14 && cmd.Rect.H == 14 {
				found = true
				if cmd.Radii != gfx.CornerRadius(want) {
					t.Fatalf("checked=%v painted box radii = %+v; want uniform %g", checked, cmd.Radii, want)
				}
			}
		}
		if !found {
			t.Fatalf("checked=%v: no 14x14 box command painted", checked)
		}
	}
}

func TestControlRadiusOverridesBeatTheToken(t *testing.T) {
	defer func() { Metrics = defaultMetrics() }()
	u := fakeUi()
	pill := float32(14)

	b := NewButton("x")
	b.Base().UI = u
	b.Bounds = gfx.R(0, 0, 72, 28)
	b.Radius = &pill
	dl := &gfx.DisplayList{}
	b.PaintSelf(dl)
	found := false
	for _, cmd := range dl.Cmds {
		if cmd.Rect.W == 72 {
			found = true
			if cmd.Radii != gfx.CornerRadius(pill) {
				t.Fatalf("button radii = %+v; want uniform %g", cmd.Radii, pill)
			}
		}
	}
	if !found {
		t.Fatal("no button rect painted")
	}

	f := NewTextField()
	f.Base().UI = u
	f.Bounds = gfx.R(0, 0, 200, 32)
	f.Radius = &pill
	dl = &gfx.DisplayList{}
	f.PaintSelf(dl)
	found = false
	for _, cmd := range dl.Cmds {
		if cmd.Rect.W == 200 {
			found = true
			if cmd.Radii != gfx.CornerRadius(pill) {
				t.Fatalf("field radii = %+v; want uniform %g", cmd.Radii, pill)
			}
		}
	}
	if !found {
		t.Fatal("no field rect painted")
	}
}
