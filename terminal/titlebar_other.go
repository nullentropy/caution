//go:build !darwin

package terminal

import "github.com/go-gl/glfw/v3.3/glfw"

func applyCustomTitlebar(*glfw.Window, string) {}

func watchFullscreen(*glfw.Window, func(on bool)) {}
