// caution linuxapp: ship a caution binary as a Linux desktop app, the
// counterpart to cmd/appbundle. There is no bundle format here. A desktop
// app on Linux is a binary plus a freedesktop .desktop entry plus icons in
// the hicolor theme, laid out the way XDG expects:
//
//	bin/<slug>
//	share/applications/<slug>.desktop
//	share/icons/hicolor/<size>x<size>/apps/<slug>.png
//
// That tree is the deliverable: copy it into ~/.local (what -install does),
// or into $DESTDIR/usr for a distro package, or tar it.
//
//	go run ./cmd/linuxapp -pkg ./cmd/demo -name "Caution Demo"
//	go run ./cmd/linuxapp -bin ./myapp -name "My App" -icon icon.png -o dist/
//	go run ./cmd/linuxapp -pkg ./cmd/demo -name "Caution Demo" -install
//
// The entry's StartupWMClass is the app id the terminal advertises as
// WM_CLASS (terminal.Options.AppID), which is how a desktop ties the running
// window to this entry for its icon and grouping. Under native Wayland that
// link is currently broken upstream (GLFW 3.3 never sets an app id), so the
// entry is correct and the compositor still cannot match it. See
// linux/README.md.
//
// Pure Go, and it runs on any OS: writing files and PNGs needs no Linux. Set
// GOOS=linux when building the binary for another machine.
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

	"github.com/nullentropy/caution/go/internal/appicon"
)

// The hicolor sizes a desktop actually looks for: 16-48 for lists and
// panels, 64-256 for launchers and switchers, 512 for scaled displays.
var iconSizes = []int{16, 24, 32, 48, 64, 128, 256, 512}

func main() {
	pkg := flag.String("pkg", "", "Go package to build (e.g. ./cmd/demo)")
	bin := flag.String("bin", "", "prebuilt binary to package (alternative to -pkg)")
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

	// The binary. GOOS/GOARCH pass through the environment, so packaging
	// for another machine is `GOOS=linux GOARCH=amd64 go run ./cmd/linuxapp`.
	binTarget := filepath.Join(binDir, slug)
	if *pkg != "" {
		cmd := exec.Command("go", "build", "-o", binTarget, *pkg)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatal("go build: ", err)
		}
	} else {
		data, err := os.ReadFile(*bin)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(binTarget, data, 0o755); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("caution: wrote %s", binTarget)

	// Icons, downscaled from one master into the hicolor theme.
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

	// The entry. Exec is absolute when installed (a launcher does not read
	// the user's PATH), while staged trees get the bare slug, since the final
	// location is whoever installs it to decide.
	execPath := slug
	if *install {
		execPath = binTarget
	}
	entry := filepath.Join(appsDir, slug+".desktop")
	if err := os.WriteFile(entry, []byte(desktopEntry(*name, *comment, execPath, slug, appID, *categories)), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("caution: wrote %s", entry)

	// Validate when the tool is around. A malformed entry is silently ignored by
	// desktops, with nothing to show for it.
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
		// Desktops cache the applications directory, and the update tools are
		// advisory, and a missing one is not an error.
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

// desktopEntry renders the freedesktop entry. Each line is a decision:
//
//   - Terminal=false: this is a GUI app, not a console program.
//   - StartupNotify=false: GLFW never completes the startup-notification
//     handshake, so true leaves the cursor spinning until it times out.
//   - StartupWMClass: the app id the window advertises (WM_CLASS), which is
//     how the desktop matches window to entry.
//   - No MimeType and no %U/%F in Exec: caution apps take no file arguments,
//     and claiming otherwise makes a desktop offer them for "open with".
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

// oneLine keeps a value on its single line: the format is line-oriented, and
// an embedded newline would silently produce a broken key.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
