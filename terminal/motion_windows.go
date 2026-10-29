//go:build windows

package terminal

import (
	"syscall"
	"unsafe"
)

const spiGetClientAreaAnimation = 0x1042

var systemParametersInfo = syscall.NewLazyDLL("user32.dll").NewProc("SystemParametersInfoW")

// Settings > Accessibility > Visual effects > Animation effects, also off in
// many RDP sessions
func systemReducedMotion() bool {
	var on int32
	ok, _, _ := systemParametersInfo.Call(spiGetClientAreaAnimation, 0, uintptr(unsafe.Pointer(&on)), 0)
	return ok != 0 && on == 0
}
