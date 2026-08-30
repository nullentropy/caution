package text

import (
	"testing"

	"github.com/nullentropy/caution/go/terminal/gfx"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/shaping"
)

// clustersOf extracts the cluster sequence in glyph (visual) order.
func clustersOf(r *Run) []int {
	out := make([]int, len(r.Glyphs))
	for i, g := range r.Glyphs {
		out[i] = g.Cluster
	}
	return out
}

func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestShapeLatinStaysSingleRunAscending(t *testing.T) {
	s := NewShaper()
	run := s.Shape(gfx.NewFont(14, gfx.FontOpts{}), 2, "Hello, world")
	if len(run.Glyphs) == 0 || run.Width <= 0 {
		t.Fatalf("empty shape: %+v", run)
	}
	for i := 1; i < len(run.Glyphs); i++ {
		if run.Glyphs[i].Cluster < run.Glyphs[i-1].Cluster {
			t.Fatalf("latin clusters must ascend: %v", clustersOf(run))
		}
		if run.Glyphs[i].X < run.Glyphs[i-1].X {
			t.Fatalf("x positions must ascend: glyph %d", i)
		}
	}
}

func TestShapeHebrewReversesToVisualOrder(t *testing.T) {
	// The embedded Go fonts have no Hebrew coverage - glyphs are .notdef -
	// but the bidi machinery is font-independent: an RTL run's glyphs come
	// back in visual order, so clusters DESCEND across the glyph array while
	// x positions ascend.
	s := NewShaper()
	run := s.Shape(gfx.NewFont(14, gfx.FontOpts{}), 2, "שלום")
	if len(run.Glyphs) != 4 {
		t.Fatalf("got %d glyphs, want 4: %v", len(run.Glyphs), clustersOf(run))
	}
	if !intsEqual(clustersOf(run), []int{3, 2, 1, 0}) {
		t.Fatalf("hebrew clusters = %v, want [3 2 1 0]", clustersOf(run))
	}
	for i := 1; i < len(run.Glyphs); i++ {
		if run.Glyphs[i].X < run.Glyphs[i-1].X {
			t.Fatalf("x positions must still ascend visually: glyph %d", i)
		}
	}
}

func TestShapeMixedDirectionOrdersRunsVisually(t *testing.T) {
	// "abc אבג def" under an LTR base: the Hebrew segment renders between
	// the Latin segments, internally right-to-left. Runes:
	//   a=0 b=1 c=2 ' '=3 א=4 ב=5 ג=6 ' '=7 d=8 e=9 f=10
	s := NewShaper()
	run := s.Shape(gfx.NewFont(14, gfx.FontOpts{}), 2, "abc אבג def")
	want := []int{0, 1, 2, 3, 6, 5, 4, 7, 8, 9, 10}
	if !intsEqual(clustersOf(run), want) {
		t.Fatalf("mixed clusters = %v, want %v", clustersOf(run), want)
	}
	// Every rune boundary maps to one cluster. Total width is the
	// sum of advances regardless of ordering.
	if run.Width <= 0 {
		t.Fatal("mixed run has no width")
	}
}

func TestShapeMixedScriptItemizes(t *testing.T) {
	// Latin + Greek + Cyrillic - scripts the embedded fonts really cover.
	// Itemization must not perturb order or drop glyphs for LTR scripts.
	s := NewShaper()
	text := "abc αβγ абв"
	run := s.Shape(gfx.NewFont(14, gfx.FontOpts{}), 2, text)
	if len(run.Glyphs) != len([]rune(text)) {
		t.Fatalf("got %d glyphs, want %d", len(run.Glyphs), len([]rune(text)))
	}
	for i, g := range run.Glyphs {
		if g.Cluster != i {
			t.Fatalf("LTR multi-script clusters must stay identity: %v", clustersOf(run))
		}
		if g.GID == 0 {
			t.Fatalf("glyph %d (%q) is .notdef - Go fonts should cover it", i, string([]rune(text)[i]))
		}
	}
}

func visualOrderForTest(rtl []bool, baseRTL bool) []int {
	inputs := make([]shaping.Input, len(rtl))
	for i, r := range rtl {
		if r {
			inputs[i].Direction = di.DirectionRTL
		} else {
			inputs[i].Direction = di.DirectionLTR
		}
	}
	base := di.DirectionLTR
	if baseRTL {
		base = di.DirectionRTL
	}
	return visualOrder(inputs, base)
}

func TestVisualOrderTwoLevel(t *testing.T) {
	// Pure function check of the run reorder rule, mirroring go-text's
	// computeBidiOrdering: base-direction runs anchor, maximal
	// opposite-direction sequences reverse.
	cases := []struct {
		name string
		rtl  []bool
		want []int
	}{
		{"all LTR", []bool{false, false, false}, []int{0, 1, 2}},
		{"one RTL between", []bool{false, true, false}, []int{0, 1, 2}},
		{"two adjacent RTL swap", []bool{false, true, true, false}, []int{0, 2, 1, 3}},
		{"leading RTL pair", []bool{true, true, false}, []int{1, 0, 2}},
		{"all RTL", []bool{true, true, true}, []int{2, 1, 0}},
	}
	for _, c := range cases {
		if got := visualOrderForTest(c.rtl, false); !intsEqual(got, c.want) {
			t.Fatalf("%s: order = %v, want %v", c.name, got, c.want)
		}
	}
	// RTL base mirrors everything; embedded LTR sequences reverse back.
	if got := visualOrderForTest([]bool{true, false, false, true}, true); !intsEqual(got, []int{3, 1, 2, 0}) {
		t.Fatalf("rtl base: order = %v, want [3 1 2 0]", got)
	}
}
