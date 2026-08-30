// caution appbundle: wrap a caution binary as a macOS .app so it ships like
// a desktop app - icon, Finder name, double-click launch. Pure Go, no
// Xcode: an .app is a directory layout, Info.plist is XML, and .icns is a
// chunked container of PNGs.
//
//	go run ./cmd/appbundle -pkg ./cmd/demo -name "Caution Demo"
//	go run ./cmd/appbundle -bin ./myapp -name "My App" -icon icon.png -o dist/
//
// The binary needs no -native flag inside a bundle: caution/native.InBundle
// detects the .app layout and apps switch on it (see cmd/demo).
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"

	"github.com/nullentropy/caution/go/internal/appicon"
)

func main() {
	pkg := flag.String("pkg", "", "Go package to build (e.g. ./cmd/demo)")
	bin := flag.String("bin", "", "prebuilt binary to bundle (alternative to -pkg)")
	name := flag.String("name", "", "app name shown in Finder and the menu bar (required)")
	id := flag.String("id", "", "bundle identifier (default dev.caution.<slug>)")
	icon := flag.String("icon", "", "1024×1024-ish PNG for the icon (default: generated caution mark)")
	out := flag.String("o", "", "output directory for <Name>.app (default .)")
	flag.Parse()

	if *name == "" || (*pkg == "" && *bin == "") {
		log.Fatal("usage: appbundle -name \"My App\" (-pkg ./cmd/demo | -bin ./myapp) [-icon icon.png] [-o dir]")
	}
	slug := appicon.Slug(*name)
	if *id == "" {
		*id = "dev.caution." + slug
	}

	binPath := *bin
	if *pkg != "" {
		binPath = filepath.Join(os.TempDir(), "caution-appbundle-"+slug)
		cmd := exec.Command("go", "build", "-o", binPath, *pkg)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatal("go build: ", err)
		}
		defer os.Remove(binPath)
	}

	app := filepath.Join(*out, *name+".app")
	macos := filepath.Join(app, "Contents", "MacOS")
	res := filepath.Join(app, "Contents", "Resources")
	for _, d := range []string{macos, res} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Fatal(err)
		}
	}

	// Executable (copy preserves the Go linker's ad-hoc signature on arm64).
	data, err := os.ReadFile(binPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(macos, slug), data, 0o755); err != nil {
		log.Fatal(err)
	}

	// Info.plist.
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleName</key><string>%[1]s</string>
	<key>CFBundleDisplayName</key><string>%[1]s</string>
	<key>CFBundleExecutable</key><string>%[2]s</string>
	<key>CFBundleIdentifier</key><string>%[3]s</string>
	<key>CFBundleVersion</key><string>1.0</string>
	<key>CFBundleShortVersionString</key><string>1.0</string>
	<key>CFBundleIconFile</key><string>app</string>
	<key>NSHighResolutionCapable</key><true/>
	<key>LSMinimumSystemVersion</key><string>11.0</string>
</dict>
</plist>
`, xmlEscape(*name), slug, xmlEscape(*id))
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		log.Fatal(err)
	}

	// Icon.
	master := appicon.Default()
	if *icon != "" {
		f, err := os.Open(*icon)
		if err != nil {
			log.Fatal(err)
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			log.Fatal("icon: ", err)
		}
		master = appicon.ToRGBA(img)
	}
	icns, err := buildICNS(master)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(res, "app.icns"), icns, 0o644); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("wrote %s (%s)\n", app, *id)
	fmt.Println("open it with Finder or `open`, ship it as a zip/dmg - no flags needed:")
	fmt.Println("the binary detects the bundle and runs in native mode.")
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func buildICNS(master *image.RGBA) ([]byte, error) {
	types := []struct {
		tag  string
		size int
	}{
		{"ic11", 32}, {"ic12", 64}, {"ic07", 128}, {"ic13", 256},
		{"ic08", 256}, {"ic14", 512}, {"ic09", 512}, {"ic10", 1024},
	}
	var chunks bytes.Buffer
	for _, t := range types {
		scaled := image.NewRGBA(image.Rect(0, 0, t.size, t.size))
		xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), master, master.Bounds(), xdraw.Src, nil)
		var pngBuf bytes.Buffer
		if err := png.Encode(&pngBuf, scaled); err != nil {
			return nil, err
		}
		chunks.WriteString(t.tag)
		_ = binary.Write(&chunks, binary.BigEndian, uint32(8+pngBuf.Len()))
		chunks.Write(pngBuf.Bytes())
	}
	var out bytes.Buffer
	out.WriteString("icns")
	_ = binary.Write(&out, binary.BigEndian, uint32(8+chunks.Len()))
	out.Write(chunks.Bytes())
	return out.Bytes(), nil
}
