package ui

import "testing"

func TestTextAreaPastesImageOnlyWhenClipboardHasNoText(t *testing.T) {
	u := realUi()
	ta := NewTextArea()
	u.Root = ta
	ta.UI = u
	u.FocusWidget(ta)
	clip := "hello"
	u.ReadClipboard = func() string { return clip }
	images := 0
	u.PasteImage = func(x, y float32) bool { images++; return true }

	u.KeyDown(Key{Name: "v", Meta: true})
	if ta.Value != "hello" || images != 0 {
		t.Fatalf("with text: value %q, images %d; want the text and no image", ta.Value, images)
	}
	clip = ""
	u.KeyDown(Key{Name: "v", Meta: true})
	if ta.Value != "hello" || images != 1 {
		t.Fatalf("without text: value %q, images %d; want one image paste", ta.Value, images)
	}
}
