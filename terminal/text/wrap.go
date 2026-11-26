package text

import (
	"strings"
	"unicode/utf8"
)

type Line struct{ Start, End int }

func Wrap(measure func(string) float32, s string, maxW float32) []Line {
	return WrapRanges(func(start, end int) float32 { return measure(s[start:end]) }, s, maxW)
}

func WrapRanges(measure func(start, end int) float32, s string, maxW float32) []Line {
	var out []Line
	p := 0
	for {
		nl := strings.IndexByte(s[p:], '\n')
		pe := len(s)
		if nl >= 0 {
			pe = p + nl
		}
		wrapPara(measure, s, p, pe, maxW, &out)
		if nl < 0 {
			break
		}
		p = pe + 1
	}
	return out
}

func wrapPara(measure func(start, end int) float32, s string, start, end int, maxW float32, out *[]Line) {
	if start >= end {
		*out = append(*out, Line{start, start})
		return
	}
	var bounds []int
	for i := start; i < end; {
		bounds = append(bounds, i)
		_, sz := utf8.DecodeRuneInString(s[i:end])
		i += sz
	}
	bounds = append(bounds, end)
	n := len(bounds) - 1

	i := 0
	for i < n {
		lo, hi := i+1, n
		for lo < hi {
			mid := (lo + hi + 1) >> 1
			if measure(bounds[i], bounds[mid]) <= maxW {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		brk := lo
		if brk < n {
			sp := -1
			for j := brk; j > i; j-- {
				if s[bounds[j]-1] == ' ' || s[bounds[j]] == ' ' {
					sp = j
					break
				}
			}
			if sp > i {
				brk = sp
			}
		}
		lineEnd := brk
		for brk < n && lineEnd > i && s[bounds[lineEnd]-1] == ' ' {
			lineEnd--
		}
		le := bounds[lineEnd]
		if lineEnd == i {
			le = bounds[brk]
		}
		*out = append(*out, Line{bounds[i], le})
		i = brk
		for i < n && s[bounds[i]] == ' ' {
			i++
		}
	}
}
