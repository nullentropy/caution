//go:build !js

package shaderx

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

const testFrag = `
precision highp float;
uniform vec2 u_res;
uniform float u_time;
uniform float amount;
uniform sampler2D u_tex;
in vec2 v_uv;
out vec4 o_color;
void main() { o_color = texture(u_tex, v_uv) * amount + vec4(u_res, u_time, 0.0); }`

func TestLooseUniformsBecomeBlock(t *testing.T) {
	m, err := Compile(Fragment, testFrag, MSL|HLSL)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Field{"u_res": {0, 8, 0}, "u_time": {8, 4, 0}, "amount": {12, 4, 0}}
	for k, f := range want {
		if m.Fields[k] != f {
			t.Errorf("%s = %+v, want %+v", k, m.Fields[k], f)
		}
	}
	if m.UBOSize != 16 || !m.HasTexture || len(m.SPIRV) == 0 {
		t.Errorf("size %d tex %v spirv %d", m.UBOSize, m.HasTexture, len(m.SPIRV))
	}
	if !strings.Contains(m.MSL, "texture(0)") || !strings.Contains(m.MSL, "buffer(0)") {
		t.Errorf("msl bindings not at slot 0:\n%s", m.MSL)
	}
	if !strings.Contains(m.HLSL, "register(b0)") || !strings.Contains(m.HLSL, "register(t0)") {
		t.Errorf("hlsl bindings not at slot 0:\n%s", m.HLSL)
	}
}

func TestCompileErrorReported(t *testing.T) {
	_, err := Compile(Fragment, "out vec4 o_color; void main() { o_color = nope; }", MSL)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v", err)
	}
}

func TestArrayElementSetsItsOwnSlot(t *testing.T) {
	m, err := Compile(Fragment, `
uniform float P[4];
out vec4 o_color;
void main() { o_color = vec4(P[0], P[1], P[2], P[3]); }`, MSL)
	if err != nil {
		t.Fatal(err)
	}
	u := NewUniforms(&Module{}, m)
	u.Set("P[2]", 7)
	u.Set("P[4]", 9)
	f := m.Fields["P"]
	for i := range 4 {
		got := math.Float32frombits(binary.LittleEndian.Uint32(u.Frag[f.Offset+i*f.Stride:]))
		want := float32(0)
		if i == 2 {
			want = 7
		}
		if got != want {
			t.Errorf("P[%d] = %v, want %v (field %+v)", i, got, want, f)
		}
	}
}
