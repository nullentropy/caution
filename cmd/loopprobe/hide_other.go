//go:build !darwin

package main

const hideSupported = false

func setAppHidden(bool) {}
