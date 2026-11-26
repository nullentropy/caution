package text

import (
	"reflect"
	"testing"
)

func meas(s string) float32 { return float32(len([]rune(s))) * 7 }

func TestWrapSingleLineFits(t *testing.T) {
	got := Wrap(meas, "ab cd", 100)
	want := []Line{{0, 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapKeepsTrailingSpacesOnALineThatFits(t *testing.T) {
	got := Wrap(meas, "ab \ncd  ", 100)
	want := []Line{{0, 3}, {4, 8}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapBreaksAtWordBoundary(t *testing.T) {
	got := Wrap(meas, "aaaa aaaa aaaa aaaa", 100)
	want := []Line{{0, 14}, {15, 19}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapEatsBreakSpaces(t *testing.T) {
	got := Wrap(meas, "aa   bb", 30)
	want := []Line{{0, 2}, {5, 7}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapHardBreaksOverlongWord(t *testing.T) {
	got := Wrap(meas, "aaaaaaaaaaaaaaa", 70)
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
	got := Wrap(meas, "éé éé", 30)
	want := []Line{{0, 4}, {5, 9}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWrapAlwaysAdvances(t *testing.T) {
	got := Wrap(meas, "abc", 1)
	want := []Line{{0, 1}, {1, 2}, {2, 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
