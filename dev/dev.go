// Package dev bundles the page script fresh per request from TypeScript
// source via esbuild's Go API, around the embedded terminal wasm (rebuild
// that with cmd/bundle)
// separate package so only in-repo tools link esbuild, apps call
// caution.ServeClient and get the embedded artifacts with no bundler
package dev

import (
	"log"
	"net/http"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/nullentropy/caution"
)

// registers / and /app.js, rebuilt per request (single-digit ms here, so a
// reload always reflects source changes)
//
// entry is the path to web/terminal.ts, "" locates it. a path that doesn't
// exist from the cwd falls back to the enclosing checkout (see SourceRoot)
// so in-repo tools run from any directory. failure is reported at startup,
// not as a per-request 500 with a blank window
func ServeClient(entry string) {
	resolved, err := resolveEntry(entry)
	if err != nil {
		log.Printf("caution dev: %v", err)
		log.Printf("caution dev: /app.js will fail - run this from a caution checkout")
	} else if resolved != entry {
		log.Printf("caution dev: bundling %s", resolved)
	}
	caution.ServeHostPage()
	caution.ServeWasm() // the embedded terminal, refresh via cmd/bundle
	http.HandleFunc("/app.js", func(w http.ResponseWriter, _ *http.Request) {
		if resolved == "" {
			http.Error(w, "caution dev: no caution checkout found - cannot bundle the client from source", http.StatusInternalServerError)
			return
		}
		res := api.Build(api.BuildOptions{
			EntryPoints: []string{resolved},
			Bundle:      true,
			Format:      api.FormatESModule,
			Target:      api.ES2022,
			Sourcemap:   api.SourceMapInline,
			Write:       false,
			LogLevel:    api.LogLevelSilent,
		})
		if len(res.Errors) > 0 {
			lines := api.FormatMessages(res.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage})
			for _, l := range lines {
				log.Printf("caution: client build: %s", l)
			}
			http.Error(w, strings.Join(lines, "\n"), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(res.OutputFiles[0].Contents)
	})
}
