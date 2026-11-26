package text

import (
	"fmt"
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"
)

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
	s := NewShaper(NewRegistry())
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
	s := NewShaper(NewRegistry())
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
	s := NewShaper(NewRegistry())
	run := s.Shape(gfx.NewFont(14, gfx.FontOpts{}), 2, "abc אבג def")
	want := []int{0, 1, 2, 3, 6, 5, 4, 7, 8, 9, 10}
	if !intsEqual(clustersOf(run), want) {
		t.Fatalf("mixed clusters = %v, want %v", clustersOf(run), want)
	}
	if run.Width <= 0 {
		t.Fatal("mixed run has no width")
	}
}

func TestShapeMixedScriptItemizes(t *testing.T) {
	s := NewShaper(NewRegistry())
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
	if got := visualOrderForTest([]bool{true, false, false, true}, true); !intsEqual(got, []int{3, 1, 2, 0}) {
		t.Fatalf("rtl base: order = %v, want [3 1 2 0]", got)
	}
}

func TestEveryStyleHasItsOwnFace(t *testing.T) {
	seen := map[*font.Face]string{}
	for _, mono := range []bool{false, true} {
		for _, w := range []int{400, 500, 700} {
			if mono && w == 500 {
				continue
			}
			for _, italic := range []bool{false, true} {
				name := fmt.Sprintf("mono=%v weight=%d italic=%v", mono, w, italic)
				face := goFace(gfx.NewFont(13, gfx.FontOpts{Mono: mono, Weight: w, Italic: italic}))
				if prev, ok := seen[face]; ok {
					t.Fatalf("%s renders with the %s face", name, prev)
				}
				seen[face] = name
			}
		}
	}
}

func TestShapeTabAdvancesToNextStop(t *testing.T) {
	s := NewShaper(NewRegistry())
	f := gfx.NewFont(14, gfx.FontOpts{})
	space := s.Shape(f, 2, " ")
	stop := space.Width * TabSize
	for _, tc := range []struct {
		text  string
		after int
		want  float32
	}{
		{"a\tb", 2, stop},
		{"abc\td", 4, stop},
		{"\tx", 1, stop},
		{"a\t\tb", 3, 2 * stop},
	} {
		run := s.Shape(f, 2, tc.text)
		var got float32 = -1
		for _, g := range run.Glyphs {
			if g.Cluster == tc.after {
				got = g.X
			}
			if []rune(tc.text)[g.Cluster] == '\t' && g.GID != space.Glyphs[0].GID {
				t.Fatalf("%q: tab drew glyph %d, want the blank space glyph %d", tc.text, g.GID, space.Glyphs[0].GID)
			}
		}
		if abs(got-tc.want) > 0.01 {
			t.Fatalf("%q: rune %d at x=%v, want %v", tc.text, tc.after, got, tc.want)
		}
	}
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
