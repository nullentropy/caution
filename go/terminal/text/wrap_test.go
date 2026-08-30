package text

import (
	"reflect"
	"testing"
)

// Fixed-advance measure: 7px per rune. Wrapping is pure geometry over it.
func meas(s string) float32 { return float32(len([]rune(s))) * 7 }

func TestWrapSingleLineFits(t *testing.T) {
	got := Wrap(meas, "ab cd", 100)
	want := []Line{{0, 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapBreaksAtWordBoundary(t *testing.T) {
	// 19 runes; 14 fit (98px <= 100). The fitted prefix ends at a word
	// end, so the break happens there.
	got := Wrap(meas, "aaaa aaaa aaaa aaaa", 100)
	want := []Line{{0, 14}, {15, 19}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapEatsBreakSpaces(t *testing.T) {
	// The spaces consumed at a break fall in the gap between lines, so
	// s[l.Start:l.End] never carries them.
	got := Wrap(meas, "aa   bb", 30) // 4 runes fit (28px)
	want := []Line{{0, 2}, {5, 7}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapHardBreaksOverlongWord(t *testing.T) {
	got := Wrap(meas, "aaaaaaaaaaaaaaa", 70) // 15 runes, 10 per line
	want := []Line{{0, 10}, {10, 15}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapHonorsNewlines(t *testing.T) {
	got := Wrap(meas, "ab\n\ncd", 100)
	want := []Line{{0, 2}, {3, 3}, {4, 6}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapTrailingNewlineYieldsEmptyLine(t *testing.T) {
	// A textarea caret can sit on the empty line after a trailing \n.
	got := Wrap(meas, "ab\n", 100)
	want := []Line{{0, 2}, {3, 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapEmptyString(t *testing.T) {
	got := Wrap(meas, "", 100)
	want := []Line{{0, 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapMultibyteOffsetsAreBytes(t *testing.T) {
	// "éé éé" - é is 2 bytes; offsets must be byte positions.
	got := Wrap(meas, "éé éé", 30) // 4 runes fit; break at the space
	want := []Line{{0, 4}, {5, 9}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapAlwaysAdvances(t *testing.T) {
	// Pathologically narrow: still one rune per line, never an infinite loop.
	got := Wrap(meas, "abc", 1)
	want := []Line{{0, 1}, {1, 2}, {2, 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
