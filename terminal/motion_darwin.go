//go:build darwin

package terminal

/*
#cgo LDFLAGS: -framework Cocoa
int caution_reduce_motion(void);
*/
import "C"

// System Settings > Accessibility > Display > Reduce motion
func systemReducedMotion() bool { return C.caution_reduce_motion() != 0 }
