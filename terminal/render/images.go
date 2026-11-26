package render

import (
	"sync"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/gpu"

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
	tex   gpu.Texture
	w, h  int
}

type decodedImage struct {
	src  string
	pix  []byte
	w, h int
	err  bool
}

type ImageStore struct {
	Fetch  func(src string) ([]byte, error)
	OnLoad func()

	mu      sync.Mutex
	entries map[string]*imageEntry
	done    []decodedImage
	dev     gpu.Device
}

func newImageStore(dev gpu.Device) *ImageStore {
	return &ImageStore{entries: map[string]*imageEntry{}, dev: dev}
}

func (s *ImageStore) Preload(src string) { s.get(src) }

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

func (s *ImageStore) discardUploads() {
	s.mu.Lock()
	s.done = nil
	s.mu.Unlock()
}

func (s *ImageStore) drainUploads() bool {
	s.mu.Lock()
	done := s.done
	s.done = nil
	entries := make([]*imageEntry, len(done))
	for i, d := range done {
		entries[i] = s.entries[d.src]
	}
	s.mu.Unlock()
	for i, d := range done {
		e := entries[i]
		if e == nil {
			continue
		}
		if max := s.dev.MaxTextureSize(); d.err || d.w <= 0 || d.h <= 0 || d.w > max || d.h > max {
			e.state = imgError
			continue
		}
		tex, err := s.dev.NewTexture(d.w, d.h, gpu.Linear)
		if err == nil {
			if err = s.dev.WriteTexture(tex, 0, d.pix); err != nil {
				s.dev.DeleteTexture(tex)
			}
		}
		if err != nil {
			log.Warn().Err(err).Str("src", d.src).Msg("caution: image upload failed")
			e.state = imgError
			continue
		}
		e.tex = tex
		e.w, e.h = d.w, d.h
		e.state = imgReady
	}
	return len(done) > 0
}

func fitImage(b gfx.Rect, iw, ih int, fit gfx.Fit) (quad gfx.Rect, u0, v0, u1, v1 float32) {
	fw, fh := float32(iw), float32(ih)
	if fit == gfx.FitContain && iw > 0 && ih > 0 {
		s := min(b.W/fw, b.H/fh)
		w, h := fw*s, fh*s
		return gfx.R(b.X+(b.W-w)/2, b.Y+(b.H-h)/2, w, h), 0, 0, 1, 1
	}
	if fit == gfx.FitCover && iw > 0 && ih > 0 {
		s := max(b.W/fw, b.H/fh)
		uw := b.W / (fw * s)
		vh := b.H / (fh * s)
		u0 = (1 - uw) / 2
		v0 = (1 - vh) / 2
		return b, u0, v0, u0 + uw, v0 + vh
	}
	return b, 0, 0, 1, 1
}
