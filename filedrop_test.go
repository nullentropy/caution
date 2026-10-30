package caution

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nullentropy/caution/terminal/proto"
	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"
)

func (tc *testClient) dropStart(x, y float64, files ...File) {
	tc.t.Helper()
	specs := []any{}
	for _, f := range files {
		specs = append(specs, map[string]any{"name": f.Name, "type": f.Type, "size": len(f.Data)})
	}
	tc.eventValue(0, "filedrop", map[string]any{"p": "start", "x": x, "y": y, "files": specs}, 0)
}

func (tc *testClient) dropData(i int, data []byte) {
	tc.t.Helper()
	tc.eventValue(0, "filedrop", map[string]any{"p": "data", "f": i, "b": base64.StdEncoding.EncodeToString(data)}, 0)
}

func (tc *testClient) dropEnd() {
	tc.t.Helper()
	tc.eventValue(0, "filedrop", map[string]any{"p": "end"}, 0)
}

func dropMount(fired chan string) MountFunc {
	return func(s *Session) *Node {
		s.OnFileDrag(func(over bool) { fired <- fmt.Sprint("drag ", over) })
		s.OnFileDrop(func(files []File, x, y float64) {
			out := fmt.Sprintf("drop %v,%v", x, y)
			for _, f := range files {
				out += fmt.Sprintf(" %s|%s|%s", f.Name, f.Type, f.Data)
			}
			fired <- out
		})
		return Panel()
	}
}

func TestChunkedDropAssemblesFilesAndPosition(t *testing.T) {
	fired := make(chan string, 8)
	tc := dialSession(t, dropMount(fired))
	m := tc.read()
	if m["fileCap"] != float64(defaultMaxDropBytes) {
		t.Fatalf("mount fileCap = %v; want the default cap", m["fileCap"])
	}

	tc.eventValue(0, "filedrag", true, 0)
	expectFire(t, fired, "drag true")
	tc.eventValue(0, "filedrag", false, 0)
	expectFire(t, fired, "drag false")

	a := File{Name: "a.txt", Type: "text/plain", Data: []byte("hello world")}
	b := File{Name: "b.bin", Data: []byte{}}
	c := File{Name: "c.txt", Type: "text/plain", Data: []byte("tail")}
	tc.dropStart(12.5, 40, a, b, c)
	tc.dropData(0, a.Data[:5])
	tc.dropData(0, a.Data[5:])
	tc.dropData(2, c.Data)
	tc.dropEnd()
	expectFire(t, fired, "drop 12.5,40 a.txt|text/plain|hello world b.bin|| c.txt|text/plain|tail")
}

func TestOverCapAndInterruptedDropsDeliverNothing(t *testing.T) {
	fired := make(chan string, 8)
	tc := dialSessionOpts(t, dropMount(fired), Options{MaxDropBytes: 10})
	m := tc.read()
	sid, seq := m["sid"].(string), seqOf(t, m)

	over := File{Name: "big", Data: []byte("0123456789a")}
	tc.dropStart(1, 1, over)
	tc.dropData(0, over.Data)
	tc.dropEnd()
	expectQuiet(t, fired)

	split := []File{{Name: "x", Data: []byte("123456")}, {Name: "y", Data: []byte("123456")}}
	tc.dropStart(1, 1, split...)
	tc.dropEnd()
	expectQuiet(t, fired)

	// a chunk past its declared size ends the drop
	tc.dropStart(1, 1, File{Name: "z", Data: []byte("abc")})
	tc.dropData(0, []byte("abcd"))
	tc.dropEnd()
	expectQuiet(t, fired)

	// data short of the declared size at end
	f := File{Name: "short", Data: []byte("abcdef")}
	tc.dropStart(1, 1, f)
	tc.dropData(0, f.Data[:3])
	tc.dropEnd()
	expectQuiet(t, fired)

	// a disconnect mid-drop discards it, the tail on the new socket is ignored
	tc.dropStart(1, 1, f)
	tc.dropData(0, f.Data[:3])
	tc.redial(sid, seq)
	tc.read()
	tc.dropData(0, f.Data[3:])
	tc.dropEnd()
	expectQuiet(t, fired)

	// and a fresh drop still works
	ok := File{Name: "ok", Data: []byte("fits")}
	tc.dropStart(2, 3, ok)
	tc.dropData(0, ok.Data)
	tc.dropEnd()
	expectFire(t, fired, "drop 2,3 ok||fits")
}

// the real client side over a real socket, since the chunking has to stay
// under the server's read limit and assemble back into the same bytes
func TestClientChunksReassembleOnTheServer(t *testing.T) {
	fired := make(chan string, 4)
	var got []File
	srv := httptest.NewServer(wsHandler(func(s *Session) *Node {
		s.OnFileDrop(func(files []File, x, y float64) { got = files; fired <- fmt.Sprint("drop ", x, y) })
		return Panel()
	}, Options{MaxDropBytes: 3 << 20}))
	t.Cleanup(srv.Close)

	c := proto.NewSession(ui.New(), proto.Config{URL: "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"})
	for deadline := time.Now().Add(3 * time.Second); c.FileDropCap() == 0; {
		if time.Now().After(deadline) {
			t.Fatal("mount never named the drop cap")
		}
		c.Pump()
		time.Sleep(5 * time.Millisecond)
	}
	if c.FileDropCap() != 3<<20 {
		t.Fatalf("cap = %d; want the server's", c.FileDropCap())
	}

	big := bytes.Repeat([]byte("0123456789abcdef"), (2*wire.MaxChunk+777)/16)
	c.NoteFileDrop([]proto.DropFile{
		{Name: "big.bin", Type: "application/octet-stream", Data: big},
		{Name: "empty"},
	}, 10, 20)
	expectFire(t, fired, "drop 10 20")
	if len(got) != 2 || got[0].Name != "big.bin" || got[0].Type != "application/octet-stream" ||
		!bytes.Equal(got[0].Data, big) || got[1].Name != "empty" || len(got[1].Data) != 0 {
		t.Fatalf("files = %d, first %q with %d bytes", len(got), got[0].Name, len(got[0].Data))
	}

	c.NoteFileDrop([]proto.DropFile{{Name: "over", Data: make([]byte, 3<<20+1)}}, 0, 0)
	expectQuiet(t, fired)
}
