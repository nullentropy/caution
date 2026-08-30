package caution

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"testing"
)

// The shaper artifact is embedded gzipped; the handler negotiates encoding.
// gzip-capable clients (every browser) get the compressed bytes verbatim;
// identity clients get a lazily inflated copy that must be valid wasm.
func TestShaperWasmContentNegotiation(t *testing.T) {
	r := httptest.NewRequest("GET", "/shaper.wasm", nil)
	r.Header.Set("Accept-Encoding", "gzip, deflate, br")
	w := httptest.NewRecorder()
	shaperWasmHandler(w, r)
	if ce := w.Header().Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", ce)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/wasm" {
		t.Fatalf("Content-Type = %q, want application/wasm", ct)
	}
	if !bytes.Equal(w.Body.Bytes(), embeddedShaperGz) {
		t.Fatal("gzip response is not the embedded artifact verbatim")
	}

	r = httptest.NewRequest("GET", "/shaper.wasm", nil)
	w = httptest.NewRecorder()
	shaperWasmHandler(w, r)
	if ce := w.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("identity Content-Encoding = %q, want none", ce)
	}
	raw := w.Body.Bytes()
	if len(raw) < 4 || string(raw[:4]) != "\x00asm" {
		t.Fatalf("identity body lacks the wasm magic (got % x)", raw[:min(4, len(raw))])
	}
	zr, err := gzip.NewReader(bytes.NewReader(embeddedShaperGz))
	if err != nil {
		t.Fatal(err)
	}
	want, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatal("identity body is not the inflation of the embedded artifact")
	}
}
