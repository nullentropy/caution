//go:build !js

package shaderx

import (
	"encoding/binary"
	"math"
)

type Uniforms struct {
	Vert, Frag []byte
	vf, ff     map[string]Field
}

func NewUniforms(vert, frag *Module) *Uniforms {
	return &Uniforms{
		Vert: make([]byte, vert.UBOSize), Frag: make([]byte, frag.UBOSize),
		vf: vert.Fields, ff: frag.Fields,
	}
}

func (u *Uniforms) Set(name string, v ...float32) {
	if f, ok := u.vf[name]; ok {
		put(u.Vert, f, v)
	}
	if f, ok := u.ff[name]; ok {
		put(u.Frag, f, v)
	}
}

func put(b []byte, f Field, v []float32) {
	for i, x := range v {
		if (i+1)*4 > f.Size {
			return
		}
		binary.LittleEndian.PutUint32(b[f.Offset+i*4:], math.Float32bits(x))
	}
}
