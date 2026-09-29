//go:build js && wasm

// The browser dialect: WebGL2 through syscall/js, presenting the same names and
// go-gl signatures as the native shims so the renderer compiles unchanged. GL
// hands out integer names for its objects and WebGL hands out JS objects, so a
// handle table maps one to the other; name 0 is null, the default target.
package glx

import (
	"errors"
	"syscall/js"
	"unsafe"
)

const ES = true

var (
	gl      js.Value
	objects = map[uint32]js.Value{}
	nextID  uint32
	scratch js.Value // Uint8Array reused for uploads
)

// SetContext hands the shim the page's WebGL2 context. Call before Init.
func SetContext(ctx js.Value) { gl = ctx }

func Init() error {
	if gl.IsUndefined() || gl.IsNull() {
		return errors.New("glx: no WebGL2 context")
	}
	scratch = js.Global().Get("Uint8Array").New(0)
	return nil
}

const (
	ARRAY_BUFFER        = 0x8892
	BLEND               = 0x0BE2
	CLAMP_TO_EDGE       = 0x812F
	COLOR_ATTACHMENT0   = 0x8CE0
	COLOR_BUFFER_BIT    = 0x00004000
	COMPILE_STATUS      = 0x8B81
	DEPTH_TEST          = 0x0B71
	DRAW_FRAMEBUFFER    = 0x8CA9
	DYNAMIC_DRAW        = 0x88E8
	FALSE               = 0
	FLOAT               = 0x1406
	FRAGMENT_SHADER     = 0x8B30
	FRAMEBUFFER         = 0x8D40
	INFO_LOG_LENGTH     = 0x8B84
	LINEAR              = 0x2601
	LINK_STATUS         = 0x8B82
	NEAREST             = 0x2600
	ONE                 = 1
	ONE_MINUS_SRC_ALPHA = 0x0303
	READ_FRAMEBUFFER    = 0x8CA8
	RGBA                = 0x1908
	SCISSOR_TEST        = 0x0C11
	STATIC_DRAW         = 0x88E4
	TEXTURE0            = 0x84C0
	TEXTURE_2D          = 0x0DE1
	TEXTURE_MAG_FILTER  = 0x2800
	TEXTURE_MIN_FILTER  = 0x2801
	TEXTURE_WRAP_S      = 0x2802
	TEXTURE_WRAP_T      = 0x2803
	TRIANGLE_STRIP      = 0x0005
	UNSIGNED_BYTE       = 0x1401
	VERTEX_SHADER       = 0x8B31
)

func alloc(v js.Value) uint32 {
	nextID++
	objects[nextID] = v
	return nextID
}

func obj(name uint32) js.Value {
	if name == 0 {
		return js.Null()
	}
	return objects[name]
}

func release(name uint32) { delete(objects, name) }

func gen(n int32, names *uint32, method string) {
	out := unsafe.Slice(names, int(n))
	for i := range out {
		out[i] = alloc(gl.Call(method))
	}
}

func del(n int32, names *uint32, method string) {
	in := unsafe.Slice(names, int(n))
	for _, name := range in {
		gl.Call(method, obj(name))
		release(name)
	}
}

// bytesAt views size bytes of Go memory starting at data.
func bytesAt(data unsafe.Pointer, size int) []byte {
	return unsafe.Slice((*byte)(data), size)
}

// upload copies b into the scratch Uint8Array and returns a view of exactly
// that length. WebGL reads the view synchronously, so one buffer serves every call.
func upload(b []byte) js.Value {
	if scratch.Get("length").Int() < len(b) {
		scratch = js.Global().Get("Uint8Array").New(len(b))
	}
	js.CopyBytesToJS(scratch, b)
	return scratch.Call("subarray", 0, len(b))
}

func cstring(p *uint8) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}

// writeLog copies s into the buffer GL would have filled, NUL terminated
// within bufSize.
func writeLog(s string, bufSize int32, length *int32, out *uint8) {
	if out == nil || bufSize <= 0 {
		return
	}
	buf := unsafe.Slice((*byte)(out), int(bufSize))
	n := copy(buf[:len(buf)-1], s)
	buf[n] = 0
	if length != nil {
		*length = int32(n)
	}
}

