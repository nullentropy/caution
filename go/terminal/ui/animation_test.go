package ui

import (
	"testing"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

// animScene is a pane that asks for continuous frames next to a label the
// server patches - the shape of every app that animates anything while its
// data moves.
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

// TestSplicedAnimationKeepsAskingForFrames pins the requirement that
// WantsAnimation is derived from the retained commands, never accumulated at
// record time. An accumulated flag, set by DisplayList.ShaderQuad and EndLayer
// as they record, would be skipped by any subtree that splices its retained
// commands instead of re-recording them, and an animated pane is byte-identical
// frame to frame, which makes it a splice candidate on every frame that changes
// anything else. The shells read the flag to decide whether to keep painting,
// so losing it stops all animation on the first unrelated patch.
func TestSplicedAnimationKeepsAskingForFrames(t *testing.T) {
	u := fakeUi()
	root, _, label := animScene()
	u.Root = root

	first := &gfx.DisplayList{}
	u.BuildFrame(first, 400, 300)
	if !first.WantsAnimation() {
		t.Fatal("the first frame must want animation")
	}

	// An unrelated patch: the label changes, the pane cannot have.
	label.Text = "tick 1"
	label.Invalidate()
	second := &gfx.DisplayList{}
	u.BuildFrame(second, 400, 300)

	if u.Reused() == 0 {
		t.Fatal("nothing was spliced, so this test proves nothing")
	}
	if !second.WantsAnimation() {
		t.Fatal("a spliced animated pane stopped asking for frames: " +
			"animation would die on the first unrelated patch")
	}
	// The commands themselves must still carry the animation, or the
	// renderer's damage plan has nothing to replay either.
	if plan := gfx.PlanDamage(second, gfx.R(0, 0, 400, 300)); len(plan.Rects) == 0 {
		t.Fatal("the spliced pane produced no animation damage")
	}
}

// TestCleanFrameKeepsAskingForFrames is the same hazard with no patch:
// a frame where every subtree splices must still report the animation, or the
// loop stops after its first quiet frame.
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

// TestStaticSceneAsksForNothing is the other direction: deriving the flag from
// the commands must not invent animation where there is none, or a still tree
// would spin the loop forever.
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
