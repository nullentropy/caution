// damageprobe: damage frames must be pixel identical to a full render
//
// one small change at a time to a scene built to make partial repaints hard,
// each damage frame compared against a from-scratch render of the same list
//
//	go run ./cmd/damageprobe
package main

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/glx"
	"github.com/nullentropy/caution/terminal/render"
	"github.com/nullentropy/caution/terminal/text"
	"github.com/nullentropy/caution/terminal/ui"
)

const (
	W, H = 700, 480
	dpr  = 2
)

func init() { runtime.LockOSThread() }

type scene struct {
	u      *ui.UI
	root   *ui.Panel
	under  *ui.Label // covered by a later sibling
	edge   *ui.Label // straddles the shadowed card's edge
	slider *ui.Panel // slides under the unchanged text
	inner  *ui.Label // inside the clipping scroller
	shadow *ui.Panel
	fx     *ui.Panel // carries an effect layer
	pane   *ui.ShaderPane
	banner *ui.Label
}

func build(sh *text.Shaper) *scene {
	u := ui.New()
	u.Measure = func(f gfx.Font, s string) *text.Run { return sh.Shape(f, dpr, s) }

	root := ui.NewPanel()
	root.Bg = ui.Theme["bg"]
	s := &scene{u: u, root: root}

	// later sibling covers half of it, replaying the label alone would put it
	// on top
	s.under = ui.NewLabel("under the card, half covered", gfx.NewFont(14, gfx.FontOpts{}), nil)
	s.under.Frame = &gfx.Rect{X: 20, Y: 20, W: 300, H: 20}
	root.Add(s.under)

	s.shadow = ui.NewPanel()
	s.shadow.Frame = &gfx.Rect{X: 150, Y: 10, W: 200, H: 90}
	s.shadow.Bg = ui.Theme["panelAlt"]
	s.shadow.Radius = gfx.CornerRadius(8)
	s.shadow.DropShadow = &ui.Shadow{Blur: 18, Color: gfx.WithAlpha(gfx.Black, 0.5), Dy: 4}
	root.Add(s.shadow)

	s.edge = ui.NewLabel("straddling the edge of the card and beyond", gfx.NewFont(13, gfx.FontOpts{}), nil)
	s.edge.Frame = &gfx.Rect{X: 160, Y: 60, W: 400, H: 18}
	root.Add(s.edge)

	s.slider = ui.NewPanel()
	s.slider.Frame = &gfx.Rect{X: 24, Y: 22, W: 60, H: 16}
	s.slider.Bg = ui.Theme["accent"]
	root.Add(s.slider)

	clip := ui.NewPanel()
	clip.Frame = &gfx.Rect{X: 20, Y: 130, W: 180, H: 60}
	clip.Bg = ui.Theme["panel"]
	clip.Clips = true
	root.Add(clip)
	s.inner = ui.NewLabel("clipped text running off the right edge", gfx.NewFont(13, gfx.FontOpts{}), nil)
	s.inner.Frame = &gfx.Rect{X: 8, Y: 20, W: 400, H: 18}
	clip.Add(s.inner)

	s.fx = ui.NewPanel()
	s.fx.Frame = &gfx.Rect{X: 230, Y: 130, W: 200, H: 120}
	s.fx.Bg = ui.Theme["panel"]
	s.fx.Effect = &ui.Effect{Frag: "vec4 effect(vec2 uv) { return src(uv) * vec4(1.0, 0.9, 0.8, 1.0); }"}
	root.Add(s.fx)
	fxLabel := ui.NewLabel("inside the effect layer", gfx.NewFont(13, gfx.FontOpts{}), nil)
	fxLabel.Frame = &gfx.Rect{X: 10, Y: 10, W: 180, H: 18}
	s.fx.Add(fxLabel)

	// animated pane. the command is identical frame to frame but u_time isn't,
	// its damage has to survive every diff
	s.pane = ui.NewShaderPane()
	s.pane.Frame = &gfx.Rect{X: 460, Y: 130, W: 200, H: 120}
	s.pane.Frag = "vec4 effect(vec2 uv) { return vec4(uv.x, uv.y, fract(u_time), 1.0); }"
	s.pane.Animate = true
	root.Add(s.pane)

	// so the tree isn't trivially small
	for i := 0; i < 24; i++ {
		row := ui.NewPanel()
		row.Frame = &gfx.Rect{X: 20, Y: float32(270 + i*8), W: 640, H: 7}
		row.Bg = ui.Theme["panelAlt"]
		root.Add(row)
	}

	u.Root = root

	bp := ui.NewPanel()
	bp.Bg = ui.Theme["accent"]
	bl := ui.NewLabel("banner above everything", gfx.NewFont(12, gfx.FontOpts{}), nil)
	bl.Anchors = &ui.Anchors{Left: f32p(18), Bottom: f32p(14)}
	bp.Add(bl)
	s.banner = bl
	u.Banner = bp
	return s
}

