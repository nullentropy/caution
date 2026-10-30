package ui

import (
	"fmt"
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/text"
)

// real shaper, build cost is dominated by text work so a fake measure would
// flatter every number
func realUi() *UI {
	u := New()
	sh := text.NewShaper(text.NewRegistry())
	u.Measure = func(f gfx.Font, s string) *text.Run { return sh.Shape(f, 2, s) }
	return u
}

// the perf scene in widget form: rows of a panel, three labels and a button
// in a scroller, ~26 widgets per row
func benchTree(rows int) (Widget, *Label) {
	root := NewPanel()
	root.Bg = Theme["bg"]
	sv := NewScrollView()
	sv.Anchors = &Anchors{Left: f32p(0), Right: f32p(0), Top: f32p(0), Bottom: f32p(0)}
	root.Add(sv)
	inner := NewVStack()
	inner.Anchors = &Anchors{Left: f32p(0), Right: f32p(0), Top: f32p(0)}
	inner.Padding = 8
	sv.Add(inner)
	var first *Label
	for i := 0; i < rows; i++ {
		row := NewHStack()
		row.StackSize = StackSize{Kind: "fixed", Px: 34}
		inner.Add(row)
		for j := 0; j < 4; j++ {
			p := NewPanel()
			p.Bg = Theme["panel"]
			p.Radius = gfx.CornerRadius(6)
			row.Add(p)
			l := NewLabel(fmt.Sprintf("row %d cell %d", i, j), gfx.NewFont(13, gfx.FontOpts{}), nil)
			p.Add(l)
			if first == nil {
				first = l
			}
		}
		row.Add(NewButton(fmt.Sprintf("Go %d", i)))
	}
	return root, first
}

func countWidgets(w Widget) int {
	n := 1
	for _, c := range w.Base().Kids {
		n += countWidgets(c)
	}
	return n
}

// pins the scene the recorded numbers were measured on, the benchmarks only
// mean anything against a tree of known size
func TestBenchTreeSize(t *testing.T) {
	u := realUi()
	root, _ := benchTree(260)
	u.Root = root
	dl := &gfx.DisplayList{}
	u.BuildFrame(dl, 1200, 800)
	widgets, cmds := countWidgets(root), len(dl.Cmds)
	t.Logf("widgets=%d cmds=%d", widgets, cmds)
	if widgets != 2603 || cmds != 2602 {
		t.Fatalf("the bench tree changed shape: %d widgets, %d commands "+
			"(was 2603/2602 - re-measure before trusting the recorded numbers)",
			widgets, cmds)
	}
}

// whole-tree rebuild, what a resize, theme change or untargeted invalidation costs
func BenchmarkBuildFrameFull(b *testing.B) {
	u := realUi()
	root, _ := benchTree(260)
	u.Root = root
	u.BuildFrame(&gfx.DisplayList{}, 1200, 800) // warm the shaper cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.Invalidate()
		u.BuildFrame(&gfx.DisplayList{}, 1200, 800)
	}
}

// targeted case: one label's text changes in a ~2600-widget tree, frame after frame
func BenchmarkBuildFrameOneLabel(b *testing.B) {
	u := realUi()
	root, label := benchTree(260)
	u.Root = root
	u.BuildFrame(&gfx.DisplayList{}, 1200, 800)
	u.BuildFrame(&gfx.DisplayList{}, 1200, 800)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		label.Text = fmt.Sprintf("tick %d", i)
		label.Invalidate()
		u.BuildFrame(&gfx.DisplayList{}, 1200, 800)
	}
}

// the floor: nothing changed (a caret blink elsewhere, a throttled table
// report). layout skips the whole tree, what's left is the marking walk
func BenchmarkBuildFrameClean(b *testing.B) {
	u := realUi()
	root, _ := benchTree(260)
	u.Root = root
	u.BuildFrame(&gfx.DisplayList{}, 1200, 800)
	u.BuildFrame(&gfx.DisplayList{}, 1200, 800)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.BuildFrame(&gfx.DisplayList{}, 1200, 800)
	}
}

func BenchmarkLayoutOnly(b *testing.B) {
	u := realUi()
	root, _ := benchTree(260)
	u.Root = root
	u.BuildFrame(&gfx.DisplayList{}, 1200, 800)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		root.Base().Bounds = gfx.R(0, 0, 1200, 800)
		root.LayoutChildren()
	}
}

func BenchmarkPaintOnly(b *testing.B) {
	u := realUi()
	root, _ := benchTree(260)
	u.Root = root
	u.BuildFrame(&gfx.DisplayList{}, 1200, 800)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		root.PaintTree(&gfx.DisplayList{}, -1)
	}
}

// correctness bar for per-subtree retention: a spliced frame must be
// byte-for-byte what a full walk produces. two identical trees run the same
// mutations, one splices, the other is forced to rebuild, every command must agree
func TestPaintReuseMatchesFullWalk(t *testing.T) {
	spliced, sLabel := benchTree(20)
	full, fLabel := benchTree(20)
	us, uf := realUi(), realUi()
	us.Root, uf.Root = spliced, full

	steps := []func(l *Label){
		func(*Label) {},
		func(l *Label) { l.Text = "changed" },
		func(l *Label) { l.Text = "changed again, and much longer than before" },
		func(l *Label) { l.Color = Theme["accent"] },
		func(l *Label) { l.Base().Parent.Base().Bounds.X += 5 },
		func(l *Label) { l.Text = "x" },
	}
	for i, step := range steps {
		step(sLabel)
		step(fLabel)
		sLabel.Invalidate()
		fLabel.Invalidate()
		ds, df := &gfx.DisplayList{}, &gfx.DisplayList{}
		us.BuildFrame(ds, 900, 600)
		uf.Invalidate() // forces the whole-list rebuild
		uf.BuildFrame(df, 900, 600)
		if len(ds.Cmds) != len(df.Cmds) {
			t.Fatalf("step %d: spliced list has %d commands, full walk %d", i, len(ds.Cmds), len(df.Cmds))
		}
		for j := range ds.Cmds {
			if !gfx.CmdEqual(&ds.Cmds[j], &df.Cmds[j]) {
				t.Fatalf("step %d: command %d differs\n spliced %+v\n full    %+v", i, j, ds.Cmds[j], df.Cmds[j])
			}
		}
		if i > 0 && us.Reused() == 0 {
			t.Fatalf("step %d: nothing was reused - the test proves nothing", i)
		}
		t.Logf("step %d: %d commands, %d reused", i, len(ds.Cmds), us.Reused())
	}
}
