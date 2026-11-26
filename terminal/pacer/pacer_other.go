//go:build !darwin

package pacer

func Install(func()) {}
func Arm(float64)    {}
func Idle()          {}
