package ui

import (
	"slices"
	"testing"
)

func TestEnterSubmitsEveryTimeAndBlurNever(t *testing.T) {
	u, tf := focusedField(t)
	var commits, submits []string
	tf.OnCommit = func(v string) { commits = append(commits, v) }
	tf.OnSubmit = func(v string) { submits = append(submits, v) }

	for _, r := range "hi" {
		u.CharInput(r)
	}
	u.KeyDown(Key{Name: "Enter"})
	u.KeyDown(Key{Name: "Enter"})
	u.CharInput('!')
	u.FocusWidget(nil)

	if want := []string{"hi", "hi"}; !slices.Equal(submits, want) {
		t.Fatalf("submits = %v; want %v", submits, want)
	}
	if want := []string{"hi", "hi!"}; !slices.Equal(commits, want) {
		t.Fatalf("commits = %v; want %v (changed-only, blur included)", commits, want)
	}
}

func TestTextAreaSubmitsOnCmdEnterOnly(t *testing.T) {
	u := realUi()
	ta := NewTextArea()
	u.Root = ta
	u.FocusWidget(ta)
	var submits []string
	ta.OnSubmit = func(v string) { submits = append(submits, v) }
	u.CharInput('a')
	u.KeyDown(Key{Name: "Enter"})
	u.KeyDown(Key{Name: "Enter", Meta: true})
	if len(submits) != 1 || submits[0] != "a\n" {
		t.Fatalf("submits = %q; want one, after the newline", submits)
	}
}
