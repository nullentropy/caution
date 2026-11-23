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
