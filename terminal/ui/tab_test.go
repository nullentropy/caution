package ui

import (
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
)

func twoFields(t *testing.T) (*UI, *TextField, *TextField) {
	t.Helper()
	u, a := focusedField(t)
	b := NewTextField()
	b.Frame = &gfx.Rect{X: 10, Y: 50, W: 200, H: 28}
	u.Root.(*Panel).Add(b)
	u.BuildFrame(&gfx.DisplayList{}, 400, 300)
	return u, a, b
}

func TestTabGoesToOnTabAndKeepsFocus(t *testing.T) {
	u, a, b := twoFields(t)
	var shifts []bool
	var got string
	var caret int
	a.OnTab = func(shift bool, v string, c int) { shifts = append(shifts, shift); got, caret = v, c }
	for _, r := range "abc" {
		u.CharInput(r)
	}
	u.KeyDown(Key{Name: "Left"})

	u.KeyDown(Key{Name: "Tab"})
	u.KeyDown(Key{Name: "Tab", Shift: true})

	if len(shifts) != 2 || shifts[0] || !shifts[1] {
		t.Fatalf("shifts = %v; want [false true]", shifts)
	}
	if got != "abc" || caret != 2 {
		t.Fatalf("value %q caret %d; want abc 2", got, caret)
	}
	if u.focused != Widget(a) || u.focused == Widget(b) {
		t.Fatal("focus moved despite OnTab")
	}
}

func TestTabMovesFocusWithoutOnTabOrWithModifier(t *testing.T) {
	u, a, b := twoFields(t)
	u.KeyDown(Key{Name: "Tab"})
	if u.focused != Widget(b) {
		t.Fatal("Tab without OnTab did not move focus")
	}
	u.FocusWidget(a)
	a.OnTab = func(bool, string, int) { t.Fatal("OnTab fired for Ctrl+Tab") }
	u.KeyDown(Key{Name: "Tab", Ctrl: true})
	if u.focused != Widget(b) {
		t.Fatal("Ctrl+Tab did not move focus")
	}
}

func TestTabOutMovesFromWidgetAndDropsStaleRequests(t *testing.T) {
	u, a, b := twoFields(t)
	u.TabOut(a, 1)
	if u.focused != Widget(b) {
		t.Fatal("TabOut did not move focus on")
	}
	u.TabOut(a, 1)
	if u.focused != Widget(b) {
		t.Fatal("TabOut from an unfocused widget moved focus")
	}
	u.TabOut(b, -1)
	if u.focused != Widget(a) {
		t.Fatal("TabOut backward did not move focus back")
	}
}

func TestForceValueWhileFocusedPutsCaretAtEnd(t *testing.T) {
	u, tf := focusedField(t)
	u.CharInput('x')
	tf.ForceValue("héllo wörld")
	var caret int
	tf.OnTab = func(_ bool, _ string, c int) { caret = c }
	u.KeyDown(Key{Name: "Tab"})
	if caret != len([]rune("héllo wörld")) {
		t.Fatalf("caret = %d; want %d", caret, len([]rune("héllo wörld")))
	}
}
