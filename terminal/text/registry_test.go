package text

import (
	"os"
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gosmallcaps"
	"golang.org/x/image/font/gofont/gosmallcapsitalic"
)

func parseTestFont(t *testing.T, data []byte) *FontFile {
	t.Helper()
	f, err := ParseFont(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func variableTestFont(t *testing.T) *FontFile {
	t.Helper()
	data, err := os.ReadFile("testdata/Estedad-VF.ttf")
	if err != nil {
		t.Fatal(err)
	}
	return parseTestFont(t, data)
}

func width(s *Shaper, f gfx.Font, str string) float32 { return s.Shape(f, 2, str).Width }

const sample = "Hamburgefonstiv 0123"

func TestRegisteredFamilyResolvesByWeightAndItalic(t *testing.T) {
	reg := NewRegistry()
	reg.Add("custom", parseTestFont(t, gosmallcaps.TTF))
	reg.Add("custom", parseTestFont(t, gobold.TTF))
	reg.Add("custom", parseTestFont(t, gosmallcapsitalic.TTF))
	s := NewShaper(reg)

	only := NewRegistry()
	only.Add("sc", parseTestFont(t, gosmallcaps.TTF))
	only.Add("sci", parseTestFont(t, gosmallcapsitalic.TTF))
	ref := NewShaper(only)

	regular := width(s, gfx.NewFont(14, gfx.FontOpts{Family: "custom"}), sample)
	if want := width(ref, gfx.NewFont(14, gfx.FontOpts{Family: "sc"}), sample); regular != want {
		t.Fatalf("weight 400 shaped at %v, the family's regular file gives %v", regular, want)
	}
	if goWidth := width(s, gfx.NewFont(14, gfx.FontOpts{}), sample); regular == goWidth {
		t.Fatal("the custom family shaped identically to the built-in face")
	}
	bold := width(s, gfx.NewFont(14, gfx.FontOpts{Family: "custom", Weight: 700}), sample)
	if want := width(s, gfx.NewFont(14, gfx.FontOpts{Weight: 700}), sample); bold != want {
		t.Fatalf("weight 700 shaped at %v, the family's bold file (the Go bold face) gives %v", bold, want)
	}
	// 600 sits between the files, the nearer range wins
	if near := width(s, gfx.NewFont(14, gfx.FontOpts{Family: "custom", Weight: 600}), sample); near != bold {
		t.Fatalf("weight 600 shaped at %v, want the bold file's %v", near, bold)
	}
	italic := width(s, gfx.NewFont(14, gfx.FontOpts{Family: "custom", Italic: true, Weight: 700}), sample)
	if want := width(ref, gfx.NewFont(14, gfx.FontOpts{Family: "sci"}), sample); italic != want {
		t.Fatalf("italic shaped at %v, the family's only italic file gives %v", italic, want)
	}
}

func TestVariableFontShapesAtTheExactWeight(t *testing.T) {
	reg := NewRegistry()
	reg.Add("var", variableTestFont(t))
	s := NewShaper(reg)
	at := func(w int) float32 { return width(s, gfx.NewFont(14, gfx.FontOpts{Family: "var", Weight: w}), sample) }
	w400, w650, w700 := at(400), at(650), at(700)
	if !(w400 < w650 && w650 < w700) {
		t.Fatalf("widths must grow with weight: 400=%v 650=%v 700=%v", w400, w650, w700)
	}
	// outside the axis range clamps to its end instead of failing
	if at(2000) != at(900) {
		t.Fatalf("weight 2000 shaped at %v, want the axis maximum's %v", at(2000), at(900))
	}
}

func TestUnknownFamilyFallsBackToTheBuiltInFace(t *testing.T) {
	s := NewShaper(NewRegistry())
	f := gfx.NewFont(14, gfx.FontOpts{Family: "nope", Weight: 700, Italic: true})
	run := s.Shape(f, 2, sample)
	if run.Width != width(s, gfx.NewFont(14, gfx.FontOpts{Weight: 700, Italic: true}), sample) {
		t.Fatal("an unknown family must shape like the built-in face of the same style")
	}
	for _, g := range run.Glyphs {
		if g.Face != goFace(f) {
			t.Fatal("an unknown family must resolve to the built-in face")
		}
	}
}

func TestRuneMissingFromTheAppFaceFallsBack(t *testing.T) {
	reg := NewRegistry()
	reg.Add("var", variableTestFont(t))
	s := NewShaper(reg)
	f := gfx.NewFont(14, gfx.FontOpts{Family: "var"})
	// the test font has Latin but no Greek, the Go face has both
	run := s.Shape(f, 2, "aα")
	if len(run.Glyphs) != 2 {
		t.Fatalf("got %d glyphs, want 2", len(run.Glyphs))
	}
	if run.Glyphs[0].Face == goFace(f) {
		t.Fatal("a covered rune must shape with the app face")
	}
	if run.Glyphs[1].Face != goFace(f) || run.Glyphs[1].GID == 0 {
		t.Fatalf("an uncovered rune must shape with the built-in face, got face=%p gid=%d", run.Glyphs[1].Face, run.Glyphs[1].GID)
	}
}

func TestDefaultsApplyToTextNamingNoFamily(t *testing.T) {
	reg := NewRegistry()
	reg.Add("var", variableTestFont(t))
	s := NewShaper(reg)
	plain, mono := gfx.NewFont(14, gfx.FontOpts{}), gfx.NewFont(14, gfx.FontOpts{Mono: true})
	before, monoBefore := width(s, plain, sample), width(s, mono, sample)
	reg.SetDefaults("var", "")
	if got := width(s, plain, sample); got == before || got != width(s, gfx.NewFont(14, gfx.FontOpts{Family: "var"}), sample) {
		t.Fatalf("after SetDefaults plain text shaped at %v, want the family's width", got)
	}
	if width(s, mono, sample) != monoBefore {
		t.Fatal("a ui default must leave mono text on its own default")
	}
	reg.SetDefaults("", "")
	if width(s, plain, sample) != before {
		t.Fatal("clearing the default must restore the built-in face")
	}
}

func TestFontArrivalDropsShapedRunsAndRasterizedGlyphs(t *testing.T) {
	reg := NewRegistry()
	s := NewShaper(reg)
	a := NewAtlas(reg)
	f := gfx.NewFont(14, gfx.FontOpts{Family: "late"})
	run := s.Shape(f, 2, sample)
	g := a.Get(run.Glyphs[0].Face, f.Size, 2, run.Glyphs[0].GID, 0)
	if g == nil {
		t.Fatal("no glyph rasterized")
	}
	a.Dirty = false

	reg.Add("late", variableTestFont(t))
	after := s.Shape(f, 2, sample)
	if after.Width == run.Width {
		t.Fatal("a cached run survived the font's arrival")
	}
	a.Get(after.Glyphs[0].Face, f.Size, 2, after.Glyphs[0].GID, 0)
	if !a.Dirty {
		t.Fatal("the atlas kept glyphs rasterized before the font arrived")
	}
}
