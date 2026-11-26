package gfx

import "fmt"

type Font struct {
	Size   float32
	Weight int
	Italic bool
	Mono   bool
	Family string
	Key    string
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
