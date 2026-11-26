//go:build !darwin

package terminal

func onMainThread() bool { return true }
