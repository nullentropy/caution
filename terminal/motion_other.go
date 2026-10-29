//go:build !darwin && !windows && !linux

package terminal

func systemReducedMotion() bool { return false }
