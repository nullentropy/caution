package text

import (
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
)

func TestEmojiFallbackShapesRealGlyphs(t *testing.T) {
	s := NewShaper(NewRegistry())
	run := s.Shape(gfx.NewFont(14, gfx.FontOpts{}), 2, "ok ✅🚀")
	if len(run.Glyphs) == 0 {
		t.Fatal("no glyphs")
	}
	sawEmoji := 0
	for _, g := range run.Glyphs {
		if g.Face == emojiFace {
			sawEmoji++
			if g.GID == 0 {
				t.Fatal("emoji glyph shaped to .notdef despite fallback coverage")
			}
		}
	}
	if sawEmoji < 2 {
		t.Fatalf("expected both emoji to shape with the fallback face, saw %d", sawEmoji)
	}
	// latin prefix stays on the primary face
	if run.Glyphs[0].Face == emojiFace {
		t.Fatal("latin text must not shape with the emoji face")
	}
}

func TestEmojiSequenceStaysOneCluster(t *testing.T) {
	s := NewShaper(NewRegistry())
	// keycap: '1' + VS16 + COMBINING ENCLOSING KEYCAP is three runes, one glyph
	// if the fallback face's GSUB is consulted. default-ignorable runes never
	// force a face change so the sequence reaches the emoji face whole
	run := s.Shape(gfx.NewFont(14, gfx.FontOpts{}), 2, "1️⃣")
	inked := 0
	for _, g := range run.Glyphs {
		if g.GID != 0 {
			inked++
		}
	}
	if inked == 0 {
		t.Fatal("keycap sequence produced no inked glyphs")
	}
	if inked > 2 {
		t.Fatalf("keycap sequence broke apart: %d inked glyphs", inked)
	}
}

func TestEmojiGlyphsRasterizeDistinctFromPrimary(t *testing.T) {
	reg := NewRegistry()
	s := NewShaper(reg)
	a := NewAtlas(reg)
	f := gfx.NewFont(14, gfx.FontOpts{})
	run := s.Shape(f, 2, "✅")
	if len(run.Glyphs) == 0 || run.Glyphs[0].Face != emojiFace {
		t.Fatalf("expected an emoji glyph, got %+v", run.Glyphs)
	}
	g := a.Get(emojiFace, f.Size, 2, run.Glyphs[0].GID, 0)
	if g == nil || g.W == 0 || g.H == 0 {
		t.Fatalf("emoji glyph rasterized to nothing: %+v", g)
	}
	// same GID from the PRIMARY face is a different (or absent) glyph, the
	// atlas key must separate the two namespaces
	p := a.Get(goFace(f), f.Size, 2, run.Glyphs[0].GID, 0)
	if p != nil && p.X0 == g.X0 && p.Y0 == g.Y0 {
		t.Fatal("atlas conflated emoji-face and primary-face GIDs")
	}
}
