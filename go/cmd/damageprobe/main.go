// damageprobe: pixel identity for real-damage frames.
//
// Renders a scene built to make partial repaints hard then makes one
// small change at a time and compares the damage frame against a from-scratch
// full render of the very same display list.
//
//	go run ./cmd/damageprobe
package main

import (
	"fmt"
	"os"
	time2 "time"

	"runtime"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/glx"
	"github.com/nullentropy/caution/go/terminal/render"
	"github.com/nullentropy/caution/go/terminal/text"
	"github.com/nullentropy/caution/go/terminal/ui"
)

const (
	W, H = 700, 480
	dpr  = 2
)

func init() { runtime.LockOSThread() }

type scene struct {
	u      *ui.Ui
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

	// A label with a later sibling painted over half of it: replaying the
	// label alone would put it on top. Painter's order has to be preserved.
	s.under = ui.NewLabel("under the card, half covered", gfx.NewFont(14, gfx.FontOpts{}), nil)
	s.under.Frame = &gfx.Rect{X: 20, Y: 20, W: 300, H: 20}
	root.Add(s.under)

	s.shadow = ui.NewPanel()
	s.shadow.Frame = &gfx.Rect{X: 150, Y: 10, W: 200, H: 90}
	s.shadow.Bg = ui.Theme["panelAlt"]
	s.shadow.Radius = gfx.CornerRadius(8)
	s.shadow.DropShadow = &ui.Shadow{Blur: 18, Color: gfx.WithAlpha(gfx.Black, 0.5), Dy: 4}
	root.Add(s.shadow)

	// Text that starts inside the card and runs out past its right edge.
	s.edge = ui.NewLabel("straddling the edge of the card and beyond", gfx.NewFont(13, gfx.FontOpts{}), nil)
	s.edge.Frame = &gfx.Rect{X: 160, Y: 60, W: 400, H: 18}
	root.Add(s.edge)

	// A bar that slides under `under`'s text without touching it.
	s.slider = ui.NewPanel()
	s.slider.Frame = &gfx.Rect{X: 24, Y: 22, W: 60, H: 16}
	s.slider.Bg = ui.Theme["accent"]
	root.Add(s.slider)

	// A clipping scroller: the label inside is cut by its parent's bounds.
	clip := ui.NewPanel()
	clip.Frame = &gfx.Rect{X: 20, Y: 130, W: 180, H: 60}
	clip.Bg = ui.Theme["panel"]
	clip.Clips = true
	root.Add(clip)
	s.inner = ui.NewLabel("clipped text running off the right edge", gfx.NewFont(13, gfx.FontOpts{}), nil)
	s.inner.Frame = &gfx.Rect{X: 8, Y: 20, W: 400, H: 18}
	clip.Add(s.inner)

	// An effect layer over a static subtree.
	s.fx = ui.NewPanel()
	s.fx.Frame = &gfx.Rect{X: 230, Y: 130, W: 200, H: 120}
	s.fx.Bg = ui.Theme["panel"]
	s.fx.Effect = &ui.Effect{Frag: "vec4 effect(vec2 uv) { return src(uv) * vec4(1.0, 0.9, 0.8, 1.0); }"}
	root.Add(s.fx)
	fxLabel := ui.NewLabel("inside the effect layer", gfx.NewFont(13, gfx.FontOpts{}), nil)
	fxLabel.Frame = &gfx.Rect{X: 10, Y: 10, W: 180, H: 18}
	s.fx.Add(fxLabel)

	// An animated pane: its damage has to survive every diff, because the
	// command is identical frame to frame and u_time is not.
	s.pane = ui.NewShaderPane()
	s.pane.Frame = &gfx.Rect{X: 460, Y: 130, W: 200, H: 120}
	s.pane.Frag = "vec4 effect(vec2 uv) { return vec4(uv.x, uv.y, fract(u_time), 1.0); }"
	s.pane.Animate = true
	root.Add(s.pane)

	// A tall stack below, so the tree is not trivially small.
	for i := 0; i < 24; i++ {
		row := ui.NewPanel()
		row.Frame = &gfx.Rect{X: 20, Y: float32(270 + i*8), W: 640, H: 7}
		row.Bg = ui.Theme["panelAlt"]
		root.Add(row)
	}

	u.Root = root

	// The banner layer paints above the whole tree.
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

	// Two renderers: `live` accumulates state across frames and gets to take
	// the damage path; `fresh` renders one list from nothing, which is the
	// ground truth for what those pixels must be.
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
		// A quad slides underneath text that did not change: the label has
		// to be replayed inside the damage rect or it loses its pixels.
		{"a quad moves under unchanged text", func() { s.slider.Frame.X = 60; s.slider.Invalidate() }, false},
		// Fewer commands than last frame: the index-aligned walk cannot
		// apply, and the prefix/suffix trim has to isolate the loss.
		{"a drop shadow disappears", func() { s.shadow.DropShadow = nil; s.shadow.Invalidate() }, false},
		// Time advances with nothing else changed: the animated pane is the
		// same command with a different u_time, and must repaint anyway.
		{"only the clock moved (animated pane)", func() {}, false},
		// A theme switch changes every color in the tree: no damage set is
		// worth scissoring, and the guard has to say so.
		// One token repainting a few thin rows is still damage worth
		// scissoring; a whole theme is not, and it moves the clear color
		// too - which no display list records.
		{"one theme token", func() { ui.ApplyTheme(map[string]string{"panelAlt": "#3a2030"}); s.u.Invalidate() }, false},
		{"the whole theme", func() {
			ui.ApplyTheme(map[string]string{"bg": "#2a1020", "panel": "#3a2030", "panelAlt": "#452838", "ink": "#ffe8f0"})
			s.u.Invalidate()
		}, true},
	}

	// Frame 0 establishes the scene; every step after it must come out
	// identical to a from-scratch render of the same list.
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
		// What the renderer is about to be told to repaint. Reported so a
		// step that quietly stopped changing anything cannot pass as proof.
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

	// replay the retained list at a fresh time and compare
	// against a from-scratch render of it
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
	bench(*ui.Tok("bg"), fbo)
	return nil
}

