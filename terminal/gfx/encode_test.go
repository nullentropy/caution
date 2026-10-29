package gfx

import (
	"fmt"
	"reflect"
	"testing"
)

func TestEncodeRoundTripsEveryCommandKind(t *testing.T) {
	dl := &DisplayList{}
	dl.PushClip(R(10, 10, 300, 200))
	dl.Rect(R(0, 0, 100, 40), Hex("#4f8cff"), RectOpts{Radius: CornerRadius(6), BorderWidth: 1.5, BorderColor: &Black})
	dl.Shadow(R(20, 20, 80, 30), WithAlpha(Black, 0.4), 12, Corners{1, 2, 3, 4}, 0, 3)
	dl.Measure = func(Font, string) (float32, float32, float32) { return 48, 11, 3 }
	dl.Text("héllo wörld", 5.5, 7.25, NewFont(13, FontOpts{Weight: 600, Italic: true}), White)
	dl.Text("", 0, 0, NewFont(12, FontOpts{Mono: true}), Transparent)
	dl.PopClip()
	dl.BeginLayer(R(0.4, 0.6, 99.2, 49.9))
	dl.Image(R(1, 2, 3, 4), "/assets/a.png", "cover", CornerRadius(2))
	dl.EndLayer("vec4 effect(vec2 uv) { return src(uv); }", map[string]float32{"u_fade": 0.5, "u_x": 2}, true)
	dl.ShaderQuad(R(-5, -5, 50, 50), "frag", nil, false)
	dl.Image(R(1, 2, 3, 4), "/assets/a.png", "fill", Corners{})
	dl.GeomAnimation = true

	bg := Hex("#16181d")
	buf := Encode(dl, bg, nil)
	got, gotBg, err := Decode(buf)
	if err != nil {
		t.Fatal(err)
	}
	if gotBg != bg {
		t.Fatalf("background %v, want %v", gotBg, bg)
	}
	if !got.GeomAnimation {
		t.Fatal("geometry animation flag lost")
	}
	if len(got.Cmds) != len(dl.Cmds) {
		t.Fatalf("%d commands, want %d", len(got.Cmds), len(dl.Cmds))
	}
	for i := range dl.Cmds {
		want, have := dl.Cmds[i], got.Cmds[i]
		if !reflect.DeepEqual(want, have) {
			t.Errorf("command %d:\n want %+v\n have %+v", i, want, have)
		}
	}
}

func TestEncodeDedupesStringsAndReusesBuffer(t *testing.T) {
	same, distinct := &DisplayList{}, &DisplayList{}
	for i := range 50 {
		same.Text("same", 0, 0, NewFont(12, FontOpts{}), White)
		distinct.Text(fmt.Sprintf("t%03d", i), 0, 0, NewFont(12, FontOpts{}), White)
	}
	one := Encode(same, Black, nil)
	two := Encode(same, Black, one[:0])
	if &one[0] != &two[0] {
		t.Fatal("encoder did not append into the buffer it was given")
	}
	if other := Encode(distinct, Black, nil); len(two) >= len(other) {
		t.Fatalf("fifty identical texts took %d bytes, fifty distinct ones %d", len(two), len(other))
	}
	got, _, err := Decode(two)
	if err != nil || len(got.Cmds) != 50 || got.Cmds[49].Text != "same" {
		t.Fatalf("decode: %v, %d cmds", err, len(got.Cmds))
	}
}

func TestDecodeRejectsTruncatedFrames(t *testing.T) {
	dl := &DisplayList{}
	dl.Text("abc", 1, 2, NewFont(12, FontOpts{}), White)
	buf := Encode(dl, Black, nil)
	for cut := 1; cut < len(buf); cut += 7 {
		if _, _, err := Decode(buf[:cut]); err == nil {
			t.Fatalf("no error decoding %d of %d bytes", cut, len(buf))
		}
	}
}
