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

	// oto stops a player once it's unreachable, so hold the playing ones
	live := o.playing[:0]
	for _, q := range o.playing {
		if q.IsPlaying() {
			live = append(live, q)
		}
	}
	o.playing = append(live, p)
	o.mu.Unlock()
}
