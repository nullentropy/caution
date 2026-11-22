//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Cocoa
void caution_probe_hide(int hidden);
*/
import "C"

const hideSupported = true

func setAppHidden(hidden bool) {
	h := 0
	if hidden {
		h = 1
	}
	C.caution_probe_hide(C.int(h))
}
