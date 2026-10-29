package ui

import (
	"testing"
	"time"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

func focusedField(t *testing.T) (*Ui, *TextField) {
	t.Helper()
	u := realUi()
	root := NewPanel()
	tf := NewTextField()
	tf.Frame = &gfx.Rect{X: 10, Y: 10, W: 200, H: 28}
	root.Add(tf)
	u.Root = root
	u.BuildFrame(&gfx.DisplayList{}, 400, 300)
	u.FocusWidget(tf)
	return u, tf
}

func TestCaretBlinksOnlyWhileWindowActive(t *testing.T) {
	u, tf := focusedField(t)
	if d := u.TickIn(); d <= 0 || d > tfBlink {
		t.Fatalf("focused field in an active window: tick %v, want a blink phase", d)
	}

	u.SetWindowActive(false)
	if d := u.TickIn(); d != 0 {
		t.Fatalf("inactive window still asks for a tick in %v", d)
	}
	if tf.blinkOn() {
		t.Fatal("inactive window still paints the caret")
	}

	tf.lastActivity = time.Now().Add(-3 * tfBlink / 2) // mid off-phase
	u.SetWindowActive(true)
	if !tf.blinkOn() {
		t.Fatal("reactivation should show the caret solid")
	}
	if d := u.TickIn(); d <= 0 || d > tfBlink {
		t.Fatalf("reactivated window: tick %v, want the blink restarted", d)
	}
}

func TestCaretSettlesSolidAfterIdle(t *testing.T) {
	u, tf := focusedField(t)

	tf.lastActivity = time.Now().Add(-tfBlinkFor + 100*time.Millisecond)
	if d := u.TickIn(); d <= 0 || d > 100*time.Millisecond {
		t.Fatalf("just before the cutoff: tick %v, want one at the cutoff", d)
	}

	tf.lastActivity = time.Now().Add(-tfBlinkFor - tfBlink) // would be an off phase
	if d := u.TickIn(); d != 0 {
		t.Fatalf("idle past the cutoff still asks for a tick in %v", d)
	}
	if !tf.blinkOn() {
		t.Fatal("a settled caret should hold solid, not hidden")
	}

	tf.OnChar('x')
	if d := u.TickIn(); d <= 0 || d > tfBlink {
		t.Fatalf("typing should restart the blink, tick %v", d)
	}
}
