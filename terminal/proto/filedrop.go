package proto

import (
	"encoding/base64"
	"log"

	"github.com/nullentropy/caution/wire"
)

type DropFile struct {
	Name string
	Type string
	Data []byte
}

// 0 until mounted
func (s *Session) FileDropCap() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fileCap
}

func (s *Session) NoteFileDrag(over bool) { s.Event(0, wire.EvFileDrag, over) }

// any goroutine. abandoned if over the cap or the socket goes
func (s *Session) NoteFileDrop(files []DropFile, x, y float64) {
	s.dropMu.Lock()
	defer s.dropMu.Unlock()
	var total int64
	specs := make([]map[string]any, len(files))
	for i, f := range files {
		total += int64(len(f.Data))
		specs[i] = map[string]any{"name": f.Name, "type": f.Type, "size": len(f.Data)}
	}
	if c := s.FileDropCap(); total > c {
		log.Printf("caution: dropped files total %d bytes, over the server's %d byte cap, skipped", total, c)
		return
	}
	send := func(v map[string]any) bool { return s.send(0, wire.EvFileDrop, v) }
	if !send(map[string]any{"p": wire.DropStart, "x": x, "y": y, "files": specs}) {
		return
	}
	for i, f := range files {
		for data := f.Data; len(data) > 0; {
			n := min(len(data), wire.MaxChunk)
			b := base64.StdEncoding.EncodeToString(data[:n])
			if !send(map[string]any{"p": wire.DropData, "f": i, "b": b}) {
				return
			}
			data = data[n:]
		}
	}
	send(map[string]any{"p": wire.DropEnd})
}
