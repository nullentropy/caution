package d3d11

/*
#cgo LDFLAGS: -ld3d11 -ldxgi
#include <stdlib.h>
#include "d3d11_windows.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/nullentropy/caution/terminal/gpu"
	"github.com/nullentropy/caution/terminal/shaderx"
)

type texture struct {
	handle  *C.dx_tex
	w, h    int
	nearest bool
}

func (t *texture) Size() (int, int) { return t.w, t.h }

type pipeline struct {
	handle    *C.dx_pipe
	u         *shaderx.Uniforms
	instanced bool
}

func (p *pipeline) Set(name string, v ...float32) { p.u.Set(name, v...) }

type Device struct {
	ctx     *C.dx_ctx
	target  *texture
	tw, th  int
	scissor *[4]int32
}

func New(hwnd unsafe.Pointer) (*Device, error) {
	var cerr *C.char
	ctx := C.dx_new(hwnd, &cerr)
	if ctx == nil {
		defer C.free(unsafe.Pointer(cerr))
		return nil, errors.New(C.GoString(cerr))
	}
	return &Device{ctx: ctx}, nil
}

// 16384 is D3D11_REQ_TEXTURE2D_U_OR_V_DIMENSION, which applies because the
// device is feature level 11_0 or higher
func (d *Device) MaxTextureSize() int { return 16384 }

func (d *Device) NewTexture(w, h int, f gpu.Filter) (gpu.Texture, error) {
	t := C.dx_texture(d.ctx, C.int(w), C.int(h))
	if t == nil {
		if d.Lost() {
			return nil, errLost
		}
		return nil, fmt.Errorf("d3d11: allocating a %dx%d texture failed", w, h)
	}
	return &texture{handle: t, w: w, h: h, nearest: f == gpu.Nearest}, nil
}

func (d *Device) WriteTexture(t gpu.Texture, y0 int, rows []byte) error {
	tt := t.(*texture)
	n := len(rows) / (tt.w * 4)
	if n == 0 {
		return nil
	}
	if y0 < 0 || y0+n > tt.h {
		panic("d3d11: WriteTexture rows outside the texture")
	}
	C.dx_write(d.ctx, tt.handle, unsafe.Pointer(&rows[0]), C.int(y0), C.int(n))
	runtime.KeepAlive(rows)
	if d.Lost() {
		return errLost
	}
	return nil
}

func (d *Device) DeleteTexture(t gpu.Texture) {
	tt := t.(*texture)
	if d.target == tt {
		d.target = nil
	}
	C.dx_tex_free(d.ctx, tt.handle)
	tt.handle = nil
}

func (d *Device) NewPipeline(vert, frag string, instanced bool) (gpu.Pipeline, error) {
	vm, err := shaderx.Compile(shaderx.Vertex, vert, shaderx.HLSL)
	if err != nil {
		return nil, err
	}
	fm, err := shaderx.Compile(shaderx.Fragment, frag, shaderx.HLSL)
	if err != nil {
		return nil, err
	}
	vs, fs := C.CString(vm.HLSL), C.CString(fm.HLSL)
	defer func() {
		C.free(unsafe.Pointer(vs))
		C.free(unsafe.Pointer(fs))
	}()
	inst := C.int(0)
	if instanced {
		inst = 1
	}
	var cerr *C.char
	h := C.dx_pipeline(d.ctx, vs, fs, C.int(vm.UBOSize), C.int(fm.UBOSize), inst, &cerr)
	if h == nil {
		defer C.free(unsafe.Pointer(cerr))
		return nil, errors.New(C.GoString(cerr))
	}
	return &pipeline{handle: h, u: shaderx.NewUniforms(vm, fm), instanced: instanced}, nil
}

func (d *Device) DeletePipeline(p gpu.Pipeline) {
	pp := p.(*pipeline)
	C.dx_pipe_free(pp.handle)
	pp.handle = nil
}

func (d *Device) applyScissor() {
	if s := d.scissor; s != nil {
		C.dx_scissor(d.ctx, 1, C.int(s[0]), C.int(s[1]), C.int(s[2]), C.int(s[3]))
	} else {
		C.dx_scissor(d.ctx, 0, 0, 0, 0, 0)
	}
}

func (d *Device) SetTarget(t gpu.Texture, devW, devH int) {
	d.target = nil
	d.tw, d.th = devW, devH
	var ptr *C.dx_tex
	if t != nil {
		d.target = t.(*texture)
		d.tw, d.th = d.target.w, d.target.h
		ptr = d.target.handle
	}
	C.dx_target(d.ctx, ptr, C.int(d.tw), C.int(d.th))
	d.applyScissor()
}

func (d *Device) Clear(r, g, b, a float32) {
	C.dx_clear(d.ctx, C.float(r), C.float(g), C.float(b), C.float(a))
}

func (d *Device) SetScissor(r *[4]int32) {
	if r != nil {
		c := *r
		r = &c
	}
	d.scissor = r
	d.applyScissor()
}

func (d *Device) Draw(p gpu.Pipeline, tex gpu.Texture, inst []float32, count int) {
	pp := p.(*pipeline)
	var ptr *C.dx_tex
	nearest := C.int(0)
	if tex != nil {
		tt := tex.(*texture)
		ptr = tt.handle
		if tt.nearest {
			nearest = 1
		}
	}
	var vub, fub, ip unsafe.Pointer
	if len(pp.u.Vert) > 0 {
		vub = unsafe.Pointer(&pp.u.Vert[0])
	}
	if len(pp.u.Frag) > 0 {
		fub = unsafe.Pointer(&pp.u.Frag[0])
	}
	if pp.instanced {
		if count <= 0 || len(inst) == 0 {
			return
		}
		ip = unsafe.Pointer(&inst[0])
	}
	C.dx_draw(d.ctx, pp.handle, ptr, nearest, vub, C.int(len(pp.u.Vert)), fub, C.int(len(pp.u.Frag)),
		ip, C.int(len(inst)*4), C.int(count))
	runtime.KeepAlive(inst)
}

func (d *Device) Copy(src gpu.Texture) { C.dx_copy(d.ctx, src.(*texture).handle) }

// D3D clip space is y-up with row 0 at the top
func (d *Device) FlipY(layer bool) float32 { return -1 }

func (d *Device) ReadPixels(t gpu.Texture) ([]byte, error) {
	tt := t.(*texture)
	out := make([]byte, tt.w*tt.h*4)
	if len(out) > 0 && C.dx_read(d.ctx, tt.handle, unsafe.Pointer(&out[0])) == 0 {
		if C.dx_is_lost(d.ctx) != 0 {
			return nil, errLost
		}
		return nil, errors.New("d3d11: readback failed")
	}
	return out, nil
}

var errLost = errors.New("d3d11: device removed")

func (d *Device) Present() bool { return C.dx_present(d.ctx) != 0 }

func (d *Device) Lost() bool { return C.dx_is_lost(d.ctx) != 0 }
