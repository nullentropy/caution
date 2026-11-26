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

	"github.com/nullentropy/caution/dev"
)

func main() {
	entry := flag.String("entry", "", "client entrypoint (default: <checkout>/web/terminal.ts)")
	out := flag.String("o", "", "output bundle path (default: <checkout>/dist/app.js)")
	pretty := flag.Bool("pretty", false, "leave the bundle unminified, for reading it")
	flag.Parse()

	root, err := dev.SourceRoot()
	if err != nil {
		log.Fatal(err)
	}
	if *entry == "" {
		*entry = filepath.Join(root, "web", "terminal.ts")
	}
	if *out == "" {
		*out = filepath.Join(root, "dist", "app.js")
	}

	if err := vendorWasmExec(root); err != nil {
		log.Fatal(err)
	}
	wasm, err := buildWasm(root)
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
	writeArtifact(*out, res.OutputFiles[0].Contents)
	writeArtifact(filepath.Join(root, "embedded", "app.js"), res.OutputFiles[0].Contents)
	writeArtifact(filepath.Join(filepath.Dir(*out), "term.wasm"), wasm)
	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	if _, err := zw.Write(wasm); err != nil {
		log.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	writeArtifact(filepath.Join(root, "embedded", "term.wasm.gz"), gz.Bytes())
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

// the loader in app.js has to match the ABI of the toolchain that built the wasm
func vendorWasmExec(root string) error {
	gorootB, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return err
	}
	goroot := string(gorootB[:len(gorootB)-1])
	src, err := os.ReadFile(filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"))
	if err != nil {
		// toolchains before Go 1.24 keep it under misc/
		src, err = os.ReadFile(filepath.Join(goroot, "misc", "wasm", "wasm_exec.js"))
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "web", "vendor"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "web", "vendor", "wasm_exec.js"), src, 0o644)
}

func buildWasm(modDir string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "caution-shaper-*.wasm")
	if err != nil {
		return nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	// -buildvcs=false makes identical source build identical bytes, so the
	// committed wasm doesn't change on every commit
	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-trimpath", "-buildvcs=false",
		"-o", tmp.Name(), "./cmd/termwasm")
	cmd.Dir = modDir
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("termwasm build: %v\n%s", err, out)
	}
	if _, err := exec.LookPath("wasm-opt"); err != nil {
		log.Printf("caution: wasm-opt not found - shipping unoptimized wasm (brew install binaryen)")
		return os.ReadFile(tmp.Name())
	}
	opt := tmp.Name() + ".opt"
	defer os.Remove(opt)
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
