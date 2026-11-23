package ui

import (
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
)

func TestABareKeyReachesTheAppOnlyWhenNothingIsTyping(t *testing.T) {
	u := realUi()
	root := NewPanel()
	tf := NewTextField()
	tf.Frame = &gfx.Rect{X: 10, Y: 10, W: 200, H: 28}
	root.Add(tf)
	u.Root = root
	u.BuildFrame(&gfx.DisplayList{}, 400, 300)
	var got []int
	u.KeyCombos = map[string]int{"j": 1, "cmd+k": 2}
	u.OnKeyCombo = func(id int) { got = append(got, id) }

	u.KeyDown(Key{Name: "j"})
	u.FocusWidget(tf)
	u.KeyDown(Key{Name: "j"})
	u.KeyDown(Key{Name: "k", Meta: true})

	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("combos fired %v, want j before focus then cmd+k only", got)
	}
}

func TestEscapeLeavesAComposerWithItsDraftAndTheNextEscapeReachesTheApp(t *testing.T) {
	u := realUi()
	root := NewPanel()
	a := NewTextArea()
	a.EnterSends = true
	a.Frame = &gfx.Rect{X: 10, Y: 10, W: 200, H: 60}
	root.Add(a)
	u.Root = root
	u.BuildFrame(&gfx.DisplayList{}, 400, 300)
	var got []int
	u.KeyCombos = map[string]int{"escape": 1}
	u.OnKeyCombo = func(id int) { got = append(got, id) }

	u.FocusWidget(a)
	for _, r := range "draft" {
		u.CharInput(r)
	}
	u.KeyDown(Key{Name: "Escape"})
	if u.IsFocused(a) || a.Value != "draft" || len(got) != 0 {
		t.Fatalf("first escape: focused %v, value %q, combos %v", u.IsFocused(a), a.Value, got)
	}
	u.KeyDown(Key{Name: "Escape"})
	if len(got) != 1 {
		t.Fatalf("second escape fired %v", got)
	}
}
