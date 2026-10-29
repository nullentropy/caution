//go:build !darwin

// Package pacer is a no-op off macOS
package pacer

func Install(func()) {}
func Arm(float64)    {}
func Idle()          {}
