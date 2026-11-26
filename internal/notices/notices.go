package notices

import (
	"bytes"
	"debug/buildinfo"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
)

const (
	caution = "github.com/nullentropy/caution"
	glfw    = "github.com/go-gl/glfw/v3.3/glfw"
)

//go:embed texts
var texts embed.FS

type Module struct {
	Path, Version, Dir string
}

type Target struct {
	GOOS, GOARCH string
	Cgo          bool
	Tags         []string
}

type Build struct {
	Target
	Main     Module
	Modules  []Module
	Packages map[string]bool
	all      map[string]Module
	pkgDirs  map[string][]string
	dir      string
}

type extra struct {
	name    string
	module  string
	pkg     string
	goos    string
	tag     string
	cgo     bool
	files   []string
	headers []string
	text    string
}

var extras = []extra{
	{name: "glslang", module: caution, pkg: caution + "/terminal/shaderx",
		files: []string{"terminal/shaderx/third_party/glslang/LICENSE.txt", "terminal/shaderx/third_party/glslang/LICENSES/AML-glslang.txt"}},
	{name: "SPIRV-Cross", module: caution, pkg: caution + "/terminal/shaderx",
		files: []string{"terminal/shaderx/third_party/spirv-cross/LICENSE"}},
	{name: "Noto Emoji font", module: caution, pkg: caution + "/terminal/text",
		files: []string{"terminal/text/fonts/OFL.txt"}},
	{name: "Go fonts", module: "golang.org/x/image", pkg: "golang.org/x/image/font/gofont/",
		files: []string{"font/gofont/ttfs/README"}},
	{name: "GLFW", module: glfw, pkg: glfw,
		files: []string{"glfw/LICENSE.md"}},
	{name: "Wayland protocols", module: glfw, pkg: glfw, goos: "linux", tag: "wayland",
		headers: []string{
			"glfw/src/wayland-xdg-shell-client-protocol.c",
			"glfw/src/wayland-xdg-decoration-unstable-v1-client-protocol.c",
			"glfw/src/wayland-viewporter-client-protocol.c",
			"glfw/src/wayland-relative-pointer-unstable-v1-client-protocol.c",
			"glfw/src/wayland-pointer-constraints-unstable-v1-client-protocol.c",
			"glfw/src/wayland-idle-inhibit-unstable-v1-client-protocol.c",
		}},
	{name: "Wine headers", module: glfw, pkg: glfw, goos: "windows",
		headers: []string{"glfw/deps/mingw/dinput.h", "glfw/deps/mingw/xinput.h"}},
	{name: "mingw-w64 runtime", goos: "windows", cgo: true, text: "texts/mingw-w64.txt"},
	{name: "mingw-w64 winpthreads", module: caution, pkg: caution + "/terminal/shaderx", goos: "windows", text: "texts/winpthreads.txt"},
}

type listedModule struct {
	Path, Version, Dir string
	Replace            *listedModule
}

func FromPackages(dir string, t Target, pkgs ...string) (*Build, error) {
	args := []string{"list", "-deps", "-json=ImportPath,Dir,Standard,DepOnly,Module"}
	if len(t.Tags) > 0 {
		args = append(args, "-tags", strings.Join(t.Tags, ","))
	}
	cmd := exec.Command("go", append(args, pkgs...)...)
	cmd.Dir = dir
	cgo := "0"
	if t.Cgo {
		cgo = "1"
	}
	cmd.Env = append(os.Environ(), "GOOS="+t.GOOS, "GOARCH="+t.GOARCH, "CGO_ENABLED="+cgo)
	out, err := run(cmd)
	if err != nil {
		return nil, err
	}
	b := &Build{Target: t, Packages: map[string]bool{}, all: map[string]Module{}, pkgDirs: map[string][]string{}, dir: dir}
	var requested []string
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p struct {
			ImportPath string
			Dir        string
			Standard   bool
			DepOnly    bool
			Module     *listedModule
		}
		if err := dec.Decode(&p); err != nil {
			return nil, err
		}
		b.Packages[p.ImportPath] = true
		if p.Standard || p.Module == nil {
			continue
		}
		version := p.Module.Version
		if p.Module.Replace != nil && p.Module.Replace.Version != "" {
			version = p.Module.Replace.Version
		}
		modDir := p.Module.Dir
		if modDir == "" && p.Dir != "" {
			modDir = strings.TrimSuffix(p.Dir, filepath.FromSlash(strings.TrimPrefix(p.ImportPath, p.Module.Path)))
		}
		b.all[p.Module.Path] = Module{p.Module.Path, version, modDir}
		b.pkgDirs[p.Module.Path] = append(b.pkgDirs[p.Module.Path], p.Dir)
		if !p.DepOnly {
			requested = append(requested, p.Module.Path)
		}
	}
	if len(requested) > 0 {
		b.Main = b.all[requested[0]]
	}
	for path, m := range b.all {
		if path != b.Main.Path && path != caution {
			b.Modules = append(b.Modules, m)
		}
	}
	sort.Slice(b.Modules, func(i, j int) bool { return b.Modules[i].Path < b.Modules[j].Path })
	return b, nil
}

