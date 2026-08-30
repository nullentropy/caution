package dev

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// This is dev-tooling only: apps built WITH caution serve the embedded
// terminal and never look for source.

// srcRelEntry is the client entrypoint, relative to the checkout root, and
// doubles as the marker that identifies the root.
const srcRelEntry = "src/terminal.ts"

// SourceRoot returns the caution checkout's root - the directory containing
// src/terminal.ts. It walks up from the working directory, then from the
// executable's directory (built binaries run from anywhere), so tools work
// regardless of where they were invoked.
func SourceRoot() (string, error) {
	var tried []string
	for _, start := range startPoints() {
		if root, ok := walkUp(start); ok {
			return root, nil
		}
		tried = append(tried, start)
	}
	return "", fmt.Errorf("no caution checkout found (looked for %s upward from %v)",
		srcRelEntry, tried)
}

// ClientEntry is the absolute path to the TypeScript entrypoint.
func ClientEntry() (string, error) {
	root, err := SourceRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, srcRelEntry), nil
}

// GoModuleDir is the checkout's Go module directory (the one holding go.mod)
// - where `go build ./cmd/...` has to run.
func GoModuleDir() (string, error) {
	root, err := SourceRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "go"), nil
}

func startPoints() []string {
	var out []string
	if wd, err := os.Getwd(); err == nil {
		out = append(out, wd)
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		out = append(out, filepath.Dir(exe))
	}
	return out
}

// walkUp climbs from dir to the filesystem root looking for the marker.
func walkUp(dir string) (string, bool) {
	for {
		if st, err := os.Stat(filepath.Join(dir, srcRelEntry)); err == nil && !st.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// resolveEntry accepts what a caller asked for and returns something that
// exists: the given path when it does, otherwise the located checkout's
// entrypoint. The fallback is logged once so the substitution is never
// silent.
func resolveEntry(entry string) (string, error) {
	if entry != "" {
		if _, err := os.Stat(entry); err == nil {
			return entry, nil
		}
	}
	resolved, err := ClientEntry()
	if err != nil {
		if entry == "" {
			return "", err
		}
		return "", errors.Join(fmt.Errorf("client entry %q not found", entry), err)
	}
	return resolved, nil
}
