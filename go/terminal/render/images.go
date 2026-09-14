package render

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"sync"

	"github.com/nullentropy/caution/go/terminal/gfx"

	"github.com/nullentropy/caution/go/terminal/glx"

	"github.com/rs/zerolog/log"
)

type imgState int

const (
	imgLoading imgState = iota
	imgReady
	imgError
)

type imageEntry struct {
	state imgState
	tex   uint32
	w, h  int
}

type decodedImage struct {
	src  string
	pix  []byte // premultiplied RGBA
	w, h int
	err  bool
}

// ImageStore mirrors the browser terminal's: src -> texture, loaded
// asynchronously, painted as a placeholder until ready. Fetching and decoding
// happen on goroutines; texture upload happens on the GL thread at the top of
// the next frame (drainUploads), which OnLoad wakes.
type ImageStore struct {
	// Fetch resolves a src (an absolute path resolved by the shell against
	// the server origin, or a full URL) to raw encoded bytes. nil disables
	// network images; data: URIs always work.
	Fetch func(src string) ([]byte, error)
	// OnLoad is called from fetch goroutines when a decode lands, so the
	// shell can invalidate and wake the event loop.
	OnLoad func()

	mu      sync.Mutex
	entries map[string]*imageEntry
	done    []decodedImage
}

func newImageStore() *ImageStore {
	return &ImageStore{entries: map[string]*imageEntry{}}
}

// Preload warms src (the `resource` op): fetch and decode start now, so the
// texture is ready (or failed) before any widget first paints it.
func (s *ImageStore) Preload(src string) { s.get(src) }

// get returns the entry for src, starting a fetch on first sight. GL thread.
func (s *ImageStore) get(src string) *imageEntry {
	s.mu.Lock()
	if e, ok := s.entries[src]; ok {
		s.mu.Unlock()
		return e
	}
	e := &imageEntry{state: imgLoading}
	s.entries[src] = e
	s.mu.Unlock()
	go s.load(src)
	return e
}

func (s *ImageStore) load(src string) {
	pix, w, h, err := s.fetchAndDecode(src)
	d := decodedImage{src: src, pix: pix, w: w, h: h, err: err != nil}
	if err != nil {
		log.Warn().Msgf("caution: image %q failed: %v", src, err)
	}
	s.mu.Lock()
	s.done = append(s.done, d)
	s.mu.Unlock()
	if s.OnLoad != nil {
		s.OnLoad()
	}
}

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

func decodeDataURI(src string) ([]byte, error) {
	comma := strings.IndexByte(src, ',')
	if comma < 0 {
		return nil, errors.New("malformed data URI")
	}
	meta, data := src[5:comma], src[comma+1:]
	if strings.HasSuffix(meta, ";base64") {
		return base64.StdEncoding.DecodeString(data)
	}
	return []byte(data), nil
}

// drainUploads turns finished decodes into GL textures. GL thread, once per
// frame. True means some image changed state - same display list, different
// pixels, so the frame cannot trust a damage diff.
func (s *ImageStore) drainUploads() bool {
	s.mu.Lock()
	done := s.done
	s.done = nil
	s.mu.Unlock()
	for _, d := range done {
		e := s.entries[d.src]
		if e == nil {
			continue
		}
		if d.err {
			e.state = imgError
			continue
		}
		glx.GenTextures(1, &e.tex)
		glx.ActiveTexture(glx.TEXTURE0)
		glx.BindTexture(glx.TEXTURE_2D, e.tex)
		glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_MIN_FILTER, glx.LINEAR)
		glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_MAG_FILTER, glx.LINEAR)
		glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_WRAP_S, glx.CLAMP_TO_EDGE)
		glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_WRAP_T, glx.CLAMP_TO_EDGE)
		glx.TexImage2D(glx.TEXTURE_2D, 0, glx.RGBA, int32(d.w), int32(d.h), 0,
			glx.RGBA, glx.UNSIGNED_BYTE, glx.Ptr(d.pix))
		e.w, e.h = d.w, d.h
		e.state = imgReady
	}
	return len(done) > 0
}

// fitImage aspect-fits an image's quad and uv into bounds.
func fitImage(b gfx.Rect, iw, ih int, fit string) (quad gfx.Rect, u0, v0, u1, v1 float32) {
	fw, fh := float32(iw), float32(ih)
	if fit == "contain" && iw > 0 && ih > 0 {
		s := min(b.W/fw, b.H/fh)
		w, h := fw*s, fh*s
		return gfx.R(b.X+(b.W-w)/2, b.Y+(b.H-h)/2, w, h), 0, 0, 1, 1
	}
	if fit == "cover" && iw > 0 && ih > 0 {
		s := max(b.W/fw, b.H/fh)
		uw := b.W / (fw * s)
		vh := b.H / (fh * s)
		u0 = (1 - uw) / 2
		v0 = (1 - vh) / 2
		return b, u0, v0, u0 + uw, v0 + vh
	}
	return b, 0, 0, 1, 1 // fill / degenerate
}
