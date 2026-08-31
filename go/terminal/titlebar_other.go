//go:build !darwin

package terminal

import "github.com/go-gl/glfw/v3.3/glfw"

// applyCustomTitlebar is macOS-only for now. Elsewhere the option is accepted
// and ignored. The Linux analog would be GLFW_DECORATED=false plus the same
// shell-side windowDrag machinery.
func applyCustomTitlebar(*glfw.Window, string) {}

func watchFullscreen(*glfw.Window, func(on bool)) {}
