//go:build linux

package terminal

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// no desktop-neutral setting everywhere yet, so GNOME's then KDE's. either
// missing (another desktop, a bare window manager) reads as full motion
func systemReducedMotion() bool {
	if out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "enable-animations").Output(); err == nil {
		if strings.TrimSpace(string(out)) == "false" {
			return true
		}
	}
	return kdeAnimationsOff()
}

// [KDE] AnimationDurationFactor=0 in kdeglobals is Plasma's animations-off
func kdeAnimationsOff() bool {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		dir = filepath.Join(home, ".config")
	}
	f, err := os.Open(filepath.Join(dir, "kdeglobals"))
	if err != nil {
		return false
	}
	defer f.Close()
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && section == "[KDE]" && strings.TrimSpace(k) == "AnimationDurationFactor" {
			return strings.TrimSpace(v) == "0"
		}
	}
	return false
}
