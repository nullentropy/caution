// Package text is the native terminal's text stack: go-text/typesetting
// shaping (kerning, ligatures, mark placement) and outline rasterization into
// a shelf-packed atlas with subpixel phases
//
// fonts are embedded, not discovered, so native pixels are deterministic
// across machines
//
// not safe for concurrent use. the terminal renders on one thread and
// go-text Faces cache internally without locks
package text

import (
	"bytes"
	"sync"

	"github.com/nullentropy/caution/terminal/gfx"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomediumitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

type faceStyle struct {
	mono, italic bool
	// 400, 500 or 700. mono has no medium
	weight int
}

var faceTTFs = map[faceStyle][]byte{
	{weight: 400}:                           goregular.TTF,
	{weight: 400, italic: true}:             goitalic.TTF,
	{weight: 500}:                           gomedium.TTF,
	{weight: 500, italic: true}:             gomediumitalic.TTF,
	{weight: 700}:                           gobold.TTF,
	{weight: 700, italic: true}:             gobolditalic.TTF,
	{mono: true, weight: 400}:               gomono.TTF,
	{mono: true, weight: 400, italic: true}: gomonoitalic.TTF,
	{mono: true, weight: 700}:               gomonobold.TTF,
	{mono: true, weight: 700, italic: true}: gomonobolditalic.TTF,
}

var (
	facesMu sync.Mutex
	faces   = map[faceStyle]*font.Face{}
)

func mustParse(ttf []byte) *font.Face {
	f, err := font.ParseTTF(bytes.NewReader(ttf))
	if err != nil {
		panic("caution: embedded font failed to parse: " + err.Error())
	}
	return f
}

// weights snap to the nearest of regular/medium/bold, which covers the
// protocol's practical range
func FaceFor(f gfx.Font) *font.Face {
	st := faceStyle{mono: f.Mono, italic: f.Italic, weight: 400}
	switch {
	case f.Weight >= 650:
		st.weight = 700
	case f.Weight >= 500 && !f.Mono:
		st.weight = 500
	}
	facesMu.Lock()
	defer facesMu.Unlock()
	face, ok := faces[st]
	if !ok {
		face = mustParse(faceTTFs[st])
		faces[st] = face
	}
	return face
}

// positioned relative to the run origin
type Placed struct {
	GID font.GID
	// pen offset, logical px
	X float32
	// rune index this glyph maps back to, for caret and selection math
	Cluster int
	// GID belongs to the emoji fallback face and the atlas must rasterize
	// from it
	Emoji bool
}

type Run struct {
	Glyphs  []Placed
	Width   float32
	Ascent  float32
	Descent float32
}

// harfbuzz plus a run cache, mirrors the browser terminal's TextShaper
//
// the segmenter splits text into runs of one script and direction (Unicode
// bidi), each shapes with its own script/language/direction, and runs are
// laid out in visual order. pure LTR is one run, so the common case is
// byte-identical to unsegmented shaping
type Shaper struct {
	hb    shaping.HarfbuzzShaper
	seg   shaping.Segmenter
	cache map[string]*Run
}

// set by emoji.go's init
var emojiFace *font.Face

// per rune: the font's own face if it covers the rune, else the emoji face
// if that does, else the primary face (.notdef, deterministic tofu instead
// of a system font). default-ignorable runes (ZWJ, variation selectors)
// never force a face change so emoji sequences stay whole
//
// force carries sequence context the per-rune interface can't see. runes
// followed by a VS16 or combining keycap take the emoji face even when the
// text face covers them ("1" is a Go-font digit but "1️⃣" must ligate in the
// emoji face's GSUB)
type fallbackFontmap struct {
	primary *font.Face
	force   map[rune]bool
}

func (m fallbackFontmap) ResolveFace(r rune) *font.Face {
	if m.force[r] {
		if _, ok := emojiFace.NominalGlyph(r); ok {
			return emojiFace
		}
	}
	if _, ok := m.primary.NominalGlyph(r); ok {
		return m.primary
	}
	if _, ok := emojiFace.NominalGlyph(r); ok {
		return emojiFace
	}
	return m.primary
}

// runes before U+FE0F (VS16) or U+20E3 (combining enclosing keycap) go to
// the emoji face, and a keycap reaching back over a VS16 forces the base too
// the set is per rune VALUE, so a string mixing "1" and "1️⃣" renders both
// digits from the emoji face, accepted edge. nil when there are no markers
// (common case, zero cost)
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
		// metrics still matter for empty runs (caret height in an empty field)
		if ext, ok := face.FontHExtents(); ok {
			scale := f.Size * dpr / float32(face.Upem())
			run.Ascent = ext.Ascender * scale / dpr
			run.Descent = -ext.Descender * scale / dpr
		}
		s.cache[key] = run
		return run
	}

	// paragraph is LTR since caution UIs are LTR chrome (a per-widget `dir`
	// prop would flow in here). script and language are seeds the segmenter
	// overrides per run
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
		// runs share the full Text with RunStart/RunEnd windows, so harfbuzz
		// sees cross-run context and clusters stay absolute rune offsets
		outs[i] = s.hb.Shape(inputs[i])
		total += len(outs[i].Glyphs)
		a := fixed2f(outs[i].LineBounds.Ascent) / dpr
		d := fixed2f(outs[i].LineBounds.Descent) / dpr
		if d < 0 {
			// go-text is y-up, descenders are negative
			d = -d
		}
		run.Ascent = max(run.Ascent, a)
		run.Descent = max(run.Descent, d)
	}

	pen := fixed.Int26_6(0)
	run.Glyphs = make([]Placed, 0, total)
	for _, li := range visualOrder(inputs, di.DirectionLTR) {
		emoji := inputs[li].Face == emojiFace
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

// visual position -> logical run index, two-level bidi like go-text's
// computeBidiOrdering. base-direction runs anchor (mirrored wholesale for an
// RTL base) and each maximal opposite-direction sequence reverses in place
// full L2 reordering needs embedding levels the segmenter doesn't expose,
// two levels covers mixed text without nested embeddings
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

func fmtDPR(dpr float32) string {
	// DPRs are small round-ish numbers (1, 1.5, 2), coarse quantization is a
	// fine cache key and avoids fmt.Sprintf on the hot path
	return string(rune('0' + int(dpr*4)))
}
