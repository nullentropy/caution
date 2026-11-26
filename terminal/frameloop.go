package terminal

import (
	"fmt"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/gpu"
	"github.com/nullentropy/caution/terminal/pacer"
	"github.com/nullentropy/caution/terminal/proto"
	"github.com/nullentropy/caution/terminal/render"
	"github.com/nullentropy/caution/terminal/ui"
)

type frameLoop struct {
	win  *glfw.Window
	dev  gpu.Device
	r    *render.Renderer
	u    *ui.UI
	sess *proto.Session
	out  presenter

	dirty     bool
	animating bool
	hidden    bool
	live      bool

	lastDraw time.Time
	nextTick time.Time
	minFrame time.Duration
}

func (f *frameLoop) dpr() float32 {
	fw, _ := f.win.GetFramebufferSize()
	w, _ := f.win.GetSize()
	if w == 0 {
		return 1
	}
	return float32(fw) / float32(w)
}

func (f *frameLoop) build(w, h int) *gfx.DisplayList {
	dl := &gfx.DisplayList{}
	f.u.NoteDPR(f.dpr())
	f.u.BuildFrame(dl, float32(w), float32(h))
	return dl
}

func (f *frameLoop) windowFrame() render.Frame {
	w, h := f.win.GetSize()
	fw, fh := f.win.GetFramebufferSize()
	return render.Frame{
		ViewW: float32(w), ViewH: float32(h),
		DevW: int32(fw), DevH: int32(fh),
		Time: float32(glfw.GetTime()),
	}
}

func (f *frameLoop) fullDraw() {
	fr := f.windowFrame()
	dl := f.build(int(fr.ViewW), int(fr.ViewH))
	fr.SkipUnpainted = !f.out.unshown
	res := f.r.Render(dl, *ui.Tok("bg"), fr)
	f.animating = dl.WantsAnimation() && !res.Oversized && !res.Stopped && !res.Empty
	f.out.frame(res)
	f.lastDraw = time.Now()
	if tick := f.u.TickIn(); tick > 0 {
		f.nextTick = f.lastDraw.Add(tick)
	} else {
		f.nextTick = time.Time{}
	}
}

// during a live resize macOS runs a modal loop and the window loop sits in
// WaitEvents, so marking dirty alone would show the last frame stretched
func (f *frameLoop) liveDraw() {
	if !f.live {
		return
	}
	f.dirty = false
	f.fullDraw()
}

func (f *frameLoop) partialDraw() bool {
	if !f.r.RenderPartial(*ui.Tok("bg"), f.windowFrame()) {
		return false
	}
	f.out.present()
	f.lastDraw = time.Now()
	return true
}

const (
	firstRetry = 50 * time.Millisecond
	maxRetry   = 2 * time.Second
)

type presenter struct {
	dev     gpu.Device
	unshown bool
	wait    time.Duration
	retryAt time.Time
}

func (p *presenter) frame(res render.FrameResult) {
	if res.Oversized || res.Stopped || res.Empty {
		p.shown()
		return
	}
	if res.Painted || p.unshown {
		p.present()
	}
}

func (p *presenter) present() {
	framePresented.Add(1)
	if p.dev.Present() {
		p.shown()
		return
	}
	p.unshown = true
	p.wait = min(max(p.wait*2, firstRetry), maxRetry)
	p.retryAt = time.Now().Add(p.wait)
}

func (p *presenter) shown() {
	p.unshown = false
	p.wait = 0
}

func (p *presenter) retryIn(now time.Time) (time.Duration, bool) {
	if !p.unshown {
		return 0, false
	}
	return max(p.retryAt.Sub(now), 0), true
}

func (f *frameLoop) animatingVisibly() bool {
	return f.animating && !f.hidden && !f.r.AnimationIsInvisible()
}

func (f *frameLoop) step() {
	frameSteps.Add(1)
	f.sess.Pump()
	if !f.nextTick.IsZero() && !time.Now().Before(f.nextTick) {
		f.u.NoteTick()
		f.dirty = true
	}
	retry, pending := f.out.retryIn(time.Now())
	if (f.dirty || pending && retry == 0) && !f.hidden {
		f.dirty = false
		f.fullDraw()
	} else if f.animatingVisibly() && !f.partialDraw() {
		f.fullDraw()
	}
}

