//go:build !darwin

package terminal

// main thread is still required here, just no cheap way to check it
func onMainThread() bool { return true }
