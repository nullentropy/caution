package render

import (
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
)

// AnimationIsInvisible reads only retained state, so no GL context needed
func TestAnimationIsInvisible(t *testing.T) {
	view := gfx.R(0, 0, 400, 300)

	// animated effect layer inside the viewport, damage exists so the frame
	// must be serviced
	visible := &gfx.DisplayList{}
	visible.BeginLayer(gfx.R(20, 20, 100, 100))
	visible.Text("x", 24, 24, gfx.Font{}, gfx.White)
	visible.EndLayer("frag", nil, true)

	// same layer scrolled far below the viewport. EndLayer still asks for
	// animation but PlanDamage clamps its rect away, so the frame would
	// paint nothing
	offscreen := &gfx.DisplayList{}
	offscreen.BeginLayer(gfx.R(20, 900, 100, 100))
	offscreen.Text("x", 24, 904, gfx.Font{}, gfx.White)
	offscreen.EndLayer("frag", nil, true)

	static := &gfx.DisplayList{}
	static.Fill(gfx.R(0, 0, 50, 50), gfx.White, gfx.Corners{})

	cases := []struct {
		name string
		dl   *gfx.DisplayList
		want bool
	}{
		{"nothing retained", nil, false},
		{"animation on screen", visible, false},
		{"animation scrolled out of view", offscreen, true},
		{"no animation at all", static, false},
	}
	for _, c := range cases {
		r := &Renderer{retained: c.dl}
		if c.dl != nil {
			r.plan = gfx.PlanDamage(c.dl, view)
		}
		if got := r.AnimationIsInvisible(); got != c.want {
			t.Fatalf("%s: AnimationIsInvisible = %v, want %v (rects=%d)",
				c.name, got, c.want, len(r.plan.Rects))
		}
	}

	// geometric animation moves bounds every frame, so even with no damage
	// rects the frame is rebuilt, never skipped
	geom := &gfx.DisplayList{}
	geom.BeginLayer(gfx.R(20, 900, 100, 100))
	geom.EndLayer("frag", nil, true)
	geom.GeomAnimation = true
	r := &Renderer{retained: geom}
	r.plan = gfx.PlanDamage(geom, view)
	if r.AnimationIsInvisible() {
		t.Fatal("a frame with geometric animation mid-flight must not be skipped")
	}
}