func FromBinary(dir, path string) ([]*Build, error) {
	bi, err := buildinfo.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Target
	for _, s := range bi.Settings {
		switch s.Key {
		case "GOOS":
			t.GOOS = s.Value
		case "GOARCH":
			t.GOARCH = s.Value
		case "CGO_ENABLED":
			t.Cgo = s.Value == "1"
		case "-tags":
			t.Tags = strings.Split(s.Value, ",")
		}
	}
	b, err := FromPackages(dir, t, bi.Path)
	if err != nil {
		return nil, fmt.Errorf("notices: run the packager from the module that builds %s: %w", bi.Path, err)
	}
	if err := sameModules(bi, b); err != nil {
		return nil, fmt.Errorf("notices: %s: %w: run the packager from the module that built it", path, err)
	}
	builds := []*Build{b}
	if b.Packages[caution] {
		cdir, err := sourceModule(dir, caution, b.moduleDir(caution))
		if err != nil {
			return nil, err
		}
		wasm, err := FromPackages(cdir, Target{GOOS: "js", GOARCH: "wasm"}, "./cmd/termwasm")
		if err != nil {
			return nil, err
		}
		builds = append(builds, wasm)
	}
	return builds, nil
}

func sourceModule(dir, path, moduleDir string) (string, error) {
	if _, err := os.Stat(filepath.Join(moduleDir, "go.mod")); err == nil {
		return moduleDir, nil
	}
	cmd := exec.Command("go", "list", "-mod=mod", "-m", "-json", path)
	cmd.Dir = dir
	out, err := run(cmd)
	if err != nil {
		return "", err
	}
	var m Module
	if err := json.Unmarshal(out, &m); err != nil {
		return "", err
	}
	if m.Dir == "" {
		return "", fmt.Errorf("notices: %s is not in the module cache (run go mod download %s)", path, path)
	}
	return m.Dir, nil
}

func sameModules(bi *debug.BuildInfo, b *Build) error {
	linked := map[string]bool{bi.Main.Path: true}
	for _, d := range bi.Deps {
		linked[d.Path] = true
		want := d.Version
		if d.Replace != nil {
			want = d.Replace.Version
		}
		got, ok := b.all[d.Path]
		if !ok {
			return fmt.Errorf("it links %s %s, which go list does not resolve", d.Path, want)
		}
		if want != "" && want != "(devel)" && got.Version != want {
			return fmt.Errorf("it links %s %s, but go list resolves %s", d.Path, want, got.Version)
		}
	}
	for path := range b.all {
		if !linked[path] {
			return fmt.Errorf("go list resolves %s, which it does not link", path)
		}
	}
	return nil
}

func run(cmd *exec.Cmd) ([]byte, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n%s", strings.Join(cmd.Args, " "), err, stderr.String())
	}
	return out, nil
}

func Write(w io.Writer, builds ...*Build) error {
	golicense, err := texts.ReadFile("texts/go.txt")
	if err != nil {
		return err
	}
	sections := map[string]string{"Go standard library and runtime": string(golicense)}
	resolved := map[string]map[Module][]string{}
	for _, b := range builds {
		for _, m := range b.Modules {
			key := m.Path + " " + m.Version
			if resolved[key] == nil {
				resolved[key] = map[Module][]string{}
			}
			resolved[key][m] = append(resolved[key][m], b.pkgDirs[m.Path]...)
		}
	}
	for key, byDir := range resolved {
		mods := make([]Module, 0, len(byDir))
		for m := range byDir {
			mods = append(mods, m)
		}
		sort.Slice(mods, func(i, j int) bool { return mods[i].Dir < mods[j].Dir })
		var parts []string
		for _, m := range mods {
			p, err := licenseParts(m, byDir[m])
			if err != nil {
				return err
			}
			for _, part := range p {
				if !slices.Contains(parts, part) {
					parts = append(parts, part)
				}
			}
		}
		sections[key] = strings.Join(parts, "\n\n")
	}
	for _, b := range builds {
		for _, e := range extras {
			if !e.applies(b) {
				continue
			}
			body, err := e.body(b)
			if err != nil {
				return err
			}
			sections[e.name] = body
		}
	}
	titles := make([]string, 0, len(sections))
	for t := range sections {
		titles = append(titles, t)
	}
	sort.Strings(titles)
	fmt.Fprintln(w, "Third-party software notices")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "This software includes the following third-party software, each under the license reproduced below.")
	for _, t := range titles {
		fmt.Fprintf(w, "\n%s\n%s\n%s\n\n%s\n", strings.Repeat("=", 72), t, strings.Repeat("=", 72), strings.TrimRight(sections[t], "\n"))
	}
	return nil
}

var licenseNames = []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE", "COPYING", "NOTICE", "NOTICE.txt", "NOTICE.md"}

func moduleLicense(m Module, pkgDirs []string) (string, error) {
	parts, err := licenseParts(m, pkgDirs)
	return strings.Join(parts, "\n\n"), err
}

