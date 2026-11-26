package ui

import (
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
)

func animScene() (*Panel, *ShaderPane, *Label) {
	root := NewPanel()
	pane := NewShaderPane()
	pane.Frame = &gfx.Rect{X: 10, Y: 10, W: 120, H: 90}
	pane.Frag = "vec4 effect(vec2 uv) { return vec4(fract(u_time), uv, 1.0); }"
	pane.Animate = true
	root.Add(pane)
	label := NewLabel("tick 0", gfx.NewFont(13, gfx.FontOpts{}), nil)
	label.Frame = &gfx.Rect{X: 10, Y: 200, W: 120, H: 18}
	root.Add(label)
	return root, pane, label
}

func TestSplicedAnimationKeepsAskingForFrames(t *testing.T) {
	u := fakeUi()
	root, _, label := animScene()
	u.Root = root

	first := &gfx.DisplayList{}
	u.BuildFrame(first, 400, 300)
	if !first.WantsAnimation() {
		t.Fatal("the first frame must want animation")
	}

	label.Text = "tick 1"
	label.InvalidateWidget()
	second := &gfx.DisplayList{}
	u.BuildFrame(second, 400, 300)

	if u.Reused() == 0 {
		t.Fatal("nothing was spliced, so this test proves nothing")
	}
	if !second.WantsAnimation() {
		t.Fatal("a spliced animated pane stopped asking for frames: " +
			"animation would die on the first unrelated patch")
	}
	if plan := gfx.PlanDamage(second, gfx.R(0, 0, 400, 300)); len(plan.Rects) == 0 {
		t.Fatal("the spliced pane produced no animation damage")
	}
}

func TestCleanFrameKeepsAskingForFrames(t *testing.T) {
	u := fakeUi()
	root, _, _ := animScene()
	u.Root = root
	u.BuildFrame(&gfx.DisplayList{}, 400, 300)

	for i := 0; i < 3; i++ {
		dl := &gfx.DisplayList{}
		u.BuildFrame(dl, 400, 300)
		if !dl.WantsAnimation() {
			t.Fatalf("quiet frame %d stopped asking for animation", i+1)
		}
	}
}

func TestStaticSceneAsksForNothing(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	root.Add(NewLabel("still", gfx.NewFont(13, gfx.FontOpts{}), nil))
	u.Root = root
	for i := 0; i < 3; i++ {
		dl := &gfx.DisplayList{}
		u.BuildFrame(dl, 400, 300)
		if dl.WantsAnimation() {
			t.Fatalf("frame %d asked for animation in a still scene", i+1)
		}
	}
}
