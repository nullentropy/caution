package ui

import (
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
)

func TestAnUntitledDialogGivesItsChildrenTheWholeCard(t *testing.T) {
	u := realUi()
	root := NewPanel()
	d := NewDialog()
	kid := NewPanel()
	kid.Frame = &gfx.Rect{X: 0, Y: 0, W: 10, H: 10}
	d.Add(kid)
	root.Add(d)
	u.Root = root
	u.BuildFrame(&gfx.DisplayList{}, 800, 600)
	if got, want := kid.Bounds.Y, d.cardRect().Y; got != want {
		t.Fatalf("child starts at y %v, want the card's top %v", got, want)
	}
}