func frame(fbo uint32, t float32) render.Frame { return frameOpt(fbo, t, true) }

// frameOpt parameterizes the blit skip. The probe reads the destination rather
// than presenting it, and the presentation check turns on what an unpainted
// frame leaves there, so it drives both settings.
func frameOpt(fbo uint32, t float32, skipUnpainted bool) render.Frame {
	return render.Frame{
		ViewW: W, ViewH: H, DevW: W * dpr, DevH: H * dpr, FBO: fbo, Time: t,
		SkipUnpainted: skipUnpainted,
	}
}

// target allocates a 2x offscreen framebuffer and a reader for its pixels.
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

// bench times the two render paths against the same big list. Alternating two
// wholly different lists forces the whole-frame path every time (the damage
// would cover the view, so the area guard declines it); alternating a list
// with a one-label variant of itself forces the damage path. Both loops
// submit through the same GL pipeline, so the difference is what a real
// invalidation stopped paying.
// benchRows is the row count for the cost measurement: two commands each, so
// this is the ~2600-command tree the build benchmarks use. Most of it ends up
// below the viewport, the way a scrolled list of thousands does.
const benchRows = 1300

func bench(bg gfx.Color, fbo uint32) {
	u := ui.New()
	sh := text.NewShaper()
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
		return
	}
	a := build()
	// Everything moved: no damage set is worth scissoring.
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

	timeIt := func(n int, lists ...*gfx.DisplayList) time2.Duration {
		start := time2.Now()
		for i := 0; i < n; i++ {
			r.Render(lists[i%len(lists)], bg, frame(fbo, float32(i)))
		}
		return time2.Since(start) / time2.Duration(n)
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
}

// -- presentation ------------------------------------------------------------

// presentation checks the contract a shell needs before it dares skip a buffer
// swap: a frame whose damage came out empty must report Painted false AND have
// left the destination byte for byte as it was. Reporting "nothing painted"
// while having disturbed the scene would put a stale or cleared frame on
// screen for a tick.
func presentation(bg gfx.Color, fbo uint32, read func() []byte) error {
	u := ui.New()
	sh := text.NewShaper()
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
	// Frame one: nothing retained, so every pixel is built and presented.
	first := r.Render(build(), bg, frame(fbo, 1))
	if !first.Painted || !first.Full {
		return fmt.Errorf("first frame reported %+v, want a painted full frame", first)
	}
	before := read()

	// Frame two: the same tree, so nothing visible can differ.
	quiet := r.Render(build(), bg, frame(fbo, 2))
	if quiet.Painted || quiet.Rects != 0 {
		return fmt.Errorf("an unchanged frame reported %+v, want nothing painted", quiet)
	}
	if n, first := compare(read(), before); n > 0 {
		return fmt.Errorf("an unchanged frame disturbed %d bytes of the destination "+
			"(first at %s) while reporting nothing painted - skipping the swap would "+
			"show a stale frame", n, first)
	}

	// And a real change still presents, or the skip would strand the screen.
	label.Text = "still life, moved"
	label.Invalidate()
	changed := r.Render(build(), bg, frame(fbo, 3))
	if !changed.Painted || changed.Rects == 0 {
		return fmt.Errorf("a changed frame reported %+v, want damage painted", changed)
	}
	if n, _ := compare(read(), before); n == 0 {
		return fmt.Errorf("a changed frame left the destination identical")
	}

	// A buffer swap leaves the destination undefined and the renderer cannot see
	// that happen, so clobber it the way a swap would and render the same
	// unchanged list both ways. By default Render must blit, because a caller
	// that presents unconditionally has to find its pixels there. Opted in,
	// Render must skip, which is only safe because such a caller does not
	// present.
	clobber := func() {
		glx.BindFramebuffer(glx.FRAMEBUFFER, fbo)
		glx.Disable(glx.SCISSOR_TEST)
		glx.ClearColor(1, 0, 1, 1) // magenta; nothing in the scene is this
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
