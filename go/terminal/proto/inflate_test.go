package proto

import (
	"testing"

	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/text"
	"github.com/nullentropy/caution/go/terminal/ui"
	"github.com/nullentropy/caution/go/wire"
)

type nullSink struct{}

func (nullSink) Event(int, wire.Event, any) {}

// The local-echo contract: a focused field ignores plain value sets, and the
// value command forces one through.
func TestFocusedFieldValueSetsHonorLocalEchoAndOverride(t *testing.T) {
	s := &Session{ui: ui.New()}
	s.store = NewNodeStore(nullSink{})
	w := s.store.Build(nodeJSON{ID: 1, Type: "textfield", P: props{"value": "draft"}})
	tf := w.(*ui.TextField)
	tf.Focused = true // the user is editing

	s.applyOp(&patchOp{Op: wire.OpSet, ID: 1, P: props{"value": "server"}})
	if tf.Value != "draft" {
		t.Fatalf("focused field took a plain value set: %q", tf.Value)
	}

	s.applyOp(&patchOp{Op: wire.OpCmd, ID: 1, Cmd: wire.CmdValue, Value: "$1,000"})
	if tf.Value != "$1,000" {
		t.Fatalf("value command did not apply: %q", tf.Value)
	}

	s.applyOp(&patchOp{Op: wire.OpCmd, ID: 1, Cmd: wire.CmdClear})
	if tf.Value != "" {
		t.Fatalf("clear command did not apply: %q", tf.Value)
	}
}

func TestTextareaSharesTheOverrideContract(t *testing.T) {
	s := &Session{ui: ui.New()}
	s.store = NewNodeStore(nullSink{})
	w := s.store.Build(nodeJSON{ID: 1, Type: "textarea", P: props{"value": "a\nb"}})
	ta := w.(*ui.TextArea)
	ta.Focused = true

	s.applyOp(&patchOp{Op: wire.OpSet, ID: 1, P: props{"value": "x"}})
	if ta.Value != "a\nb" {
		t.Fatalf("focused textarea took a plain value set: %q", ta.Value)
	}
	s.applyOp(&patchOp{Op: wire.OpCmd, ID: 1, Cmd: wire.CmdValue, Value: "x\ny"})
	if ta.Value != "x\ny" {
		t.Fatalf("value command did not apply: %q", ta.Value)
	}
}

func TestMountFocusesTheNodeItNames(t *testing.T) {
	u := ui.New()
	u.Measure = func(gfx.Font, string) *text.Run { return &text.Run{} }
	s := &Session{ui: u}
	s.store = NewNodeStore(nullSink{})
	s.handle(&serverMsg{T: wire.MsgMount, Focus: 2, Root: &nodeJSON{
		ID: 1, Type: "panel", Kids: []nodeJSON{{ID: 2, Type: "textfield"}},
	}})
	u.BuildFrame(&gfx.DisplayList{}, 400, 300) // focus requests land at paint
	if !u.IsFocused(s.store.ByID[2]) {
		t.Fatal("mount did not focus node 2")
	}
}

func TestRemoveDropsTheSubtreeAndKeepsTheRest(t *testing.T) {
	ns := NewNodeStore(nullSink{})
	root := ns.Build(nodeJSON{ID: 1, Type: wire.Panel, Kids: []nodeJSON{
		{ID: 2, Type: wire.Panel, Kids: []nodeJSON{{ID: 3, Type: wire.Panel}}},
		{ID: 4, Type: wire.Glass, P: props{}},
	}})
	kept := ns.ByID[4]
	ns.Remove(2)
	for _, id := range []int{2, 3} {
		if _, ok := ns.ByID[id]; ok {
			t.Fatalf("node %d survived its subtree's removal", id)
		}
	}
	if kids := root.Base().Kids; len(kids) != 1 || kids[0] != kept {
		t.Fatalf("root kids = %v; want only node 4", kids)
	}
	if id := kept.(*ui.Glass).IdOf(kept); id != 4 {
		t.Fatalf("IdOf(node 4) = %d", id)
	}
}
