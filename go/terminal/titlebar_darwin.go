//go:build darwin

package terminal

/*
#cgo LDFLAGS: -framework Cocoa
void caution_custom_titlebar(void *nswindow, int style);
*/
import "C"

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// applyCustomTitlebar restyles the created window for app-drawn chrome:
// full-bleed content with the system traffic lights floating over it, at
// the position the style picks.
func applyCustomTitlebar(win *glfw.Window, style string) {
	s := 0
	switch style {
	case "compact":
		s = 1
	case "tall":
		s = 2
	}
	C.caution_custom_titlebar(unsafe.Pointer(win.GetCocoaWindow()), C.int(s))
}