func ActiveTexture(texture uint32)        { gl.Call("activeTexture", texture) }
func AttachShader(program, shader uint32) { gl.Call("attachShader", obj(program), obj(shader)) }
func BindBuffer(target, buffer uint32)    { gl.Call("bindBuffer", target, obj(buffer)) }
func BindFramebuffer(target, framebuffer uint32) {
	gl.Call("bindFramebuffer", target, obj(framebuffer))
}
func BindTexture(target, texture uint32)          { gl.Call("bindTexture", target, obj(texture)) }
func BindVertexArray(array uint32)                { gl.Call("bindVertexArray", obj(array)) }
func BlendFunc(sfactor, dfactor uint32)           { gl.Call("blendFunc", sfactor, dfactor) }
func Clear(mask uint32)                           { gl.Call("clear", mask) }
func ClearColor(r, g, b, a float32)               { gl.Call("clearColor", r, g, b, a) }
func CompileShader(shader uint32)                 { gl.Call("compileShader", obj(shader)) }
func CreateProgram() uint32                       { return alloc(gl.Call("createProgram")) }
func CreateShader(xtype uint32) uint32            { return alloc(gl.Call("createShader", xtype)) }
func DeleteFramebuffers(n int32, fbs *uint32)     { del(n, fbs, "deleteFramebuffer") }
func DeleteTextures(n int32, textures *uint32)    { del(n, textures, "deleteTexture") }
func Disable(cap uint32)                          { gl.Call("disable", cap) }
func DrawArrays(mode uint32, first, count int32)  { gl.Call("drawArrays", mode, first, count) }
func Enable(cap uint32)                           { gl.Call("enable", cap) }
func EnableVertexAttribArray(index uint32)        { gl.Call("enableVertexAttribArray", index) }
func GenBuffers(n int32, buffers *uint32)         { gen(n, buffers, "createBuffer") }
func GenFramebuffers(n int32, fbs *uint32)        { gen(n, fbs, "createFramebuffer") }
func GenTextures(n int32, textures *uint32)       { gen(n, textures, "createTexture") }
func GenVertexArrays(n int32, arrays *uint32)     { gen(n, arrays, "createVertexArray") }
func LinkProgram(program uint32)                  { gl.Call("linkProgram", obj(program)) }
func Scissor(x, y, width, height int32)           { gl.Call("scissor", x, y, width, height) }
func TexParameteri(target, pname uint32, p int32) { gl.Call("texParameteri", target, pname, p) }
func Uniform1f(location int32, v0 float32)        { gl.Call("uniform1f", obj(uint32(location)), v0) }
func Uniform1i(location int32, v0 int32)          { gl.Call("uniform1i", obj(uint32(location)), v0) }
func Uniform2f(location int32, v0, v1 float32)    { gl.Call("uniform2f", obj(uint32(location)), v0, v1) }
func UseProgram(program uint32)                   { gl.Call("useProgram", obj(program)) }
func VertexAttribDivisor(index, divisor uint32)   { gl.Call("vertexAttribDivisor", index, divisor) }
func Viewport(x, y, width, height int32)          { gl.Call("viewport", x, y, width, height) }

func DeleteProgram(program uint32) {
	gl.Call("deleteProgram", obj(program))
	release(program)
}

func DeleteShader(shader uint32) {
	gl.Call("deleteShader", obj(shader))
	release(shader)
}

func Uniform4f(location int32, v0, v1, v2, v3 float32) {
	gl.Call("uniform4f", obj(uint32(location)), v0, v1, v2, v3)
}

func DrawArraysInstanced(mode uint32, first, count, instancecount int32) {
	gl.Call("drawArraysInstanced", mode, first, count, instancecount)
}

func BlitFramebuffer(srcX0, srcY0, srcX1, srcY1, dstX0, dstY0, dstX1, dstY1 int32, mask, filter uint32) {
	gl.Call("blitFramebuffer", srcX0, srcY0, srcX1, srcY1, dstX0, dstY0, dstX1, dstY1, mask, filter)
}

func FramebufferTexture2D(target, attachment, textarget, texture uint32, level int32) {
	gl.Call("framebufferTexture2D", target, attachment, textarget, obj(texture), level)
}

