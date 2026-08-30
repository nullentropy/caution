//go:build angle && darwin

package egl

/*
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore
#include <dlfcn.h>
#include <stdlib.h>
#include <objc/message.h>
#include <objc/runtime.h>

// windowLayer makes the NSWindow's content view layer-backed at the given
// scale and returns the CALayer, ANGLE's native window type on macOS.
static void* windowLayer(void* nsWindow, double scale) {
	id view = ((id (*)(id, SEL))objc_msgSend)((id)nsWindow, sel_registerName("contentView"));
	((void (*)(id, SEL, BOOL))objc_msgSend)(view, sel_registerName("setWantsLayer:"), 1);
	id layer = ((id (*)(id, SEL))objc_msgSend)(view, sel_registerName("layer"));
	((void (*)(id, SEL, double))objc_msgSend)(layer, sel_registerName("setContentsScale:"), scale);
	return layer;
}
*/
import "C"

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unsafe"
)

// ANGLE on macOS renders through Metal.
func angleType() int32 { return angleTypeMetal }

func trimLibName(dir string) string {
	if strings.HasSuffix(dir, ".dylib") {
		return filepath.Dir(dir)
	}
	return dir
}

// findANGLE falls back to the newest Chrome installation's copy.
func findANGLE() (string, error) {
	matches, _ := filepath.Glob("/Applications/Google Chrome.app/Contents/Frameworks/" +
		"Google Chrome Framework.framework/Versions/*/Libraries/libEGL.dylib")
	if len(matches) > 0 {
		sort.Strings(matches)
		return filepath.Dir(matches[len(matches)-1]), nil
	}
	return "", fmt.Errorf("caution: ANGLE not found, set CAUTION_ANGLE to a directory " +
		"containing libEGL.dylib and libGLESv2.dylib (a Chrome install ships them, " +
		"or build ANGLE / MetalANGLE)")
}

var eglHandle unsafe.Pointer

// openANGLE opens GLES first so libEGL's internal lookup finds ANGLE's GL,
// never the system's.
func openANGLE(dir string) error {
	if _, err := dlopen(filepath.Join(dir, "libGLESv2.dylib")); err != nil {
		return err
	}
	h, err := dlopen(filepath.Join(dir, "libEGL.dylib"))
	if err != nil {
		return err
	}
	eglHandle = h
	return nil
}

func symbol(name string) unsafe.Pointer {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return C.dlsym(eglHandle, cs)
}

func dlopen(path string) (unsafe.Pointer, error) {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	h := C.dlopen(cs, C.RTLD_NOW|C.RTLD_GLOBAL)
	if h == nil {
		return nil, fmt.Errorf("dlopen %s: %s", path, C.GoString(C.dlerror()))
	}
	return h, nil
}

// nativeWindow turns the NSWindow into the CALayer ANGLE wants.
func nativeWindow(nsWindow unsafe.Pointer, scale float64) (unsafe.Pointer, error) {
	layer := C.windowLayer(nsWindow, C.double(scale))
	if layer == nil {
		return nil, fmt.Errorf("caution: window has no backing layer")
	}
	return layer, nil
}
