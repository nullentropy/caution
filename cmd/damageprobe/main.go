package main

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/nullentropy/caution/terminal"
	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/gpu"
	"github.com/nullentropy/caution/terminal/render"
	"github.com/nullentropy/caution/terminal/text"
	"github.com/nullentropy/caution/terminal/ui"
)

const (
	W, H = 700, 480
	dpr  = 2
)

func init() { runtime.LockOSThread() }

var dev gpu.Device

type scene struct {
	u      *ui.UI
	root   *ui.Panel
	under  *ui.Label
	edge   *ui.Label
	slider *ui.Panel
	inner  *ui.Label
	shadow *ui.Panel
	fx     *ui.Panel
	pane   *ui.ShaderPane
	banner *ui.Label
}

func build(sh *text.Shaper) *scene {
	u := ui.New()
	u.Measure = func(f gfx.Font, s string) *text.Run { return sh.Shape(f, dpr, s) }

	root := ui.NewPanel()
	root.Bg = ui.Theme["bg"]
	s := &scene{u: u, root: root}

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

	s.pane = ui.NewShaderPane()
	s.pane.Frame = &gfx.Rect{X: 460, Y: 130, W: 200, H: 120}
	s.pane.Frag = "vec4 effect(vec2 uv) { return vec4(uv.x, uv.y, fract(u_time), 1.0); }"
	s.pane.Animate = true
	root.Add(s.pane)

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
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	glfw.WindowHint(glfw.Visible, glfw.False)
	win, err := glfw.CreateWindow(W, H, "damageprobe", nil, nil)
	if err != nil {
		return err
	}
	dev, err = terminal.OpenDevice(win)
	if err != nil {
		return err
	}

	fbo, read := target()

	live, err := render.New(dev)
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
		{"one label's text (overlapped by a later sibling)", func() { s.under.Text = "under the card, TEXT CHANGED"; s.under.InvalidateWidget() }, false},
		{"text straddling the card edge", func() { s.edge.Text = "straddling the EDGE of the card and beyond"; s.edge.InvalidateWidget() }, false},
		{"text inside a clipping parent", func() { s.inner.Text = "clipped text RUNNING off the right edge"; s.inner.InvalidateWidget() }, false},
		{"the shadowed card moves", func() { s.shadow.Frame.X = 170; s.shadow.InvalidateWidget() }, false},
		{"a label inside the effect layer", func() {
			l := s.fx.Base().Kids[0].(*ui.Label)
			l.Text = "inside the EFFECT layer"
			l.InvalidateWidget()
		}, false},
		{"the banner above the tree", func() { s.banner.Text = "banner CHANGED above everything"; s.banner.InvalidateWidget() }, false},
		{"a shadow's blur grows past its widget", func() { s.shadow.DropShadow.Blur = 30; s.shadow.InvalidateWidget() }, false},
		{"one row far down the list", func() { w := s.root.Base().Kids[12]; w.Base().Frame.X += 3; w.Base().InvalidateWidget() }, false},
		{"a quad moves under unchanged text", func() { s.slider.Frame.X = 60; s.slider.InvalidateWidget() }, false},
		{"a drop shadow disappears", func() { s.shadow.DropShadow = nil; s.shadow.InvalidateWidget() }, false},
		{"only the clock moved (animated pane)", func() {}, false},
		{"one theme token", func() { ui.ApplyTheme(map[string]string{"panelAlt": "#3a2030"}); s.u.InvalidateAll() }, false},
		{"the whole theme", func() {
			ui.ApplyTheme(map[string]string{"bg": "#2a1020", "panel": "#3a2030", "panelAlt": "#452838", "ink": "#ffe8f0"})
			s.u.InvalidateAll()
		}, true},
	}

	t := float32(1.0)
	prev := s.frame()
	live.Render(prev, *ui.Tok("bg"), frame(fbo, t))
	dev.Present()
	fails := 0
	view := gfx.R(0, 0, W, H)
	for i, step := range steps {
		step.do()
		t += 0.25
		before := live.Frames
		dl := s.frame()
		rects, _ := gfx.DiffDamage(prev, dl, view, gfx.PlanDamage(dl, view).Rects)
		prev = dl
		bg := *ui.Tok("bg")
		live.Render(dl, bg, frame(fbo, t))
		dev.Present()
		got := read()
		path := live.Frames.Damage - before.Damage
		fresh, err := render.New(dev)
		if err != nil {
			return err
		}
		fresh.Render(dl, bg, frame(fbo, t))
		dev.Present()
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
		okPartial := live.RenderPartial(*ui.Tok("bg"), frame(fbo, t))
		dev.Present()
		if !okPartial {
			return fmt.Errorf("animation continuation %d refused the retained list", i+1)
		}
		got := read()
		fresh, err := render.New(dev)
		if err != nil {
			return err
		}
		fresh.Render(prev, *ui.Tok("bg"), frame(fbo, t))
		dev.Present()
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

func frame(fbo gpu.Texture, t float32) render.Frame { return frameOpt(fbo, t, true) }

func frameOpt(fbo gpu.Texture, t float32, skipUnpainted bool) render.Frame {
	return render.Frame{
		ViewW: W, ViewH: H, DevW: W * dpr, DevH: H * dpr, Target: fbo, Time: t,
		SkipUnpainted: skipUnpainted,
	}
}

func target() (gpu.Texture, func() []byte) {
	tex, err := dev.NewTexture(W*dpr, H*dpr, gpu.Nearest)
	if err != nil {
		panic(err)
	}
	return tex, func() []byte {
		pix, err := dev.ReadPixels(tex)
		if err != nil {
			panic(err)
		}
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
				first = fmt.Sprintf("(%d,%d)", px%(W*dpr), px/(W*dpr))
			}
		}
	}
	return n, first
}

