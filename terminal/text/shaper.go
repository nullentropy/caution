// go-text Faces cache without locks
package text

import (
	"github.com/nullentropy/caution/terminal/gfx"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

type Placed struct {
	GID     font.GID
	X       float32
	Cluster int
	Face    *font.Face
}

const TabSize = 8

func NextTabStop(x, stop float32) float32 {
	if stop <= 0 {
		return x
	}
	return (float32(int(x/stop)) + 1) * stop
}

type Run struct {
	Glyphs  []Placed
	Width   float32
	Ascent  float32
	Descent float32
}

type Shaper struct {
	reg *Registry
	gen int
	seg shaping.Segmenter
	// go-text caches its harfbuzz font per parsed *font.Font, so faces that
	// share a file but differ in variation coords each need their own shaper
	hbs   map[*font.Face]*shaping.HarfbuzzShaper
	cache map[string]*Run
}

var emojiFace *font.Face

type fallbackFontmap struct {
	chain []*font.Face
	force map[rune]bool
}

func (m fallbackFontmap) ResolveFace(r rune) *font.Face {
	if m.force[r] {
		if _, ok := emojiFace.NominalGlyph(r); ok {
			return emojiFace
		}
	}
	for _, face := range m.chain {
		if _, ok := face.NominalGlyph(r); ok {
			return face
		}
	}
	return m.chain[0]
}

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

func NewShaper(reg *Registry) *Shaper {
	return &Shaper{reg: reg, hbs: map[*font.Face]*shaping.HarfbuzzShaper{}, cache: make(map[string]*Run)}
}

func fixed2f(v fixed.Int26_6) float32 { return float32(v) / 64 }

func (s *Shaper) sync() {
	if s.gen != s.reg.gen {
		s.gen = s.reg.gen
		clear(s.cache)
		clear(s.hbs)
	}
}

func (s *Shaper) hb(face *font.Face) *shaping.HarfbuzzShaper {
	h, ok := s.hbs[face]
	if !ok {
		h = &shaping.HarfbuzzShaper{}
		s.hbs[face] = h
	}
	return h
}

func (s *Shaper) Shape(f gfx.Font, dpr float32, textStr string) *Run {
	s.sync()
	key := f.Key + "|" + fmtDPR(dpr) + "|" + textStr
	if hit, ok := s.cache[key]; ok {
		return hit
	}
	if len(s.cache) > 4000 {
		s.cache = make(map[string]*Run)
	}

	chain := s.reg.chain(f)
	face := chain[0]
	sizeDev := fixed.Int26_6(f.Size*dpr*64 + 0.5)

	runes := []rune(textStr)
	run := &Run{}
	if len(runes) == 0 {
		if ext, ok := face.FontHExtents(); ok {
			scale := f.Size * dpr / float32(face.Upem())
			run.Ascent = ext.Ascender * scale / dpr
			run.Descent = -ext.Descender * scale / dpr
		}
		s.cache[key] = run
		return run
	}

	inputs := s.seg.Split(shaping.Input{
		Text:      runes,
		RunStart:  0,
		RunEnd:    len(runes),
		Direction: di.DirectionLTR,
		Face:      face,
		Size:      sizeDev,
		Script:    language.Latin,
		Language:  language.NewLanguage("en"),
	}, fallbackFontmap{chain: chain, force: emojiForced(runes)})

	outs := make([]shaping.Output, len(inputs))
	total := 0
	for i := range inputs {
		outs[i] = s.hb(inputs[i].Face).Shape(inputs[i])
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

	var space *Run
	pen := fixed.Int26_6(0)
	run.Glyphs = make([]Placed, 0, total)
	for _, li := range visualOrder(inputs, di.DirectionLTR) {
		for _, g := range outs[li].Glyphs {
			if runes[g.ClusterIndex] == '\t' {
				// fonts have no tab glyph and harfbuzz would draw .notdef
				if space == nil {
					space = s.Shape(f, dpr, " ")
				}
				x := fixed2f(pen) / dpr
				run.Glyphs = append(run.Glyphs, Placed{
					GID:     space.Glyphs[0].GID,
					X:       x,
					Cluster: g.ClusterIndex,
					Face:    space.Glyphs[0].Face,
				})
				pen = fixed.Int26_6(NextTabStop(x, space.Width*TabSize)*dpr*64 + 0.5)
				continue
			}
			run.Glyphs = append(run.Glyphs, Placed{
				GID:     g.GlyphID,
				X:       fixed2f(pen+g.XOffset) / dpr,
				Cluster: g.ClusterIndex,
				Face:    inputs[li].Face,
			})
			pen += g.XAdvance
		}
	}
	run.Width = fixed2f(pen) / dpr

	s.cache[key] = run
	return run
}

func visualOrder(inputs []shaping.Input, base di.Direction) []int {
	n := len(inputs)
	vi := make([]int, n)
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
	return string(rune('0' + int(dpr*4)))
}
