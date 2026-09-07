package audio

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"math"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	Rate     = 48000
	Channels = 2

	fadeSamples = Rate / 100 * Channels
)

type Sink interface {
	Play(r io.Reader)
}

type Store struct {
	Fetch    func(src string) ([]byte, error)
	OpenSink func() (Sink, error)

	mu      sync.Mutex
	opened  bool // open has been started
	deaf    bool // the open failed so drop all sounds
	sink    Sink
	pending []*voice // waiting for the device to finish opening
	clips   map[string]*clipEntry
	voices  map[string][]*voice
}

type clipEntry struct {
	samples     []float32
	err         error
	loading     bool
	pendingOnce bool
	pendingLoop bool
}

type voice struct {
	samples  []float32
	pos      int
	loop     bool
	stop     atomic.Bool
	fadeLeft int
	finished bool
	done     func()
}

func (v *voice) Read(p []byte) (int, error) {
	if v.finished {
		return 0, io.EOF
	}
	frames := len(p) / (4 * Channels)
	if frames == 0 {
		return 0, io.ErrShortBuffer
	}
	if v.stop.Load() && v.fadeLeft < 0 {
		v.fadeLeft = fadeSamples
	}
	i := 0
	for ; i < frames*Channels; i++ {
		if v.pos >= len(v.samples) {
			if !v.loop {
				break
			}
			v.pos = 0
		}
		f := v.samples[v.pos]
		v.pos++
		if v.fadeLeft >= 0 {
			if v.fadeLeft == 0 {
				break
			}
			f *= float32(v.fadeLeft) / float32(fadeSamples)
			v.fadeLeft--
		}
		binary.LittleEndian.PutUint32(p[i*4:], math.Float32bits(f))
	}
	if i == 0 {
		v.finished = true
		v.done()
		return 0, io.EOF
	}
	return i * 4, nil
}

func (s *Store) Preload(src string) { s.load(src, false, false) }

func (s *Store) Play(src string) { s.load(src, true, false) }

func (s *Store) Loop(src string) { s.load(src, true, true) }

func (s *Store) Stop(src string) {
	s.mu.Lock()
	if e := s.clips[src]; e != nil {
		e.pendingOnce, e.pendingLoop = false, false
	}
	for _, v := range s.voices[src] {
		v.stop.Store(true)
	}
	s.mu.Unlock()
}

func (s *Store) StopAll() {
	s.mu.Lock()
	for _, e := range s.clips {
		e.pendingOnce, e.pendingLoop = false, false
	}
	for _, vs := range s.voices {
		for _, v := range vs {
			v.stop.Store(true)
		}
	}
	s.mu.Unlock()
}

func (s *Store) load(src string, play, loop bool) {
	s.mu.Lock()
	if s.clips == nil {
		s.clips = map[string]*clipEntry{}
	}
	e := s.clips[src]
	fresh := e == nil
	if fresh {
		e = &clipEntry{loading: true}
		s.clips[src] = e
	}
	if e.loading {
		if play && loop {
			e.pendingLoop = true
		} else if play {
			e.pendingOnce = true
		}
		s.mu.Unlock()
		if fresh {
			go s.fetch(src, e)
		}
		return
	}
	samples, err := e.samples, e.err
	s.mu.Unlock()
	if play && err == nil {
		s.start(src, samples, loop)
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
	once, loop := e.pendingOnce, e.pendingLoop
	e.pendingOnce, e.pendingLoop = false, false
	s.mu.Unlock()
	if err != nil {
		return
	}
	if once {
		s.start(src, samples, false)
	}
	if loop {
		s.start(src, samples, true)
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

func (s *Store) start(src string, samples []float32, loop bool) {
	if len(samples) == 0 {
		return
	}
	s.mu.Lock()
	if loop {
		for _, v := range s.voices[src] {
			if v.loop && !v.stop.Load() {
				s.mu.Unlock()
				return
			}
		}
	}
	if s.deaf {
		s.mu.Unlock()
		return
	}
	v := &voice{samples: samples, loop: loop, fadeLeft: -1}
	v.done = func() {
		s.mu.Lock()
		vs := s.voices[src]
		for i, w := range vs {
			if w == v {
				s.voices[src] = append(vs[:i], vs[i+1:]...)
				break
			}
		}
		if len(s.voices[src]) == 0 {
			delete(s.voices, src)
		}
		s.mu.Unlock()
	}
	if s.voices == nil {
		s.voices = map[string][]*voice{}
	}
	s.voices[src] = append(s.voices[src], v)

	if s.sink != nil {
		sink := s.sink
		s.mu.Unlock()
		sink.Play(v)
		return
	}

	s.pending = append(s.pending, v)
	if s.opened {
		s.mu.Unlock()
		return
	}
	s.opened = true
	open := s.OpenSink
	if open == nil {
		open = openOto
	}
	s.mu.Unlock()
	go func() {
		sink, err := open()
		s.mu.Lock()
		if err != nil {
			log.Printf("caution: audio output unavailable: %v", err)
			s.deaf = true
			dropped := s.pending
			s.pending = nil
			s.mu.Unlock()
			for _, pv := range dropped {
				pv.done()
			}
			return
		}
		s.sink = sink
		queued := s.pending
		s.pending = nil
		s.mu.Unlock()
		for _, pv := range queued {
			if pv.stop.Load() {
				pv.done()
				continue
			}
			sink.Play(pv)
		}
	}()
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
