package render

import (
	"fmt"
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/text"
)

func textFrame(r *Renderer, s string, size float32) { textFrameAt(r, s, size, 4) }

func textFrameAt(r *Renderer, s string, size, x float32) {
	dl := &gfx.DisplayList{}
	dl.Text(s, x, 4, gfx.NewFont(size, gfx.FontOpts{}), gfx.White)
	r.Render(dl, gfx.Hex("#16181d"), Frame{ViewW: 1600, ViewH: 600, DevW: 1600, DevH: 600})
}

func TestAtlasResetMidBatchKeepsQueuedGlyphs(t *testing.T) {
	dev := newFakeDev()
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	for size := float32(60); size < 260; size += 2 {
		textFrame(r, "abcdefghijklmnop", size)
	}
	if dev.texels < 2*text.AtlasSize*text.AtlasSize {
		t.Fatalf("drew %d glyph texels, not enough to fill the atlas twice", dev.texels)
	}
	if dev.blank > 0 {
		t.Fatalf("%d of %d glyph quads sampled cleared atlas space", dev.blank, dev.glyphs)
	}
}

func TestAtlasUploadsOnlyChangedRows(t *testing.T) {
	dev := newFakeDev()
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	textFrame(r, "first line", 14)
	before := dev.writtenRows
	textFrame(r, "QUVWXYZ", 14)
	if got := dev.writtenRows - before; got == 0 || got > 40 {
		t.Fatalf("uploaded %d atlas rows for one line of 14px glyphs", got)
	}
	before, drawn := dev.writtenRows, dev.glyphs
	textFrameAt(r, "QUVWXYZ", 14, 5)
	if dev.glyphs == drawn {
		t.Fatal("the moved text was not redrawn")
	}
	if got := dev.writtenRows - before; got != 0 {
		t.Fatalf("uploaded %d rows for glyphs already in the atlas", got)
	}
}

func TestDamageFillIsOpaque(t *testing.T) {
	dev := newFakeDev()
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	bg := gfx.RGBA(0.1, 0.1, 0.1, 0.5)
	frame := Frame{ViewW: 400, ViewH: 300, DevW: 400, DevH: 300}
	for _, x := range []float32{10, 120} {
		dl := &gfx.DisplayList{}
		dl.Fill(gfx.R(x, 10, 50, 50), gfx.RGBA(1, 0, 0, 1), gfx.Corners{})
		r.Render(dl, bg, frame)
	}
	if r.Frames.Damage == 0 || len(dev.solidAlphas) == 0 {
		t.Fatalf("the move did not take the damage path: %+v", r.Frames)
	}
	for _, a := range dev.solidAlphas {
		if a != 1 {
			t.Fatalf("damage fill alpha %v, want 1", a)
		}
	}
}

func TestImagesOutsideTextureLimitsShowError(t *testing.T) {
	dev := newFakeDev()
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{0, 0}, {fakeMaxTexture + 1, 1}, {1, fakeMaxTexture + 1}} {
		src := fmt.Sprintf("data:%dx%d", size[0], size[1])
		r.Images.entries[src] = &imageEntry{}
		r.Images.done = append(r.Images.done, decodedImage{src: src, w: size[0], h: size[1], pix: make([]byte, size[0]*size[1]*4)})
		r.Images.drainUploads()
		if st := r.Images.entries[src].state; st != imgError {
			t.Errorf("%dx%d image: state %v, want imgError", size[0], size[1], st)
		}
	}
}

func TestLayerTallerThanTextureLimitRenders(t *testing.T) {
	dev := newFakeDev()
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	dl := &gfx.DisplayList{}
	tall := gfx.R(0, 0, 200, 3*fakeMaxTexture)
	dl.BeginLayer(tall)
	dl.Fill(tall, gfx.White, gfx.Corners{})
	dl.EndLayer("vec4 effect(vec2 uv) { return src(uv); }", nil, false)
	r.Render(dl, gfx.Hex("#16181d"), Frame{ViewW: 400, ViewH: 300, DevW: 800, DevH: 600})
}

func TestFramePastTextureLimitPaintsNothing(t *testing.T) {
	dev := newFakeDev()
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	dl := &gfx.DisplayList{}
	dl.Fill(gfx.R(0, 0, 10, 10), gfx.White, gfx.Corners{})
	dl.ShaderQuad(gfx.R(20, 20, 10, 10), "vec4 effect(vec2 uv) { return vec4(fract(u_time)); }", nil, true)
	bg := gfx.Hex("#16181d")
	if !r.Render(dl, bg, Frame{ViewW: 300, ViewH: 100, DevW: 600, DevH: 200}).Painted {
		t.Fatal("the fitting frame did not paint")
	}
	fr := Frame{ViewW: 3000, ViewH: 100, DevW: 2 * 3000, DevH: 200}
	if res := r.Render(dl, bg, fr); res.Painted || !res.Oversized {
		t.Fatalf("frame larger than the texture limit: %+v", res)
	}
}
