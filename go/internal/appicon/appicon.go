package appicon

import (
	"image"
	"math"
	"strings"

	xdraw "golang.org/x/image/draw"
)

func Default() *image.RGBA {
	const size = 1024
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	const (
		inset  = 100.0
		radius = 180.0
		cx     = size / 2.0
		cy     = size / 2.0
	)
	tile := [4]uint8{0x16, 0x18, 0x1d, 0xff}
	rings := [][3]uint8{{0x4f, 0x8c, 0xff}, {0xa7, 0x8b, 0xfa}, {0x22, 0xc5, 0x5e}, {0x16, 0x18, 0x1d}}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Rounded-rect SDF for the tile.
			px := math.Max(math.Abs(float64(x)+0.5-cx)-(cx-inset-radius), 0)
			py := math.Max(math.Abs(float64(y)+0.5-cy)-(cy-inset-radius), 0)
			d := math.Hypot(px, py) - radius
			if d > 0.5 {
				continue
			}
			c := tile
			// Concentric rings, 80px bands from the center out.
			if dist := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy); dist <= 320 {
				band := rings[int(dist/80)%len(rings)]
				c = [4]uint8{band[0], band[1], band[2], 0xff}
			}
			a := 1.0
			if d > -0.5 { // edge AA
				a = 0.5 - d
			}
			o := img.PixOffset(x, y)
			img.Pix[o] = uint8(float64(c[0]) * a)
			img.Pix[o+1] = uint8(float64(c[1]) * a)
			img.Pix[o+2] = uint8(float64(c[2]) * a)
			img.Pix[o+3] = uint8(float64(c[3]) * a)
		}
	}
	return img
}

// buildICNS packs PNG renditions into the .icns container: "icns" + total
// length, then type+length+PNG chunks. macOS accepts PNG payloads for the
// ic07...ic14 types.

func ToRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok {
		return r
	}
	out := image.NewRGBA(img.Bounds())
	xdraw.Copy(out, image.Point{}, img, img.Bounds(), xdraw.Src, nil)
	return out
}

// defaultIcon draws the caution mark: a dark rounded tile with the demo's
// concentric rings - recognizable, and no asset to ship.

func Slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// Scale resizes with CatmullRom - icons are downscaled from one 1024px
// master, and a soft filter beats nearest-neighbour crunch at 16px.
func Scale(master *image.RGBA, size int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(out, out.Bounds(), master, master.Bounds(), xdraw.Src, nil)
	return out
}
