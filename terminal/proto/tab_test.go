package proto

import (
	"testing"

	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"
)

func TestTabOutCommandMovesFocusFromTheWidget(t *testing.T) {
	s := &Session{ui: ui.New()}
	a, b := ui.NewTextField(), ui.NewTextField()
	root := ui.NewPanel()
	root.Add(a)
	root.Add(b)
	s.ui.Root = root
	s.ui.FocusWidget(a)

	s.command(a, wire.CmdTabOut, "1")
	if !s.ui.IsFocused(b) {
		t.Fatal("tabout 1 did not focus the next field")
	}
	s.command(b, wire.CmdTabOut, "-1")
	if !s.ui.IsFocused(a) {
		t.Fatal("tabout -1 did not focus the previous field")
	}
}