const benchRows = 1300

func bench(bg gfx.Color, fbo gpu.Texture) error {
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
	r, err := render.New(dev)
	if err != nil {
		return err
	}
	a := build()
	root.Base().Bounds.Y += 3
	for _, k := range root.Base().Kids {
		k.Base().Frame.Y += 3
	}
	u.InvalidateAll()
	far := build()
	for _, k := range root.Base().Kids {
		k.Base().Frame.Y -= 3
	}
	u.InvalidateAll()
	a = build()
	first.Text = "row 0 - PATCHED"
	first.InvalidateWidget()
	near := build()

	timeIt := func(n int, lists ...*gfx.DisplayList) time.Duration {
		start := time.Now()
		for i := 0; i < n; i++ {
			r.Render(lists[i%len(lists)], bg, frame(fbo, float32(i)))
			dev.Present()
		}
		return time.Since(start) / time.Duration(n)
	}
	const n = 300
	timeIt(20, a, far)
	full := timeIt(n, a, far)
	timeIt(20, a, near)
	dmg := timeIt(n, a, near)
	fmt.Printf("\nrender, %d commands: whole frame %.0fµs, one-label damage %.0fµs (%+.0f%%)\n",
		len(a.Cmds), float64(full.Microseconds()), float64(dmg.Microseconds()),
		100*(float64(dmg)/float64(full)-1))
	fmt.Printf("frames: %+v\n", r.Frames)
	return nil
}

func presentation(bg gfx.Color, fbo gpu.Texture, read func() []byte) error {
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

	r, err := render.New(dev)
	if err != nil {
		return err
	}
	first := r.Render(build(), bg, frame(fbo, 1))
	dev.Present()
	if !first.Painted || !first.Full {
		return fmt.Errorf("first frame reported %+v, want a painted full frame", first)
	}
	before := read()

	quiet := r.Render(build(), bg, frame(fbo, 2))
	dev.Present()
	if quiet.Painted || quiet.Rects != 0 {
		return fmt.Errorf("an unchanged frame reported %+v, want nothing painted", quiet)
	}
	if n, first := compare(read(), before); n > 0 {
		return fmt.Errorf("an unchanged frame disturbed %d bytes of the destination "+
			"(first at %s) while reporting nothing painted - skipping the swap would "+
			"show a stale frame", n, first)
	}

	label.Text = "still life, moved"
	label.InvalidateWidget()
	changed := r.Render(build(), bg, frame(fbo, 3))
	dev.Present()
	if !changed.Painted || changed.Rects == 0 {
		return fmt.Errorf("a changed frame reported %+v, want damage painted", changed)
	}
	if n, _ := compare(read(), before); n == 0 {
		return fmt.Errorf("a changed frame left the destination identical")
	}

	clobber := func() {
		dev.SetTarget(fbo, W*dpr, H*dpr)
		dev.SetScissor(nil)
		dev.Clear(1, 0, 1, 1)
		dev.Present()
	}
	good := read()

	clobber()
	res := r.Render(build(), bg, frameOpt(fbo, 4, false))
	dev.Present()
	dev.Present()
	if res.Painted {
		return fmt.Errorf("unchanged frame reported %+v, want nothing painted", res)
	}
	if n, at := compare(read(), good); n > 0 {
		return fmt.Errorf("with SkipUnpainted off, an unchanged frame left %d bytes "+
			"of the destination unwritten (first at %s) - a caller that presents "+
			"every frame would show garbage", n, at)
	}

	clobber()
	res = r.Render(build(), bg, frameOpt(fbo, 5, true))
	dev.Present()
	if res.Painted {
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
