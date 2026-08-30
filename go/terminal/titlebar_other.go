//go:build !darwin

package terminal

import "github.com/go-gl/glfw/v3.3/glfw"

// applyCustomTitlebar is macOS-only for now: on other platforms the option
// is accepted and ignored (the Linux analog - GLFW_DECORATED=false plus the
// same shell-side windowDrag machinery - is ledgered in PLAN).
func applyCustomTitlebar(*glfw.Window, string) {}
