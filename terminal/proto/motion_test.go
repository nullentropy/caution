package proto

import (
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"
)

// a theme patch crossfades unless motion is reduced, so whether it lands at
// once shows which motion setting the op installed
func TestMotionOpControlsThemeCrossfade(t *testing.T) {
	saved := *ui.Theme["accent"]
	defer func() { *ui.Theme["accent"] = saved }()
	s := &Session{ui: ui.New()}
	s.applyOp(&patchOp{Op: wire.OpMotion, Motion: wire.MotionReduced})
	s.applyOp(&patchOp{Op: wire.OpTheme, Tokens: map[string]string{"accent": "#ff0000"}})
	if *ui.Theme["accent"] != gfx.Hex("#ff0000") {
		t.Fatal("theme crossfaded after a reduced motion op")
	}
	s.applyOp(&patchOp{Op: wire.OpMotion, Motion: wire.MotionFull})
	s.applyOp(&patchOp{Op: wire.OpTheme, Tokens: map[string]string{"accent": "#00ff00"}})
	if *ui.Theme["accent"] == gfx.Hex("#00ff00") {
		t.Fatal("theme switched at once after a full motion op")
	}
}
