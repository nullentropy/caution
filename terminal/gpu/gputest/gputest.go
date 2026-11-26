package gputest

import (
	"errors"
	"fmt"

	"github.com/nullentropy/caution/terminal/gpu"
)

var (
	ErrLost  = errors.New("gputest: device lost")
	ErrAlloc = errors.New("gputest: allocation failed")
)

type Texture struct {
	W, H int
	Pix  []byte
}

func (t *Texture) Size() (int, int) { return t.W, t.H }

type Pipeline struct{}

func (Pipeline) Set(string, ...float32) {}

type Device struct {
	MaxTexture         int
	FailPipelinesAfter int
	LoseAtCall         int
	FailTexture        func(w, h int) bool
	FailWrite          func(w, h int) bool
	DropPresents       bool
	OnDraw             func(tex *Texture, inst []float32, count int)
	OnWrite            func(t *Texture, y0, rows int)

	Calls     int
	pipelines int
	textures  int
	target    *Texture
	targetW   int
	targetH   int
}

func New() *Device { return &Device{MaxTexture: 4096} }

func (d *Device) Lost() bool { return d.LoseAtCall > 0 && d.Calls >= d.LoseAtCall }

func (d *Device) call() bool {
	d.Calls++
	return !d.Lost()
}

func (d *Device) MaxTextureSize() int { return d.MaxTexture }

func (d *Device) NewTexture(w, h int, _ gpu.Filter) (gpu.Texture, error) {
	if w <= 0 || h <= 0 || w > d.MaxTexture || h > d.MaxTexture {
		panic(fmt.Sprintf("gputest: %dx%d texture is outside 1..%d", w, h, d.MaxTexture))
	}
	if !d.call() {
		return nil, ErrLost
	}
	if d.FailTexture != nil && d.FailTexture(w, h) {
		return nil, ErrAlloc
	}
	d.textures++
	return &Texture{W: w, H: h, Pix: make([]byte, w*h*4)}, nil
}

func (d *Device) WriteTexture(t gpu.Texture, y0 int, rows []byte) error {
	tt := t.(*Texture)
	n := len(rows) / (tt.W * 4)
	if len(rows)%(tt.W*4) != 0 || y0 < 0 || y0+n > tt.H {
		panic(fmt.Sprintf("gputest: %d bytes at row %d don't fit a %dx%d texture", len(rows), y0, tt.W, tt.H))
	}
	if !d.call() {
		return ErrLost
	}
	if d.FailWrite != nil && d.FailWrite(tt.W, tt.H) {
		return ErrAlloc
	}
	copy(tt.Pix[y0*tt.W*4:], rows)
	if d.OnWrite != nil {
		d.OnWrite(tt, y0, n)
	}
	return nil
}

func (d *Device) DeleteTexture(gpu.Texture) {
	d.call()
	d.textures--
}

func (d *Device) Textures() int { return d.textures }

func (d *Device) NewPipeline(_, _ string, _ bool) (gpu.Pipeline, error) {
	if !d.call() {
		return nil, ErrLost
	}
	if d.FailPipelinesAfter > 0 && d.pipelines >= d.FailPipelinesAfter {
		return nil, errors.New("gputest: pipeline compile failed")
	}
	d.pipelines++
	return Pipeline{}, nil
}

func (d *Device) DeletePipeline(gpu.Pipeline) {
	d.call()
	d.pipelines--
}

func (d *Device) Pipelines() int { return d.pipelines }

func (d *Device) SetTarget(t gpu.Texture, devW, devH int) {
	d.call()
	d.target, d.targetW, d.targetH = nil, devW, devH
	if t != nil {
		d.target = t.(*Texture)
		d.targetW, d.targetH = d.target.W, d.target.H
	}
}

func (d *Device) Clear(_, _, _, _ float32) { d.call() }
func (d *Device) SetScissor(*[4]int32)     { d.call() }

func (d *Device) Draw(p gpu.Pipeline, tex gpu.Texture, inst []float32, count int) {
	if p == nil {
		panic("gputest: Draw with a nil pipeline")
	}
	if !d.call() {
		return
	}
	tt, _ := tex.(*Texture)
	if d.OnDraw != nil {
		d.OnDraw(tt, inst, count)
	}
}

func (d *Device) Copy(src gpu.Texture) {
	s := src.(*Texture)
	if s.W != d.targetW || s.H != d.targetH {
		panic(fmt.Sprintf("gputest: Copy of %dx%d onto a %dx%d target", s.W, s.H, d.targetW, d.targetH))
	}
	d.call()
}

func (d *Device) FlipY(bool) float32 { return -1 }

func (d *Device) ReadPixels(t gpu.Texture) ([]byte, error) {
	tt := t.(*Texture)
	if !d.call() {
		return nil, ErrLost
	}
	return append([]byte(nil), tt.Pix...), nil
}

func (d *Device) Present() bool { return d.call() && !d.DropPresents }
