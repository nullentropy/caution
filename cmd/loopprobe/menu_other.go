//go:build !darwin

package main

import "fmt"

func runMenu() error {
	return fmt.Errorf("-menu probes AppKit menu tracking; darwin only")
}
