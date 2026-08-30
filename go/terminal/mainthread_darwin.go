//go:build darwin

package terminal

/*
#include <pthread.h>
*/
import "C"

// onMainThread reports whether the caller is running on the process's main
// thread. macOS is the strictest platform here: AppKit traps inside window
// creation rather than returning an error, so Run checks first and explains
// itself instead of dying in a system library.
func onMainThread() bool { return C.pthread_main_np() != 0 }
