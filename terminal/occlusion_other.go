//go:build !darwin

package terminal

import "github.com/go-gl/glfw/v3.3/glfw"

func watchOcclusion(*glfw.Window, func(visible bool)) bool { return true }
