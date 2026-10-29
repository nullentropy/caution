package ui

import (
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
)

// a row inserted above another, so one frame both fades and slides
func insertRow(u *UI, root *Panel, noFade bool) *gfx.DisplayList {
	moved := NewPanel()
	moved.Frame = &gfx.Rect{X: 0, Y: 0, W: 100, H: 20}
	stack := NewVStack()
	stack.NoFade = noFade
	stack.Add(moved)
	root.Add(stack)
	u.Root = root
	dl := &gfx.DisplayList{}
	u.BuildFrame(dl, 400, 300)

	row := NewPanel()
	row.Frame = &gfx.Rect{X: 0, Y: 0, W: 100, H: 20}
	stack.Kids = append([]Widget{row}, stack.Kids...)
	row.Base().Parent = stack
	u.NoteStructural()
	dl = &gfx.DisplayList{}
	u.BuildFrame(dl, 400, 300)
	return dl
}

func TestInsertFadesAndSlidesByDefault(t *testing.T) {
	u := fakeUi()
	dl := insertRow(u, NewPanel(), false)
	if count(dl, gfx.CmdLayerBegin) == 0 {
		t.Fatal("an inserted row did not fade")
	}
	if !dl.GeomAnimation {
		t.Fatal("the row the insert pushed down did not slide")
	}
}

func TestReducedMotionSnaps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		motion Motion
		system bool
	}{
		{"session reduced", MotionReduced, false},
		{"system reduced", MotionSystem, true},
	} {
		u := fakeUi()
		u.SetMotion(tc.motion)
		u.SetSystemReducedMotion(tc.system)
		dl := insertRow(u, NewPanel(), false)
		if n := count(dl, gfx.CmdLayerBegin); n != 0 {
			t.Fatalf("%s: inserted row faded through %d layers", tc.name, n)
		}
		if dl.GeomAnimation || dl.WantsAnimation() {
			t.Fatalf("%s: the frame still animates", tc.name)
		}
	}
}

func TestSessionFullOverridesSystem(t *testing.T) {
	u := fakeUi()
	u.SetMotion(MotionFull)
	u.SetSystemReducedMotion(true)
	if count(insertRow(u, NewPanel(), false), gfx.CmdLayerBegin) == 0 {
		t.Fatal("MotionFull did not override the OS preference")
	}
}

func TestNoFadeSubtreeSkipsFadeButStillSlides(t *testing.T) {
	u := fakeUi()
	dl := insertRow(u, NewPanel(), true)
	if n := count(dl, gfx.CmdLayerBegin); n != 0 {
		t.Fatalf("a row inserted under NoFade faded through %d layers", n)
	}
	if !dl.GeomAnimation {
		t.Fatal("NoFade should not stop the reflow slide")
	}
}

func TestReducedMotionThemeSwitchesAtOnce(t *testing.T) {
	saved := *Theme["accent"]
	defer func() { *Theme["accent"] = saved }()
	u := fakeUi()
	u.SetMotion(MotionReduced)
	u.AnimateTheme(map[string]string{"accent": "#ff0000"})
	if got := *Theme["accent"]; got != gfx.Hex("#ff0000") {
		t.Fatalf("accent is %v right after the switch, want it applied at once", got)
	}
	if u.tween != nil {
		t.Fatal("a theme tween started under reduced motion")
	}
}
