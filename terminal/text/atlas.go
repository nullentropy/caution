package text

import (
	"image"
	"image/draw"
	"log"
	"math"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/vector"
)

const AtlasSize = 2048

const Phases = 4

type Glyph struct {
	X0, Y0, X1, Y1 int
	W, H           int
	BX, BY         int
	Colored        bool
}

type glyphKey struct {
	face  *font.Face
	size  float32
	dpr   string
	gid   font.GID
	phase uint8
}

type Atlas struct {
	Pix     []byte
	OnReset func()

	dirtyY0, dirtyY1 int

	reg        *Registry
	gen        int
	glyphs     map[glyphKey]*Glyph
	x, y, rowH int
}

func NewAtlas(reg *Registry) *Atlas {
	return &Atlas{
		Pix:     make([]byte, AtlasSize*AtlasSize*4),
		reg:     reg,
		gen:     reg.gen,
		glyphs:  make(map[glyphKey]*Glyph),
		x:       1,
		y:       1,
		dirtyY0: 0,
		dirtyY1: AtlasSize,
	}
}

func (a *Atlas) TakeDirty() (y0, y1 int) {
	y0, y1 = a.dirtyY0, a.dirtyY1
	a.dirtyY0, a.dirtyY1 = 0, 0
	return y0, y1
}

func (a *Atlas) markDirty(y0, y1 int) {
	y0, y1 = max(0, y0), min(AtlasSize, y1)
	if a.dirtyY1 <= a.dirtyY0 {
		a.dirtyY0, a.dirtyY1 = y0, y1
		return
	}
	a.dirtyY0, a.dirtyY1 = min(a.dirtyY0, y0), max(a.dirtyY1, y1)
}

func (a *Atlas) Get(face *font.Face, size, dpr float32, gid font.GID, phase int) *Glyph {
	if a.gen != a.reg.gen {
		a.gen = a.reg.gen
		a.Reset()
	}
	key := glyphKey{face, size, fmtDPR(dpr), gid, uint8(phase)}
	if hit, ok := a.glyphs[key]; ok {
		return hit
	}

	data := face.GlyphData(gid)
	outline, ok := data.(font.GlyphOutline)
	if !ok || len(outline.Segments) == 0 {
		a.glyphs[key] = nil
		return nil
	}

	// outlines are y-up
	scale := size * dpr / float32(face.Upem())
	off := float32(phase) / Phases
	tx := func(p opentype.SegmentPoint) (float32, float32) {
		return p.X*scale + off, -p.Y * scale
	}

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
		return a.Get(face, size, dpr, gid, phase)
	}

	gx, gy := a.x, a.y
	a.x += w + 1
	if h > a.rowH {
		a.rowH = h
	}

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
	a.markDirty(gy-1, gy+h+1)
	return g
}

func (a *Atlas) Reset() {
	if a.OnReset != nil {
		a.OnReset()
	}
	clear(a.glyphs)
	clear(a.Pix)
	a.x, a.y, a.rowH = 1, 1, 0
	a.markDirty(0, AtlasSize)
}
