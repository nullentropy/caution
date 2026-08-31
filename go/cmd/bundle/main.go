// caution bundle: write the terminal (the fixed client runtime) to static
// artifacts. Three copies of each, one build: <checkout>/dist (override with
// -o) for out-of-module consumers, go/embedded/ - the copies go:embed bakes
// into the Go SDK - and clj/resources/caution/, the copies the Clojure SDK
// serves from its classpath so a published clj artifact needs no Go
// checkout. Run this after client changes and commit the embedded copies;
// they are the SDKs' shipping artifacts. Paths resolve from the enclosing
// caution checkout, so it runs from any directory.
//
// Two artifacts per run: app.js (esbuild over src/terminal.ts) and
// shaper.wasm (cmd/shaperwasm - the native text stack compiled to wasm, so
// the browser shapes and rasterizes text with the same engine and fonts as
// the native terminal). The SDK-embedded and classpath copies of the wasm
// are gzip-compressed (~7.4MB -> ~2MB): both servers negotiate
// Content-Encoding and decompress in-process for identity clients, so the
// wire and the embedding binary both pay the small size. dist/ keeps the
// raw wasm for out-of-module consumers. The Go toolchain's wasm_exec.js is
// refreshed into src/vendor/ first, so the bundled loader always matches
// the toolchain that built the wasm.
//
//	go run ./cmd/bundle [-o path/app.js] [-pretty]
package main

import (
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/nullentropy/caution/go/dev"
)

func main() {
	entry := flag.String("entry", "", "client entrypoint (default: <checkout>/src/terminal.ts)")
	out := flag.String("o", "", "output bundle path (default: <checkout>/dist/app.js)")
	pretty := flag.Bool("pretty", false, "leave the bundle unminified, for reading it")
	flag.Parse()

	// Every path hangs off the checkout root, so bundle runs from any cwd.
	root, err := dev.SourceRoot()
	if err != nil {
		log.Fatal(err)
	}
	modDir := filepath.Join(root, "go")
	if *entry == "" {
		*entry = filepath.Join(root, "src", "terminal.ts")
	}
	if *out == "" {
		*out = filepath.Join(root, "dist", "app.js")
	}

	if err := vendorWasmExec(root); err != nil {
		log.Fatal(err)
	}
	wasm, err := buildShaperWasm(modDir)
	if err != nil {
		log.Fatal(err)
	}

	res := api.Build(api.BuildOptions{
		EntryPoints:       []string{*entry},
		Bundle:            true,
		Format:            api.FormatESModule,
		Target:            api.ES2022,
		Write:             false,
		LogLevel:          api.LogLevelWarning,
		MinifyWhitespace:  !*pretty,
		MinifyIdentifiers: !*pretty,
		MinifySyntax:      !*pretty,
	})
	if len(res.Errors) > 0 {
		for _, m := range api.FormatMessages(res.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage}) {
			log.Print(m)
		}
		os.Exit(1)
	}
	for _, path := range []string{
		*out,
		filepath.Join(modDir, "embedded", "app.js"),
		filepath.Join(root, "clj", "resources", "caution", "app.js"),
	} {
		writeArtifact(path, res.OutputFiles[0].Contents)
	}
	writeArtifact(filepath.Join(filepath.Dir(*out), "shaper.wasm"), wasm)
	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	if _, err := zw.Write(wasm); err != nil {
		log.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(modDir, "embedded", "shaper.wasm.gz"),
		filepath.Join(root, "clj", "resources", "caution", "shaper.wasm.gz"),
	} {
		writeArtifact(path, gz.Bytes())
	}
}

func writeArtifact(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("caution: wrote %s (%d KB)", path, len(data)/1024)
}

// vendorWasmExec refreshes src/vendor/wasm_exec.js from the active Go
// toolchain, so the loader bundled into app.js always matches the runtime
// ABI of the wasm it instantiates.
func vendorWasmExec(root string) error {
	gorootB, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return err
	}
	goroot := string(gorootB[:len(gorootB)-1])
	src, err := os.ReadFile(filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"))
	if err != nil {
		// pre-1.24 toolchains kept it under misc/
		src, err = os.ReadFile(filepath.Join(goroot, "misc", "wasm", "wasm_exec.js"))
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "vendor"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "src", "vendor", "wasm_exec.js"), src, 0o644)
}

// buildShaperWasm compiles cmd/shaperwasm for js/wasm and returns the
// binary, shrunk through binaryen's wasm-opt when it's installed (a soft
// dependency: the wasm is correct either way, wasm-opt just makes it
// smaller - committed artifacts should be built on a machine that has it).
func buildShaperWasm(modDir string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "caution-shaper-*.wasm")
	if err != nil {
		return nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	// -buildvcs=false keeps git metadata (revision, dirty flag, timestamp) out of
	// the binary. It changes on every commit, and this is a committed artifact, so
	// without it the file churns and every browser downloads ~100 bytes of
	// repository trivia. With it, identical source produces identical bytes.
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-trimpath", "-buildvcs=false",
		"-o", tmp.Name(), "./cmd/shaperwasm")
	cmd.Dir = modDir // the module, not our cwd
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("shaperwasm build: %v\n%s", err, out)
	}
	if _, err := exec.LookPath("wasm-opt"); err != nil {
		log.Printf("caution: wasm-opt not found - shipping unoptimized wasm (brew install binaryen)")
		return os.ReadFile(tmp.Name())
	}
	opt := tmp.Name() + ".opt"
	defer os.Remove(opt)
	// The feature flags match what the Go toolchain emits; -Oz is size-first.
	oc := exec.Command("wasm-opt", "-Oz",
		"--enable-bulk-memory", "--enable-nontrapping-float-to-int",
		"--enable-sign-ext", "--enable-mutable-globals",
		"--strip-producers", "--strip-target-features",
		"-o", opt, tmp.Name())
	if out, err := oc.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("wasm-opt: %v\n%s", err, out)
	}
	return os.ReadFile(opt)
}
