package audio

import (
	"io"

	"github.com/ebitengine/oto/v3"
)

type otoSink struct{ ctx *oto.Context }

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
	o.ctx.NewPlayer(r).Play()
}
