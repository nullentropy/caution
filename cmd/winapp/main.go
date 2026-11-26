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

	"github.com/nullentropy/caution/internal/appicon"
	"github.com/nullentropy/caution/internal/notices"
)

func main() {
	pkg := flag.String("pkg", "", "Go package to build (e.g. ./cmd/demo)")
	name := flag.String("name", "", "app name, used for the .exe and the manifest identity (required)")
	id := flag.String("id", "", "manifest identity (default dev.caution.<slug>)")
	icon := flag.String("icon", "", "PNG for the icon, 256px or larger (default: generated caution mark)")
	out := flag.String("o", "", "output directory (default .)")
	arch := flag.String("arch", "amd64", "target architecture: amd64 or arm64")
	cc := flag.String("cc", "", "C compiler for the cross build (default x86_64-w64-mingw32-gcc for amd64)")
	cxx := flag.String("cxx", "", "C++ compiler for the cross build (default: -cc with gcc swapped for g++)")
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

	syso := buildSyso(res, *arch)

	// the linker only picks up a .syso in the package dir being built
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
	data, nb, err := notices.Stage("", func(out string) error {
		err := build(*pkg, out, *arch, *cc, *cxx)
		os.Remove(sysoPath)
		if err != nil {
			return err
		}
		return verify(out)
	})
	os.Remove(sysoPath)
	if err != nil {
		log.Fatal("winapp: ", err)
	}
	if *out != "" {
		if err := os.MkdirAll(*out, 0o755); err != nil {
			log.Fatal("winapp: ", err)
		}
	}
	if err := os.WriteFile(exePath, data, 0o755); err != nil {
		log.Fatal("winapp: ", err)
	}
	fmt.Println("wrote", exePath)
	noticesPath := filepath.Join(*out, *name+"-THIRD_PARTY_NOTICES.txt")
	if err := os.WriteFile(noticesPath, nb, 0o644); err != nil {
		log.Fatal("winapp: ", err)
	}
	fmt.Println("wrote", noticesPath)
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

func packageDir(pkg string) (string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", pkg)
	cmd.Stderr = os.Stderr
	dir, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", pkg, err)
	}
	return strings.TrimSpace(string(dir)), nil
}

func build(pkg, exePath, arch, cc, cxx string) error {
	if cc == "" && arch == "amd64" {
		cc = "x86_64-w64-mingw32-gcc"
	}
	if cxx == "" && strings.HasSuffix(cc, "gcc") {
		cxx = strings.TrimSuffix(cc, "gcc") + "g++"
	}
	// -H windowsgui keeps a console window from opening behind the app
	cmd := exec.Command("go", "build", "-ldflags", "-H windowsgui", "-o", exePath, pkg)
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH="+arch, "CGO_ENABLED=1")
	if cc != "" {
		cmd.Env = append(cmd.Env, "CC="+cc)
	}
	if cxx != "" {
		cmd.Env = append(cmd.Env, "CXX="+cxx)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building %s: %w (a mingw toolchain is needed to cross-compile: brew install mingw-w64)", pkg, err)
	}
	return nil
}

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
		if name&0x80000000 == 0 {
			out[name] = true
		}
	}
	return out
}
