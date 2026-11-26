package metal

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/nullentropy/caution/terminal/gpu"
)

const testVert = `
layout(location=0) in vec2 a_unit;
void main() { gl_Position = vec4(a_unit * 2.0 - 1.0, 0.0, 1.0); }`

const testFrag = `
out vec4 o_color;
void main() { o_color = vec4(1.0); }`

// the device runs on the Go main thread, where no autorelease pool exists
// unless the backend makes one
func TestNoAutoreleaseWithoutPool(t *testing.T) {
	if os.Getenv("METAL_POOL_CHILD") == "1" {
		runtime.LockOSThread()
		fmt.Printf("thread 0x%x\n", currentThread())
		renderFrames(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNoAutoreleaseWithoutPool$")
	cmd.Env = append(os.Environ(), "METAL_POOL_CHILD=1", "OBJC_DEBUG_MISSING_POOLS=YES")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	var thread string
	for _, line := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "thread "); ok {
			thread = "(" + rest + ")"
		}
	}
	if thread == "" {
		t.Fatalf("child printed no thread id:\n%s", out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "no pool in place") && strings.Contains(line, thread) {
			t.Fatalf("objects autoreleased with no pool on the calling thread: %s", line)
		}
	}
}

func renderFrames(t *testing.T) {
	d, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	tex, err := d.NewTexture(64, 64, gpu.Nearest)
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.NewPipeline(testVert, testFrag, false)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]byte, 64*4*8)
	for range 20 {
		d.SetTarget(tex, 64, 64)
		d.Clear(0, 0, 0, 1)
		d.SetScissor(&[4]int32{0, 0, 32, 32})
		d.Draw(p, nil, nil, 1)
		if err := d.WriteTexture(tex, 8, rows); err != nil {
			t.Fatal(err)
		}
		d.Present()
	}
	if _, err := d.ReadPixels(tex); err != nil {
		t.Fatal(err)
	}
	d.DeleteTexture(tex)
}

func TestWriteOutsideTheTexturePanics(t *testing.T) {
	d, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	tex, err := d.NewTexture(4, 4, gpu.Linear)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("writing rows 3..5 of a 4-row texture did not panic")
		}
	}()
	d.WriteTexture(tex, 3, make([]byte, 4*4*2))
}
