package ui

import (
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/text"
)

// fakeUi with a wider mono face (10px per rune against 7), so mixed lines
// measure differently from plain ones
func richUi() *UI {
	u := New()
	u.Measure = func(f gfx.Font, s string) *text.Run {
		adv := float32(7)
		if f.Mono {
			adv = 10
		}
		runes := []rune(s)
		glyphs := make([]text.Placed, len(runes))
		for i := range runes {
			glyphs[i] = text.Placed{GID: 1, X: float32(i) * adv, Cluster: i}
		}
		return &text.Run{Glyphs: glyphs, Width: float32(len(runes)) * adv, Ascent: 10, Descent: 3}
	}
	return u
}

// a rich text stretched across a root of the given width, so its height
// comes from HeightForWidth like an anchored label's does
func mountRich(u *UI, w float32, spans ...Span) *RichText {
	r := NewRichText(gfx.NewFont(13, gfx.FontOpts{}), nil)
	r.SetSpans(spans)
	r.Anchors = &Anchors{Left: f32p(0), Right: f32p(0), Top: f32p(0)}
	root := NewPanel()
	root.Add(r)
	u.Root = root
	u.BuildFrame(&gfx.DisplayList{}, w, 300)
	return r
}

type placedText struct {
	s    string
	x, y float32
}

func paintedText(dl *gfx.DisplayList) []placedText {
	var out []placedText
	for _, c := range dl.Cmds {
		if c.Kind == gfx.CmdText {
			out = append(out, placedText{c.Text, c.X, c.Y})
		}
	}
	return out
}

func TestRichTextWrapsAcrossSpanBoundary(t *testing.T) {
	u := richUi()
	// "foobar" straddles the spans and must stay whole, the break lands at the space
	r := mountRich(u, 50, Span{Text: "foo"}, Span{Text: "bar baz", Bold: true})
	dl := &gfx.DisplayList{}
	r.PaintSelf(dl)
	want := []placedText{{"foo", 0, 0}, {"bar", 21, 0}, {"baz", 0, 16}}
	got := paintedText(dl)
	if len(got) != len(want) {
		t.Fatalf("painted %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("painted %v, want %v", got, want)
		}
	}
	if r.Bounds.H != 29 {
		t.Fatalf("two lines of 13px with 25%% leading should be 29 tall, got %g", r.Bounds.H)
	}
}

func TestRichTextHeightFollowsMixedFaceWidths(t *testing.T) {
	u := richUi()
	r := mountRich(u, 200, Span{Text: "aaaa "}, Span{Text: "bbbb", Mono: true})
	// 35px of plain plus 40px of mono: one line at 80, two at 70
	if h, _ := r.HeightForWidth(80); h != 13 {
		t.Fatalf("one line should be 13 tall, got %g", h)
	}
	if h, _ := r.HeightForWidth(70); h != 29 {
		t.Fatalf("two lines should be 29 tall, got %g", h)
	}
}

func TestRichTextSelectionCopiesAcrossSpansAndLines(t *testing.T) {
	u := richUi()
	r := mountRich(u, 50, Span{Text: "foo"}, Span{Text: "bar baz", Bold: true})
	r.Selectable = true

	// drag from inside "foo" on the first line into "baz" on the second
	u.PointerDown(28, 5)
	u.PointerMove(14, 20)
	u.PointerUp(14, 20)
	if got := u.SelectedText(); got != "ar ba" {
		t.Fatalf("selected %q, want %q", got, "ar ba")
	}

	// a double-click takes the whole word even though it spans two styles
	u.PointerDown(10, 5)
	u.PointerUp(10, 5)
	u.PointerDown(10, 5)
	u.PointerUp(10, 5)
	if got := u.SelectedText(); got != "foobar" {
		t.Fatalf("double-click selected %q, want %q", got, "foobar")
	}
}

func TestRichTextSpanClickFiresActionDragDoesNot(t *testing.T) {
	u := richUi()
	var cursor string
	u.SetCursor = func(name string) { cursor = name }
	r := mountRich(u, 200, Span{Text: "see "}, Span{Text: "here", Action: "open"}, Span{Text: " now"})
	r.Selectable = true
	var fired []string
	r.OnSpanClick = func(a string) { fired = append(fired, a) }

	u.PointerDown(40, 5)
	u.PointerUp(40, 5)
	if len(fired) != 1 || fired[0] != "open" {
		t.Fatalf("click on the span fired %v, want [open]", fired)
	}

	u.PointerDown(30, 5)
	u.PointerMove(50, 5)
	u.PointerUp(50, 5)
	if len(fired) != 1 {
		t.Fatalf("a drag that selected %q fired %v", u.SelectedText(), fired)
	}
	if got := u.SelectedText(); got == "" {
		t.Fatal("the drag should have selected text")
	}

	u.PointerDown(10, 5)
	u.PointerUp(10, 5)
	if len(fired) != 1 {
		t.Fatalf("click on plain text fired %v", fired)
	}

	u.PointerMove(40, 5)
	if cursor != "pointer" {
		t.Fatalf("cursor over the span is %q, want pointer", cursor)
	}
	u.PointerMove(10, 5)
	if cursor != "text" {
		t.Fatalf("cursor over plain selectable text is %q, want text", cursor)
	}
}

func TestRichTextClickWorksWithoutSelection(t *testing.T) {
	u := richUi()
	r := mountRich(u, 200, Span{Text: "go", Action: "run"})
	var fired []string
	r.OnSpanClick = func(a string) { fired = append(fired, a) }
	u.PointerDown(5, 5)
	u.PointerUp(5, 5)
	if len(fired) != 1 || fired[0] != "run" {
		t.Fatalf("fired %v, want [run]", fired)
	}
}
