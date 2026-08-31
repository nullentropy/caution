//go:build darwin

package terminal

/*
#cgo LDFLAGS: -framework Cocoa
void caution_custom_titlebar(void *nswindow, int style);
void caution_watch_fullscreen(void *nswindow);
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

var onFullscreen func(bool)

//export cautionFullscreenChanged
func cautionFullscreenChanged(on C.int) {
	if onFullscreen != nil {
		onFullscreen(on != 0)
	}
}

// watchFullscreen reports the window entering and leaving the system's own
// fullscreen, which is not Options.Fullscreen: that one takes over a monitor at
// a video mode, while this is the green button and Cmd+Ctrl+F.
func watchFullscreen(win *glfw.Window, fn func(on bool)) {
	onFullscreen = fn
	C.caution_watch_fullscreen(unsafe.Pointer(win.GetCocoaWindow()))
}
