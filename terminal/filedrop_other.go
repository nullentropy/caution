//go:build !darwin

package terminal

import "github.com/go-gl/glfw/v3.3/glfw"

// GLFW has no drag enter or leave callback
func watchFileDrag(*glfw.Window, func(over bool)) {}
