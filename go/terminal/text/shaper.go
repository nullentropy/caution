// Package text is the native terminal's text stack: shaping via
// go-text/typesetting (kerning, ligatures, mark placement) and glyph
// rasterization from font outlines into a shelf-packed atlas with subpixel
// phases.
//
// Fonts are embedded, not discovered: native pixels are deterministic across
// machines because there is no system font stack underneath.
//
// Not safe for concurrent use - the terminal renders on one thread, and
// go-text Faces cache internally without locks.
package text

import (
	"bytes"
	"sync"

	"github.com/nullentropy/caution/go/terminal/gfx"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

var (
	facesOnce sync.Once
	faces     struct {
		regular, medium, bold, italic, mono *font.Face
	}
)

func mustParse(ttf []byte) *font.Face {
	f, err := font.ParseTTF(bytes.NewReader(ttf))
	if err != nil {
		panic("caution: embedded font failed to parse: " + err.Error())
	}
	return f
}

func loadFaces() {
	faces.regular = mustParse(goregular.TTF)
	faces.medium = mustParse(gomedium.TTF)
	faces.bold = mustParse(gobold.TTF)
	faces.italic = mustParse(goitalic.TTF)
	faces.mono = mustParse(gomono.TTF)
}

// FaceFor maps a font spec onto an embedded face. Weights snap to the nearest
// of regular/medium/bold, which covers the protocol's practical range.
func FaceFor(f gfx.Font) *font.Face {
	facesOnce.Do(loadFaces)
	switch {
	case f.Mono:
		return faces.mono
	case f.Italic:
		return faces.italic
	case f.Weight >= 650:
		return faces.bold
	case f.Weight >= 500:
		return faces.medium
	default:
		return faces.regular
	}
}

// Placed is one shaped glyph, positioned relative to the run origin.
type Placed struct {
	GID font.GID
	// X is the pen offset in logical px.
	X float32
	// Cluster is the rune index this glyph maps back to, for caret and
	// selection math.
	Cluster int
	// Emoji marks glyphs from the emoji fallback face - the GID belongs to
	// that face, and the atlas must rasterize from it.
	Emoji bool
}

type Run struct {
	Glyphs  []Placed
	Width   float32
	Ascent  float32
	Descent float32
}

// Shaper wraps harfbuzz shaping with a run cache, mirroring the browser
// terminal's TextShaper.
//
// Text is itemized before shaping: the segmenter splits it into runs of one
// script and direction (bidi via the Unicode algorithm), each run shapes with
// its resolved script/language/direction, and the runs are laid out in visual
// order. Pure-LTR text segments into one run, so the common case is
// byte-identical to unsegmented shaping.
type Shaper struct {
	hb    shaping.HarfbuzzShaper
	seg   shaping.Segmenter
	cache map[string]*Run
}

// emojiFace is the embedded emoji fallback (set by emoji.go's init when the
// face is embedded; nil disables fallback entirely). Monochrome by choice:
// the engine is shared with the browser via wasm, and the color Noto face
// is 10.7MB of CBDT bitmaps against the outline face's 1.9MB - emoji render
// as tintable outlines, identically in both terminals.
var emojiFace *font.Face

// fallbackFontmap resolves faces per rune for the segmenter: the font's own
// face when it covers the rune, the emoji face when it doesn't and the
// emoji face does, the primary face otherwise (.notdef - deterministic tofu
// rather than a system font). Default-ignorable runes (ZWJ, variation
// selectors) never force a face change, so emoji sequences stay whole.
//
// force carries sequence context the per-rune interface can't see: rune
// values that a VS16 or a combining keycap follows in this string must take
// the emoji face even when the text face covers them ("1" is a Go-font
// digit, but "1️⃣" must ligate inside the emoji face's GSUB).
type fallbackFontmap struct {
	primary *font.Face
	force   map[rune]bool
}

func (m fallbackFontmap) ResolveFace(r rune) *font.Face {
	if emojiFace != nil && m.force[r] {
		if _, ok := emojiFace.NominalGlyph(r); ok {
			return emojiFace
		}
	}
	if _, ok := m.primary.NominalGlyph(r); ok {
		return m.primary
	}
	if emojiFace != nil {
		if _, ok := emojiFace.NominalGlyph(r); ok {
			return emojiFace
		}
	}
	return m.primary
}

// emojiForced scans a string for emoji-presentation markers: each rune
// preceding U+FE0F (VS16) or U+20E3 (combining enclosing keycap) is forced
// onto the emoji face, and a keycap reaching back over a VS16 forces the
// base as well. The set is per rune VALUE within one string - a string
// mixing "1" and "1️⃣" renders both digits from the emoji face, an accepted
// edge. nil when the string has no markers (the common case, zero cost).
func emojiForced(runes []rune) map[rune]bool {
	var m map[rune]bool
	mark := func(r rune) {
		if m == nil {
			m = make(map[rune]bool)
		}
		m[r] = true
	}
	for i, r := range runes {
		if r != 0xFE0F && r != 0x20E3 {
			continue
		}
		if i > 0 {
			mark(runes[i-1])
			if runes[i-1] == 0xFE0F && i > 1 {
				mark(runes[i-2])
			}
		}
	}
	return m
}

func NewShaper() *Shaper {
	return &Shaper{cache: make(map[string]*Run)}
}

func fixed2f(v fixed.Int26_6) float32 { return float32(v) / 64 }

func (s *Shaper) Shape(f gfx.Font, dpr float32, textStr string) *Run {
	key := f.Key + "|" + fmtDPR(dpr) + "|" + textStr
	if hit, ok := s.cache[key]; ok {
		return hit
	}
	if len(s.cache) > 4000 {
		s.cache = make(map[string]*Run)
	}

	face := FaceFor(f)
	sizeDev := fixed.Int26_6(f.Size*dpr*64 + 0.5)

	runes := []rune(textStr)
	run := &Run{}
	if len(runes) == 0 {
		// Metrics still matter for empty runs (caret height in an empty field).
		if ext, ok := face.FontHExtents(); ok {
			scale := f.Size * dpr / float32(face.Upem())
			run.Ascent = ext.Ascender * scale / dpr
			run.Descent = -ext.Descender * scale / dpr
		}
		s.cache[key] = run
		return run
	}

	// The paragraph context is LTR: caution UIs are LTR chrome (a future
	// per-widget `dir` prop would flow in here). Script and language are
	// seeds the segmenter overrides per run.
	inputs := s.seg.Split(shaping.Input{
		Text:      runes,
		RunStart:  0,
		RunEnd:    len(runes),
		Direction: di.DirectionLTR,
		Face:      face,
		Size:      sizeDev,
		Script:    language.Latin,
		Language:  language.NewLanguage("en"),
	}, fallbackFontmap{primary: face, force: emojiForced(runes)})

	outs := make([]shaping.Output, len(inputs))
	total := 0
	for i := range inputs {
		// Runs share the full Text with RunStart/RunEnd windows, so harfbuzz
		// sees cross-run context and cluster indices stay absolute rune
		// offsets into the whole string.
		outs[i] = s.hb.Shape(inputs[i])
		total += len(outs[i].Glyphs)
		a := fixed2f(outs[i].LineBounds.Ascent) / dpr
		d := fixed2f(outs[i].LineBounds.Descent) / dpr
		if d < 0 {
			// go-text follows the y-up convention where descenders are negative.
			d = -d
		}
		run.Ascent = max32(run.Ascent, a)
		run.Descent = max32(run.Descent, d)
	}

	pen := fixed.Int26_6(0)
	run.Glyphs = make([]Placed, 0, total)
	for _, li := range visualOrder(inputs, di.DirectionLTR) {
		emoji := emojiFace != nil && inputs[li].Face == emojiFace
		for _, g := range outs[li].Glyphs {
			run.Glyphs = append(run.Glyphs, Placed{
				GID:     g.GlyphID,
				X:       fixed2f(pen+g.XOffset) / dpr,
				Cluster: g.ClusterIndex,
				Emoji:   emoji,
			})
			pen += g.XAdvance
		}
	}
	run.Width = fixed2f(pen) / dpr

	s.cache[key] = run
	return run
}

// visualOrder maps visual position -> logical run index under the two-level
// bidi approximation the go-text line wrapper also uses (its
// computeBidiOrdering): base-direction runs anchor their positions (mirrored
// wholesale for an RTL base), and each maximal sequence of opposite-direction
// runs reverses in place. Full multi-level reordering (rule L2 over embedding
// levels) would need the levels, which the segmenter's bidi pass does not
// expose - two levels covers direction-mixed text without nested embeddings.
func visualOrder(inputs []shaping.Input, base di.Direction) []int {
	n := len(inputs)
	vi := make([]int, n) // logical index -> visual position
	for i := range vi {
		if base.Progression() == di.TowardTopLeft {
			vi[i] = n - 1 - i
		} else {
			vi[i] = i
		}
	}
	swap := func(lo, hi int) {
		for ; lo < hi; lo, hi = lo+1, hi-1 {
			vi[lo], vi[hi] = vi[hi], vi[lo]
		}
	}
	start := -1
	for i := range inputs {
		if inputs[i].Direction.Progression() == base.Progression() {
			if start != -1 {
				swap(start, i-1)
				start = -1
			}
		} else if start == -1 {
			start = i
		}
	}
	if start != -1 {
		swap(start, n-1)
	}
	order := make([]int, n)
	for logical, visual := range vi {
		order[visual] = logical
	}
	return order
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func fmtDPR(dpr float32) string {
	// DPRs are small round-ish numbers (1, 1.5, 2); a coarse quantization is a
	// fine cache key and avoids fmt.Sprintf on the hot path.
	return string(rune('0' + int(dpr*4)))
}
