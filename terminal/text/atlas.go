package text

import (
	"image"
	"image/draw"
	"log"
	"math"

	"github.com/nullentropy/caution/terminal/gfx"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/vector"
)

const AtlasSize = 2048

// horizontal subpixel offsets per glyph. snapping every glyph to a whole
// device pixel keeps it crisp but quantizes the gaps, so spacing wobbles by
// up to half a pixel and reads as bad kerning. each glyph is rasterized at
// Phases offsets and the renderer picks the nearest, so spacing lands within
// 1/Phases of true and the bitmap stays sharp
const Phases = 4

// region is device pixels including a 1px transparent pad on each side
// BX/BY offset from the device-px pen position at the baseline to the quad's
// top-left
type Glyph struct {
	X0, Y0, X1, Y1 int
	W, H           int
	BX, BY         int
	// color glyphs (emoji) must not be tinted. outline rasterization never sets
	// it, it's here so the renderer contract matches the browser's when bitmap
	// glyphs arrive
	Colored bool
}

type glyphKey struct {
	font  string
	dpr   string
	gid   font.GID
	phase uint8
	emoji bool
}

// shelf-packed CPU buffer the renderer uploads wholesale when Dirty
// glyphs are white on transparent, premultiplied so every channel is the
// coverage, and the shader tints them. resets wholesale when full, cheap and
// fine until we need incremental eviction
type Atlas struct {
	// AtlasSize squared, premultiplied RGBA
	Pix []byte
	// Pix has glyphs the GPU texture hasn't seen
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

// rasterizes on miss. phase is a subpixel offset index in [0, Phases)
// emoji selects the fallback face, the GID namespace follows the face that
// shaped the run. nil means no ink (spaces), cached too
func (a *Atlas) Get(f gfx.Font, dpr float32, gid font.GID, phase int, emoji bool) *Glyph {
	key := glyphKey{f.Key, fmtDPR(dpr), gid, uint8(phase), emoji}
	if hit, ok := a.glyphs[key]; ok {
		return hit
	}

	face := FaceFor(f)
	if emoji {
		face = emojiFace
	}
	data := face.GlyphData(gid)
	outline, ok := data.(font.GlyphOutline)
	if !ok || len(outline.Segments) == 0 {
		a.glyphs[key] = nil
		return nil
	}

	// font units -> device px, y flipped (outlines are y-up), subpixel shift
	// applied before the bounding box so the box grows with it
	scale := f.Size * dpr / float32(face.Upem())
	off := float32(phase) / Phases
	tx := func(p opentype.SegmentPoint) (float32, float32) {
		return p.X*scale + off, -p.Y * scale
	}

	// conservative ink bounds from every on-curve and control point, curves
	// stay inside their control hull
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

	// 1px transparent pad all around, linear sampling bleeds into neighbors
	// otherwise
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

	// coverage, then blit as premultiplied white
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