func f32p(v float32) *float32 { return &v }

func (s *scene) frame() *gfx.DisplayList {
	dl := &gfx.DisplayList{}
	s.u.BuildFrame(dl, W, H)
	return dl
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "damageprobe:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := glfw.Init(); err != nil {
		return err
	}
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	glfw.WindowHint(glfw.Visible, glfw.False)
	win, err := glfw.CreateWindow(W, H, "damageprobe", nil, nil)
	if err != nil {
		return err
	}
	win.MakeContextCurrent()
	if err := glx.Init(); err != nil {
		return err
	}

	fbo, read := target()

	// live keeps state and takes the damage path, fresh renders from nothing
	// and is the ground truth
	live, err := render.New()
	if err != nil {
		return err
	}

	s := build(live.Shaper)
	steps := []struct {
		name     string
		do       func()
		wantFull bool
	}{
		{"nothing changed", func() {}, false},
		{"one label's text (overlapped by a later sibling)", func() { s.under.Text = "under the card, TEXT CHANGED"; s.under.Invalidate() }, false},
		{"text straddling the card edge", func() { s.edge.Text = "straddling the EDGE of the card and beyond"; s.edge.Invalidate() }, false},
		{"text inside a clipping parent", func() { s.inner.Text = "clipped text RUNNING off the right edge"; s.inner.Invalidate() }, false},
		{"the shadowed card moves", func() { s.shadow.Frame.X = 170; s.shadow.Invalidate() }, false},
		{"a label inside the effect layer", func() {
			l := s.fx.Base().Kids[0].(*ui.Label)
			l.Text = "inside the EFFECT layer"
			l.Invalidate()
		}, false},
		{"the banner above the tree", func() { s.banner.Text = "banner CHANGED above everything"; s.banner.Invalidate() }, false},
		{"a shadow's blur grows past its widget", func() { s.shadow.DropShadow.Blur = 30; s.shadow.Invalidate() }, false},
		{"one row far down the list", func() { w := s.root.Base().Kids[12]; w.Base().Frame.X += 3; w.Base().Invalidate() }, false},
		// the unchanged label has to be replayed inside the damage rect
		{"a quad moves under unchanged text", func() { s.slider.Frame.X = 60; s.slider.Invalidate() }, false},
		// fewer commands than last frame, index-aligned walk can't apply so
		// the prefix/suffix trim has to isolate the loss
		{"a drop shadow disappears", func() { s.shadow.DropShadow = nil; s.shadow.Invalidate() }, false},
		// same command, different u_time, must repaint anyway
		{"only the clock moved (animated pane)", func() {}, false},
		// one token is still worth scissoring. a whole theme isn't, and it
		// moves the clear color which no display list records
		{"one theme token", func() { ui.ApplyTheme(map[string]string{"panelAlt": "#3a2030"}); s.u.Invalidate() }, false},
		{"the whole theme", func() {
			ui.ApplyTheme(map[string]string{"bg": "#2a1020", "panel": "#3a2030", "panelAlt": "#452838", "ink": "#ffe8f0"})
			s.u.Invalidate()
		}, true},
	}

	t := float32(1.0)
	prev := s.frame()
	live.Render(prev, *ui.Tok("bg"), frame(fbo, t))
	fails := 0
	view := gfx.R(0, 0, W, H)
	for i, step := range steps {
		step.do()
		t += 0.25
		before := live.Frames
		dl := s.frame()
		// reported so a step that quietly stopped changing anything can't pass
		rects, _ := gfx.DiffDamage(prev, dl, view, gfx.PlanDamage(dl, view).Rects)
		prev = dl
		bg := *ui.Tok("bg")
		live.Render(dl, bg, frame(fbo, t))
		got := read()
		path := live.Frames.Damage - before.Damage
		fresh, err := render.New()
		if err != nil {
			return err
		}
		fresh.Render(dl, bg, frame(fbo, t))
		want := read()

		diff, first := compare(got, want)
		how := "FULL"
		if path == 1 {
			how = "damage"
		}
		status := "identical"
		if diff > 0 {
			status = fmt.Sprintf("MISMATCH %d bytes, first at %s", diff, first)
			fails++
		}
		if step.wantFull != (path == 0) {
			status += " - WRONG PATH"
			fails++
		}
		fmt.Printf("step %2d [%s] reused=%3d damage=%d  %-46s %s\n",
			i+1, how, s.u.Reused(), len(rects), step.name, status)
	}
	if fails > 0 {
		return fmt.Errorf("%d/%d steps mismatched", fails, len(steps))
	}
	fmt.Printf("\nall %d steps byte-identical; frames: %+v\n", len(steps), live.Frames)

	for i := range 3 {
		t += 0.25
		if !live.RenderPartial(*ui.Tok("bg"), frame(fbo, t)) {
			return fmt.Errorf("animation continuation %d refused the retained list", i+1)
		}
		got := read()
		fresh, err := render.New()
		if err != nil {
			return err
		}
		fresh.Render(prev, *ui.Tok("bg"), frame(fbo, t))
		diff, first := compare(got, read())
		status := "identical"
		if diff > 0 {
			status = fmt.Sprintf("MISMATCH %d bytes, first at %s", diff, first)
			fails++
		}
		fmt.Printf("continuation %d (t=%.2f, no tree walk)                          %s\n", i+1, t, status)
	}
	if fails > 0 {
		return fmt.Errorf("%d checks mismatched", fails)
	}
	if err := presentation(*ui.Tok("bg"), fbo, read); err != nil {
		return err
	}
	return bench(*ui.Tok("bg"), fbo)
}

