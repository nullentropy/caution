//go:build !js

package shaderx

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
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
	if f, ok := lookup(u.vf, name); ok {
		put(u.Vert, f, v)
	}
	if f, ok := lookup(u.ff, name); ok {
		put(u.Frag, f, v)
	}
}

func lookup(fields map[string]Field, name string) (Field, bool) {
	if f, ok := fields[name]; ok {
		return f, true
	}
	open := strings.IndexByte(name, '[')
	if open < 0 || !strings.HasSuffix(name, "]") {
		return Field{}, false
	}
	f, ok := fields[name[:open]]
	i, err := strconv.Atoi(name[open+1 : len(name)-1])
	if !ok || err != nil || f.Stride == 0 || i < 0 || (i+1)*f.Stride > f.Size {
		return Field{}, false
	}
	return Field{Offset: f.Offset + i*f.Stride, Size: f.Stride}, true
}

func put(b []byte, f Field, v []float32) {
	for i, x := range v {
		if (i+1)*4 > f.Size {
			return
		}
		binary.LittleEndian.PutUint32(b[f.Offset+i*4:], math.Float32bits(x))
	}
}
