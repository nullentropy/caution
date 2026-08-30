package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

const (
	gridW = 64
	gridH = 26
	scale = 8
	// Frames is the flipbook length; the demo cycles /nyan/0.png ... /5.png.
	Frames = 6
)

var (
	tan      = color.NRGBA{0xf5, 0xd7, 0x9f, 0xff}
	frosting = color.NRGBA{0xf2, 0x9d, 0xd0, 0xff}
	sprinkle = color.NRGBA{0xd6, 0x53, 0xa8, 0xff}
	gray     = color.NRGBA{0x8a, 0x8a, 0x8a, 0xff}
	grayDark = color.NRGBA{0x5f, 0x5f, 0x5f, 0xff}
	blackish = color.NRGBA{0x20, 0x20, 0x20, 0xff}
	cheek    = color.NRGBA{0xf0, 0x8b, 0xb0, 0xff}
	white    = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	space    = color.NRGBA{0x06, 0x07, 0x12, 0xff}

	rainbow = []color.NRGBA{
		{0xe5, 0x3d, 0x3d, 0xff}, // red
		{0xf2, 0x8c, 0x2e, 0xff}, // orange
		{0xf5, 0xe0, 0x42, 0xff}, // yellow
		{0x54, 0xc4, 0x4d, 0xff}, // green
		{0x3d, 0x8b, 0xe5, 0xff}, // blue
		{0x8a, 0x4d, 0xc4, 0xff}, // violet
	}
)

// NyanFrame renders one flipbook frame as PNG bytes.
func NyanFrame(frame int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, gridW*scale, gridH*scale))

	cell := func(x, y int, c color.NRGBA) {
		if x < 0 || y < 0 || x >= gridW || y >= gridH {
			return
		}
		for py := 0; py < scale; py++ {
			for px := 0; px < scale; px++ {
				img.SetNRGBA(x*scale+px, y*scale+py, c)
			}
		}
	}
	fill := func(x0, y0, w, h int, c color.NRGBA) {
		for y := y0; y < y0+h; y++ {
			for x := x0; x < x0+w; x++ {
				cell(x, y, c)
			}
		}
	}

	// Space.
	fill(0, 0, gridW, gridH, space)

	// Stars: white plus-shapes drifting left one cell per frame, wrapping.
	starSeed := []struct{ x, y int }{
		{5, 3}, {17, 12}, {29, 20}, {40, 5}, {52, 15}, {60, 9}, {11, 22}, {46, 22}, {57, 2},
	}
	for i, st := range starSeed {
		x := (st.x - frame*2 + gridW) % gridW
		y := st.y
		tw := (frame + i) % 3 // twinkle: dot -> plus -> dot
		cell(x, y, white)
		if tw == 1 {
			cell(x-1, y, white)
			cell(x+1, y, white)
			cell(x, y-1, white)
			cell(x, y+1, white)
		}
	}

	bob := frame % 2 // the classic every-other-frame bounce
	bodyX, bodyY := 36, 7+bob

	// Rainbow trail: six 2-cell stripes in a square wave that scrolls with
	// the frame - the wave flips every 4 cells.
	for x := 0; x < bodyX-1; x++ {
		off := 0
		if ((x+frame*2)/4)%2 == 0 {
			off = 1
		}
		for s, c := range rainbow {
			fill(x, bodyY+s*2+off-1, 1, 2, c)
		}
	}

	// Pop-tart body: tan slab, pink frosting inset, sprinkles.
	fill(bodyX, bodyY, 18, 12, tan)
	fill(bodyX+1, bodyY+1, 16, 10, frosting)
	for i, sp := range []struct{ x, y int }{{3, 3}, {7, 2}, {11, 4}, {5, 7}, {9, 8}, {13, 7}, {2, 5}, {14, 2}} {
		if (i+frame)%4 != 0 { // a sprinkle blinks now and then
			cell(bodyX+sp.x, bodyY+sp.y, sprinkle)
		}
	}

	// Tail: a gray zigzag flapping behind the tart.
	tailY := bodyY + 5
	if frame%2 == 0 {
		fill(bodyX-4, tailY-1, 4, 2, gray)
		fill(bodyX-6, tailY-3, 3, 2, gray)
	} else {
		fill(bodyX-4, tailY, 4, 2, gray)
		fill(bodyX-6, tailY+2, 3, 2, gray)
	}

	// Legs: four stubs shuffling with the frame.
	legOff := frame % 2
	for i := 0; i < 4; i++ {
		fill(bodyX+1+i*5+legOff, bodyY+12, 2, 2, gray)
	}

	// Head: gray blob overlapping the tart's right edge, ears, face.
	headX, headY := bodyX+12, bodyY+2
	fill(headX, headY, 10, 8, gray)
	fill(headX+1, headY-1, 8, 1, gray)
	cell(headX, headY-2, grayDark) // left ear
	cell(headX+1, headY-1, gray)
	cell(headX+9, headY-2, grayDark) // right ear
	cell(headX+8, headY-1, gray)
	// Eyes (with glints), cheeks, mouth.
	cell(headX+2, headY+2, blackish)
	cell(headX+7, headY+2, blackish)
	cell(headX+2, headY+1, white)
	cell(headX+7, headY+1, white)
	cell(headX+1, headY+4, cheek)
	cell(headX+8, headY+4, cheek)
	cell(headX+4, headY+4, blackish)
	cell(headX+5, headY+5, blackish)
	cell(headX+6, headY+4, blackish)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // deterministic in-memory encode; cannot fail at runtime
	}
	return buf.Bytes()
}
