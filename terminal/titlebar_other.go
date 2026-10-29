//go:build !darwin

package terminal

import "github.com/go-gl/glfw/v3.3/glfw"

// macOS only for now, the option is accepted and ignored
// linux would be GLFW_DECORATED=false plus the same shell-side windowDrag
func applyCustomTitlebar(*glfw.Window, string) {}

func watchFullscreen(*glfw.Window, func(on bool)) {}
