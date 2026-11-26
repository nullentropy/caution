package ui

import (
	"math"

	"github.com/nullentropy/caution/terminal/gfx"
)

func cellGlyph(r rune) bool {
	if r >= 0x2580 && r <= 0x259f {
		return true
	}
	_, ok := boxArms[r]
	return ok
}

func paintCellGlyph(dl *gfx.DisplayList, r rune, cell gfx.Rect, fg gfx.Color) {
	if r >= 0x2580 && r <= 0x259f {
		paintBlock(dl, r, cell, fg)
		return
	}
	paintBox(dl, boxArms[r], cell, fg)
}

func paintBlock(dl *gfx.DisplayList, r rune, c gfx.Rect, fg gfx.Color) {
	fill := func(x0, y0, x1, y1 float32) {
		x0, x1 = snap(c.X+c.W*x0), snap(c.X+c.W*x1)
		y0, y1 = snap(c.Y+c.H*y0), snap(c.Y+c.H*y1)
		if x1 > x0 && y1 > y0 {
			dl.Fill(gfx.R(x0, y0, x1-x0, y1-y0), fg, gfx.Corners{})
		}
	}
	shade := func(a float32) {
		ink := fg
		ink.A *= a
		dl.Fill(c, ink, gfx.Corners{})
	}
	const ul, ur, ll, lr = 1, 2, 4, 8
	quads := func(q int) {
		if q&ul != 0 {
			fill(0, 0, 0.5, 0.5)
		}
		if q&ur != 0 {
			fill(0.5, 0, 1, 0.5)
		}
		if q&ll != 0 {
			fill(0, 0.5, 0.5, 1)
		}
		if q&lr != 0 {
			fill(0.5, 0.5, 1, 1)
		}
	}
	switch {
	case r == 0x2580:
		fill(0, 0, 1, 0.5)
	case r >= 0x2581 && r <= 0x2588:
		fill(0, 1-float32(r-0x2580)/8, 1, 1)
	case r >= 0x2589 && r <= 0x258f:
		fill(0, 0, float32(0x2590-r)/8, 1)
	case r == 0x2590:
		fill(0.5, 0, 1, 1)
	case r == 0x2591:
		shade(0.25)
	case r == 0x2592:
		shade(0.5)
	case r == 0x2593:
		shade(0.75)
	case r == 0x2594:
		fill(0, 0, 1, 0.125)
	case r == 0x2595:
		fill(0.875, 0, 1, 1)
	default:
		quads([...]int{ll, lr, ul, ul | ll | lr, ul | lr, ul | ur | ll, ul | ur | lr, ur, ur | ll, ur | ll | lr}[r-0x2596])
	}
}

func snap(v float32) float32 { return float32(math.Round(float64(v))) }

const (
	none = iota
	light
	heavy
	double
)

type arms [4]uint8

