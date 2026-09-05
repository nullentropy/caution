package audio

import (
	"encoding/base64"
	"errors"
	"log"
	"net/url"
	"strings"
	"sync"
)

const (
	Rate     = 48000
	Channels = 2
)

type Sink interface {
	Play(samples []float32)
}

type Store struct {
	Fetch    func(src string) ([]byte, error)
	OpenSink func() (Sink, error)

	mu     sync.Mutex
	opened bool
	sink   Sink
	clips  map[string]*clipEntry
}

type clipEntry struct {
	samples []float32
	err     error
	loading bool
	pending bool
}

func (s *Store) Preload(src string) { s.load(src, false) }

func (s *Store) Play(src string) { s.load(src, true) }

func (s *Store) load(src string, play bool) {
	s.mu.Lock()
	if s.clips == nil {
		s.clips = map[string]*clipEntry{}
	}
	e := s.clips[src]
	if e == nil {
		e = &clipEntry{loading: true, pending: play}
		s.clips[src] = e
		s.mu.Unlock()
		go s.fetch(src, e)
		return
	}
	if e.loading {
		if play {
			e.pending = true
		}
		s.mu.Unlock()
		return
	}
	samples, err := e.samples, e.err
	s.mu.Unlock()
	if play && err == nil {
		s.play(samples)
	}
}

func (s *Store) fetch(src string, e *clipEntry) {
	raw, err := s.bytes(src)
	var samples []float32
	if err == nil {
		var clip *Clip
		if clip, err = Decode(raw); err == nil {
			samples = clip.Convert(Rate, Channels)
		}
	}
	if err != nil {
		log.Printf("caution: sound %s: %v", src, err)
	}
	s.mu.Lock()
	e.samples, e.err, e.loading = samples, err, false
	pending := e.pending
	e.pending = false
	s.mu.Unlock()
	if pending && err == nil {
		s.play(samples)
	}
}

func (s *Store) bytes(src string) ([]byte, error) {
	if strings.HasPrefix(src, "data:") {
		return decodeDataURI(src)
	}
	if s.Fetch == nil {
		return nil, errors.New("no fetcher configured")
	}
	return s.Fetch(src)
}

func (s *Store) play(samples []float32) {
	s.mu.Lock()
	if !s.opened {
		s.opened = true
		open := s.OpenSink
		if open == nil {
			open = openOto
		}
		var err error
		if s.sink, err = open(); err != nil {
			log.Printf("caution: audio output unavailable: %v", err)
		}
	}
	sink := s.sink
	s.mu.Unlock()
	if sink != nil && len(samples) > 0 {
		sink.Play(samples)
	}
}

func decodeDataURI(src string) ([]byte, error) {
	comma := strings.IndexByte(src, ',')
	if comma < 0 {
		return nil, errors.New("malformed data URI")
	}
	meta, payload := src[5:comma], src[comma+1:]
	if strings.HasSuffix(meta, ";base64") {
		return base64.StdEncoding.DecodeString(payload)
	}
	unescaped, err := url.PathUnescape(payload)
	if err != nil {
		return nil, err
	}
	return []byte(unescaped), nil
}
