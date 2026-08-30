package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

// glassFixture: a root panel holding a design-like subtree and a glass
// covering everything - the designer's canvas shape.
//
//	root (0,0 400x300)
//	+- panelA (frame 20,20 100x80)
//	|   `- labelB (frame 30,30 60x20 -> abs 50,50)
//	+- panelC (clipping, 200,20 100x80)
//	|   `- labelD (abs 210,30 60x20)
//	`- glass (fills root)
func glassFixture(u *Ui) (root *Panel, g *Glass, ids map[Widget]int) {
	root = NewPanel()
	root.Bounds = gfx.R(0, 0, 400, 300)
	panelA := NewPanel()
	panelA.Bounds = gfx.R(20, 20, 100, 80)
	labelB := NewLabel("b", gfx.NewFont(13, gfx.FontOpts{}), nil)
	labelB.Bounds = gfx.R(50, 50, 60, 20)
	panelA.Add(labelB)
	panelC := NewPanel()
	panelC.Bounds = gfx.R(200, 20, 100, 80)
	panelC.Clips = true
	labelD := NewLabel("d", gfx.NewFont(13, gfx.FontOpts{}), nil)
	labelD.Bounds = gfx.R(210, 30, 60, 20)
	panelC.Add(labelD)
	g = NewGlass()
	g.Bounds = gfx.R(0, 0, 400, 300)
	root.Add(panelA)
	root.Add(panelC)
	root.Add(g)
	root.Base().UI = u
	panelA.Base().UI = u
	g.Base().UI = u

	ids = map[Widget]int{root: 1, panelA: 2, labelB: 3, panelC: 4, labelD: 5}
	g.IdOf = func(w Widget) int { return ids[w] }
	return root, g, ids
}

func TestGlassPickResolvesDeepestTarget(t *testing.T) {
	u := fakeUi()
	_, g, _ := glassFixture(u)
	var picks [][3]float32
	g.OnPick = func(x, y float32, target int) {
		picks = append(picks, [3]float32{x, y, float32(target)})
	}

	g.OnPointerDown(60, 55, 1)   // inside labelB (deepest)
	g.OnPointerDown(25, 90, 1)   // inside panelA only
	g.OnPointerDown(390, 290, 1) // dead space -> root
	g.OnPointerDown(215, 35, 1)  // inside clipped panelC's labelD

	want := [][3]float32{
		{60, 55, 3},
		{25, 90, 2},
		{390, 290, 1},
		{215, 35, 5},
	}
	if len(picks) != len(want) {
		t.Fatalf("picks = %v, want %v", picks, want)
	}
	for i := range want {
		if picks[i] != want[i] {
			t.Fatalf("pick %d = %v, want %v", i, picks[i], want[i])
		}
	}
}

func TestGlassClipExcludesOutsideChildren(t *testing.T) {
	u := fakeUi()
	root, g, ids := glassFixture(u)
	// A child hanging outside its clipping parent is unreachable there.
	outside := NewLabel("x", gfx.NewFont(13, gfx.FontOpts{}), nil)
	outside.Bounds = gfx.R(350, 200, 40, 20)
	for _, k := range root.Base().Kids {
		if p, ok := k.(*Panel); ok && p.Base().Clips {
			p.Add(outside)
		}
	}
	ids[outside] = 9
	var target int
	g.OnPick = func(_, _ float32, tg int) { target = tg }
	g.OnPointerDown(360, 210, 1)
	if target != 1 {
		t.Fatalf("clipped-out child resolved to %d, want root (1)", target)
	}
}

func TestGlassDragDropAndUnsubscribedTransparency(t *testing.T) {
	u := fakeUi()
	_, g, _ := glassFixture(u)
	if g.Interactive() {
		t.Fatal("an unsubscribed glass must not intercept input")
	}
	var drops [][2]float32
	g.OnPick = func(_, _ float32, _ int) {}
	g.OnDropAt = func(x, y float32) { drops = append(drops, [2]float32{x, y}) }
	if !g.Interactive() {
		t.Fatal("a subscribed glass intercepts input")
	}
	g.OnPointerDown(10, 10, 1)
	g.OnPointerUp(30, 40)
	if len(drops) != 1 || drops[0] != [2]float32{30, 40} {
		t.Fatalf("drops = %v, want [[30 40]]", drops)
	}
	// No pick, no drop: release without press reports nothing.
	g.OnPointerUp(50, 50)
	if len(drops) != 1 {
		t.Fatalf("release without press must not drop: %v", drops)
	}
}

func TestGlassRightGestureCaptureAndCoalescing(t *testing.T) {
	u := fakeUi()
	root, g, _ := glassFixture(u)
	u.Root = root // RightDown walks the tree from the Ui, unlike direct dispatch
	var log []string
	g.OnRPick = func(x, y float32) { log = append(log, fmt.Sprintf("pick %g,%g", x, y)) }
	g.OnRDragTo = func(x, y float32) { log = append(log, fmt.Sprintf("drag %g,%g", x, y)) }
	g.OnRDropAt = func(x, y float32) { log = append(log, fmt.Sprintf("drop %g,%g", x, y)) }

	if !u.RightDown(60, 55) {
		t.Fatal("right press over a subscribed glass must capture")
	}
	// Inside the coalesce window the move is dropped silently...
	u.PointerMove(80, 70)
	// ...and after it, positions flow.
	g.rSent = time.Now().Add(-2 * dragCoalesce)
	u.PointerMove(100, 90)
	u.RightUp(120, 110)

	want := []string{"pick 60,55", "drag 100,90", "drop 120,110"}
	if len(log) != len(want) {
		t.Fatalf("gesture log = %v; want %v", log, want)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("gesture log[%d] = %q; want %q", i, log[i], want[i])
		}
	}

	// Unsubscribed: the press is not captured, so the shell falls through to
	// the context menu, and moves route nowhere.
	g.OnRPick = nil
	if u.RightDown(60, 55) {
		t.Fatal("right press over an unsubscribed glass must not capture")
	}
}
