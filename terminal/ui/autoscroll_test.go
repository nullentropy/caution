package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
)

func TestSelectionDragPastAScrollViewEdgeScrollsAndExtends(t *testing.T) {
	u := richUi()
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i)
	}
	r := NewRichText(gfx.NewFont(13, gfx.FontOpts{}), nil)
	r.SetSpans([]Span{{Text: strings.Join(lines, "\n")}})
	r.Selectable = true
	r.Anchors = &Anchors{Left: f32p(0), Right: f32p(0), Top: f32p(0)}
	sv := NewScrollView()
	sv.Frame = &gfx.Rect{X: 0, Y: 0, W: 200, H: 100}
	sv.Add(r)
	root := NewPanel()
	root.Add(sv)
	u.Root = root
	draw := func() { u.BuildFrame(&gfx.DisplayList{}, 200, 300) }
	held := func(x, y float32) {
		u.PointerMove(x, y)
		for i := 0; i < 60 && u.TickIn() > 0; i++ {
			u.NoteTick()
			draw()
		}
	}
	draw()

	u.PointerDown(2, 5)
	held(199, 160)
	if sv.ScrollY == 0 {
		t.Fatal("dragging below the scroll view did not scroll it")
	}
	if got := u.SelectedText(); !strings.HasPrefix(got, "line 00") || !strings.HasSuffix(got, "line 39") {
		t.Fatalf("selected %q, want everything from line 00 to line 39", got)
	}
	u.PointerUp(199, 160)
	if d := u.TickIn(); d != 0 {
		t.Fatalf("still ticking every %v after release", d)
	}

	u.PointerDown(60, 90)
	held(0, -40)
	if sv.ScrollY != 0 {
		t.Fatalf("dragging above the scroll view left it at %v", sv.ScrollY)
	}
	if got := u.SelectedText(); !strings.HasPrefix(got, "line 00") {
		t.Fatalf("selected %q, want it to reach line 00", got)
	}
	u.PointerUp(0, -40)
}
