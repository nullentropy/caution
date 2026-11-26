//go:build darwin

// AppKit menu tracking holds the main thread in a modal run loop. glfw.WaitEvents
// never returns inside that loop and posted wakes are not delivered, but
// timers in kCFRunLoopCommonModes still fire
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

func Install(fn func()) {
	fire = fn
	C.caution_pacer_install()
}

func Arm(interval float64) { C.caution_pacer_arm(C.double(interval)) }

func Idle() { C.caution_pacer_idle() }
