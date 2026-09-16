package ui

import (
	"testing"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

func TestHostTextLeavesEditingKeysToTheHost(t *testing.T) {
	u, played := soundUi()
	u.HostText = true
	f := NewTextField()
	mounted(u, f)
	u.setFocus(f, false)

	if f.OnChar('a') || f.Value != "" {
		t.Fatalf("host mode inserted a character: %q", f.Value)
	}
	for _, name := range []string{"Backspace", "Left", "a"} {
		if f.OnKey(Key{Name: name, Meta: name == "a"}) {
			t.Fatalf("%s handled in host mode", name)
		}
	}

	var committed, input []string
	f.OnCommit = func(v string) { committed = append(committed, v) }
	f.OnInput = func(v string) { input = append(input, v) }
	u.SyncText("hel", 3, 3, true)
	u.SyncText("hello", 5, 5, true)
	u.SyncText("hello", 1, 4, false)
	if f.Value != "hello" || f.selStart != 1 || f.selEnd != 4 {
		t.Fatalf("value %q sel %d..%d", f.Value, f.selStart, f.selEnd)
	}
	if len(input) != 2 {
		t.Fatalf("input fired %d times; want 2", len(input))
	}
	wantPlayed(t, played, "/type.wav", "/type.wav")

	if !f.OnKey(Key{Name: "Enter"}) || len(committed) != 1 || committed[0] != "hello" {
		t.Fatalf("enter committed %v", committed)
	}
	u.SyncText("hellx", 5, 5, true)
	if !f.OnKey(Key{Name: "Escape"}) || f.Value != "hello" {
		t.Fatalf("escape reverted to %q", f.Value)
	}
	if f.OnKey(Key{Name: "Escape"}) {
		t.Fatal("escape on a clean field should bubble")
	}
}

func TestHostTextAreaKeepsVerticalMovesAndCmdEnter(t *testing.T) {
	u := fakeUi()
	u.HostText = true
	a := NewTextArea()
	mounted(u, a)
	u.setFocus(a, false)
	u.SyncText("one\ntwo", 7, 7, false)

	if a.OnKey(Key{Name: "Enter"}) {
		t.Fatal("plain enter belongs to the host textarea")
	}
	if !a.OnKey(Key{Name: "Up"}) || a.selEnd >= 4 {
		t.Fatalf("up did not move to the first line: caret %d", a.selEnd)
	}
	var committed int
	a.OnCommit = func(string) { committed++ }
	if !a.OnKey(Key{Name: "Enter", Meta: true}) || committed != 1 {
		t.Fatal("cmd+enter should commit")
	}
}

func TestFocusedTextReportsTheEditorTheFunnelMirrors(t *testing.T) {
	u := fakeUi()
	f := NewTextField()
	f.Placeholder = "name"
	f.Value = "ab"
	a := NewTextArea()
	root := NewPanel()
	root.Add(f)
	root.Add(a)
	u.Root = root
	layout(u, root, 400, 300)

	if _, ok := u.FocusedText(); ok {
		t.Fatal("nothing focused yet")
	}
	u.setFocus(f, false)
	st, ok := u.FocusedText()
	if !ok || st.Value != "ab" || st.Placeholder != "name" || st.Multiline || st.Bounds != f.Bounds {
		t.Fatalf("state %+v ok=%v", st, ok)
	}
	u.setFocus(a, false)
	if st, ok := u.FocusedText(); !ok || !st.Multiline {
		t.Fatalf("text area state %+v ok=%v", st, ok)
	}
}

func TestSemanticsFlattenMeaningWithStableIDs(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	root.Add(NewLabel("hi", cbFont, nil))
	root.Add(NewPanel()) // decorative
	btn := NewButton("go")
	root.Add(btn)
	cb := NewCheckbox("opt")
	cb.Checked = true
	root.Add(cb)
	secret := NewTextField()
	secret.Sensitive = true
	secret.Value = "hunter2"
	root.Add(secret)
	img := NewImageView()
	root.Add(img)
	u.Root = root
	layout(u, root, 400, 300)
	u.setFocus(btn, false)

	nodes := u.Semantics()
	roles := []string{}
	for _, n := range nodes {
		roles = append(roles, n.Role)
	}
	want := []string{"text", "button", "checkbox", "textbox"}
	if len(roles) != len(want) {
		t.Fatalf("roles %v; want %v", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("roles %v; want %v", roles, want)
		}
	}
	if !nodes[1].Focused || nodes[0].Focused {
		t.Fatal("focus not reported on the button")
	}
	if !nodes[2].Checked || nodes[3].Value != "" {
		t.Fatalf("checkbox %+v textbox %+v", nodes[2].Semantic, nodes[3].Semantic)
	}
	again := u.Semantics()
	for i := range nodes {
		if again[i].ID != nodes[i].ID || nodes[i].ID == 0 {
			t.Fatalf("ids drifted: %d then %d", nodes[i].ID, again[i].ID)
		}
	}

	clicks := 0
	btn.OnClick = func() { clicks++ }
	u.Activate(btn, -1)
	if clicks != 1 || u.focused != btn {
		t.Fatalf("activate: clicks=%d focused=%v", clicks, u.focused)
	}
}

func TestTableSemanticsListVisibleRows(t *testing.T) {
	u := fakeUi()
	tv := NewTableView()
	tv.Columns = []TableColumn{{Key: "a", Title: "A"}, {Key: "b", Title: "B"}}
	tv.RowCount = 100
	tv.Frame = &gfx.Rect{W: 400, H: 300}
	tv.ApplyRows(0, [][]string{{"1", "x"}, {"2", "y"}, {"3", "z"}}, nil, false)
	mounted(u, tv)
	var selected []int
	tv.OnRowSelect = func(row int, _ string) { selected = append(selected, row) }

	s := tv.Semantics()
	if s.Role != "grid" || s.RowCount != 100 || len(s.Rows) != 3 || s.Rows[2].Label != "3 · z" {
		t.Fatalf("grid %+v", s)
	}
	u.Activate(tv, 1)
	if len(selected) != 1 || selected[0] != 1 || !tv.Semantics().Rows[1].Selected {
		t.Fatalf("row activation selected %v", selected)
	}
}