func licenseParts(m Module, pkgDirs []string) ([]string, error) {
	if m.Dir == "" {
		return nil, fmt.Errorf("notices: %s %s is not in the module cache (run go mod download)", m.Path, m.Version)
	}
	dirs := map[string]bool{m.Dir: true}
	for _, d := range pkgDirs {
		for ; strings.HasPrefix(d, m.Dir+string(filepath.Separator)); d = filepath.Dir(d) {
			dirs[d] = true
		}
	}
	sorted := make([]string, 0, len(dirs))
	for d := range dirs {
		sorted = append(sorted, d)
	}
	sort.Strings(sorted)
	var parts []string
	for _, d := range sorted {
		for _, n := range licenseNames {
			data, err := os.ReadFile(filepath.Join(d, n))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if d == m.Dir {
				parts = append(parts, strings.TrimRight(string(data), "\n"))
			} else {
				rel, _ := filepath.Rel(m.Dir, filepath.Join(d, n))
				parts = append(parts, filepath.ToSlash(rel)+":\n\n"+strings.TrimRight(string(data), "\n"))
			}
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("notices: no license file in %s (%s)", m.Path, m.Dir)
	}
	return parts, nil
}

func (e extra) applies(b *Build) bool {
	if e.goos != "" && e.goos != b.GOOS {
		return false
	}
	if e.tag != "" && !slices.Contains(b.Tags, e.tag) {
		return false
	}
	if e.cgo && !b.Cgo {
		return false
	}
	if e.module == "" {
		return true
	}
	if b.moduleDir(e.module) == "" {
		return false
	}
	if strings.HasSuffix(e.pkg, "/") {
		for p := range b.Packages {
			if strings.HasPrefix(p, e.pkg) {
				return true
			}
		}
		return false
	}
	return b.Packages[e.pkg]
}

func (b *Build) moduleDir(path string) string { return b.all[path].Dir }

func (e extra) body(b *Build) (string, error) {
	if e.text != "" {
		data, err := texts.ReadFile(e.text)
		return string(data), err
	}
	dir, err := sourceModule(b.dir, e.module, b.moduleDir(e.module))
	if err != nil {
		return "", err
	}
	var parts []string
	for _, f := range e.files {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return "", fmt.Errorf("notices: %s: %w", e.name, err)
		}
		parts = append(parts, strings.TrimRight(string(data), "\n"))
	}
	seen := map[string]bool{}
	for _, f := range e.headers {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return "", fmt.Errorf("notices: %s: %w", e.name, err)
		}
		h, err := copyrightComment(string(data))
		if err != nil {
			return "", fmt.Errorf("notices: %s: %s: %w", e.name, f, err)
		}
		if !seen[h] {
			seen[h] = true
			parts = append(parts, h)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func copyrightComment(src string) (string, error) {
	for {
		start := strings.Index(src, "/*")
		if start < 0 {
			return "", errors.New("no copyright comment")
		}
		end := strings.Index(src[start:], "*/")
		if end < 0 {
			return "", errors.New("unterminated comment")
		}
		block := src[start+2 : start+end]
		src = src[start+end+2:]
		if !strings.Contains(block, "Copyright") {
			continue
		}
		var lines []string
		for _, l := range strings.Split(block, "\n") {
			l = strings.TrimPrefix(strings.TrimSpace(l), "*")
			lines = append(lines, strings.TrimPrefix(l, " "))
		}
		return strings.Trim(strings.Join(lines, "\n"), "\n"), nil
	}
}

func WriteFile(path string, builds ...*Build) error {
	var buf bytes.Buffer
	if err := Write(&buf, builds...); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func Repo(dir string) ([]*Build, error) {
	var builds []*Build
	for _, t := range []Target{
		{GOOS: "darwin", GOARCH: "arm64", Cgo: true},
		{GOOS: "linux", GOARCH: "amd64", Cgo: true},
		{GOOS: "linux", GOARCH: "amd64", Cgo: true, Tags: []string{"wayland"}},
		{GOOS: "windows", GOARCH: "amd64", Cgo: true},
	} {
		b, err := FromPackages(dir, t, ".", "./cmd/term")
		if err != nil {
			return nil, err
		}
		builds = append(builds, b)
	}
	b, err := FromPackages(dir, Target{GOOS: "js", GOARCH: "wasm"}, "./cmd/termwasm")
	if err != nil {
		return nil, err
	}
	return append(builds, b), nil
}

func Stage(bin string, build func(out string) error) ([]byte, []byte, error) {
	if build != nil {
		dir, err := os.MkdirTemp("", "caution-package-")
		if err != nil {
			return nil, nil, err
		}
		defer os.RemoveAll(dir)
		bin = filepath.Join(dir, "app")
		if err := build(bin); err != nil {
			return nil, nil, err
		}
	}
	builds, err := FromBinary(".", bin)
	if err != nil {
		return nil, nil, err
	}
	var text bytes.Buffer
	if err := Write(&text, builds...); err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(bin)
	return data, text.Bytes(), err
}
