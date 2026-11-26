package text

import (
	"bytes"
	"slices"
	"sync"

	"github.com/nullentropy/caution/terminal/gfx"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
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
)

type faceStyle struct {
	mono, italic bool
	weight       int
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
	goFacesMu sync.Mutex
	goFaces   = map[faceStyle]*font.Face{}
)

func mustParse(ttf []byte) *font.Face {
	f, err := font.ParseTTF(bytes.NewReader(ttf))
	if err != nil {
		panic("caution: embedded font failed to parse: " + err.Error())
	}
	return f
}

func goFace(f gfx.Font) *font.Face {
	st := faceStyle{mono: f.Mono, italic: f.Italic, weight: 400}
	switch {
	case f.Weight >= 650:
		st.weight = 700
	case f.Weight >= 500 && !f.Mono:
		st.weight = 500
	}
	goFacesMu.Lock()
	defer goFacesMu.Unlock()
	face, ok := goFaces[st]
	if !ok {
		face = mustParse(faceTTFs[st])
		goFaces[st] = face
	}
	return face
}

var (
	wghtTag = opentype.MustNewTag("wght")
	fvarTag = opentype.MustNewTag("fvar")
)

// go-text keeps variation coords on the Face, so each weight gets its own
// Face over the shared Font
type FontFile struct {
	font       *font.Font
	italic     bool
	minW, maxW int
	variable   bool
	faces      map[int]*font.Face
}

func ParseFont(data []byte) (*FontFile, error) {
	ld, err := opentype.NewLoader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	ft, err := font.NewFont(ld)
	if err != nil {
		return nil, err
	}
	desc := ft.Describe()
	w := int(desc.Aspect.Weight)
	if w == 0 {
		w = 400
	}
	ff := &FontFile{font: ft, italic: desc.Aspect.Style == font.StyleItalic, minW: w, maxW: w, faces: map[int]*font.Face{}}
	if raw, err := ld.RawTable(fvarTag); err == nil {
		if fv, _, err := tables.ParseFvar(raw); err == nil {
			for _, ax := range fv.FvarRecords.Axis {
				if ax.Tag == wghtTag {
					ff.variable = true
					ff.minW, ff.maxW = int(ax.Minimum), int(ax.Maximum)
				}
			}
		}
	}
	return ff, nil
}

func (ff *FontFile) at(weight int) *font.Face {
	w := ff.minW
	if ff.variable {
		w = min(max(weight, ff.minW), ff.maxW)
	}
	if f, ok := ff.faces[w]; ok {
		return f
	}
	f := font.NewFace(ff.font)
	if ff.variable {
		f.SetVariations([]font.Variation{{Tag: wghtTag, Value: float32(w)}})
	}
	ff.faces[w] = f
	return f
}

func (ff *FontFile) distance(weight int) int {
	switch {
	case weight < ff.minW:
		return ff.minW - weight
	case weight > ff.maxW:
		return weight - ff.maxW
	}
	return 0
}

type Registry struct {
	families map[string][]*FontFile
	ui, mono string
	fallback []string
	gen      int
}

func NewRegistry() *Registry {
	return &Registry{families: map[string][]*FontFile{}}
}

func (r *Registry) Add(family string, f *FontFile) {
	r.families[family] = append(r.families[family], f)
	r.gen++
}

func (r *Registry) SetDefaults(ui, mono string, fallback []string) {
	if r.ui == ui && r.mono == mono && slices.Equal(r.fallback, fallback) {
		return
	}
	r.ui, r.mono, r.fallback = ui, mono, slices.Clone(fallback)
	r.gen++
}

func (r *Registry) Gen() int { return r.gen }

func (r *Registry) appFace(f gfx.Font) *font.Face {
	family := f.Family
	if family == "" {
		family = r.ui
		if f.Mono {
			family = r.mono
		}
	}
	return r.familyFace(family, f)
}

func (r *Registry) familyFace(family string, f gfx.Font) *font.Face {
	files := r.families[family]
	if len(files) == 0 {
		return nil
	}
	var best *FontFile
	bestKey := [2]int{}
	for _, ff := range files {
		key := [2]int{0, ff.distance(f.Weight)}
		if ff.italic != f.Italic {
			key[0] = 1
		}
		if best == nil || key[0] < bestKey[0] || (key[0] == bestKey[0] && key[1] < bestKey[1]) {
			best, bestKey = ff, key
		}
	}
	return best.at(f.Weight)
}

func (r *Registry) chain(f gfx.Font) []*font.Face {
	primary := r.appFace(f)
	if primary == nil {
		primary = goFace(f)
	}
	chain := append(make([]*font.Face, 0, 3+len(r.fallback)), primary)
	for _, family := range r.fallback {
		if face := r.familyFace(family, f); face != nil {
			chain = append(chain, face)
		}
	}
	if primary != goFace(f) {
		chain = append(chain, goFace(f))
	}
	return append(chain, emojiFace)
}
