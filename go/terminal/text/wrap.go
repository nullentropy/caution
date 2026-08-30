package text

import (
	"strings"
	"unicode/utf8"
)

// Line is one wrapped line as a [Start, End) byte range into the source
// string. The separators consumed at a break (the spaces and the newline)
// fall in the gap between consecutive ranges, so selections and copies map
// back to exact source offsets. Mirrors src/gfx/text/wrap.ts.
type Line struct{ Start, End int }

// Wrap greedily breaks s to fit maxW, honoring '\n'. measure must be
// monotonic in prefix length (it is: it's an advance sum).
func Wrap(measure func(string) float32, s string, maxW float32) []Line {
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

func wrapPara(measure func(string) float32, s string, start, end int, maxW float32, out *[]Line) {
	if start >= end {
		*out = append(*out, Line{start, start}) // empty line (blank paragraph, trailing \n)
		return
	}
	// Rune boundaries as byte offsets: bounds[i] is where rune i starts;
	// bounds[n] == end.
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
		// Longest prefix that fits - binary search over rune count.
		lo, hi := i+1, n
		for lo < hi {
			mid := (lo + hi + 1) >> 1
			if measure(s[bounds[i]:bounds[mid]]) <= maxW {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		brk := lo
		if brk < n {
			// Back off to the last break opportunity - either side of a space
			// so words stay whole. None means a hard break mid-word.
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
		// The line ends before its trailing spaces; the next starts after them.
		lineEnd := brk
		for lineEnd > i && s[bounds[lineEnd]-1] == ' ' {
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
