package proto

import (
	"testing"

	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"
)

func TestSelectionMenuPickCarriesTheRangeAndClipboardOpWrites(t *testing.T) {
	sink := &recordingSink{}
	s := &Session{ui: ui.New()}
	s.store = NewNodeStore(sink)
	w := s.store.Build(nodeJSON{ID: 1, Type: "richtext", P: props{
		"spans":            []any{props{"text": "see here"}},
		"selectionContext": []any{props{"id": 1.0, "title": "Copy"}},
		"on":               []any{"selection-context"},
	}})
	b := w.Base()
	if len(b.SelectionItems) != 1 || b.SelectionItems[0].Title != "Copy" {
		t.Fatalf("selection items %v", b.SelectionItems)
	}
	b.OnSelectionPick(1, 4, 8)
	want := map[string]any{"id": 1, "start": 4, "end": 8}
	if len(sink.events) != 1 || sink.events[0].ev != wire.EvSelContext {
		t.Fatalf("pick sent %v", sink.events)
	}
	got := sink.events[0].value.(map[string]any)
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("pick sent %v, want %v", got, want)
		}
	}

	var clip string
	s.ui.WriteClipboard = func(t string) { clip = t }
	s.applyOp(&patchOp{Op: wire.OpClip, Text: "**here**"})
	if clip != "**here**" {
		t.Fatalf("clipboard got %q", clip)
	}
}
