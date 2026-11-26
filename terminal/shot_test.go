package terminal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/gpu"
	"github.com/nullentropy/caution/terminal/gpu/gputest"
	"github.com/nullentropy/caution/terminal/render"
)

func TestShotFromLostDeviceFails(t *testing.T) {
	dev := gputest.New()
	target, capture, err := shotTarget(dev, 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := target.Size(); w != 80 || h != 60 {
		t.Fatalf("shot target %dx%d, want 80x60", w, h)
	}
	dev.LoseAtCall = dev.Calls + 1
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := capture(path); err == nil {
		t.Fatal("capture from a lost device succeeded")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("capture from a lost device wrote %s", path)
	}
}

func TestShotPastTextureLimitIsRefused(t *testing.T) {
	dev := gputest.New()
	if _, _, err := shotTarget(dev, dev.MaxTexture, 10); err == nil {
		t.Fatal("2x shot past the texture limit was accepted")
	}
	if dev.Calls != 0 {
		t.Fatalf("refused shot still made %d device calls", dev.Calls)
	}
}

func TestShotFailsWhenTheRendererStops(t *testing.T) {
	dev := gputest.New()
	r, err := render.New(dev)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	dev.FailTexture = func(w, h int) bool {
		if w == 80 && h == 60 {
			seen++
			return seen == 2
		}
		return false
	}
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := RenderPNG(dev, r, &gfx.DisplayList{}, gfx.Hex("#000000"), 40, 30, path); err == nil {
		t.Fatal("shot succeeded after the scene texture failed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed shot wrote %s", path)
	}
}

func TestRenderPNGFreesItsTarget(t *testing.T) {
	dev := gputest.New()
	r, err := render.New(dev)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := RenderPNG(dev, r, &gfx.DisplayList{}, gfx.Hex("#000000"), 40, 30, filepath.Join(dir, "a.png")); err != nil {
		t.Fatal(err)
	}
	live := dev.Textures()
	for i := 0; i < 3; i++ {
		if err := RenderPNG(dev, r, &gfx.DisplayList{}, gfx.Hex("#000000"), 40, 30, filepath.Join(dir, "b.png")); err != nil {
			t.Fatal(err)
		}
	}
	if dev.Textures() != live {
		t.Fatalf("%d textures live after three more shots, want %d", dev.Textures(), live)
	}
}

func TestShotTargetIsFreedWhenDrawingFails(t *testing.T) {
	dev := gputest.New()
	before := dev.Textures()
	err := withShot(dev, 40, 30, filepath.Join(t.TempDir(), "shot.png"), func(gpu.Texture) error {
		return errors.New("draw failed")
	})
	if err == nil {
		t.Fatal("a failed draw produced a shot")
	}
	if dev.Textures() != before {
		t.Fatalf("%d textures live after a failed shot, want %d", dev.Textures(), before)
	}
}
