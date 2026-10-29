// Package gfx is the terminal's drawing vocabulary: logical-pixel geometry,
// straight-alpha colors, fonts and the retained display list
package gfx

// logical pixels
type Rect struct {
	X, Y, W, H float32
}

func R(x, y, w, h float32) Rect { return Rect{x, y, w, h} }

func Intersect(a, b Rect) Rect {
	x := max32(a.X, b.X)
	y := max32(a.Y, b.Y)
	right := min32(a.X+a.W, b.X+b.W)
	bottom := min32(a.Y+a.H, b.Y+b.H)
	return Rect{x, y, max32(0, right-x), max32(0, bottom-y)}
}

func Inset(r Rect, d float32) Rect {
	return Rect{r.X + d, r.Y + d, r.W - d*2, r.H - d*2}
}

func Inflate(r Rect, d float32) Rect { return Inset(r, -d) }

// shared interior area, edge-touching rects don't overlap
func Overlaps(a, b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func Union(a, b Rect) Rect {
	x := min32(a.X, b.X)
	y := min32(a.Y, b.Y)
	right := max32(a.X+a.W, b.X+b.W)
	bottom := max32(a.Y+a.H, b.Y+b.H)
	return Rect{x, y, right - x, bottom - y}
}

// big enough for any viewport, small enough for f32
var NoClip = Rect{-1e6, -1e6, 2e6, 2e6}

// clockwise from top-left
type Corners struct {
	TL, TR, BR, BL float32
}

func CornerRadius(r float32) Corners { return Corners{r, r, r, r} }

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
