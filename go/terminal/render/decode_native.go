//go:build !js

package render

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

func (s *ImageStore) fetchAndDecode(src string) ([]byte, int, int, error) {
	var raw []byte
	var err error
	if strings.HasPrefix(src, "data:") {
		raw, err = decodeDataURI(src)
	} else if s.Fetch != nil {
		raw, err = s.Fetch(src)
	} else {
		err = errors.New("no fetcher configured")
	}
	if err != nil {
		return nil, 0, 0, err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	// Premultiplied RGBA - the pipeline's texture contract (the browser
	// terminal uploads with UNPACK_PREMULTIPLY_ALPHA_WEBGL; here we do it).
	pix := make([]byte, w*h*4)
	o := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, a := img.At(x, y).RGBA() // 16-bit, already premultiplied
			pix[o] = byte(r >> 8)
			pix[o+1] = byte(g >> 8)
			pix[o+2] = byte(bb >> 8)
			pix[o+3] = byte(a >> 8)
			o += 4
		}
	}
	return pix, w, h, nil
}