func frame(fbo uint32, t float32) render.Frame { return frameOpt(fbo, t, true) }

// the probe reads the destination instead of presenting it, and the
// presentation check depends on what an unpainted frame leaves there
func frameOpt(fbo uint32, t float32, skipUnpainted bool) render.Frame {
	return render.Frame{
		ViewW: W, ViewH: H, DevW: W * dpr, DevH: H * dpr, FBO: fbo, Time: t,
		SkipUnpainted: skipUnpainted,
	}
}

func target() (uint32, func() []byte) {
	devW, devH := int32(W*dpr), int32(H*dpr)
	var tex, fbo uint32
	glx.GenTextures(1, &tex)
	glx.ActiveTexture(glx.TEXTURE0)
	glx.BindTexture(glx.TEXTURE_2D, tex)
	glx.TexImage2D(glx.TEXTURE_2D, 0, glx.RGBA, devW, devH, 0, glx.RGBA, glx.UNSIGNED_BYTE, nil)
	glx.GenFramebuffers(1, &fbo)
	glx.BindFramebuffer(glx.FRAMEBUFFER, fbo)
	glx.FramebufferTexture2D(glx.FRAMEBUFFER, glx.COLOR_ATTACHMENT0, glx.TEXTURE_2D, tex, 0)
	return fbo, func() []byte {
		pix := make([]byte, devW*devH*4)
		glx.BindFramebuffer(glx.FRAMEBUFFER, fbo)
		glx.ReadPixels(0, 0, devW, devH, glx.RGBA, glx.UNSIGNED_BYTE, glx.Ptr(pix))
		return pix
	}
}

