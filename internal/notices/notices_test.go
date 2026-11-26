package notices

import (
	"bytes"
	"debug/buildinfo"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

const root = "../.."

func TestCommittedNoticesAreCurrent(t *testing.T) {
	builds, err := Repo(root)
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := Write(&want, builds...); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "THIRD_PARTY_NOTICES.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatal("THIRD_PARTY_NOTICES.txt is out of date: run go run ./cmd/notices")
	}
}

func TestVendoredFilesHaveNotices(t *testing.T) {
	var covered []string
	for _, e := range extras {
		if e.module == caution {
			for _, f := range e.files {
				covered = append(covered, filepath.Dir(f))
			}
		}
	}
	isCovered := func(dir string) bool { return slices.Contains(covered, dir) }
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata":
				return filepath.SkipDir
			}
			if filepath.Base(filepath.Dir(path)) == "third_party" && !isCovered(rel) {
				t.Errorf("%s is vendored but has no entry in extras", rel)
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".ttf", ".otf", ".woff", ".woff2":
			if !isCovered(filepath.ToSlash(filepath.Dir(rel))) {
				t.Errorf("%s is bundled but has no entry in extras", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBinaryAndPackagesAgree(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "term")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/term")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	builds, err := FromBinary(root, bin)
	if err != nil {
		t.Fatal(err)
	}
	fromBin := builds[0]
	fromPkgs, err := FromPackages(root, Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Cgo: true}, "./cmd/term")
	if err != nil {
		t.Fatal(err)
	}
	paths := func(b *Build) []string {
		var p []string
		for _, m := range b.Modules {
			if m.Dir == "" {
				t.Errorf("%s has no directory", m.Path)
			}
			p = append(p, m.Path+" "+m.Version)
		}
		slices.Sort(p)
		return p
	}
	if a, b := paths(fromBin), paths(fromPkgs); !slices.Equal(a, b) {
		t.Fatalf("binary lists %v, packages list %v", a, b)
	}
	bi, err := buildinfo.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	var linked []string
	for _, d := range bi.Deps {
		if d.Path != caution {
			linked = append(linked, d.Path+" "+d.Version)
		}
	}
	slices.Sort(linked)
	if got := paths(fromBin); !slices.Equal(got, linked) {
		t.Fatalf("notices list %v, the binary links %v", got, linked)
	}
	if fromBin.Main.Dir == "" || fromBin.GOOS != runtime.GOOS || !fromBin.Cgo {
		t.Fatalf("binary build info: %+v", fromBin)
	}
}

func TestBinaryOutsideAnyModuleFails(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "notices")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/notices")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if _, err := FromBinary(t.TempDir(), bin); err == nil {
		t.Fatal("a binary read outside its module produced notices")
	}
}

