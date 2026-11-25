package caution

import "testing"

func TestSelectionContextPickRunsWithTheRange(t *testing.T) {
	var start, end int
	rt := RichText(Span{Text: "see here"}).Selectable().SelectionContext(
		SelectionItem{Title: "Copy", OnPick: func(s, e int) { start, end = s, e }})
	s := liveSession(Panel().Kids(rt))
	s.dispatch(clientMsg{T: "ev", ID: rt.id, Ev: "selection-context", Value: map[string]any{"id": 1.0, "start": 4.0, "end": 8.0}, Seq: 1})
	if start != 4 || end != 8 {
		t.Fatalf("pick ran with %d..%d, want 4..8", start, end)
	}
}

func TestWriteClipboardQueuesAClipboardOp(t *testing.T) {
	s := liveSession(Panel())
	s.WriteClipboard("**here**")
	want := `[{"op":"clipboard","text":"**here**"}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
}
