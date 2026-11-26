package terminal

import (
	"testing"
	"time"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/gpu/gputest"
	"github.com/nullentropy/caution/terminal/render"
)

func TestDroppedPresentIsRedrawn(t *testing.T) {
	dev := gputest.New()
	r, err := render.New(dev)
	if err != nil {
		t.Fatal(err)
	}
	out := presenter{dev: dev}
	dl := &gfx.DisplayList{}
	dl.Fill(gfx.R(10, 10, 40, 20), gfx.White, gfx.Corners{})
	draw := func() int {
		before := framePresented.Load()
		fr := render.Frame{ViewW: 100, ViewH: 80, DevW: 200, DevH: 160, SkipUnpainted: !out.unshown}
		out.frame(r.Render(dl, gfx.Hex("#000000"), fr))
		return int(framePresented.Load() - before)
	}

	dev.DropPresents = true
	if n := draw(); n != 1 || !out.unshown {
		t.Fatalf("first frame presented %d times, unshown %v", n, out.unshown)
	}
	dev.DropPresents = false
	if n := draw(); n != 1 || out.unshown {
		t.Fatalf("unchanged frame after a dropped present: presented %d times, unshown %v", n, out.unshown)
	}
	if n := draw(); n != 0 {
		t.Fatalf("unchanged frame after a shown one presented %d times", n)
	}
}

func TestStoppedRendererEndsTheRedraw(t *testing.T) {
	dev := gputest.New()
	out := presenter{dev: dev, unshown: true}
	out.frame(render.FrameResult{Stopped: true})
	if out.unshown {
		t.Fatal("a stopped renderer left a redraw pending")
	}
}

func TestFailingPresentsBackOff(t *testing.T) {
	dev := gputest.New()
	dev.DropPresents = true
	out := presenter{dev: dev}
	var waits []time.Duration
	for i := 0; i < 8; i++ {
		out.present()
		waits = append(waits, out.wait)
	}
	want := []time.Duration{firstRetry, 2 * firstRetry, 4 * firstRetry, 8 * firstRetry,
		16 * firstRetry, 32 * firstRetry, maxRetry, maxRetry}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("retry waits %v, want %v", waits, want)
		}
	}
	if retry, pending := out.retryIn(time.Now()); !pending || retry <= maxRetry/2 {
		t.Fatalf("retry in %v (pending %v) right after a failed present", retry, pending)
	}
	if retry, pending := out.retryIn(time.Now().Add(maxRetry)); !pending || retry != 0 {
		t.Fatalf("retry in %v (pending %v) once the wait has passed", retry, pending)
	}
	dev.DropPresents = false
	out.present()
	if _, pending := out.retryIn(time.Now()); pending || out.wait != 0 {
		t.Fatalf("a shown frame left pending %v, wait %v", pending, out.wait)
	}
}
