//go:build !js

package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nullentropy/caution/terminal/shaderx"
)

type hlslField struct{ typ, name, sem string }

func hlslStruct(t *testing.T, src, name string) []hlslField {
	t.Helper()
	m := regexp.MustCompile(`(?s)struct ` + name + `\s*\{(.*?)\};`).FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("no struct %s in:\n%s", name, src)
	}
	var out []hlslField
	line := regexp.MustCompile(`^(?:nointerpolation\s+)?(\w+)\s+(\w+)\s*:\s*(\w+);$`)
	for _, l := range strings.Split(m[1], "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		f := line.FindStringSubmatch(l)
		if f == nil {
			t.Fatalf("struct %s: unexpected member %q", name, l)
		}
		out = append(out, hlslField{f[1], f[2], f[3]})
	}
	return out
}

func compileHLSL(t *testing.T, st shaderx.Stage, src string) *shaderx.Module {
	t.Helper()
	m, err := shaderx.Compile(st, src, shaderx.HLSL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.HLSL, " main(SPIRV_Cross_Input stage_input)") {
		t.Errorf("no main entry point:\n%s", m.HLSL)
	}
	return m
}

func checkCbuffer(t *testing.T, m *shaderx.Module) {
	t.Helper()
	if m.UBOSize == 0 {
		if strings.Contains(m.HLSL, "cbuffer") {
			t.Errorf("no uniforms reflected but HLSL has a cbuffer:\n%s", m.HLSL)
		}
		return
	}
	if !regexp.MustCompile(`cbuffer \w+ : register\(b0\)`).MatchString(m.HLSL) {
		t.Fatalf("cbuffer not at b0:\n%s", m.HLSL)
	}
	po := regexp.MustCompile(`\w+ _\d+_(\w+) : packoffset\(c(\d+)(?:\.([xyzw]))?\);`)
	seen := 0
	for _, f := range po.FindAllStringSubmatch(m.HLSL, -1) {
		reg, _ := strconv.Atoi(f[2])
		off := reg * 16
		if f[3] != "" {
			off += strings.Index("xyzw", f[3]) * 4
		}
		want, ok := m.Fields[f[1]]
		if !ok {
			t.Errorf("packoffset for %s, which shaderx did not reflect", f[1])
			continue
		}
		if want.Offset != off {
			t.Errorf("%s at byte %d in HLSL, shaderx says %d", f[1], off, want.Offset)
		}
		seen++
	}
	if seen != len(m.Fields) {
		t.Errorf("%d packoffsets for %d reflected fields:\n%s", seen, len(m.Fields), m.HLSL)
	}
}

func checkTexture(t *testing.T, m *shaderx.Module) {
	t.Helper()
	if !strings.Contains(m.HLSL, "Texture2D<float4> u_tex : register(t0);") ||
		!regexp.MustCompile(`SamplerState \w+ : register\(s0\);`).MatchString(m.HLSL) {
		t.Errorf("texture or sampler not at t0/s0:\n%s", m.HLSL)
	}
}

func checkInputs(t *testing.T, m *shaderx.Module, instanced bool) {
	t.Helper()
	in := hlslStruct(t, m.HLSL, "SPIRV_Cross_Input")
	n := 1
	if instanced {
		n = 8
	}
	if len(in) > n {
		t.Fatalf("%d vertex inputs, layout has %d", len(in), n)
	}
	for _, f := range in {
		loc, err := strconv.Atoi(strings.TrimPrefix(f.sem, "TEXCOORD"))
		if err != nil || !strings.HasPrefix(f.sem, "TEXCOORD") || loc >= n {
			t.Errorf("vertex input %s : %s is not in the layout", f.name, f.sem)
			continue
		}
		want := "float4"
		if loc == 0 {
			want = "float2"
		}
		if f.typ != want {
			t.Errorf("%s : %s is %s, layout gives %s", f.name, f.sem, f.typ, want)
		}
	}
}

// D3D11 links stages by register, so pixel inputs must be a prefix of the
// vertex outputs
func checkLink(t *testing.T, vs, ps *shaderx.Module) {
	t.Helper()
	out := hlslStruct(t, vs.HLSL, "SPIRV_Cross_Output")
	in := hlslStruct(t, ps.HLSL, "SPIRV_Cross_Input")
	if len(out) == 0 || out[len(out)-1].sem != "SV_Position" {
		t.Errorf("vertex output does not end in SV_Position: %+v", out)
	}
	if len(in) > len(out) {
		t.Fatalf("pixel inputs %+v exceed vertex outputs %+v", in, out)
	}
	for i := range in {
		if in[i].sem != out[i].sem || in[i].typ != out[i].typ {
			t.Errorf("pixel input %d is %s %s, vertex output is %s %s", i, in[i].typ, in[i].sem, out[i].typ, out[i].sem)
		}
	}
	if !strings.Contains(ps.HLSL, "SV_Target0") {
		t.Errorf("pixel shader writes no SV_Target0:\n%s", ps.HLSL)
	}
}

func checkGlslang(t *testing.T, name, stage, src string) {
	t.Helper()
	bin, err := exec.LookPath("glslangValidator")
	if err != nil {
		return
	}
	p := filepath.Join(t.TempDir(), name+".hlsl")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "-D", "-V", "-e", "main", "-S", stage, "-o", p+".spv", p).CombinedOutput(); err != nil {
		t.Errorf("glslangValidator rejects %s: %v\n%s\n%s", name, err, out, src)
	} else {
		t.Logf("glslangValidator -D accepts %s", name)
	}
}

func TestHLSLForD3D11(t *testing.T) {
	vs := compileHLSL(t, shaderx.Vertex, vertSrc)
	fs := compileHLSL(t, shaderx.Fragment, fragSrc)
	checkCbuffer(t, vs)
	checkCbuffer(t, fs)
	checkInputs(t, vs, true)
	checkTexture(t, fs)
	checkLink(t, vs, fs)
	checkGlslang(t, "unified_vert", "vert", vs.HLSL)
	checkGlslang(t, "unified_frag", "frag", fs.HLSL)

	qv := compileHLSL(t, shaderx.Vertex, quadVertSrc)
	checkCbuffer(t, qv)
	checkInputs(t, qv, false)
	checkGlslang(t, "quad_vert", "vert", qv.HLSL)
	for name, frag := range map[string]string{
		"composite": wrapUserFrag(passthroughFrag, []string{"amount", "bad name"}, true),
		"quad":      wrapUserFrag(`vec4 effect(vec2 uv) { return vec4(uv, fract(u_time), 1.0) * amount; }`, []string{"amount"}, false),
	} {
		m := compileHLSL(t, shaderx.Fragment, frag)
		checkCbuffer(t, m)
		if m.HasTexture {
			checkTexture(t, m)
		}
		checkLink(t, qv, m)
		checkGlslang(t, name+"_frag", "frag", m.HLSL)
	}
}
