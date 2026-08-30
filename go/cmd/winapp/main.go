// caution winapp: ship a caution binary as a Windows desktop app, the
// counterpart to cmd/appbundle and cmd/linuxapp. Windows has no bundle
// directory: an app is a single .exe carrying its icon and manifest as PE
// resources, so this builds the resource object, links it in, and asks for
// the GUI subsystem so no console window opens alongside the app.
//
//	go run ./cmd/winapp -pkg ./cmd/demo -name "Caution Demo"
//
// Cross-compiles from mac w/ mingw toolchain installed
// (brew install mingw-w64)
//
// TODO:
// - a version resource (the right-click Properties tab)
// - authenticode signing
package main

import (
	"debug/pe"
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

	"github.com/nullentropy/caution/go/internal/appicon"
)

func main() {
	pkg := flag.String("pkg", "", "Go package to build (e.g. ./cmd/demo)")
	name := flag.String("name", "", "app name, used for the .exe and the manifest identity (required)")
	id := flag.String("id", "", "manifest identity (default dev.caution.<slug>)")
	icon := flag.String("icon", "", "PNG for the icon, 256px or larger (default: generated caution mark)")
	out := flag.String("o", "", "output directory (default .)")
	arch := flag.String("arch", "amd64", "target architecture: amd64 or arm64")
	cc := flag.String("cc", "", "C compiler for the cross build (default x86_64-w64-mingw32-gcc for amd64)")
	flag.Parse()

	if *pkg == "" || *name == "" {
		log.Fatal("winapp: -pkg and -name are required")
	}
	if *arch != "amd64" && *arch != "arm64" {
		log.Fatalf("winapp: unsupported -arch %q (amd64 or arm64)", *arch)
	}
	appID := *id
	if appID == "" {
		appID = "dev.caution." + appicon.Slug(*name)
	}
	exePath := filepath.Join(*out, *name+".exe")

	master, err := loadIcon(*icon)
	if err != nil {
		log.Fatal("winapp: ", err)
	}
	res, err := iconResources(master)
	if err != nil {
		log.Fatal("winapp: ", err)
	}
	res = append(res, resource{typ: rtManifest, id: 1, data: manifestXML(appID, "1.0.0.0")})

	syso, err := buildSyso(res, *arch)
	if err != nil {
		log.Fatal("winapp: ", err)
	}

	// The linker only sees resources through a .syso sitting in the package
	// being built, so the file is written there and removed afterwards.
	pkgDir, err := packageDir(*pkg)
	if err != nil {
		log.Fatal("winapp: ", err)
	}
	sysoPath := filepath.Join(pkgDir, "caution_winapp_"+*arch+".syso")
	if _, err := os.Stat(sysoPath); err == nil {
		log.Fatalf("winapp: %s already exists, refusing to overwrite it", sysoPath)
	}
	if err := os.WriteFile(sysoPath, syso, 0o644); err != nil {
		log.Fatal("winapp: ", err)
	}
	defer os.Remove(sysoPath)

	if err := build(*pkg, exePath, *arch, *cc); err != nil {
		log.Fatal("winapp: ", err)
	}
	if err := verify(exePath); err != nil {
		log.Fatal("winapp: ", err)
	}
	fmt.Println("wrote", exePath)
}

func loadIcon(path string) (*image.RGBA, error) {
	if path == "" {
		return appicon.Default(), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	return appicon.ToRGBA(img), nil
}

// packageDir resolves a package pattern to the directory holding its source,
// which is where the linker looks for .syso files.
func packageDir(pkg string) (string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", pkg)
	cmd.Stderr = os.Stderr
	dir, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", pkg, err)
	}
	return strings.TrimSpace(string(dir)), nil
}

func build(pkg, exePath, arch, cc string) error {
	if cc == "" && arch == "amd64" {
		cc = "x86_64-w64-mingw32-gcc"
	}
	// -H windowsgui keeps a console window from opening behind the app.
	cmd := exec.Command("go", "build", "-ldflags", "-H windowsgui", "-o", exePath, pkg)
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH="+arch, "CGO_ENABLED=1")
	if cc != "" {
		cmd.Env = append(cmd.Env, "CC="+cc)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building %s: %w (a mingw toolchain is needed to cross-compile: brew install mingw-w64)", pkg, err)
	}
	return nil
}

// verify reads the linked .exe back and checks the two things that are easy
// to get silently wrong: the subsystem (a console app flashes a terminal
// window) and the resources (an icon that never made it in).
func verify(path string) error {
	f, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	defer f.Close()

	const subsystemWindowsGUI = 2
	var subsystem uint16
	switch oh := f.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		subsystem = oh.Subsystem
	case *pe.OptionalHeader32:
		subsystem = oh.Subsystem
	default:
		return fmt.Errorf("%s has no optional header", path)
	}
	if subsystem != subsystemWindowsGUI {
		return fmt.Errorf("%s is subsystem %d, wanted %d (windowsgui)", path, subsystem, subsystemWindowsGUI)
	}

	sec := f.Section(".rsrc")
	if sec == nil {
		return fmt.Errorf("%s has no .rsrc section, so the icon and manifest did not land", path)
	}
	data, err := sec.Data()
	if err != nil {
		return fmt.Errorf("reading .rsrc: %w", err)
	}
	have := resourceTypes(data)
	for _, want := range []struct {
		typ  uint32
		what string
	}{{rtIcon, "icon images"}, {rtGroupIcon, "icon group"}, {rtManifest, "manifest"}} {
		if !have[want.typ] {
			return fmt.Errorf("%s is missing its %s (resource type %d)", path, want.what, want.typ)
		}
	}
	return nil
}

// resourceTypes reads the top level of the resource tree, which is all that
// is needed to answer "did the icon and manifest make it in".
func resourceTypes(rsrc []byte) map[uint32]bool {
	out := map[uint32]bool{}
	if len(rsrc) < 16 {
		return out
	}
	named := binary.LittleEndian.Uint16(rsrc[12:])
	ids := binary.LittleEndian.Uint16(rsrc[14:])
	for i := 0; i < int(named)+int(ids); i++ {
		at := 16 + i*8
		if at+8 > len(rsrc) {
			break
		}
		name := binary.LittleEndian.Uint32(rsrc[at:])
		if name&0x80000000 == 0 { // an id, not a string
			out[name] = true
		}
	}
	return out
}
