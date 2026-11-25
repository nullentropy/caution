//go:build !darwin

package terminal

func clipboardImage() []byte { return nil }
