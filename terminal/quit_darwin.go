//go:build darwin

package terminal

/*
#cgo LDFLAGS: -framework Cocoa
void caution_filter_quit(void);
*/
import "C"

//export cautionQuitAllowed
func cautionQuitAllowed(sender *C.char) C.int {
	if quitAllowed(C.GoString(sender)) {
		return 1
	}
	return 0
}

// main thread, after the first window so AppKit's own handler is in place
func installQuitFilter() { C.caution_filter_quit() }
