package ui

import (
	"slices"
	"testing"
)

func menuTitles(u *UI) []string {
	m, ok := u.Overlay.(*contextMenu)
	if !ok {
		return nil
	}
	var out []string
	for _, it := range m.items {
		out = append(out, it.Title)
	}
	return out
}

func TestRightClickOnASelectionOpensTheSelectionMenu(t *testing.T) {
	u := richUi()
	r := mountRich(u, 200, Span{Text: "foo "}, Span{Text: "bar", Bold: true})
	r.Selectable = true
	row := u.Root.Base()
	row.ContextItems = []ContextItem{{ID: 1, Title: "Copy message"}}
	type pick struct{ id, start, end int }
	var got []pick
	row.SelectionItems = []ContextItem{{ID: 1, Title: "Copy"}, {ID: 2, Title: "Copy as Markdown"}}
	row.OnSelectionPick = func(id, start, end int) { got = append(got, pick{id, start, end}) }

	u.ContextClick(10, 5)
	if titles := menuTitles(u); !slices.Equal(titles, []string{"Copy message"}) {
		t.Fatalf("with nothing selected the menu is %v", titles)
	}
	u.CloseOverlay()

	u.PointerDown(35, 5)
	u.PointerUp(35, 5)
	u.PointerDown(35, 5)
	u.PointerUp(35, 5)
	if s := u.SelectedText(); s != "bar" {
		t.Fatalf("selected %q, want bar", s)
	}
	u.ContextClick(10, 5)
	if titles := menuTitles(u); !slices.Equal(titles, []string{"Copy", "Copy as Markdown"}) {
		t.Fatalf("with bar selected the menu is %v", titles)
	}
	u.Overlay.(*contextMenu).onPick(2)
	if want := []pick{{2, 4, 7}}; !slices.Equal(got, want) {
		t.Fatalf("picks %v, want %v", got, want)
	}
}