func workspaceNotices(t *testing.T, imports string) string {
	t.Helper()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	var goLine string
	for _, l := range strings.Split(string(mod), "\n") {
		if strings.HasPrefix(l, "go ") {
			goLine = l
		}
	}
	ws := t.TempDir()
	app := filepath.Join(ws, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.work":     goLine + "\n\nuse (\n\t./app\n\t" + abs + "\n)\n",
		"app/go.mod":  "module example.com/app\n\n" + goLine + "\n",
		"app/main.go": "package main\n\n" + imports + "\n\nfunc main() {}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(ws, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(ws, "appbin")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = app
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	builds, err := FromBinary(app, bin)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Write(&out, builds...); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestWorkspaceAppGetsOnlyWhatItLinks(t *testing.T) {
	out := workspaceNotices(t, "import \""+caution+"/wire\"\n\nvar _ = wire.EvMenu")
	for _, name := range []string{"glslang", "SPIRV-Cross", "Noto Emoji font", "GLFW", "github.com/go-text/typesetting"} {
		if strings.Contains(out, "\n"+name+"\n") || strings.Contains(out, "\n"+name+" ") {
			t.Errorf("an app that only imports wire got the %s notice", name)
		}
	}
}

func TestEmbeddedBrowserTerminalIsCovered(t *testing.T) {
	out := workspaceNotices(t, "import _ \""+caution+"\"")
	for _, name := range []string{"\nNoto Emoji font\n", "\ngithub.com/go-text/typesetting ", "\ngolang.org/x/image "} {
		if !strings.Contains(out, name) {
			t.Errorf("an app that embeds the browser terminal is missing %q", strings.TrimSpace(name))
		}
	}
}

func TestNestedLicenseOfALinkedPackage(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"LICENSE": "root terms", "sub/LICENSE": "sub terms", "sub/pkg/x.go": "", "other/LICENSE": "other terms"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := moduleLicense(Module{Path: "example.com/m", Dir: dir}, []string{filepath.Join(dir, "sub", "pkg")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "root terms") || !strings.Contains(got, "sub/LICENSE:\n\nsub terms") || strings.Contains(got, "other terms") {
		t.Fatalf("notices for a linked sub/pkg:\n%s", got)
	}
}

func TestSameModules(t *testing.T) {
	bi := &debug.BuildInfo{Main: debug.Module{Path: "example.com/app"}, Deps: []*debug.Module{{Path: "example.com/a", Version: "v1.0.0"}}}
	listed := func(mods ...Module) *Build {
		b := &Build{all: map[string]Module{"example.com/app": {Path: "example.com/app"}}}
		for _, m := range mods {
			b.all[m.Path] = m
		}
		return b
	}
	if err := sameModules(bi, listed(Module{Path: "example.com/a", Version: "v1.0.0"})); err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string]*Build{
		"other version": listed(Module{Path: "example.com/a", Version: "v1.1.0"}),
		"missing":       listed(),
		"extra":         listed(Module{Path: "example.com/a", Version: "v1.0.0"}, Module{Path: "example.com/b", Version: "v1.0.0"}),
	} {
		if sameModules(bi, b) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestNestedLicensesMergeAcrossBuilds(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"LICENSE": "root terms", "a/LICENSE": "a terms", "b/LICENSE": "b terms"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := Module{Path: "example.com/m", Version: "v1.0.0", Dir: dir}
	build := func(sub string) *Build {
		return &Build{Modules: []Module{m}, pkgDirs: map[string][]string{m.Path: {filepath.Join(dir, sub)}}}
	}
	var out bytes.Buffer
	if err := Write(&out, build("a"), build("b")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "a terms") || !strings.Contains(out.String(), "b terms") {
		t.Fatalf("a module linked differently by two builds lost a nested license:\n%s", out.String())
	}
}

func TestVendoredApp(t *testing.T) {
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	var goLine string
	for _, l := range strings.Split(string(mod), "\n") {
		if strings.HasPrefix(l, "go ") {
			goLine = l
		}
	}
	app := t.TempDir()
	files := map[string]string{
		"go.mod":  "module example.com/vapp\n\n" + goLine + "\n\nrequire " + caution + " v0.0.0\n\nreplace " + caution + " => " + abs + "\n",
		"main.go": "package main\n\nimport (\n\t_ \"" + caution + "\"\n\t_ \"" + caution + "/terminal/text\"\n)\n\nfunc main() {}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(app, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"mod", "tidy"}, {"mod", "vendor"}, {"build", "-o", "appbin", "."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = app
		cmd.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	builds, err := FromBinary(app, filepath.Join(app, "appbin"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Write(&out, builds...); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"\ngithub.com/gorilla/websocket ", "\nNoto Emoji font\n", "\nGo fonts\n"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("a vendored app's notices are missing %q", strings.TrimSpace(name))
		}
	}
}

func TestAnyGoFontGetsTheFontNotice(t *testing.T) {
	out := workspaceNotices(t, "import _ \"golang.org/x/image/font/gofont/gomono\"")
	if !strings.Contains(out, "\nGo fonts\n") {
		t.Fatal("an app that embeds gomono is missing the Go fonts notice")
	}
}

func TestSameModuleFromTwoDirectories(t *testing.T) {
	vendored, cached := t.TempDir(), t.TempDir()
	for path, body := range map[string]string{
		filepath.Join(vendored, "LICENSE"):        "root terms",
		filepath.Join(vendored, "sub", "LICENSE"): "sub terms",
		filepath.Join(cached, "LICENSE"):          "root terms",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	build := func(dir string, pkgs ...string) *Build {
		m := Module{Path: "example.com/m", Version: "v1.0.0", Dir: dir}
		return &Build{Modules: []Module{m}, pkgDirs: map[string][]string{m.Path: pkgs}}
	}
	var out bytes.Buffer
	if err := Write(&out, build(vendored, filepath.Join(vendored, "sub")), build(cached, cached)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "sub terms") || strings.Count(out.String(), "root terms") != 1 {
		t.Fatalf("notices for one module resolved from two directories:\n%s", out.String())
	}
}
