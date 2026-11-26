package render

import (
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/gpu/gputest"
)

func faultFrames(t *testing.T, r *Renderer, dev *fakeDev) {
	t.Helper()
	r.Images.entries["img"] = &imageEntry{}
	r.Images.done = append(r.Images.done, decodedImage{src: "img", w: 8, h: 8, pix: make([]byte, 8*8*4)})
	bg := gfx.Hex("#16181d")
	fr := Frame{ViewW: 200, ViewH: 120, DevW: 400, DevH: 240}
	scene := func(label string, x float32) *gfx.DisplayList {
		dl := &gfx.DisplayList{}
		dl.Shadow(gfx.R(x, 10, 60, 30), gfx.RGBA(0, 0, 0, 0.5), 6, gfx.CornerRadius(4), 0, 2)
		dl.Fill(gfx.R(x, 10, 60, 30), gfx.White, gfx.CornerRadius(4))
		dl.Text(label, 12, 50, gfx.NewFont(13, gfx.FontOpts{}), gfx.White)
		dl.Image(gfx.R(120, 10, 20, 20), "img", gfx.FitCover, gfx.Corners{})
		dl.BeginLayer(gfx.R(10, 70, 80, 40))
		dl.Fill(gfx.R(10, 70, 80, 40), gfx.RGBA(0.2, 0.4, 0.8, 1), gfx.Corners{})
		dl.EndLayer("vec4 effect(vec2 uv) { return src(uv) * fract(u_time); }", nil, true)
		dl.ShaderQuad(gfx.R(100, 70, 40, 40), "vec4 effect(vec2 uv) { return vec4(uv, 0.0, 1.0); }", nil, false)
		return dl
	}
	for i, x := range []float32{10, 10, 14} {
		fr.Time = float32(i)
		fr.SkipUnpainted = i > 0
		r.Render(scene("frame", x), bg, fr)
		dev.Present()
	}
	r.Render(scene("frame changed", 14), bg, fr)
	dev.Present()
	fr.Time = 5
	r.RenderPartial(bg, fr)
	dev.Present()
	shot, err := dev.NewTexture(400, 240, 0)
	if err != nil {
		return
	}
	fr.Target = shot
	r.Render(scene("shot", 14), bg, fr)
	dev.ReadPixels(shot)
}

func TestRendererSurvivesDeviceLossAtEveryCall(t *testing.T) {
	clean := newFakeDev()
	r, err := New(clean)
	if err != nil {
		t.Fatal(err)
	}
	faultFrames(t, r, clean)
	t.Logf("losing the device at each of %d calls", clean.Calls)
	for k := 1; k <= clean.Calls; k++ {
		dev := newFakeDev()
		dev.LoseAtCall = k
		r, err := New(dev)
		if err != nil {
			continue
		}
		faultFrames(t, r, dev)
		if !dev.Lost() {
			t.Fatalf("loss at call %d of %d never happened", k, clean.Calls)
		}
	}
}

func TestRendererSurvivesPipelineFailures(t *testing.T) {
	dev := newFakeDev()
	dev.FailPipelinesAfter = 1
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	faultFrames(t, r, dev)

	broken := &gputest.Device{MaxTexture: fakeMaxTexture, LoseAtCall: 1}
	if _, err := New(&fakeDev{Device: broken}); err == nil {
		t.Fatal("New succeeded without its pipeline")
	}
}

func TestLostDeviceStopsFrames(t *testing.T) {
	dev := newFakeDev()
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	dl := &gfx.DisplayList{}
	dl.ShaderQuad(gfx.R(0, 0, 10, 10), "vec4 effect(vec2 uv) { return vec4(fract(u_time)); }", nil, true)
	fr := Frame{ViewW: 100, ViewH: 100, DevW: 200, DevH: 200}
	r.Render(dl, gfx.Hex("#16181d"), fr)
	dev.LoseAtCall = dev.Calls + 1
	dev.Present()
	if res := r.Render(dl, gfx.Hex("#16181d"), fr); !res.Stopped || res.Painted {
		t.Fatalf("frame on a lost device: %+v", res)
	}
	if r.RenderPartial(gfx.Hex("#16181d"), fr) {
		t.Fatal("replayed animation on a lost device")
	}
}

func imageFrame(t *testing.T, dev *fakeDev) (*Renderer, FrameResult) {
	t.Helper()
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	r.Images.entries["img"] = &imageEntry{}
	r.Images.done = append(r.Images.done, decodedImage{src: "img", w: 8, h: 8, pix: make([]byte, 8*8*4)})
	dl := &gfx.DisplayList{}
	dl.Image(gfx.R(10, 10, 20, 20), "img", gfx.FitCover, gfx.Corners{})
	return r, r.Render(dl, gfx.Hex("#16181d"), Frame{ViewW: 100, ViewH: 100, DevW: 200, DevH: 200})
}

