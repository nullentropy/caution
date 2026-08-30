//go:build !angle && !windows

// Package glx pins the terminal's GL dialect at build time. The default is
// desktop OpenGL 4.1 core - macOS's (deprecated, frozen) ceiling. Building
// with `-tags angle` swaps in OpenGL ES 3 through ANGLE's Metal backend
// instead (see terminal/egl)
package glx

import gl "github.com/go-gl/gl/v4.1-core/gl"

// ES reports the dialect: shader version headers differ (410 core / 300 es).
const ES = false

// Init loads GL function pointers for the current context.
func Init() error { return gl.Init() }

const (
	ARRAY_BUFFER        = gl.ARRAY_BUFFER
	BLEND               = gl.BLEND
	CLAMP_TO_EDGE       = gl.CLAMP_TO_EDGE
	COLOR_ATTACHMENT0   = gl.COLOR_ATTACHMENT0
	COLOR_BUFFER_BIT    = gl.COLOR_BUFFER_BIT
	COMPILE_STATUS      = gl.COMPILE_STATUS
	DEPTH_TEST          = gl.DEPTH_TEST
	DRAW_FRAMEBUFFER    = gl.DRAW_FRAMEBUFFER
	DYNAMIC_DRAW        = gl.DYNAMIC_DRAW
	FALSE               = gl.FALSE
	FLOAT               = gl.FLOAT
	FRAGMENT_SHADER     = gl.FRAGMENT_SHADER
	FRAMEBUFFER         = gl.FRAMEBUFFER
	INFO_LOG_LENGTH     = gl.INFO_LOG_LENGTH
	LINEAR              = gl.LINEAR
	LINK_STATUS         = gl.LINK_STATUS
	NEAREST             = gl.NEAREST
	ONE                 = gl.ONE
	ONE_MINUS_SRC_ALPHA = gl.ONE_MINUS_SRC_ALPHA
	READ_FRAMEBUFFER    = gl.READ_FRAMEBUFFER
	RGBA                = gl.RGBA
	SCISSOR_TEST        = gl.SCISSOR_TEST
	STATIC_DRAW         = gl.STATIC_DRAW
	TEXTURE0            = gl.TEXTURE0
	TEXTURE_2D          = gl.TEXTURE_2D
	TEXTURE_MAG_FILTER  = gl.TEXTURE_MAG_FILTER
	TEXTURE_MIN_FILTER  = gl.TEXTURE_MIN_FILTER
	TEXTURE_WRAP_S      = gl.TEXTURE_WRAP_S
	TEXTURE_WRAP_T      = gl.TEXTURE_WRAP_T
	TRIANGLE_STRIP      = gl.TRIANGLE_STRIP
	UNSIGNED_BYTE       = gl.UNSIGNED_BYTE
	VERTEX_SHADER       = gl.VERTEX_SHADER
)

var (
	ActiveTexture                 = gl.ActiveTexture
	AttachShader                  = gl.AttachShader
	BindBuffer                    = gl.BindBuffer
	BindFramebuffer               = gl.BindFramebuffer
	BindTexture                   = gl.BindTexture
	BindVertexArray               = gl.BindVertexArray
	BlendFunc                     = gl.BlendFunc
	BlitFramebuffer               = gl.BlitFramebuffer
	BufferData                    = gl.BufferData
	BufferSubData                 = gl.BufferSubData
	Clear                         = gl.Clear
	ClearColor                    = gl.ClearColor
	CompileShader                 = gl.CompileShader
	CreateProgram                 = gl.CreateProgram
	CreateShader                  = gl.CreateShader
	DeleteFramebuffers            = gl.DeleteFramebuffers
	DeleteProgram                 = gl.DeleteProgram
	DeleteShader                  = gl.DeleteShader
	DeleteTextures                = gl.DeleteTextures
	Disable                       = gl.Disable
	DrawArrays                    = gl.DrawArrays
	DrawArraysInstanced           = gl.DrawArraysInstanced
	Enable                        = gl.Enable
	EnableVertexAttribArray       = gl.EnableVertexAttribArray
	FramebufferTexture2D          = gl.FramebufferTexture2D
	GenBuffers                    = gl.GenBuffers
	GenFramebuffers               = gl.GenFramebuffers
	GenTextures                   = gl.GenTextures
	GenVertexArrays               = gl.GenVertexArrays
	GetProgramInfoLog             = gl.GetProgramInfoLog
	GetProgramiv                  = gl.GetProgramiv
	GetShaderInfoLog              = gl.GetShaderInfoLog
	GetShaderiv                   = gl.GetShaderiv
	GetUniformLocation            = gl.GetUniformLocation
	LinkProgram                   = gl.LinkProgram
	Ptr                           = gl.Ptr
	ReadPixels                    = gl.ReadPixels
	Scissor                       = gl.Scissor
	ShaderSource                  = gl.ShaderSource
	Str                           = gl.Str
	Strs                          = gl.Strs
	TexImage2D                    = gl.TexImage2D
	TexParameteri                 = gl.TexParameteri
	Uniform1f                     = gl.Uniform1f
	Uniform1i                     = gl.Uniform1i
	Uniform2f                     = gl.Uniform2f
	Uniform4f                     = gl.Uniform4f
	UseProgram                    = gl.UseProgram
	VertexAttribDivisor           = gl.VertexAttribDivisor
	VertexAttribPointerWithOffset = gl.VertexAttribPointerWithOffset
	Viewport                      = gl.Viewport
)
