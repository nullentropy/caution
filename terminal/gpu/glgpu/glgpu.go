//go:build js && wasm

package glgpu

import (
	"fmt"
	"strings"

	"github.com/nullentropy/caution/terminal/glx"
	"github.com/nullentropy/caution/terminal/gpu"
)

type texture struct {
	id, fbo uint32
	w, h    int
}

func (t *texture) Size() (int, int) { return t.w, t.h }

type pipeline struct {
	prog      uint32
	locs      map[string]int32
	instanced bool
}

func (p *pipeline) Set(name string, v ...float32) {
	loc, ok := p.locs[name]
	if !ok {
		loc = glx.GetUniformLocation(p.prog, glx.Str(name+"\x00"))
		p.locs[name] = loc
	}
	if loc < 0 {
		return
	}
	glx.UseProgram(p.prog)
	switch len(v) {
	case 1:
		glx.Uniform1f(loc, v[0])
	case 2:
		glx.Uniform2f(loc, v[0], v[1])
	case 4:
		glx.Uniform4f(loc, v[0], v[1], v[2], v[3])
	}
}

type Device struct {
	vao, quadVao     uint32
	quadBuf, instBuf uint32
	instCap          int
	fbo              uint32
	targetH          int32
	scissor          *[4]int32
	maxTex           int
}

func New() *Device {
	d := &Device{maxTex: glx.GetInteger(glx.MAX_TEXTURE_SIZE)}
	glx.GenVertexArrays(1, &d.vao)
	glx.BindVertexArray(d.vao)
	glx.GenBuffers(1, &d.quadBuf)
	glx.BindBuffer(glx.ARRAY_BUFFER, d.quadBuf)
	unit := []float32{0, 0, 1, 0, 0, 1, 1, 1}
	glx.BufferData(glx.ARRAY_BUFFER, len(unit)*4, glx.Ptr(unit), glx.STATIC_DRAW)
	glx.EnableVertexAttribArray(0)
	glx.VertexAttribPointerWithOffset(0, 2, glx.FLOAT, false, 0, 0)
	glx.GenBuffers(1, &d.instBuf)
	glx.BindBuffer(glx.ARRAY_BUFFER, d.instBuf)
	d.instCap = 4096 * gpu.InstanceFloats * 4
	glx.BufferData(glx.ARRAY_BUFFER, d.instCap, nil, glx.DYNAMIC_DRAW)
	for i := uint32(0); i < 7; i++ {
		glx.EnableVertexAttribArray(1 + i)
		glx.VertexAttribPointerWithOffset(1+i, 4, glx.FLOAT, false, gpu.InstanceFloats*4, uintptr(i*16))
		glx.VertexAttribDivisor(1+i, 1)
	}
	glx.BindVertexArray(0)

	glx.GenVertexArrays(1, &d.quadVao)
	glx.BindVertexArray(d.quadVao)
	glx.BindBuffer(glx.ARRAY_BUFFER, d.quadBuf)
	glx.EnableVertexAttribArray(0)
	glx.VertexAttribPointerWithOffset(0, 2, glx.FLOAT, false, 0, 0)
	glx.BindVertexArray(0)

	glx.Disable(glx.DEPTH_TEST)
	glx.Enable(glx.BLEND)
	glx.BlendFunc(glx.ONE, glx.ONE_MINUS_SRC_ALPHA)
	return d
}

func (d *Device) MaxTextureSize() int { return d.maxTex }

func (d *Device) NewTexture(w, h int, f gpu.Filter) (gpu.Texture, error) {
	t := &texture{w: w, h: h}
	filter := int32(glx.LINEAR)
	if f == gpu.Nearest {
		filter = glx.NEAREST
	}
	glx.GenTextures(1, &t.id)
	glx.ActiveTexture(glx.TEXTURE0)
	glx.BindTexture(glx.TEXTURE_2D, t.id)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_MIN_FILTER, filter)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_MAG_FILTER, filter)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_WRAP_S, glx.CLAMP_TO_EDGE)
	glx.TexParameteri(glx.TEXTURE_2D, glx.TEXTURE_WRAP_T, glx.CLAMP_TO_EDGE)
	glx.TexImage2D(glx.TEXTURE_2D, 0, glx.RGBA, int32(w), int32(h), 0, glx.RGBA, glx.UNSIGNED_BYTE, nil)
	return t, nil
}

