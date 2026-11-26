package dev

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const srcRelEntry = "web/terminal.ts"

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

func ClientEntry() (string, error) {
	root, err := SourceRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, srcRelEntry), nil
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
