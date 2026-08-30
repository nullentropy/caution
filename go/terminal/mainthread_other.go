//go:build !darwin

package terminal

// onMainThread is a darwin-only check (see mainthread_darwin.go). Elsewhere
// the main-thread requirement is real but unenforceable this cheaply.
func onMainThread() bool { return true }
