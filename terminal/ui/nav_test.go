package ui

import "testing"

func TestUpAndDownGoToOnNavAndLeaveTheCaret(t *testing.T) {
	u, a := focusedField(t)
	var dirs []int
	a.OnNav = func(dir int) { dirs = append(dirs, dir) }
	for _, r := range "abc" {
		u.CharInput(r)
	}
	u.KeyDown(Key{Name: "Left"})

	u.KeyDown(Key{Name: "Down"})
	u.KeyDown(Key{Name: "Down"})
	u.KeyDown(Key{Name: "Up"})

	if len(dirs) != 3 || dirs[0] != 1 || dirs[1] != 1 || dirs[2] != -1 {
		t.Fatalf("dirs = %v; want [1 1 -1]", dirs)
	}
	if _, e := a.selRange(); e != 2 {
		t.Fatalf("caret %d; want 2, where Left left it", e)
	}
}

func TestUpAndDownMoveTheCaretWithoutOnNavOrWithModifier(t *testing.T) {
	u, a := focusedField(t)
	for _, r := range "abc" {
		u.CharInput(r)
	}
	u.KeyDown(Key{Name: "Up"})
	if _, e := a.selRange(); e != 0 {
		t.Fatalf("Up without OnNav left the caret at %d; want 0", e)
	}
	a.OnNav = func(int) { t.Fatal("OnNav fired for Shift+Down") }
	u.KeyDown(Key{Name: "Down", Shift: true})
	if s, e := a.selRange(); s != 0 || e != 3 {
		t.Fatalf("Shift+Down selected %d..%d; want 0..3", s, e)
	}
}