func paintBox(dl *gfx.DisplayList, a arms, c gfx.Rect, fg gfx.Color) {
	t := max(1, snap(c.W/8))
	g := t
	cx, cy := snap(c.X+c.W/2-t/2), snap(c.Y+c.H/2-t/2)
	thick := func(w uint8) float32 {
		if w == heavy {
			return 2 * t
		}
		return t
	}
	anyDouble := func(i, j int) bool { return a[i] == double || a[j] == double }
	vertDouble, horizDouble := anyDouble(0, 2), anyDouble(1, 3)
	hline := func(x0, x1, y, h float32) {
		if x1 > x0 {
			dl.Fill(gfx.R(x0, y, x1-x0, h), fg, gfx.Corners{})
		}
	}
	vline := func(y0, y1, x, w float32) {
		if y1 > y0 {
			dl.Fill(gfx.R(x, y0, w, y1-y0), fg, gfx.Corners{})
		}
	}

	reach := func(perpDouble bool, perp1, perp2 uint8) float32 {
		switch {
		case perpDouble:
			return g + t
		case perp1 == heavy || perp2 == heavy:
			return t
		case perp1 != none || perp2 != none:
			return t / 2
		}
		return t / 2
	}

	dstop := func(near, far bool) float32 {
		switch {
		case near:
			return g
		case far:
			return -g
		}
		return 0
	}
	up, right, down, left := a[0] != none, a[1] != none, a[2] != none, a[3] != none
	mid := func(v float32) float32 { return v + t/2 }
	X, Y := mid(cx), mid(cy)

	switch a[1] {
	case light, heavy:
		w := thick(a[1])
		hline(X-reach(vertDouble, a[0], a[2]), c.X+c.W, snap(Y-w/2), w)
	case double:
		hline(snap(X+stopIf(vertDouble, dstop(up, down))), c.X+c.W, snap(Y-g-t/2), t)
		hline(snap(X+stopIf(vertDouble, dstop(down, up))), c.X+c.W, snap(Y+g-t/2), t)
	}

	switch a[3] {
	case light, heavy:
		w := thick(a[3])
		hline(c.X, X+reach(vertDouble, a[0], a[2]), snap(Y-w/2), w)
	case double:
		hline(c.X, snap(X-stopIf(vertDouble, dstop(up, down))), snap(Y-g-t/2), t)
		hline(c.X, snap(X-stopIf(vertDouble, dstop(down, up))), snap(Y+g-t/2), t)
	}

	switch a[2] {
	case light, heavy:
		w := thick(a[2])
		vline(Y-reach(horizDouble, a[1], a[3]), c.Y+c.H, snap(X-w/2), w)
	case double:
		vline(snap(Y+stopIf(horizDouble, dstop(left, right))), c.Y+c.H, snap(X-g-t/2), t)
		vline(snap(Y+stopIf(horizDouble, dstop(right, left))), c.Y+c.H, snap(X+g-t/2), t)
	}

	switch a[0] {
	case light, heavy:
		w := thick(a[0])
		vline(c.Y, Y+reach(horizDouble, a[1], a[3]), snap(X-w/2), w)
	case double:
		vline(c.Y, snap(Y-stopIf(horizDouble, dstop(left, right))), snap(X-g-t/2), t)
		vline(c.Y, snap(Y-stopIf(horizDouble, dstop(right, left))), snap(X+g-t/2), t)
	}
}

func stopIf(perpDouble bool, v float32) float32 {
	if perpDouble {
		return v
	}
	return 0
}

const L, H, D = light, heavy, double

var boxArms = map[rune]arms{
	'─': {0, L, 0, L}, '━': {0, H, 0, H}, '│': {L, 0, L, 0}, '┃': {H, 0, H, 0},
	'┌': {0, L, L, 0}, '┏': {0, H, H, 0}, '┐': {0, 0, L, L}, '┓': {0, 0, H, H},
	'└': {L, L, 0, 0}, '┗': {H, H, 0, 0}, '┘': {L, 0, 0, L}, '┛': {H, 0, 0, H},
	'├': {L, L, L, 0}, '┣': {H, H, H, 0}, '┤': {L, 0, L, L}, '┫': {H, 0, H, H},
	'┬': {0, L, L, L}, '┳': {0, H, H, H}, '┴': {L, L, 0, L}, '┻': {H, H, 0, H},
	'┼': {L, L, L, L}, '╋': {H, H, H, H},
	'═': {0, D, 0, D}, '║': {D, 0, D, 0},
	'╒': {0, D, L, 0}, '╓': {0, L, D, 0}, '╔': {0, D, D, 0},
	'╕': {0, 0, L, D}, '╖': {0, 0, D, L}, '╗': {0, 0, D, D},
	'╘': {L, D, 0, 0}, '╙': {D, L, 0, 0}, '╚': {D, D, 0, 0},
	'╛': {L, 0, 0, D}, '╜': {D, 0, 0, L}, '╝': {D, 0, 0, D},
	'╞': {L, D, L, 0}, '╟': {D, L, D, 0}, '╠': {D, D, D, 0},
	'╡': {L, 0, L, D}, '╢': {D, 0, D, L}, '╣': {D, 0, D, D},
	'╤': {0, D, L, D}, '╥': {0, L, D, L}, '╦': {0, D, D, D},
	'╧': {L, D, 0, D}, '╨': {D, L, 0, L}, '╩': {D, D, 0, D},
	'╪': {L, D, L, D}, '╫': {D, L, D, L}, '╬': {D, D, D, D},
	'╴': {0, 0, 0, L}, '╵': {L, 0, 0, 0}, '╶': {0, L, 0, 0}, '╷': {0, 0, L, 0},
	'╸': {0, 0, 0, H}, '╹': {H, 0, 0, 0}, '╺': {0, H, 0, 0}, '╻': {0, 0, H, 0},
}
