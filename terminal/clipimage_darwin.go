//go:build darwin

package terminal

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void *caution_clipboard_png(int *len);
*/
import "C"

func clipboardImage() []byte {
	var n C.int
	p := C.caution_clipboard_png(&n)
	if p == nil {
		return nil
	}
	defer C.free(p)
	return C.GoBytes(p, n)
}