func compare(got, want []byte) (int, string) {
	n := 0
	first := ""
	for i := range got {
		if got[i] != want[i] {
			n++
			if first == "" {
				px := i / 4
				first = fmt.Sprintf("(%d,%d)", px%(W*dpr), H*dpr-1-px/(W*dpr))
			}
		}
	}
	return n, first
}

// -- cost -------------------------------------------------------------------

// two commands each, the ~2600-command tree the build benchmarks use
// mostly below the viewport like a long scrolled list
const benchRows = 1300

// two different lists force the whole-frame path (area guard declines),
// a list and a one-label variant force the damage path. same GL pipeline
// both ways so the difference is what invalidation saves
func bench(bg gfx.Color, fbo uint32) error {
	u := ui.New()
	sh := text.NewShaper(text.NewRegistry())
	u.Measure = func(f gfx.Font, s string) *text.Run { return sh.Shape(f, dpr, s) }
	root := ui.NewPanel()
	root.Bg = ui.Theme["bg"]
	var first *ui.Label
	for i := 0; i < benchRows; i++ {
		y := float32(20 + i*5)
		p := ui.NewPanel()
		p.Frame = &gfx.Rect{X: 20, Y: y, W: W - 40, H: 4}
		p.Bg = ui.Theme["panelAlt"]
		root.Add(p)
		l := ui.NewLabel(fmt.Sprintf("row %d - static", i), gfx.NewFont(11, gfx.FontOpts{}), nil)
		l.Frame = &gfx.Rect{X: 26, Y: y - 4, W: 200, H: 14}
		root.Add(l)
		if first == nil {
			first = l
		}
	}
	u.Root = root
	build := func() *gfx.DisplayList {
		dl := &gfx.DisplayList{}
		u.BuildFrame(dl, W, H)
		return dl
	}
	r, err := render.New()
	if err != nil {
		return err
	}
	a := build()
	// everything moved, nothing worth scissoring
	root.Base().Bounds.Y += 3
	for _, k := range root.Base().Kids {
		k.Base().Frame.Y += 3
	}
	u.Invalidate()
	far := build()
	for _, k := range root.Base().Kids {
		k.Base().Frame.Y -= 3
	}
	u.Invalidate()
	a = build()
	first.Text = "row 0 - PATCHED"
	first.Invalidate()
	near := build()

	timeIt := func(n int, lists ...*gfx.DisplayList) time.Duration {
		start := time.Now()
		for i := 0; i < n; i++ {
			r.Render(lists[i%len(lists)], bg, frame(fbo, float32(i)))
		}
		return time.Since(start) / time.Duration(n)
	}
	const n = 300
	timeIt(20, a, far) // warm
	full := timeIt(n, a, far)
	timeIt(20, a, near)
	dmg := timeIt(n, a, near)
	fmt.Printf("\nrender, %d commands: whole frame %.0fµs, one-label damage %.0fµs (%+.0f%%)\n",
		len(a.Cmds), float64(full.Microseconds()), float64(dmg.Microseconds()),
		100*(float64(dmg)/float64(full)-1))
	fmt.Printf("frames: %+v\n", r.Frames)
	return nil
}

// -- presentation ------------------------------------------------------------

