//go:build js && wasm

package render

import (
	"errors"
	"syscall/js"
)

// The browser decodes through an Image element, which covers every format
// the page itself can show (SVG included). The pixels come back through a 2D
// canvas unpremultiplied, so the premultiply the pipeline expects happens
// here, as it does natively.
func (s *ImageStore) fetchAndDecode(src string) ([]byte, int, int, error) {
	img := js.Global().Get("Image").New()
	img.Set("crossOrigin", "anonymous")
	done := make(chan bool, 1)
	onload := js.FuncOf(func(js.Value, []js.Value) any { done <- true; return nil })
	onerror := js.FuncOf(func(js.Value, []js.Value) any { done <- false; return nil })
	defer onload.Release()
	defer onerror.Release()
	img.Set("onload", onload)
	img.Set("onerror", onerror)
	img.Set("src", src)
	if !<-done {
		return nil, 0, 0, errors.New("image failed to load")
	}
	w, h := img.Get("naturalWidth").Int(), img.Get("naturalHeight").Int()
	if w == 0 || h == 0 {
		return nil, 0, 0, errors.New("image has no size")
	}
	doc := js.Global().Get("document")
	canvas := doc.Call("createElement", "canvas")
	canvas.Set("width", w)
	canvas.Set("height", h)
	ctx := canvas.Call("getContext", "2d")
	ctx.Call("drawImage", img, 0, 0)
	data := ctx.Call("getImageData", 0, 0, w, h).Get("data")
	pix := make([]byte, w*h*4)
	js.CopyBytesToGo(pix, data)
	for i := 0; i < len(pix); i += 4 {
		a := uint32(pix[i+3])
		pix[i] = byte(uint32(pix[i]) * a / 255)
		pix[i+1] = byte(uint32(pix[i+1]) * a / 255)
		pix[i+2] = byte(uint32(pix[i+2]) * a / 255)
	}
	return pix, w, h, nil
}
