package proto

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/text"
	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"
)

// a session wired like a shell's: a registry, a shaper measuring through it,
// and a fetch that serves the test font for /fonts/var.ttf
func fontSession(t *testing.T) (*Session, *ui.UI, *text.Shaper) {
	t.Helper()
	data, err := os.ReadFile("../text/testdata/Estedad-VF.ttf")
	if err != nil {
		t.Fatal(err)
	}
	reg := text.NewRegistry()
	sh := text.NewShaper(reg)
	u := ui.New()
	u.Measure = func(f gfx.Font, s string) *text.Run { return sh.Shape(f, 2, s) }
	s := &Session{
		ui:    u,
		fonts: reg,
		queue: make(chan func(), 256),
		fetch: func(src string) ([]byte, error) {
			if src == "/fonts/var.ttf" {
				return data, nil
			}
			return nil, errors.New("not found")
		},
	}
	s.store = NewNodeStore(s)
	return s, u, sh
}

// drains the queue until cond holds or the deadline passes
func pumpUntil(t *testing.T, s *Session, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		s.Pump()
		time.Sleep(time.Millisecond)
	}
}

func mountWithFonts(label string, srcs ...string) *serverMsg {
	return &serverMsg{
		T:         wire.MsgMount,
		Root:      &nodeJSON{ID: 1, Type: "label", P: props{"text": label}},
		Resources: &resourceSpec{Fonts: []fontSpec{{Family: "Body", Srcs: srcs}}},
		Fonts:     &fontDefaults{UI: "Body"},
	}
}

func TestMountWaitsForItsFontsThenBuildsTheTree(t *testing.T) {
	s, u, sh := fontSession(t)
	plain := gfx.NewFont(14, gfx.FontOpts{})
	before := sh.Shape(plain, 2, "Hamburgefonstiv").Width

	s.handle(mountWithFonts("hi", "/fonts/var.ttf"))
	if u.Root != nil {
		t.Fatal("the tree went up before its fonts arrived")
	}
	pumpUntil(t, s, func() bool { return u.Root != nil })
	if l, ok := u.Root.(*ui.Label); !ok || l.Text != "hi" {
		t.Fatalf("root = %T, want the mounted label", u.Root)
	}
	if sh.Shape(plain, 2, "Hamburgefonstiv").Width == before {
		t.Fatal("the mount's default family did not reach text that names none")
	}
}

func TestMessagesBehindAHeldMountApplyInOrder(t *testing.T) {
	s, u, _ := fontSession(t)
	s.handle(mountWithFonts("hi", "/fonts/var.ttf"))
	s.handle(&serverMsg{T: wire.MsgPatch, Seq: 2, Ops: []patchOp{{Op: wire.OpSet, ID: 1, P: props{"text": "patched"}}}})
	pumpUntil(t, s, func() bool { return u.Root != nil })
	if l := u.Root.(*ui.Label); l.Text != "patched" {
		t.Fatalf("label text %q, want the patch that queued behind the mount", l.Text)
	}
	if s.lastSeq != 2 {
		t.Fatalf("lastSeq %d, want the replayed patch's 2", s.lastSeq)
	}
}

func TestFontThatFailsIsSkippedAndTheMountProceeds(t *testing.T) {
	s, u, _ := fontSession(t)
	s.handle(mountWithFonts("hi", "/fonts/missing.ttf"))
	pumpUntil(t, s, func() bool { return u.Root != nil })
	if s.fontsLoading != 0 {
		t.Fatalf("%d fonts still counted as loading after a failure", s.fontsLoading)
	}
}

func TestLateFontChangesMeasuredWidths(t *testing.T) {
	s, u, _ := fontSession(t)
	s.handle(&serverMsg{T: wire.MsgMount, Root: &nodeJSON{ID: 1, Type: "label", P: props{"text": "hi", "font": "Body"}}})
	l := u.Root.(*ui.Label)
	epoch := u.MeasureEpoch()
	before := u.Measure(l.Font, "Hamburgefonstiv").Width

	s.applyOp(&patchOp{Op: wire.OpResource, Fonts: []fontSpec{{Family: "Body", Srcs: []string{"/fonts/var.ttf"}}}})
	pumpUntil(t, s, func() bool { return u.MeasureEpoch() != epoch })
	if after := u.Measure(l.Font, "Hamburgefonstiv").Width; after == before {
		t.Fatal("the label still measures with the fallback face after its font arrived")
	}

	// the same src again is a no-op, nothing refetches and nothing remeasures
	epoch = u.MeasureEpoch()
	s.applyOp(&patchOp{Op: wire.OpResource, Fonts: []fontSpec{{Family: "Body", Srcs: []string{"/fonts/var.ttf"}}}})
	if s.fontsLoading != 0 {
		t.Fatal("a repeated src started a second fetch")
	}
}

func TestFontPropsReachTheWidgets(t *testing.T) {
	s, u, _ := fontSession(t)
	var measured []string
	u.Measure = func(f gfx.Font, str string) *text.Run {
		measured = append(measured, f.Family)
		return &text.Run{Width: float32(len(str)) * 7, Ascent: 10, Descent: 3}
	}
	w := s.store.Build(nodeJSON{ID: 1, Type: "label", P: props{"text": "x", "font": "Body"}})
	if l := w.(*ui.Label); l.Font.Family != "Body" {
		t.Fatalf("label family %q, want Body", l.Font.Family)
	}
	w = s.store.Build(nodeJSON{ID: 2, Type: "textfield", P: props{"font": "Code"}})
	if f := w.(*ui.TextField); f.Family != "Code" {
		t.Fatalf("textfield family %q, want Code", f.Family)
	}
	w = s.store.Build(nodeJSON{ID: 3, Type: "richtext", P: props{
		"font":  "Body",
		"spans": []any{props{"text": "one "}, props{"text": "two", "font": "Code"}},
	}})
	rt := w.(*ui.RichText)
	rt.Base().UI = u
	rt.IntrinsicSize()
	seen := map[string]bool{}
	for _, fam := range measured {
		seen[fam] = true
	}
	if !seen["Body"] || !seen["Code"] {
		t.Fatalf("spans measured with families %v, want the widget's Body and the span's Code", measured)
	}
}
