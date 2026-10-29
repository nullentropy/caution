//go:build darwin

// Package pacer keeps frames flowing through AppKit's modal event loops
// menu tracking parks the main thread in a modal run loop where
// glfw.WaitEvents never returns and posted wakes queue undelivered, but
// timers in kCFRunLoopCommonModes still fire. the shell arms one while
// animation needs frames and the callback steps a frame only when the main
// loop is actually starved
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

// main thread only, call once
func Install(fn func()) {
	fire = fn
	C.caution_pacer_install()
}

// next fire interval seconds out, then 60Hz until re-armed or idled
func Arm(interval float64) { C.caution_pacer_arm(C.double(interval)) }

// pushes the next fire to the far future, a quiet app pays nothing
func Idle() { C.caution_pacer_idle() }