func TestFailedImageAllocationKeepsDrawing(t *testing.T) {
	is8 := func(w, h int) bool { return w == 8 && h == 8 }
	for name, set := range map[string]func(*fakeDev){
		"texture": func(d *fakeDev) { d.FailTexture = is8 },
		"upload":  func(d *fakeDev) { d.FailWrite = is8 },
	} {
		dev := newFakeDev()
		set(dev)
		r, res := imageFrame(t, dev)
		if res.Stopped || !res.Painted {
			t.Errorf("%s failure stopped drawing: %+v", name, res)
		}
		next := &gfx.DisplayList{}
		next.Fill(gfx.R(0, 0, 10, 10), gfx.White, gfx.Corners{})
		if res := r.Render(next, gfx.Hex("#16181d"), Frame{ViewW: 100, ViewH: 100, DevW: 200, DevH: 200}); res.Stopped || !res.Painted {
			t.Errorf("%s failure stopped the next frame: %+v", name, res)
		}
		if st := r.Images.entries["img"].state; st != imgError {
			t.Errorf("%s failure left the image in state %v, want imgError", name, st)
		}
	}
}

func TestFailedSceneAllocationStops(t *testing.T) {
	dev := newFakeDev()
	dev.FailTexture = func(w, h int) bool { return w == 200 && h == 200 }
	r, res := imageFrame(t, dev)
	if !res.Stopped {
		t.Fatalf("frame with no scene texture: %+v", res)
	}
	if res := r.Render(&gfx.DisplayList{}, gfx.Hex("#16181d"), Frame{ViewW: 50, ViewH: 50, DevW: 100, DevH: 100}); !res.Stopped {
		t.Fatalf("drawing resumed after the scene failed: %+v", res)
	}
}

func TestFailedLayerAllocationStops(t *testing.T) {
	dev := newFakeDev()
	dev.FailTexture = func(w, h int) bool { return w == 80 && h == 40 }
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	dl := &gfx.DisplayList{}
	dl.BeginLayer(gfx.R(0, 0, 80, 40))
	dl.Fill(gfx.R(0, 0, 80, 40), gfx.White, gfx.Corners{})
	dl.EndLayer("vec4 effect(vec2 uv) { return src(uv); }", nil, false)
	fr := Frame{ViewW: 100, ViewH: 100, DevW: 100, DevH: 100}
	r.Render(dl, gfx.Hex("#16181d"), fr)
	if res := r.Render(dl, gfx.Hex("#16181d"), fr); !res.Stopped {
		t.Fatalf("drawing continued after a layer failed: %+v", res)
	}
}

func TestEmptyFrameDrawsNothing(t *testing.T) {
	dev := newFakeDev()
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	dl := &gfx.DisplayList{}
	dl.Fill(gfx.R(0, 0, 10, 10), gfx.White, gfx.Corners{})
	for _, fr := range []Frame{{}, {ViewW: 100, ViewH: 100}, {DevW: 100, DevH: 100}} {
		if res := r.Render(dl, gfx.Hex("#16181d"), fr); res.Painted || res.Stopped || !res.Empty {
			t.Fatalf("frame %+v: %+v", fr, res)
		}
	}
}

func TestStoppedRendererDropsDecodedImages(t *testing.T) {
	dev := newFakeDev()
	r, err := New(dev)
	if err != nil {
		t.Fatal(err)
	}
	dev.LoseAtCall = dev.Calls
	r.Images.entries["img"] = &imageEntry{}
	r.Images.done = append(r.Images.done, decodedImage{src: "img", w: 8, h: 8, pix: make([]byte, 8*8*4)})
	r.Render(&gfx.DisplayList{}, gfx.Hex("#16181d"), Frame{ViewW: 100, ViewH: 100, DevW: 100, DevH: 100})
	if n := len(r.Images.done); n != 0 {
		t.Fatalf("%d decoded images still queued after drawing stopped", n)
	}
}

func TestNewReleasesPipelineWhenAtlasFails(t *testing.T) {
	dev := newFakeDev()
	dev.FailTexture = func(w, h int) bool { return true }
	if _, err := New(dev); err == nil {
		t.Fatal("New succeeded without an atlas")
	}
	if dev.Pipelines() != 0 {
		t.Fatalf("%d pipelines left after New failed", dev.Pipelines())
	}
}
