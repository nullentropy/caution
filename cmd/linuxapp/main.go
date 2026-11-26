package main

import (
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nullentropy/caution/internal/appicon"
	"github.com/nullentropy/caution/internal/notices"
)

var iconSizes = []int{16, 24, 32, 48, 64, 128, 256, 512}

func main() {
	pkg := flag.String("pkg", "", "Go package to build (e.g. ./cmd/demo)")
	bin := flag.String("bin", "", "prebuilt binary to package, run from the module that built it (alternative to -pkg)")
	name := flag.String("name", "", "app name shown in launchers (required)")
	id := flag.String("id", "", "app id / StartupWMClass (default: slug of -name)")
	icon := flag.String("icon", "", "square PNG for the icon (default: generated caution mark)")
	comment := flag.String("comment", "", "one-line description for launchers")
	categories := flag.String("categories", "Utility;", "freedesktop Categories (semicolon-terminated)")
	out := flag.String("o", "", "staging directory to write the tree into (default .)")
	install := flag.Bool("install", false, "install into ~/.local instead of staging")
	flag.Parse()

	if *name == "" || (*pkg == "" && *bin == "") {
		log.Fatal(`usage: linuxapp -name "My App" (-pkg ./cmd/demo | -bin ./myapp) [-icon icon.png] [-o dir | -install]`)
	}
	slug := appicon.Slug(*name)
	appID := *id
	if appID == "" {
		appID = slug
	}
	if *comment == "" {
		*comment = *name + " - a caution app"
	}

	var build func(string) error
	if *pkg != "" {
		build = func(out string) error {
			cmd := exec.Command("go", "build", "-o", out, *pkg)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("go build: %w", err)
			}
			return nil
		}
	}
	data, nb, err := notices.Stage(*bin, build)
	if err != nil {
		log.Fatal(err)
	}

	root := *out
	if *install {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		root = filepath.Join(home, ".local")
	}

	binDir := filepath.Join(root, "bin")
	appsDir := filepath.Join(root, "share", "applications")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		log.Fatal(err)
	}

	binTarget := filepath.Join(binDir, slug)
	staged := binTarget + ".new"
	if err := os.WriteFile(staged, data, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.Rename(staged, binTarget); err != nil {
		os.Remove(staged)
		log.Fatal(err)
	}
	log.Printf("caution: wrote %s", binTarget)

	docDir := filepath.Join(root, "share", "doc", slug)
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		log.Fatal(err)
	}
	noticesPath := filepath.Join(docDir, "THIRD_PARTY_NOTICES.txt")
	if err := os.WriteFile(noticesPath, nb, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("caution: wrote %s", noticesPath)

	master := appicon.Default()
	if *icon != "" {
		f, err := os.Open(*icon)
		if err != nil {
			log.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			log.Fatal("icon: ", err)
		}
		master = appicon.ToRGBA(img)
	}
	for _, size := range iconSizes {
		dir := filepath.Join(root, "share", "icons", "hicolor",
			fmt.Sprintf("%dx%d", size, size), "apps")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, slug+".png"))
		if err != nil {
			log.Fatal(err)
		}
		if err := png.Encode(f, appicon.Scale(master, size)); err != nil {
			log.Fatal(err)
		}
		f.Close()
	}
	log.Printf("caution: wrote %d icons under %s", len(iconSizes),
		filepath.Join(root, "share", "icons", "hicolor"))

	// an installed entry needs an absolute Exec because launchers don't read
	// PATH
	execPath := slug
	if *install {
		execPath = binTarget
	}
	entry := filepath.Join(appsDir, slug+".desktop")
	if err := os.WriteFile(entry, []byte(desktopEntry(*name, *comment, execPath, slug, appID, *categories)), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("caution: wrote %s", entry)

	// desktops silently ignore a malformed entry
	if _, err := exec.LookPath("desktop-file-validate"); err == nil {
		outB, err := exec.Command("desktop-file-validate", entry).CombinedOutput()
		if len(outB) > 0 {
			log.Printf("desktop-file-validate:\n%s", outB)
		}
		if err != nil {
			log.Fatal("desktop-file-validate rejected the entry")
		}
		log.Printf("caution: desktop-file-validate clean")
	} else {
		log.Printf("caution: desktop-file-validate not found - entry unvalidated (apt install desktop-file-utils)")
	}

	if *install {
		if _, err := exec.LookPath("update-desktop-database"); err == nil {
			_ = exec.Command("update-desktop-database", appsDir).Run()
		}
		fmt.Printf("\ninstalled %q into ~/.local - it should appear in your launcher.\n", *name)
		fmt.Printf("PATH needs ~/.local/bin for `%s` on the command line.\n", slug)
	} else {
		fmt.Printf("\nstaged %q in %s\n", *name, root)
		fmt.Printf("install with:  cp -r %s/* ~/.local/    (or $DESTDIR/usr for a package)\n", root)
	}
}

// StartupNotify is false because GLFW never finishes the startup handshake,
// and true would leave the cursor spinning until it times out
func desktopEntry(name, comment, execPath, iconName, appID, categories string) string {
	if !strings.HasSuffix(categories, ";") {
		categories += ";"
	}
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Version=1.5",
		"Name=" + oneLine(name),
		"Comment=" + oneLine(comment),
		"Exec=" + oneLine(execPath),
		"Icon=" + iconName,
		"Terminal=false",
		"StartupNotify=false",
		"StartupWMClass=" + appID,
		"Categories=" + categories,
		"",
	}, "\n")
}

// a newline in a value silently breaks the entry
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
