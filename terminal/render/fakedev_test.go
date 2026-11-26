package render

import (
	"github.com/nullentropy/caution/terminal/gpu/gputest"
)

const fakeMaxTexture = 4096

type fakeDev struct {
	*gputest.Device
	glyphs, blank int
	texels        int
	writtenRows   int
	solidAlphas   []float32
}

func newFakeDev() *fakeDev {
	d := &fakeDev{Device: gputest.New()}
	d.MaxTexture = fakeMaxTexture
	d.OnWrite = func(_ *gputest.Texture, _, rows int) { d.writtenRows += rows }
	d.OnDraw = d.countGlyphs
	return d
}

func (d *fakeDev) countGlyphs(tex *gputest.Texture, inst []float32, count int) {
	for i := 0; i < count && (i+1)*floatsPerInstance <= len(inst); i++ {
		in := inst[i*floatsPerInstance:]
		mode := int(in[17] + 0.5)
		if mode == modeSolid {
			d.solidAlphas = append(d.solidAlphas, in[11])
		}
		if mode != modeTextured || tex == nil {
			continue
		}
		x0, y0 := int(in[20]*float32(tex.W)), int(in[21]*float32(tex.H))
		x1, y1 := int(in[22]*float32(tex.W)), int(in[23]*float32(tex.H))
		cov := 0
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				cov += int(tex.Pix[(y*tex.W+x)*4+3])
			}
		}
		d.glyphs++
		d.texels += (x1 - x0) * (y1 - y0)
		if cov == 0 {
			d.blank++
		}
	}
}
