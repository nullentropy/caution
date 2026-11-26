package proto

import (
	"encoding/base64"
	"fmt"
	"log"

	"github.com/nullentropy/caution/wire"
)

type DropFile struct {
	Name string
	Type string
	Data []byte
}

func (s *Session) FileDropCap() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fileCap
}

func (s *Session) NoteFileDrag(over bool) { s.Event(0, wire.EvFileDrag, over) }

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

func (s *Session) PasteImage(data []byte, x, y float64) {
	if c := s.FileDropCap(); int64(len(data)) > c {
		s.notice(fmt.Sprintf("image not pasted: %s is over the %s upload limit", megabytes(int64(len(data))), megabytes(c)))
		return
	}
	go s.NoteFileDrop([]DropFile{{Name: "image.png", Type: "image/png", Data: data}}, x, y)
}

func megabytes(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }
