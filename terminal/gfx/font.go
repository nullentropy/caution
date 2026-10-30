package gfx

import "fmt"

// both terminals run the same text package, which maps (Family, Weight,
// Italic, Mono) onto embedded or app-registered faces, so rendering is
// deterministic across machines with no system font stack to vary
type Font struct {
	// logical px
	Size   float32
	Weight int
	Italic bool
	Mono   bool
	// app-registered family. "" takes the session default for the style
	Family string
	// stable cache key for shaping and widget measurement memos
	Key string
}

type FontOpts struct {
	Weight int
	Italic bool
	Mono   bool
	Family string
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
		Family: opts.Family,
		Key:    fmt.Sprintf("%d%s:%g|%s", w, suffix, size, opts.Family),
	}
}
