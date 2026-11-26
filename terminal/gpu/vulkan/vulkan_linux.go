package vulkan

/*
#cgo LDFLAGS: -lvulkan
#include <stdlib.h>
#include "vulkan_linux.h"
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
	handle *C.vktex
	w, h   int
}

func (t *texture) Size() (int, int) { return t.w, t.h }

type pipeline struct {
	handle    *C.vkpipe
	u         *shaderx.Uniforms
	instanced bool
}

func (p *pipeline) Set(name string, v ...float32) { p.u.Set(name, v...) }

type Device struct {
	ctx *C.vkctx
}

// gl_FragCoord starts at the top left in Vulkan
const blitVert = `
layout(location=0) in vec2 a_unit;
void main() { gl_Position = vec4(a_unit * 2.0 - 1.0, 0.0, 1.0); }`

const blitFrag = `
uniform sampler2D u_tex;
out vec4 o_color;
void main() {
  ivec2 s = textureSize(u_tex, 0);
  o_color = texelFetch(u_tex, min(ivec2(gl_FragCoord.xy), s - 1), 0);
}`

func NewXlib(display unsafe.Pointer, window uint64) (*Device, error) {
	return open(C.VKW_XLIB, display, window)
}

func NewWayland(display, surface unsafe.Pointer) (*Device, error) {
	return open(C.VKW_WAYLAND, display, uint64(uintptr(surface)))
}

func NewOffscreen() (*Device, error) { return open(C.VKW_NONE, nil, 0) }

func open(platform C.int, a unsafe.Pointer, b uint64) (*Device, error) {
	vm, err := shaderx.Compile(shaderx.Vertex, blitVert, 0)
	if err != nil {
		return nil, fmt.Errorf("vulkan blit vertex: %w", err)
	}
	fm, err := shaderx.Compile(shaderx.Fragment, blitFrag, 0)
	if err != nil {
		return nil, fmt.Errorf("vulkan blit fragment: %w", err)
	}
	var cerr *C.char
	ctx := C.vk_new(platform, a, C.uint64_t(b),
		(*C.uint32_t)(unsafe.Pointer(&vm.SPIRV[0])), C.int(len(vm.SPIRV)),
		(*C.uint32_t)(unsafe.Pointer(&fm.SPIRV[0])), C.int(len(fm.SPIRV)), &cerr)
	runtime.KeepAlive(vm)
	runtime.KeepAlive(fm)
	if ctx == nil {
		defer C.free(unsafe.Pointer(cerr))
		return nil, errors.New(C.GoString(cerr))
	}
	return &Device{ctx: ctx}, nil
}

func (d *Device) Name() string { return C.GoString(C.vk_device_name(d.ctx)) }

func (d *Device) MaxTextureSize() int { return int(C.vk_max_texture(d.ctx)) }

func (d *Device) NewTexture(w, h int, f gpu.Filter) (gpu.Texture, error) {
	nearest := C.int(0)
	if f == gpu.Nearest {
		nearest = 1
	}
	t := C.vk_texture(d.ctx, C.int(w), C.int(h), nearest)
	if t == nil {
		if d.Lost() {
			return nil, errLost
		}
		return nil, fmt.Errorf("vulkan: allocating a %dx%d texture failed", w, h)
	}
	return &texture{handle: t, w: w, h: h}, nil
}

func (d *Device) WriteTexture(t gpu.Texture, y0 int, rows []byte) error {
	tt := t.(*texture)
	n := len(rows) / (tt.w * 4)
	if n == 0 {
		return nil
	}
	if y0 < 0 || y0+n > tt.h {
		panic("vulkan: WriteTexture rows outside the texture")
	}
	if C.vk_write(d.ctx, tt.handle, unsafe.Pointer(&rows[0]), C.int(y0), C.int(n)) == 0 {
		if d.Lost() {
			return errLost
		}
		return fmt.Errorf("vulkan: uploading %d rows failed", n)
	}
	return nil
}

func (d *Device) DeleteTexture(t gpu.Texture) { C.vk_delete_texture(d.ctx, t.(*texture).handle) }

func (d *Device) NewPipeline(vert, frag string, instanced bool) (gpu.Pipeline, error) {
	vm, err := shaderx.Compile(shaderx.Vertex, vert, 0)
	if err != nil {
		return nil, err
	}
	fm, err := shaderx.Compile(shaderx.Fragment, frag, 0)
	if err != nil {
		return nil, err
	}
	inst, tex := C.int(0), C.int(0)
	if instanced {
		inst = 1
	}
	if fm.HasTexture || vm.HasTexture {
		tex = 1
	}
	var cerr *C.char
	h := C.vk_pipeline(d.ctx,
		(*C.uint32_t)(unsafe.Pointer(&vm.SPIRV[0])), C.int(len(vm.SPIRV)),
		(*C.uint32_t)(unsafe.Pointer(&fm.SPIRV[0])), C.int(len(fm.SPIRV)), inst, tex, &cerr)
	runtime.KeepAlive(vm)
	runtime.KeepAlive(fm)
	if h == nil {
		defer C.free(unsafe.Pointer(cerr))
		return nil, errors.New(C.GoString(cerr))
	}
	return &pipeline{handle: h, u: shaderx.NewUniforms(vm, fm), instanced: instanced}, nil
}

func (d *Device) DeletePipeline(p gpu.Pipeline) { C.vk_delete_pipeline(d.ctx, p.(*pipeline).handle) }

func (d *Device) SetTarget(t gpu.Texture, devW, devH int) {
	if t == nil {
		C.vk_set_target(d.ctx, nil, C.int(devW), C.int(devH))
		return
	}
	tt := t.(*texture)
	C.vk_set_target(d.ctx, tt.handle, C.int(tt.w), C.int(tt.h))
}

func (d *Device) Clear(r, g, b, a float32) {
	C.vk_clear(d.ctx, C.float(r), C.float(g), C.float(b), C.float(a))
}

func (d *Device) SetScissor(r *[4]int32) {
	if r == nil {
		C.vk_scissor(d.ctx, 0, 0, 0, 0, 0)
		return
	}
	C.vk_scissor(d.ctx, 1, C.int(r[0]), C.int(r[1]), C.int(r[2]), C.int(r[3]))
}

func (d *Device) Draw(p gpu.Pipeline, tex gpu.Texture, inst []float32, count int) {
	pp := p.(*pipeline)
	var ptr *C.vktex
	if tex != nil {
		ptr = tex.(*texture).handle
	}
	var vub, fub, ip unsafe.Pointer
	if len(pp.u.Vert) > 0 {
		vub = unsafe.Pointer(&pp.u.Vert[0])
	}
	if len(pp.u.Frag) > 0 {
		fub = unsafe.Pointer(&pp.u.Frag[0])
	}
	n := 0
	if pp.instanced {
		if count <= 0 || len(inst) < count*gpu.InstanceFloats {
			return
		}
		n = count * gpu.InstanceFloats * 4
		ip = unsafe.Pointer(&inst[0])
	}
	C.vk_draw(d.ctx, pp.handle, ptr, vub, C.int(len(pp.u.Vert)), fub, C.int(len(pp.u.Frag)), ip, C.int(n), C.int(count))
	runtime.KeepAlive(inst)
	runtime.KeepAlive(pp)
}

func (d *Device) Copy(src gpu.Texture) { C.vk_copy(d.ctx, src.(*texture).handle) }

// Vulkan clip space is y-down
func (d *Device) FlipY(layer bool) float32 { return 1 }

func (d *Device) ReadPixels(t gpu.Texture) ([]byte, error) {
	tt := t.(*texture)
	out := make([]byte, tt.w*tt.h*4)
	if C.vk_read(d.ctx, tt.handle, unsafe.Pointer(&out[0])) == 0 {
		if C.vk_is_lost(d.ctx) != 0 {
			return nil, errLost
		}
		return nil, errors.New("vulkan: readback failed")
	}
	return out, nil
}

func (d *Device) Present() bool { return C.vk_present(d.ctx) != 0 }

func (d *Device) Lost() bool { return C.vk_is_lost(d.ctx) != 0 }

var errLost = errors.New("vulkan: drawing stopped after a device error")
