package terminal

import (
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/nullentropy/caution/terminal/proto"
)

// off the main thread, a big file would stall the window
func sendDroppedFiles(sess *proto.Session, paths []string, x, y float64) {
	go func() {
		limit := sess.FileDropCap()
		var files []proto.DropFile
		var total int64
		for _, p := range paths {
			st, err := os.Stat(p)
			if err != nil || !st.Mode().IsRegular() {
				log.Printf("caution: dropped %s skipped, not a readable file", p)
				continue
			}
			// check before reading so an over-cap drop costs no memory
			if total += st.Size(); total > limit {
				log.Printf("caution: dropped files exceed the server's %d byte cap, skipped", limit)
				return
			}
			data, err := os.ReadFile(p)
			if err != nil {
				log.Printf("caution: dropped %s skipped: %v", p, err)
				continue
			}
			files = append(files, proto.DropFile{Name: filepath.Base(p), Type: mediaType(p, data), Data: data})
		}
		if len(files) > 0 {
			sess.NoteFileDrop(files, x, y)
		}
	}()
}

func mediaType(path string, data []byte) string {
	t := mime.TypeByExtension(filepath.Ext(path))
	if t == "" {
		t = http.DetectContentType(data)
	}
	if base, _, err := mime.ParseMediaType(t); err == nil {
		return base
	}
	return t
}
