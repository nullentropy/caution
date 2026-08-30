//go:build angle || windows

// Package egl is the ANGLE bridge: it loads ANGLE's libEGL/libGLESv2 at
// runtime (never linked at build time, so ANGLE stays a runtime choice and
// not a vendored dependency), asks EGL for a display on the platform's own
// backend, and owns the context and surface the terminal renders through.
//
// It is the GL path on Windows, where desktop GL drivers are unreliable, and
// an opt-in on macOS under `-tags angle`, where it renders through Metal.
// Discovery is the same everywhere: $CAUTION_ANGLE names a directory holding
// the two libraries, otherwise the newest Chrome installation's copy is used.
//
// This file is the platform-neutral bit. Each OS supplies four things:
// where the libraries live, how to open them and resolve a symbol, which
// ANGLE backend to ask for, and what EGL calls a native window.
package egl

/*
#include <stdint.h>
#include <stdlib.h>

// EGL entry points arrive as void*; each signature gets a trampoline.
static void* tGetProcAddress(void* f, const char* n) {
	return ((void* (*)(const char*))f)(n);
}
static void* tGetPlatformDisplayEXT(void* f, unsigned int p, void* d, const int32_t* a) {
	return ((void* (*)(unsigned int, void*, const int32_t*))f)(p, d, a);
}
static unsigned int tInitialize(void* f, void* d, int32_t* maj, int32_t* min) {
	return ((unsigned int (*)(void*, int32_t*, int32_t*))f)(d, maj, min);
}
static unsigned int tChooseConfig(void* f, void* d, const int32_t* a, void** cfgs, int32_t n, int32_t* got) {
	return ((unsigned int (*)(void*, const int32_t*, void**, int32_t, int32_t*))f)(d, a, cfgs, n, got);
}
static void* tCreateContext(void* f, void* d, void* c, void* share, const int32_t* a) {
	return ((void* (*)(void*, void*, void*, const int32_t*))f)(d, c, share, a);
}
static void* tCreatePbufferSurface(void* f, void* d, void* c, const int32_t* a) {
	return ((void* (*)(void*, void*, const int32_t*))f)(d, c, a);
}
static void* tCreateWindowSurface(void* f, void* d, void* c, void* win, const int32_t* a) {
	return ((void* (*)(void*, void*, void*, const int32_t*))f)(d, c, win, a);
}
static unsigned int tMakeCurrent(void* f, void* d, void* draw, void* read, void* ctx) {
	return ((unsigned int (*)(void*, void*, void*, void*))f)(d, draw, read, ctx);
}
static unsigned int tSwapBuffers(void* f, void* d, void* s) {
	return ((unsigned int (*)(void*, void*))f)(d, s);
}
static unsigned int tSwapInterval(void* f, void* d, int32_t n) {
	return ((unsigned int (*)(void*, int32_t))f)(d, n);
}
static int32_t tGetError(void* f) {
	return ((int32_t (*)(void))f)();
}
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

const (
	platformANGLE     = 0x3202 // EGL_PLATFORM_ANGLE_ANGLE
	platformANGLEType = 0x3203 // EGL_PLATFORM_ANGLE_TYPE_ANGLE
	angleTypeD3D11    = 0x3208 // EGL_PLATFORM_ANGLE_TYPE_D3D11_ANGLE
	angleTypeMetal    = 0x3489 // EGL_PLATFORM_ANGLE_TYPE_METAL_ANGLE
	eglNone           = 0x3038
	eglRedSize        = 0x3024
	eglGreenSize      = 0x3023
	eglBlueSize       = 0x3022
	eglAlphaSize      = 0x3021
	eglSurfaceType    = 0x3033
	eglWindowBit      = 0x0004
	eglPbufferBit     = 0x0001
	eglRenderableType = 0x3040
	eglES3Bit         = 0x0040
	eglClientVersion  = 0x3098
	eglWidth          = 0x3057
	eglHeight         = 0x3056
)

var (
	fGetProcAddress unsafe.Pointer
	fGetPlatformDisplayEXT,
	fInitialize,
	fChooseConfig,
	fCreateContext,
	fCreatePbufferSurface,
	fCreateWindowSurface,
	fMakeCurrent,
	fSwapBuffers,
	fSwapInterval,
	fGetError unsafe.Pointer

	display unsafe.Pointer
	config  unsafe.Pointer
	context unsafe.Pointer
	surface unsafe.Pointer
)

// libDir locates ANGLE: $CAUTION_ANGLE if set, else the platform's search.
// A trailing library filename is tolerated, since pointing at the .dylib or
// .dll rather than its directory is the obvious mistake to make.
func libDir() (string, error) {
	if d := os.Getenv("CAUTION_ANGLE"); d != "" {
		return trimLibName(d), nil
	}
	return findANGLE()
}

// Load finds and opens ANGLE, resolving every EGL entry point used here.
func Load() error {
	dir, err := libDir()
	if err != nil {
		return err
	}
	if err := openANGLE(dir); err != nil {
		return err
	}
	fGetProcAddress = symbol("eglGetProcAddress")
	if fGetProcAddress == nil {
		return fmt.Errorf("caution: ANGLE in %s has no eglGetProcAddress", dir)
	}
	for name, dst := range map[string]*unsafe.Pointer{
		"eglGetPlatformDisplayEXT": &fGetPlatformDisplayEXT,
		"eglInitialize":            &fInitialize,
		"eglChooseConfig":          &fChooseConfig,
		"eglCreateContext":         &fCreateContext,
		"eglCreatePbufferSurface":  &fCreatePbufferSurface,
		"eglCreateWindowSurface":   &fCreateWindowSurface,
		"eglMakeCurrent":           &fMakeCurrent,
		"eglSwapBuffers":           &fSwapBuffers,
		"eglSwapInterval":          &fSwapInterval,
		"eglGetError":              &fGetError,
	} {
		if *dst = ProcAddr(name); *dst == nil {
			return fmt.Errorf("caution: ANGLE is missing %s", name)
		}
	}
	return nil
}

// ProcAddr resolves a GL/EGL entry point from ANGLE (glx.Init's loader).
func ProcAddr(name string) unsafe.Pointer {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return C.tGetProcAddress(fGetProcAddress, cs)
}

func eglErr(what string) error {
	return fmt.Errorf("caution: %s failed (egl error 0x%x)", what, int32(C.tGetError(fGetError)))
}

// InitDisplay asks ANGLE for a display on the platform's backend and builds
// the ES3 context. Call once, after Load, before any surface.
func InitDisplay() error {
	attrs := []int32{platformANGLEType, angleType(), eglNone}
	display = C.tGetPlatformDisplayEXT(fGetPlatformDisplayEXT, platformANGLE, nil,
		(*C.int32_t)(unsafe.Pointer(&attrs[0])))
	if display == nil {
		return eglErr("eglGetPlatformDisplayEXT")
	}
	var maj, min int32
	if C.tInitialize(fInitialize, display, (*C.int32_t)(unsafe.Pointer(&maj)),
		(*C.int32_t)(unsafe.Pointer(&min))) == 0 {
		return eglErr("eglInitialize")
	}
	cfgAttrs := []int32{
		eglRedSize, 8, eglGreenSize, 8, eglBlueSize, 8, eglAlphaSize, 8,
		eglRenderableType, eglES3Bit,
		eglSurfaceType, eglWindowBit | eglPbufferBit,
		eglNone,
	}
	var got int32
	if C.tChooseConfig(fChooseConfig, display, (*C.int32_t)(unsafe.Pointer(&cfgAttrs[0])),
		&config, 1, (*C.int32_t)(unsafe.Pointer(&got))) == 0 || got == 0 {
		return eglErr("eglChooseConfig")
	}
	ctxAttrs := []int32{eglClientVersion, 3, eglNone}
	context = C.tCreateContext(fCreateContext, display, config, nil,
		(*C.int32_t)(unsafe.Pointer(&ctxAttrs[0])))
	if context == nil {
		return eglErr("eglCreateContext(es3)")
	}
	return nil
}

// MakeCurrentPbuffer backs the context with an offscreen pbuffer, the shot
// path, where all real rendering targets the terminal's own FBO anyway.
func MakeCurrentPbuffer(w, h int) error {
	attrs := []int32{eglWidth, int32(max(1, w)), eglHeight, int32(max(1, h)), eglNone}
	surface = C.tCreatePbufferSurface(fCreatePbufferSurface, display, config,
		(*C.int32_t)(unsafe.Pointer(&attrs[0])))
	if surface == nil {
		return eglErr("eglCreatePbufferSurface")
	}
	if C.tMakeCurrent(fMakeCurrent, display, surface, surface, context) == 0 {
		return eglErr("eglMakeCurrent(pbuffer)")
	}
	return nil
}

// MakeCurrentWindow attaches the context to the platform window handle the
// shell passes in (an NSWindow on macOS, an HWND on Windows). scale is the
// window's content scale, which matters where the native window type is a
// layer rather than the window itself.
func MakeCurrentWindow(handle unsafe.Pointer, scale float64) error {
	native, err := nativeWindow(handle, scale)
	if err != nil {
		return err
	}
	attrs := []int32{eglNone}
	surface = C.tCreateWindowSurface(fCreateWindowSurface, display, config, native,
		(*C.int32_t)(unsafe.Pointer(&attrs[0])))
	if surface == nil {
		return eglErr("eglCreateWindowSurface")
	}
	if C.tMakeCurrent(fMakeCurrent, display, surface, surface, context) == 0 {
		return eglErr("eglMakeCurrent(window)")
	}
	return nil
}

// SwapBuffers presents the window surface.
func SwapBuffers() { C.tSwapBuffers(fSwapBuffers, display, surface) }

// SwapInterval sets vsync on the current surface.
func SwapInterval(n int) { C.tSwapInterval(fSwapInterval, display, C.int32_t(n)) }
