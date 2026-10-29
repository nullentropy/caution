package gfx

import "fmt"

// the browser resolves a CSS family list. natively the text package maps
// (Weight, Italic, Mono) onto embedded faces, so rendering is deterministic
// across machines with no system font stack to vary
type Font struct {
	// logical px
	Size   float32
	Weight int
	Italic bool
	Mono   bool
	// stable cache key for shaping and atlas lookups
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