func (f *frameLoop) run() {
	f.live = true
	starve := 25 * time.Millisecond
	if f.minFrame > starve {
		starve = f.minFrame + f.minFrame/2
	}
	pacer.Install(func() {
		if time.Since(f.lastDraw) >= starve {
			f.step()
		}
	})
	pacerArmed := false
	for !f.win.ShouldClose() {
		f.step()
		paint := f.animatingVisibly()
		if paint != pacerArmed {
			pacerArmed = paint
			if paint {
				pacer.Arm(1.0 / 60.0)
			} else {
				pacer.Idle()
			}
		}
		retry, pending := f.out.retryIn(time.Now())
		switch tick := f.u.TickIn(); {
		case paint:
			if wait := f.minFrame - time.Since(f.lastDraw); wait > 0 {
				glfw.WaitEventsTimeout(wait.Seconds())
			} else {
				glfw.PollEvents()
			}
		case pending && !f.hidden && (tick <= 0 || retry < tick):
			glfw.WaitEventsTimeout(retry.Seconds())
		case tick > 0 && !f.hidden:
			glfw.WaitEventsTimeout(tick.Seconds())
		default:
			glfw.WaitEvents()
		}
	}
}

func (f *frameLoop) shot(o Options) error {
	inputs, err := parseShotInputs(o.Clicks)
	if err != nil {
		return err
	}
	err = withShot(f.dev, o.W, o.H, o.Shot, func(target gpu.Texture) error {
		frame := func(t float32) render.Frame {
			return render.Frame{
				ViewW: float32(o.W), ViewH: float32(o.H),
				DevW: int32(o.W * 2), DevH: int32(o.H * 2),
				Target: target, Time: t,
			}
		}
		start := glfw.GetTime()
		for glfw.GetTime()-start < o.SettleMs/1000 {
			f.sess.Pump()
			for len(inputs) > 0 && (glfw.GetTime()-start)*1000 >= inputs[0].atMs {
				f.replay(inputs[0])
				inputs = inputs[1:]
			}
			if err := shotFailed(f.r.Render(f.build(o.W, o.H), *ui.Tok("bg"), frame(float32(glfw.GetTime()-start)))); err != nil {
				return err
			}
			f.dev.Present()
			glfw.WaitEventsTimeout(0.02)
		}
		f.sess.Pump()
		return shotFailed(f.r.Render(f.build(o.W, o.H), *ui.Tok("bg"), frame(float32(o.SettleMs/1000))))
	})
	if err != nil {
		return err
	}
	fmt.Println("wrote", o.Shot)
	return nil
}

func (f *frameLoop) replay(in shotInput) {
	u := f.u
	switch in.kind {
	case inputMenu:
		if !menuPerform(in.menu) && (u.Menubar == nil || !u.Menubar.Perform(in.menu)) {
			fmt.Printf("menu probe: no item with id %d\n", in.menu)
		}
	case inputAux:
		u.AuxDown(in.aux)
	case inputType:
		for _, r := range in.typ {
			u.CharInput(r)
		}
	case inputKey:
		u.KeyDown(parseKeyChord(in.key))
	case inputMove:
		u.PointerMove(in.x, in.y)
	case inputRightClick:
		u.PointerMove(in.x, in.y)
		u.ContextClick(in.x, in.y)
	case inputDrag:
		u.PointerMove(in.x, in.y)
		u.PointerDown(in.x, in.y)
		u.PointerMove((in.x+in.x2)/2, (in.y+in.y2)/2)
		u.PointerMove(in.x2, in.y2)
		u.PointerUp(in.x2, in.y2)
	case inputRightDrag:
		u.PointerMove(in.x, in.y)
		if !u.RightDown(in.x, in.y) {
			u.ContextClick(in.x, in.y)
		}
		u.PointerMove((in.x+in.x2)/2, (in.y+in.y2)/2)
		u.PointerMove(in.x2, in.y2)
		u.RightUp(in.x2, in.y2)
	case inputWheel:
		u.Wheel(in.x, in.y, in.x2, in.y2)
	default:
		u.PointerMove(in.x, in.y)
		u.PointerDown(in.x, in.y)
		u.PointerUp(in.x, in.y)
	}
}
