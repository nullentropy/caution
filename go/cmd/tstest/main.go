// Command tstest runs the TS client's test suite: it bundles src/test/main.ts
// with esbuild (the same toolchain that builds the terminal) and executes the
// result with node. Dev-only, requiring node on PATH.
//
// Run from the repo root: go run -C go ./cmd/tstest
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/evanw/esbuild/pkg/api"
)

func main() {
	res := api.Build(api.BuildOptions{
		EntryPoints: []string{"../src/test/main.ts"},
		Bundle:      true,
		Format:      api.FormatESModule,
		Target:      api.ES2022,
		Write:       false,
		LogLevel:    api.LogLevelSilent,
	})
	if len(res.Errors) > 0 {
		for _, l := range api.FormatMessages(res.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage}) {
			fmt.Fprint(os.Stderr, l)
		}
		os.Exit(1)
	}

	out := filepath.Join(os.TempDir(), "caution-ts-tests.mjs")
	if err := os.WriteFile(out, res.OutputFiles[0].Contents, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.Remove(out)

	node, err := exec.LookPath("node")
	if err != nil {
		fmt.Fprintln(os.Stderr, "caution: tstest needs node on PATH (dev-only dependency)")
		os.Exit(1)
	}
	cmd := exec.Command(node, out)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
