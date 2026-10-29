//go:build darwin

package terminal

/*
#include <pthread.h>
*/
import "C"

// AppKit traps inside window creation instead of returning an error,
// so Run checks this first and says why
func onMainThread() bool { return C.pthread_main_np() != 0 }