func VertexAttribPointerWithOffset(index uint32, size int32, xtype uint32, normalized bool, stride int32, offset uintptr) {
	gl.Call("vertexAttribPointer", index, size, xtype, normalized, stride, int(offset))
}

func BufferData(target uint32, size int, data unsafe.Pointer, usage uint32) {
	if data == nil {
		gl.Call("bufferData", target, size, usage)
		return
	}
	gl.Call("bufferData", target, upload(bytesAt(data, size)), usage)
}

func BufferSubData(target uint32, offset, size int, data unsafe.Pointer) {
	gl.Call("bufferSubData", target, offset, upload(bytesAt(data, size)))
}

// TexImage2D only ever sees RGBA / UNSIGNED_BYTE here, which fixes the byte
// count go-gl's pointer signature leaves out.
func TexImage2D(target uint32, level, internalformat, width, height, border int32, format, xtype uint32, pixels unsafe.Pointer) {
	if pixels == nil {
		gl.Call("texImage2D", target, level, internalformat, width, height, border, format, xtype, js.Null())
		return
	}
	gl.Call("texImage2D", target, level, internalformat, width, height, border, format, xtype,
		upload(bytesAt(pixels, int(width)*int(height)*4)))
}

func ReadPixels(x, y, width, height int32, format, xtype uint32, pixels unsafe.Pointer) {
	n := int(width) * int(height) * 4
	dst := js.Global().Get("Uint8Array").New(n)
	gl.Call("readPixels", x, y, width, height, format, xtype, dst)
	js.CopyBytesToGo(bytesAt(pixels, n), dst)
}

func ShaderSource(shader uint32, count int32, xstring **uint8, length *int32) {
	src := ""
	ptrs := unsafe.Slice(xstring, int(count))
	for _, p := range ptrs {
		src += cstring(p)
	}
	gl.Call("shaderSource", obj(shader), src)
}

func GetShaderiv(shader, pname uint32, params *int32) {
	switch pname {
	case COMPILE_STATUS:
		*params = FALSE
		if gl.Call("getShaderParameter", obj(shader), pname).Bool() {
			*params = 1
		}
	case INFO_LOG_LENGTH:
		*params = int32(len(gl.Call("getShaderInfoLog", obj(shader)).String())) + 1
	}
}

func GetProgramiv(program, pname uint32, params *int32) {
	switch pname {
	case LINK_STATUS:
		*params = FALSE
		if gl.Call("getProgramParameter", obj(program), pname).Bool() {
			*params = 1
		}
	case INFO_LOG_LENGTH:
		*params = int32(len(gl.Call("getProgramInfoLog", obj(program)).String())) + 1
	}
}

func GetShaderInfoLog(shader uint32, bufSize int32, length *int32, infoLog *uint8) {
	writeLog(gl.Call("getShaderInfoLog", obj(shader)).String(), bufSize, length, infoLog)
}

func GetProgramInfoLog(program uint32, bufSize int32, length *int32, infoLog *uint8) {
	writeLog(gl.Call("getProgramInfoLog", obj(program)).String(), bufSize, length, infoLog)
}

// GetUniformLocation returns -1 for a missing uniform, as GL does, and
// otherwise a handle to the location object.
func GetUniformLocation(program uint32, name *uint8) int32 {
	loc := gl.Call("getUniformLocation", obj(program), cstring(name))
	if loc.IsNull() {
		return -1
	}
	return int32(alloc(loc))
}

// Ptr, Str and Strs match go-gl's helpers: pointers into Go memory that the
// calls above read back.
func Ptr(data any) unsafe.Pointer {
	switch v := data.(type) {
	case nil:
		return nil
	case []float32:
		if len(v) == 0 {
			return nil
		}
		return unsafe.Pointer(&v[0])
	case []byte:
		if len(v) == 0 {
			return nil
		}
		return unsafe.Pointer(&v[0])
	}
	panic("glx: unsupported Ptr type")
}

func Str(s string) *uint8 {
	if len(s) == 0 || s[len(s)-1] != 0 {
		panic("glx: Str needs a NUL-terminated string")
	}
	return unsafe.StringData(s)
}

func Strs(strs ...string) (**uint8, func()) {
	ptrs := make([]*uint8, len(strs))
	for i, s := range strs {
		ptrs[i] = Str(s)
	}
	return &ptrs[0], func() {}
}
