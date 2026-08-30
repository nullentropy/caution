//go:build darwin

// Package pacer keeps frames flowing through AppKit's modal event loops.
// Menu tracking (and kin) parks the main thread inside a modal run loop
// where glfw.WaitEvents never returns and posted wakes queue undelivered
// but timers in kCFRunLoopCommonModes still fire. The shell installs one,
// arms it while animation needs frames, and the callback steps a frame only
// when the main loop is demonstrably starved.
package pacer

/*
#cgo LDFLAGS: -framework CoreFoundation
void caution_pacer_install(void);
void caution_pacer_arm(double interval);
void caution_pacer_idle(void);
*/
import "C"

var fire func()

//export cautionPacerFire
func cautionPacerFire() {
	if fire != nil {
		fire()
	}
}

// Install registers the (idle) timer on the main run loop and the callback
// it fires. Main thread only; call once.
func Install(fn func()) {
	fire = fn
	C.caution_pacer_install()
}

// Arm schedules the next fire interval seconds out (repeating at the
// installed 60Hz cadence thereafter until re-armed or idled).
func Arm(interval float64) { C.caution_pacer_arm(C.double(interval)) }

// Idle pushes the next fire to the far future - a quiet app pays nothing.
func Idle() { C.caution_pacer_idle() }