// GL puts the first row at t=0, which is where every sampler expects the
// top row
func (d *Device) WriteTexture(t gpu.Texture, y0 int, rows []byte) error {
	tt := t.(*texture)
	n := len(rows) / (tt.w * 4)
	if n == 0 {
		return nil
	}
	if y0 < 0 || y0+n > tt.h {
		panic("glgpu: WriteTexture rows outside the texture")
	}
	glx.ActiveTexture(glx.TEXTURE0)
	glx.BindTexture(glx.TEXTURE_2D, tt.id)
	glx.TexSubImage2D(glx.TEXTURE_2D, 0, 0, int32(y0), int32(tt.w), int32(n),
		glx.RGBA, glx.UNSIGNED_BYTE, glx.Ptr(rows))
	return nil
}

func (d *Device) DeleteTexture(t gpu.Texture) {
	tt := t.(*texture)
	if tt.fbo != 0 {
		glx.DeleteFramebuffers(1, &tt.fbo)
	}
	glx.DeleteTextures(1, &tt.id)
}

func (t *texture) framebuffer() uint32 {
	if t.fbo == 0 {
		glx.GenFramebuffers(1, &t.fbo)
		glx.BindFramebuffer(glx.FRAMEBUFFER, t.fbo)
		glx.FramebufferTexture2D(glx.FRAMEBUFFER, glx.COLOR_ATTACHMENT0, glx.TEXTURE_2D, t.id, 0)
	}
	return t.fbo
}

func compile(xtype uint32, src string) (uint32, error) {
	sh := glx.CreateShader(xtype)
	csources, free := glx.Strs(src + "\x00")
	glx.ShaderSource(sh, 1, csources, nil)
	free()
	glx.CompileShader(sh)
	var status int32
	glx.GetShaderiv(sh, glx.COMPILE_STATUS, &status)
	if status == glx.FALSE {
		var n int32
		glx.GetShaderiv(sh, glx.INFO_LOG_LENGTH, &n)
		infoLog := strings.Repeat("\x00", int(n)+1)
		glx.GetShaderInfoLog(sh, n, nil, glx.Str(infoLog))
		glx.DeleteShader(sh)
		return 0, fmt.Errorf("shader compile failed: %s", strings.TrimRight(infoLog, "\x00"))
	}
	return sh, nil
}

func (d *Device) NewPipeline(vert, frag string, instanced bool) (gpu.Pipeline, error) {
	vs, err := compile(glx.VERTEX_SHADER, "#version 300 es\n"+vert)
	if err != nil {
		return nil, err
	}
	fs, err := compile(glx.FRAGMENT_SHADER, "#version 300 es\nprecision highp float;\n"+frag)
	if err != nil {
		glx.DeleteShader(vs)
		return nil, err
	}
	prog := glx.CreateProgram()
	glx.AttachShader(prog, vs)
	glx.AttachShader(prog, fs)
	glx.LinkProgram(prog)
	glx.DeleteShader(vs)
	glx.DeleteShader(fs)
	var status int32
	glx.GetProgramiv(prog, glx.LINK_STATUS, &status)
	if status == glx.FALSE {
		var n int32
		glx.GetProgramiv(prog, glx.INFO_LOG_LENGTH, &n)
		infoLog := strings.Repeat("\x00", int(n)+1)
		glx.GetProgramInfoLog(prog, n, nil, glx.Str(infoLog))
		glx.DeleteProgram(prog)
		return nil, fmt.Errorf("program link failed: %s", strings.TrimRight(infoLog, "\x00"))
	}
	p := &pipeline{prog: prog, locs: map[string]int32{}, instanced: instanced}
	if loc := glx.GetUniformLocation(prog, glx.Str("u_tex\x00")); loc >= 0 {
		glx.UseProgram(prog)
		glx.Uniform1i(loc, 0)
	}
	return p, nil
}

func (d *Device) DeletePipeline(p gpu.Pipeline) { glx.DeleteProgram(p.(*pipeline).prog) }

