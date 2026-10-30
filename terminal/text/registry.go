package text

import (
	"bytes"
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

// -- built-in faces -----------------------------------------------------------

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

// weights snap to the nearest of regular/medium/bold, which covers the
// protocol's practical range
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

// -- app faces ----------------------------------------------------------------

var (
	wghtTag = opentype.MustNewTag("wght")
	fvarTag = opentype.MustNewTag("fvar")
)

// one parsed file. a static file covers one weight, a variable file its fvar
// wght range. go-text keeps variation coords on the Face, so each weight
// drawn gets its own Face over the shared Font
type FontFile struct {
	font       *font.Font
	italic     bool
	minW, maxW int
	variable   bool
	faces      map[int]*font.Face
}

// weight, italic and the wght range come from the file. safe off the main
// thread, nothing here touches a registry
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

// distance from a weight to the file's range, 0 when inside it
func (ff *FontFile) distance(weight int) int {
	switch {
	case weight < ff.minW:
		return ff.minW - weight
	case weight > ff.maxW:
		return weight - ff.maxW
	}
	return 0
}

// family -> files, plus the default families for text that names none.
// main thread only, like everything else in the package
type Registry struct {
	families map[string][]*FontFile
	ui, mono string
	// bumped whenever a lookup could resolve differently. the shaper and
	// atlas compare against it and drop their caches
	gen int
}

func NewRegistry() *Registry {
	return &Registry{families: map[string][]*FontFile{}}
}

func (r *Registry) Add(family string, f *FontFile) {
	r.families[family] = append(r.families[family], f)
	r.gen++
}

// "" means the built-in Go faces
func (r *Registry) SetDefaults(ui, mono string) {
	if r.ui == ui && r.mono == mono {
		return
	}
	r.ui, r.mono = ui, mono
	r.gen++
}

func (r *Registry) Gen() int { return r.gen }

// the family's closest file for the style: italic match first, then the
// file whose range holds the weight, else the nearest range. nil when the
// family is unknown or not loaded yet
func (r *Registry) appFace(f gfx.Font) *font.Face {
	family := f.Family
	if family == "" {
		family = r.ui
		if f.Mono {
			family = r.mono
		}
	}
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

// per-rune fallback order: the app face if any, the Go face for the same
// style, then emoji
func (r *Registry) chain(f gfx.Font) []*font.Face {
	chain := make([]*font.Face, 0, 3)
	if face := r.appFace(f); face != nil {
		chain = append(chain, face)
	}
	return append(chain, goFace(f), emojiFace)
}
