package proto

import (
	"testing"

	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"
)

type recordingSink struct{ events []recorded }

type recorded struct {
	id    int
	ev    wire.Event
	value any
}

func (r *recordingSink) Event(id int, ev wire.Event, value any) {
	r.events = append(r.events, recorded{id, ev, value})
}

func TestRichTextSpansInflateReplaceAndClickBack(t *testing.T) {
	sink := &recordingSink{}
	s := &Session{ui: ui.New()}
	s.store = NewNodeStore(sink)
	w := s.store.Build(nodeJSON{ID: 1, Type: "richtext", P: props{
		"spans": []any{props{"text": "see "}, props{"text": "here", "bold": true, "action": "open"}},
		"on":    []any{"span-click"},
	}})
	rt, ok := w.(*ui.RichText)
	if !ok {
		t.Fatalf("richtext inflated as %T", w)
	}
	if rt.Text() != "see here" {
		t.Fatalf("text %q, want the spans joined", rt.Text())
	}

	s.applyOp(&patchOp{Op: wire.OpSet, ID: 1, P: props{"spans": []any{props{"text": "gone"}}}})
	if rt.Text() != "gone" {
		t.Fatalf("a spans set should replace the whole list, got %q", rt.Text())
	}

	rt.OnSpanClick("open")
	if len(sink.events) != 1 || sink.events[0] != (recorded{1, wire.EvSpanClick, "open"}) {
		t.Fatalf("span click sent %v, want one span-click carrying the action", sink.events)
	}
}
