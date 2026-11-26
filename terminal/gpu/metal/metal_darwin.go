package metal

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -fmodules
#cgo LDFLAGS: -framework Metal -framework QuartzCore -framework Cocoa
#include <stdlib.h>
#include "metal_darwin.h"
#include <pthread.h>
static uintptr_t current_thread(void) { return (uintptr_t)pthread_self(); }
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
	handle  unsafe.Pointer
	w, h    int
	nearest bool
}

func (t *texture) Size() (int, int) { return t.w, t.h }

type pipeline struct {
	handle    unsafe.Pointer
	u         *shaderx.Uniforms
	instanced bool
}

func (p *pipeline) Set(name string, v ...float32) { p.u.Set(name, v...) }

type Device struct {
	ctx          unsafe.Pointer
	target       *texture
	tw, th       int
	open         bool
	pendingClear *[4]float32
	scissor      *[4]int32
}

func New(nswindow unsafe.Pointer) (*Device, error) {
	var cerr *C.char
	ctx := C.mtl_new(nswindow, &cerr)
	if ctx == nil {
		defer C.free(unsafe.Pointer(cerr))
		return nil, errors.New(C.GoString(cerr))
	}
	return &Device{ctx: ctx}, nil
}

// every Apple silicon GPU family supports 16384
func (d *Device) MaxTextureSize() int { return 16384 }

func (d *Device) NewTexture(w, h int, f gpu.Filter) (gpu.Texture, error) {
	if d.Lost() {
		return nil, errLost
	}
	t := C.mtl_texture(d.ctx, C.int(w), C.int(h))
	if t == nil {
		return nil, fmt.Errorf("metal: allocating a %dx%d texture failed", w, h)
	}
	return &texture{handle: t, w: w, h: h, nearest: f == gpu.Nearest}, nil
}

func (d *Device) WriteTexture(t gpu.Texture, y0 int, rows []byte) error {
	if d.Lost() {
		return errLost
	}
	tt := t.(*texture)
	n := len(rows) / (tt.w * 4)
	if n == 0 {
		return nil
	}
	if y0 < 0 || y0+n > tt.h {
		panic("metal: WriteTexture rows outside the texture")
	}
	d.open = false
	if C.mtl_write(d.ctx, tt.handle, unsafe.Pointer(&rows[0]), C.int(tt.w), C.int(y0), C.int(n)) == 0 {
		return fmt.Errorf("metal: uploading %d rows failed", n)
	}
	return nil
}

func (d *Device) DeleteTexture(t gpu.Texture) { C.mtl_release_later(d.ctx, t.(*texture).handle) }

func (d *Device) NewPipeline(vert, frag string, instanced bool) (gpu.Pipeline, error) {
	if d.Lost() {
		return nil, errLost
	}
	vm, err := shaderx.Compile(shaderx.Vertex, vert, shaderx.MSL)
	if err != nil {
		return nil, err
	}
	fm, err := shaderx.Compile(shaderx.Fragment, frag, shaderx.MSL)
	if err != nil {
		return nil, err
	}
	vs, ve, fs, fe := C.CString(vm.MSL), C.CString(vm.MSLEntry), C.CString(fm.MSL), C.CString(fm.MSLEntry)
	defer func() {
		C.free(unsafe.Pointer(vs))
		C.free(unsafe.Pointer(ve))
		C.free(unsafe.Pointer(fs))
		C.free(unsafe.Pointer(fe))
	}()
	inst := C.int(0)
	if instanced {
		inst = 1
	}
	var cerr *C.char
	h := C.mtl_pipeline(d.ctx, vs, ve, fs, fe, inst, &cerr)
	if h == nil {
		defer C.free(unsafe.Pointer(cerr))
		return nil, errors.New(C.GoString(cerr))
	}
	return &pipeline{handle: h, u: shaderx.NewUniforms(vm, fm), instanced: instanced}, nil
}

func (d *Device) DeletePipeline(p gpu.Pipeline) { C.mtl_release_later(d.ctx, p.(*pipeline).handle) }

