package audio

import (
	"io"
	"sync"

	"github.com/ebitengine/oto/v3"
)

type otoSink struct {
	ctx *oto.Context

	mu      sync.Mutex
	playing []*oto.Player
}

func openOto() (Sink, error) {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   Rate,
		ChannelCount: Channels,
		Format:       oto.FormatFloat32LE,
	})
	if err != nil {
		return nil, err
	}
	<-ready
	return &otoSink{ctx: ctx}, nil
}

func (o *otoSink) Play(r io.Reader) {
	p := o.ctx.NewPlayer(r)
	p.Play()
	o.mu.Lock()

	// we have to keep references to playing sounds otherwise once they become
	// unreachable (after GC) they'll stop before we want them to
	live := o.playing[:0]
	for _, q := range o.playing {
		if q.IsPlaying() {
			live = append(live, q)
		}
	}
	o.playing = append(live, p)
	o.mu.Unlock()
}