func (d *Device) SetTarget(t gpu.Texture, devW, devH int) {
	d.fbo = 0
	if t != nil {
		tt := t.(*texture)
		d.fbo = tt.framebuffer()
		devW, devH = tt.w, tt.h
	}
	glx.BindFramebuffer(glx.FRAMEBUFFER, d.fbo)
	glx.Viewport(0, 0, int32(devW), int32(devH))
	d.targetH = int32(devH)
	d.applyScissor()
}

// GL scissor is bottom-origin
func (d *Device) applyScissor() {
	if d.scissor == nil {
		glx.Disable(glx.SCISSOR_TEST)
		return
	}
	sc := *d.scissor
	glx.Enable(glx.SCISSOR_TEST)
	glx.Scissor(sc[0], d.targetH-sc[1]-sc[3], sc[2], sc[3])
}

func (d *Device) SetScissor(r *[4]int32) {
	d.scissor = r
	d.applyScissor()
}

func (d *Device) Clear(r, g, b, a float32) {
	glx.Disable(glx.SCISSOR_TEST)
	glx.ClearColor(r, g, b, a)
	glx.Clear(glx.COLOR_BUFFER_BIT)
	d.applyScissor()
}

func (d *Device) Draw(p gpu.Pipeline, tex gpu.Texture, inst []float32, count int) {
	pp := p.(*pipeline)
	glx.UseProgram(pp.prog)
	if tex != nil {
		glx.ActiveTexture(glx.TEXTURE0)
		glx.BindTexture(glx.TEXTURE_2D, tex.(*texture).id)
	}
	if !pp.instanced {
		glx.BindVertexArray(d.quadVao)
		glx.DrawArrays(glx.TRIANGLE_STRIP, 0, 4)
		glx.BindVertexArray(0)
		return
	}
	glx.BindVertexArray(d.vao)
	glx.BindBuffer(glx.ARRAY_BUFFER, d.instBuf)
	// orphan the buffer and use SubData at a constant size, because BufferData
	// at varying sizes makes the driver allocate and copy on every flush
	size := len(inst) * 4
	for size > d.instCap {
		d.instCap *= 2
	}
	glx.BufferData(glx.ARRAY_BUFFER, d.instCap, nil, glx.DYNAMIC_DRAW)
	glx.BufferSubData(glx.ARRAY_BUFFER, 0, size, glx.Ptr(inst))
	glx.DrawArraysInstanced(glx.TRIANGLE_STRIP, 0, 4, int32(count))
	glx.BindVertexArray(0)
}

func (d *Device) Copy(src gpu.Texture) {
	s := src.(*texture)
	glx.Disable(glx.SCISSOR_TEST)
	glx.BindFramebuffer(glx.READ_FRAMEBUFFER, s.framebuffer())
	glx.BindFramebuffer(glx.DRAW_FRAMEBUFFER, d.fbo)
	glx.BlitFramebuffer(0, 0, int32(s.w), int32(s.h), 0, 0, int32(s.w), int32(s.h), glx.COLOR_BUFFER_BIT, glx.NEAREST)
	glx.BindFramebuffer(glx.FRAMEBUFFER, d.fbo)
	d.applyScissor()
}

func (d *Device) FlipY(layer bool) float32 {
	if layer {
		return 1
	}
	return -1
}

func (d *Device) ReadPixels(t gpu.Texture) ([]byte, error) {
	tt := t.(*texture)
	pix := make([]byte, tt.w*tt.h*4)
	glx.BindFramebuffer(glx.FRAMEBUFFER, tt.framebuffer())
	glx.ReadPixels(0, 0, int32(tt.w), int32(tt.h), glx.RGBA, glx.UNSIGNED_BYTE, glx.Ptr(pix))
	glx.BindFramebuffer(glx.FRAMEBUFFER, d.fbo)
	stride := tt.w * 4
	out := make([]byte, len(pix))
	for y := range tt.h {
		copy(out[y*stride:(y+1)*stride], pix[(tt.h-1-y)*stride:(tt.h-y)*stride])
	}
	return out, nil
}

// the browser presents when the frame callback returns
func (d *Device) Present() bool { return true }

func (d *Device) Lost() bool { return false }
