package ui

import (
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
)

func TestAGlassWithAHoverFillPaintsItOnlyWhileHovered(t *testing.T) {
	g := NewGlass()
	fill := gfx.Hex("#223344")
	g.HoverFill = &fill
	g.Bounds = gfx.R(0, 0, 40, 20)
	idle := &gfx.DisplayList{}
	g.PaintSelf(idle)
	g.OnHoverChange(true)
	hot := &gfx.DisplayList{}
	g.PaintSelf(hot)
	if len(idle.Cmds) != 0 || len(hot.Cmds) == 0 {
		t.Fatalf("idle painted %d commands, hovered %d; want 0 and some", len(idle.Cmds), len(hot.Cmds))
	}
}