// a shell can only skip the swap if an empty-damage frame reports Painted
// false AND left the destination byte for byte. otherwise a stale or cleared
// frame hits the screen for a tick
func presentation(bg gfx.Color, fbo uint32, read func() []byte) error {
	u := ui.New()
	sh := text.NewShaper(text.NewRegistry())
	u.Measure = func(f gfx.Font, s string) *text.Run { return sh.Shape(f, dpr, s) }
	root := ui.NewPanel()
	root.Bg = ui.Theme["bg"]
	card := ui.NewPanel()
	card.Frame = &gfx.Rect{X: 40, Y: 40, W: 300, H: 120}
	card.Bg = ui.Theme["panelAlt"]
	card.Radius = gfx.CornerRadius(8)
	root.Add(card)
	label := ui.NewLabel("still life", gfx.NewFont(14, gfx.FontOpts{}), nil)
	label.Frame = &gfx.Rect{X: 56, Y: 60, W: 260, H: 20}
	root.Add(label)
	u.Root = root
	build := func() *gfx.DisplayList {
		dl := &gfx.DisplayList{}
		u.BuildFrame(dl, W, H)
		return dl
	}

	r, err := render.New()
	if err != nil {
		return err
	}
	// nothing retained, everything painted
	first := r.Render(build(), bg, frame(fbo, 1))
	if !first.Painted || !first.Full {
		return fmt.Errorf("first frame reported %+v, want a painted full frame", first)
	}
	before := read()

	// same tree
	quiet := r.Render(build(), bg, frame(fbo, 2))
	if quiet.Painted || quiet.Rects != 0 {
		return fmt.Errorf("an unchanged frame reported %+v, want nothing painted", quiet)
	}
	if n, first := compare(read(), before); n > 0 {
		return fmt.Errorf("an unchanged frame disturbed %d bytes of the destination "+
			"(first at %s) while reporting nothing painted - skipping the swap would "+
			"show a stale frame", n, first)
	}

	// a real change still presents or the skip strands the screen
	label.Text = "still life, moved"
	label.Invalidate()
	changed := r.Render(build(), bg, frame(fbo, 3))
	if !changed.Painted || changed.Rects == 0 {
		return fmt.Errorf("a changed frame reported %+v, want damage painted", changed)
	}
	if n, _ := compare(read(), before); n == 0 {
		return fmt.Errorf("a changed frame left the destination identical")
	}

	// a swap leaves the destination undefined behind the renderer's back, so
	// clobber it like a swap would. by default Render must blit, a caller that
	// always presents needs its pixels there. opted in it must skip, safe only
	// because that caller doesn't present
	clobber := func() {
		glx.BindFramebuffer(glx.FRAMEBUFFER, fbo)
		glx.Disable(glx.SCISSOR_TEST)
		glx.ClearColor(1, 0, 1, 1) // magenta, nothing in the scene is this
		glx.Clear(glx.COLOR_BUFFER_BIT)
	}
	good := read()

	clobber()
	if res := r.Render(build(), bg, frameOpt(fbo, 4, false)); res.Painted {
		return fmt.Errorf("unchanged frame reported %+v, want nothing painted", res)
	}
	if n, at := compare(read(), good); n > 0 {
		return fmt.Errorf("with SkipUnpainted off, an unchanged frame left %d bytes "+
			"of the destination unwritten (first at %s) - a caller that presents "+
			"every frame would show garbage", n, at)
	}

	clobber()
	if res := r.Render(build(), bg, frameOpt(fbo, 5, true)); res.Painted {
		return fmt.Errorf("unchanged frame reported %+v, want nothing painted", res)
	}
	if n, _ := compare(read(), good); n == 0 {
		return fmt.Errorf("with SkipUnpainted on, the blit happened anyway - the " +
			"skip is not being exercised, so the opt-in proves nothing")
	}

	fmt.Printf("presentation: unchanged frame paints nothing and disturbs nothing; "+
		"changed frame paints %d rect(s); the blit is skipped only when the caller "+
		"opted in\n", changed.Rects)
	return nil
}
