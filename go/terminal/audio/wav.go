package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Clip is decoded PCM: interleaved samples in [-1, 1].
type Clip struct {
	Rate     int
	Channels int
	Samples  []float32
}

const (
	wavPCM        = 1
	wavFloat      = 3
	wavExtensible = 0xFFFE
)

func Decode(data []byte) (*Clip, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	var (
		format, channels, bits int
		rate                   int
		pcm                    []byte
		haveFmt                bool
	)
	for pos := 12; pos+8 <= len(data); {
		id := string(data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(data[pos+4:]))
		body := pos + 8
		end := min(body+size, len(data))
		chunk := data[body:end]

		switch id {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, errors.New("short fmt chunk")
			}
			format = int(binary.LittleEndian.Uint16(chunk[0:]))
			channels = int(binary.LittleEndian.Uint16(chunk[2:]))
			rate = int(binary.LittleEndian.Uint32(chunk[4:]))
			bits = int(binary.LittleEndian.Uint16(chunk[14:]))
			if format == wavExtensible {
				if len(chunk) < 26 {
					return nil, errors.New("short extensible fmt chunk")
				}
				format = int(binary.LittleEndian.Uint16(chunk[24:]))
			}
			haveFmt = true
		case "data":
			pcm = chunk
		}
		pos = end + size%2 // chunks are word aligned
	}
	if !haveFmt || pcm == nil {
		return nil, errors.New("missing fmt or data chunk")
	}
	if channels < 1 || rate < 1 {
		return nil, fmt.Errorf("bad format: %d channels at %d Hz", channels, rate)
	}
	frame := channels * bits / 8
	if frame == 0 {
		return nil, fmt.Errorf("bad format: %d bits per sample", bits)
	}
	n := len(pcm) / frame * channels
	out := make([]float32, n)
	switch {
	case format == wavPCM && bits == 8:
		for i := range out {
			out[i] = (float32(pcm[i]) - 128) / 128
		}
	case format == wavPCM && bits == 16:
		for i := range out {
			out[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
		}
	case format == wavPCM && bits == 24:
		for i := range out {
			b := pcm[i*3 : i*3+3]
			v := int32(b[0]) | int32(b[1])<<8 | int32(b[2])<<16
			if v&0x800000 != 0 {
				v -= 1 << 24
			}
			out[i] = float32(v) / 8388608
		}
	case format == wavPCM && bits == 32:
		for i := range out {
			out[i] = float32(int32(binary.LittleEndian.Uint32(pcm[i*4:]))) / 2147483648
		}
	case format == wavFloat && bits == 32:
		for i := range out {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(pcm[i*4:]))
		}
	case format == wavFloat && bits == 64:
		for i := range out {
			out[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(pcm[i*8:])))
		}
	default:
		return nil, fmt.Errorf("unsupported WAV encoding: format %d, %d bits", format, bits)
	}
	return &Clip{Rate: rate, Channels: channels, Samples: out}, nil
}

// Convert returns the clip's samples at another rate and channel count
func (c *Clip) Convert(rate, channels int) []float32 {
	frames := len(c.Samples) / c.Channels
	if frames == 0 {
		return nil
	}
	outFrames := frames
	if rate != c.Rate {
		outFrames = int(math.Round(float64(frames) * float64(rate) / float64(c.Rate)))
	}
	out := make([]float32, outFrames*channels)
	step := float64(c.Rate) / float64(rate)
	for i := range outFrames {
		pos := float64(i) * step
		f0 := min(int(pos), frames-1)
		f1 := f0 + 1
		if f1 >= frames {
			f1 = frames - 1
		}
		t := float32(pos - float64(f0))
		for ch := range channels {
			src := ch
			if src >= c.Channels {
				src = c.Channels - 1
			}
			a := c.Samples[f0*c.Channels+src]
			b := c.Samples[f1*c.Channels+src]
			out[i*channels+ch] = a + (b-a)*t
		}
	}
	return out
}
