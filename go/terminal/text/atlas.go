package text

import (
	"image"
	"image/draw"
	"log"
	"math"

	"github.com/nullentropy/caution/go/terminal/gfx"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/vector"
)

const AtlasSize = 2048

// Phases is the number of horizontal subpixel offsets per glyph. Snapping
// every glyph to a whole device pixel keeps each one crisp but quantizes the
// gaps between them, so inter-letter spacing wobbles by up to half a pixel
// and the text reads as badly kerned. Instead each glyph is rasterized at
// Phases fractional offsets and the renderer picks the nearest: spacing lands
// within 1/Phases of its true position, and the bitmap is still rasterized
// where it's drawn, so it stays sharp.
const Phases = 4

// Glyph is an atlas entry. Region coordinates are device pixels and include
// a 1px transparent pad on each side; BX/BY offset from the (device-px) pen
// position at the baseline to the quad's top-left.
type Glyph struct {
	X0, Y0, X1, Y1 int
	W, H           int
	BX, BY         int
	// Colored marks color glyphs (emoji), which must not be tinted. Outline
	// rasterization never produces these; the field exists so the renderer
	// contract matches the browser terminal's when bitmap glyphs arrive.
	Colored bool
}

type glyphKey struct {
	font  string
	dpr   string
	gid   font.GID
	phase uint8
	emoji bool
}

// Atlas rasterizes glyph outlines into a shelf-packed CPU pixel buffer that
// the renderer uploads wholesale when Dirty. Glyphs are drawn white on
// transparent (premultiplied, so every channel is the coverage value); the
// shader tints them. When full it resets wholesale - cheap, and acceptable
// until incremental eviction is needed.
type Atlas struct {
	// Pix is AtlasSize squared premultiplied RGBA.
	Pix []byte
	// Dirty is set when Pix has glyphs the GPU texture hasn't seen.
	Dirty bool

	glyphs     map[glyphKey]*Glyph
	x, y, rowH int
}

func NewAtlas() *Atlas {
	return &Atlas{
		Pix:    make([]byte, AtlasSize*AtlasSize*4),
		glyphs: make(map[glyphKey]*Glyph),
		x:      1,
		y:      1,
	}
}

// Get returns the atlas entry for a glyph, rasterizing on miss. phase is a
// subpixel offset index in [0, Phases). emoji selects the fallback face -
// the GID namespace follows the face that shaped the run. nil means "no
// ink" (spaces) - the miss is cached too.
func (a *Atlas) Get(f gfx.Font, dpr float32, gid font.GID, phase int, emoji bool) *Glyph {
	key := glyphKey{f.Key, fmtDPR(dpr), gid, uint8(phase), emoji}
	if hit, ok := a.glyphs[key]; ok {
		return hit
	}

	face := FaceFor(f)
	if emoji && emojiFace != nil {
		face = emojiFace
	}
	data := face.GlyphData(gid)
	outline, ok := data.(font.GlyphOutline)
	if !ok || len(outline.Segments) == 0 {
		a.glyphs[key] = nil
		return nil
	}

	// Font units -> device px, y flipped (outlines are y-up), with the
	// subpixel shift applied before the bounding box so the box grows with it.
	scale := f.Size * dpr / float32(face.Upem())
	off := float32(phase) / Phases
	tx := func(p opentype.SegmentPoint) (float32, float32) {
		return p.X*scale + off, -p.Y * scale
	}

	// Conservative ink bounds: every on-curve and control point (curves stay
	// inside their control hull).
	minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, seg := range outline.Segments {
		for _, p := range seg.ArgsSlice() {
			x, y := tx(p)
			minX = min(minX, x)
			minY = min(minY, y)
			maxX = max(maxX, x)
			maxY = max(maxY, y)
		}
	}
	if !(maxX > minX) || !(maxY > minY) {
		a.glyphs[key] = nil
		return nil
	}

	// Integer box with a 1px transparent pad all around (linear sampling
	// bleeds into neighbors otherwise).
	x0 := int(math.Floor(float64(minX))) - 1
	y0 := int(math.Floor(float64(minY))) - 1
	w := int(math.Ceil(float64(maxX))) - x0 + 1
	h := int(math.Ceil(float64(maxY))) - y0 + 1
	if w > AtlasSize-2 || h > AtlasSize-2 {
		return nil
	}

	if a.x+w+1 > AtlasSize {
		a.x = 1
		a.y += a.rowH + 1
		a.rowH = 0
	}
	if a.y+h+1 > AtlasSize {
		log.Println("caution: glyph atlas full, resetting")
		a.Reset()
		return a.Get(f, dpr, gid, phase, emoji)
	}

	gx, gy := a.x, a.y
	a.x += w + 1
	if h > a.rowH {
		a.rowH = h
	}

	// Rasterize coverage, then blit as premultiplied white.
	ras := vector.NewRasterizer(w, h)
	ras.DrawOp = draw.Src
	open := false
	for _, seg := range outline.Segments {
		switch seg.Op {
		case opentype.SegmentOpMoveTo:
			if open {
				ras.ClosePath()
			}
			x, y := tx(seg.Args[0])
			ras.MoveTo(x-float32(x0), y-float32(y0))
			open = true
		case opentype.SegmentOpLineTo:
			x, y := tx(seg.Args[0])
			ras.LineTo(x-float32(x0), y-float32(y0))
		case opentype.SegmentOpQuadTo:
			cx, cy := tx(seg.Args[0])
			x, y := tx(seg.Args[1])
			ras.QuadTo(cx-float32(x0), cy-float32(y0), x-float32(x0), y-float32(y0))
		case opentype.SegmentOpCubeTo:
			c1x, c1y := tx(seg.Args[0])
			c2x, c2y := tx(seg.Args[1])
			x, y := tx(seg.Args[2])
			ras.CubeTo(c1x-float32(x0), c1y-float32(y0), c2x-float32(x0), c2y-float32(y0), x-float32(x0), y-float32(y0))
		}
	}
	if open {
		ras.ClosePath()
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	ras.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})

	for py := 0; py < h; py++ {
		row := (gy+py)*AtlasSize + gx
		for px := 0; px < w; px++ {
			cov := mask.Pix[py*mask.Stride+px]
			o := (row + px) * 4
			a.Pix[o] = cov
			a.Pix[o+1] = cov
			a.Pix[o+2] = cov
			a.Pix[o+3] = cov
		}
	}

	g := &Glyph{
		X0: gx, Y0: gy, X1: gx + w, Y1: gy + h,
		W: w, H: h,
		BX: x0, BY: y0,
	}
	a.glyphs[key] = g
	a.Dirty = true
	return g
}

func (a *Atlas) Reset() {
	clear(a.glyphs)
	clear(a.Pix)
	a.x, a.y, a.rowH = 1, 1, 0
	a.Dirty = true
}
