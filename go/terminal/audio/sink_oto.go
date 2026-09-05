package audio

import (
	"bytes"
	"encoding/binary"
	"math"

	"github.com/ebitengine/oto/v3"
)

type otoSink struct{ ctx *oto.Context }

// openOto opens the platform's audio device through oto
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

func (o *otoSink) Play(samples []float32) {
	buf := make([]byte, len(samples)*4)
	for i, f := range samples {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	o.ctx.NewPlayer(bytes.NewReader(buf)).Play()
}
