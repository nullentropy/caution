//go:build !js

package shaderx

/*
#cgo CXXFLAGS: -std=c++17 -O2 -w -DENABLE_SPIRV -I${SRCDIR}/third_party/glslang -I${SRCDIR}/third_party/spirv-cross
#cgo windows CXXFLAGS: -DGLSLANG_OSINCLUDE_WIN32
// mingw links the C++ runtime as DLLs a stock Windows install lacks
#cgo windows LDFLAGS: -static
#cgo unix CXXFLAGS: -DGLSLANG_OSINCLUDE_UNIX
#include <stdlib.h>
#include "sx.h"
*/
import "C"

import (
	"errors"
	"strings"
	"sync"
	"unsafe"
)

type Stage int

const (
	Vertex Stage = iota
	Fragment
)

type Target int

const (
	MSL  Target = C.SX_MSL
	HLSL Target = C.SX_HLSL
)

const (
	VertexUBO   = 0
	FragmentUBO = 1
	TexBinding  = 2
)

type Field struct{ Offset, Size int }

type Module struct {
	SPIRV      []uint32
	MSL        string
	MSLEntry   string
	HLSL       string
	UBOSize    int
	Fields     map[string]Field
	HasTexture bool
}

// glslang keeps process-global state
var mu sync.Mutex

// the source is compiled as GLSL 4.50, which requires an explicit binding on the texture
func Compile(stage Stage, body string, targets Target) (*Module, error) {
	src := "#version 450\n" + strings.Replace(body,
		"uniform sampler2D u_tex;", "layout(binding=2) uniform sampler2D u_tex;", 1)
	ubo := VertexUBO
	if stage == Fragment {
		ubo = FragmentUBO
	}
	cs := C.CString(src)
	defer C.free(unsafe.Pointer(cs))
	var o C.sx_out
	mu.Lock()
	rc := C.sx_compile(C.int(stage), cs, C.uint(ubo), C.int(targets), &o)
	mu.Unlock()
	defer C.sx_free(&o)
	if rc != 0 {
		return nil, errors.New(strings.TrimSpace(C.GoString(o.err)))
	}
	m := &Module{
		SPIRV:      append([]uint32(nil), unsafe.Slice((*uint32)(unsafe.Pointer(o.spirv)), int(o.spirv_words))...),
		UBOSize:    int(o.ubo_size),
		Fields:     make(map[string]Field, int(o.n_members)),
		HasTexture: o.has_tex != 0,
	}
	if o.msl != nil {
		m.MSL = C.GoString(o.msl)
		m.MSLEntry = C.GoString(o.msl_entry)
	}
	if o.hlsl != nil {
		m.HLSL = C.GoString(o.hlsl)
	}
	n := int(o.n_members)
	names := unsafe.Slice(o.names, n)
	offs := unsafe.Slice(o.offsets, n)
	sizes := unsafe.Slice(o.sizes, n)
	for i := range n {
		m.Fields[C.GoString(names[i])] = Field{Offset: int(offs[i]), Size: int(sizes[i])}
	}
	return m, nil
}
