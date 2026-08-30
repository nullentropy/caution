package gfx

import "fmt"

// Font selects a face and size. The browser terminal resolves a CSS family
// list; here the text package maps (Weight, Italic, Mono) onto embedded
// faces, which is what makes native rendering deterministic across machines -
// there is no system font stack to vary under us.
type Font struct {
	// Size in logical px.
	Size   float32
	Weight int
	Italic bool
	Mono   bool
	// Key is a stable cache key for shaping and atlas lookups.
	Key string
}

type FontOpts struct {
	Weight int
	Italic bool
	Mono   bool
}

func NewFont(size float32, opts FontOpts) Font {
	w := opts.Weight
	if w == 0 {
		w = 400
	}
	suffix := ""
	if opts.Italic {
		suffix += "i"
	}
	if opts.Mono {
		suffix += "m"
	}
	return Font{
		Size:   size,
		Weight: w,
		Italic: opts.Italic,
		Mono:   opts.Mono,
		Key:    fmt.Sprintf("%d%s:%g", w, suffix, size),
	}
}
