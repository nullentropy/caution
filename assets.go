package caution

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"net/http"
	"strings"
	"sync"
)

//go:embed embedded/app.js
var embeddedClient []byte

//go:embed embedded/term.wasm.gz
var embeddedWasmGz []byte

var wasmRaw struct {
	once  sync.Once
	bytes []byte
}

const hostPage = `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>caution</title>
    <style>
      html, body { margin: 0; height: 100%; overflow: hidden; background: #16181d; }
      canvas { display: block; width: 100vw; height: 100vh; }
    </style>
  </head>
  <body>
    <canvas id="root"></canvas>
    <script type="module" src="/app.js"></script>
  </body>
</html>
`

func ServeAssets(prefix, dir string) {
	http.Handle(prefix, http.StripPrefix(prefix, http.FileServer(http.Dir(dir))))
}

func ServeClient() {
	ServeHostPage()
	ServeTerminal()
}

func ServeTerminal() {
	http.HandleFunc("/app.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(embeddedClient)
	})
	ServeWasm()
}

func ServeWasm() {
	http.HandleFunc("/term.wasm", wasmHandler)
}

func wasmHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/wasm")
	w.Header().Set("Cache-Control", "max-age=3600")
	w.Header().Set("Vary", "Accept-Encoding")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(embeddedWasmGz)
		return
	}
	wasmRaw.once.Do(func() {
		zr, err := gzip.NewReader(bytes.NewReader(embeddedWasmGz))
		if err != nil {
			return
		}
		if raw, err := io.ReadAll(zr); err == nil {
			wasmRaw.bytes = raw
		}
	})
	if wasmRaw.bytes == nil {
		http.Error(w, "caution: embedded term.wasm.gz is corrupt", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(wasmRaw.bytes)
}

func ServeHostPage() { http.HandleFunc("/", HostPage) }

func HostPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(hostPage))
}
