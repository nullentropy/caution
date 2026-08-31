package ui

import (
	"testing"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

// The windowDrag routing contract: a press in a marked region moves the
// window ONLY when nothing interactive claims it - buttons in a custom
// titlebar keep working - and overlays suspend dragging entirely.
func TestWindowDragRouting(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	root.Bounds = gfx.R(0, 0, 400, 300)

	bar := NewPanel() // the app's own titlebar
	bar.Bounds = gfx.R(0, 0, 400, 40)
	bar.WindowDrag = true
	title := NewLabel("my app", gfx.NewFont(13, gfx.FontOpts{}), nil)
	title.Bounds = gfx.R(100, 10, 80, 20)
	bar.Add(title)
	btn := NewButton("menu")
	btn.Bounds = gfx.R(340, 8, 50, 24)
	bar.Add(btn)
	root.Add(bar)

	body := NewPanel()
	body.Bounds = gfx.R(0, 40, 400, 260)
	root.Add(body)
	root.Base().UI = u
	u.Root = root

	if !u.WindowDragAt(20, 20) {
		t.Fatal("bare press on the marked bar must drag")
	}
	if !u.WindowDragAt(120, 20) {
		t.Fatal("press on a non-interactive label inside the bar must drag through")
	}
	if u.WindowDragAt(350, 20) {
		t.Fatal("press on a button inside the bar must NOT drag - the button wins")
	}
	if u.WindowDragAt(200, 150) {
		t.Fatal("press outside any marked region must not drag")
	}
	u.Overlay = NewPanel() // an open menu suspends window dragging
	if u.WindowDragAt(20, 20) {
		t.Fatal("an open overlay must suspend window dragging")
	}
}

func TestAuxGoesUpstream(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	root.Add(NewButton("under the pointer"))
	u.Root = root
	layout(u, root, 200, 100)

	var got []int
	u.OnAux = func(b int) { got = append(got, b) }
	u.AuxDown(4)
	u.AuxDown(5)
	if len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Fatalf("aux presses reported %v; want [4 5]", got)
	}
}

func TestAuxClosesAnOpenPopover(t *testing.T) {
	u := fakeUi()
	u.Root = NewPanel()
	u.Overlay = NewPanel()
	fired := 0
	u.OnAux = func(int) { fired++ }
	u.AuxDown(4)
	if u.Overlay != nil {
		t.Fatal("an aux press left the popover open")
	}
	if fired != 0 {
		t.Fatal("the press that closed the popover also navigated")
	}
	u.AuxDown(4)
	if fired != 1 {
		t.Fatalf("aux fired %d times after the popover closed; want 1", fired)
	}
}
