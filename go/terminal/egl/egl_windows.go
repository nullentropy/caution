//go:build windows

package egl

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// ANGLE on Windows renders through D3D11, which falls back to WARP (the
// software rasterizer present on every Windows install) when no usable GPU
// driver is there. That fallback is the reason ANGLE is the default GL path
// here rather than an opt-in: desktop GL drivers on Windows are the least
// reliable part of the stack.
func angleType() int32 { return angleTypeD3D11 }

func trimLibName(dir string) string {
	if strings.EqualFold(filepath.Ext(dir), ".dll") {
		return filepath.Dir(dir)
	}
	return dir
}

// findANGLE falls back to the newest Chrome installation's copy. Chrome
// installs per-machine under Program Files or per-user under LocalAppData,
// with the libraries in a versioned directory beside chrome.exe.
func findANGLE() (string, error) {
	var roots []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots, filepath.Join(v, "Google", "Chrome", "Application"))
		}
	}
	var found []string
	for _, root := range roots {
		m, _ := filepath.Glob(filepath.Join(root, "*", "libEGL.dll"))
		found = append(found, m...)
	}
	if len(found) > 0 {
		sort.Strings(found)
		return filepath.Dir(found[len(found)-1]), nil
	}
	return "", fmt.Errorf("caution: ANGLE not found, set CAUTION_ANGLE to a directory " +
		"containing libEGL.dll and libGLESv2.dll (a Chrome or Edge install ships " +
		"them, or build ANGLE yourself)")
}

var eglModule syscall.Handle

// openANGLE opens GLES first so libEGL's internal lookup finds ANGLE's GL,
// never whatever else is on the search path.
func openANGLE(dir string) error {
	if _, err := syscall.LoadLibrary(filepath.Join(dir, "libGLESv2.dll")); err != nil {
		return fmt.Errorf("caution: loading %s: %w", filepath.Join(dir, "libGLESv2.dll"), err)
	}
	h, err := syscall.LoadLibrary(filepath.Join(dir, "libEGL.dll"))
	if err != nil {
		return fmt.Errorf("caution: loading %s: %w", filepath.Join(dir, "libEGL.dll"), err)
	}
	eglModule = h
	return nil
}

func symbol(name string) unsafe.Pointer {
	p, err := syscall.GetProcAddress(eglModule, name)
	if err != nil {
		return nil
	}
	return unsafe.Pointer(p)
}

// nativeWindow: EGL's native window type on Windows is the HWND itself, so
// the handle passes straight through and scale is irrelevant (Windows scales
// by resizing the window, not by a backing layer).
func nativeWindow(hwnd unsafe.Pointer, _ float64) (unsafe.Pointer, error) {
	if hwnd == nil {
		return nil, fmt.Errorf("caution: window has no HWND")
	}
	return hwnd, nil
}