func (d *Device) flushPending() {
	if d.pendingClear != nil {
		d.ensureOpen()
	}
	C.mtl_end(d.ctx)
	d.open = false
}

func (d *Device) ensureOpen() bool {
	if d.open {
		return true
	}
	var ptr unsafe.Pointer
	if d.target != nil {
		ptr = d.target.handle
	}
	clear, c := C.int(0), [4]float32{}
	if d.pendingClear != nil {
		clear, c = 1, *d.pendingClear
	}
	if C.mtl_begin(d.ctx, ptr, C.int(d.tw), C.int(d.th), clear,
		C.float(c[0]), C.float(c[1]), C.float(c[2]), C.float(c[3])) == 0 {
		return false
	}
	d.pendingClear = nil
	d.open = true
	d.applyScissor()
	return true
}

func (d *Device) applyScissor() {
	if s := d.scissor; s != nil {
		C.mtl_scissor(d.ctx, 1, C.int(s[0]), C.int(s[1]), C.int(s[2]), C.int(s[3]))
	} else {
		C.mtl_scissor(d.ctx, 0, 0, 0, 0, 0)
	}
}

func (d *Device) SetTarget(t gpu.Texture, devW, devH int) {
	if d.Lost() {
		return
	}
	d.flushPending()
	d.target = nil
	d.tw, d.th = devW, devH
	if t != nil {
		d.target = t.(*texture)
		d.tw, d.th = d.target.w, d.target.h
	}
}

func (d *Device) Clear(r, g, b, a float32) {
	if d.Lost() {
		return
	}
	C.mtl_end(d.ctx)
	d.open = false
	d.pendingClear = &[4]float32{r, g, b, a}
}

func (d *Device) SetScissor(r *[4]int32) {
	if d.Lost() {
		return
	}
	d.scissor = r
	if d.open {
		d.applyScissor()
	}
}

func (d *Device) Draw(p gpu.Pipeline, tex gpu.Texture, inst []float32, count int) {
	if d.Lost() {
		return
	}
	if !d.ensureOpen() {
		return
	}
	pp := p.(*pipeline)
	var ptr unsafe.Pointer
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
	instanced := C.int(0)
	if pp.instanced {
		instanced = 1
		ip = unsafe.Pointer(&inst[0])
	}
	C.mtl_draw(d.ctx, pp.handle, ptr, nearest, vub, C.int(len(pp.u.Vert)), fub, C.int(len(pp.u.Frag)),
		ip, C.int(len(inst)*4), C.int(count), instanced)
	runtime.KeepAlive(inst)
}

func (d *Device) Copy(src gpu.Texture) {
	if d.Lost() {
		return
	}
	if !d.ensureOpen() {
		return
	}
	C.mtl_scissor(d.ctx, 0, 0, 0, 0, 0)
	C.mtl_copy(d.ctx, src.(*texture).handle)
	d.applyScissor()
}

// Metal's clip space is y-up with row 0 at the top, so every target draws
// top-down the same way
func (d *Device) FlipY(layer bool) float32 { return -1 }

func (d *Device) ReadPixels(t gpu.Texture) ([]byte, error) {
	if d.Lost() {
		return nil, errLost
	}
	d.flushPending()
	tt := t.(*texture)
	out := make([]byte, tt.w*tt.h*4)
	if C.mtl_read(d.ctx, tt.handle, unsafe.Pointer(&out[0]), C.int(tt.w), C.int(tt.h)) == 0 {
		return nil, errors.New("metal: readback command buffer failed")
	}
	return out, nil
}

func (d *Device) Lost() bool { return C.mtl_is_lost(d.ctx) != 0 }

func (d *Device) Present() bool {
	if d.Lost() {
		return false
	}
	d.flushPending()
	return C.mtl_present(d.ctx) != 0
}

func currentThread() uintptr { return uintptr(C.current_thread()) }

var errLost = errors.New("metal: drawing stopped after a device error")
