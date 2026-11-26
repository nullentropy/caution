package dev

import (
	"log"
	"net/http"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/nullentropy/caution"
)

func ServeClient(entry string) {
	resolved, err := resolveEntry(entry)
	if err != nil {
		log.Printf("caution dev: %v", err)
		log.Printf("caution dev: /app.js will fail - run this from a caution checkout")
	} else if resolved != entry {
		log.Printf("caution dev: bundling %s", resolved)
	}
	caution.ServeHostPage()
	caution.ServeWasm()
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
