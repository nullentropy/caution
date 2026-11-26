package terminal

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func saveToDownloads(name string, data []byte) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("caution: save %s: %v", name, err)
		return
	}
	dir := filepath.Join(home, "Downloads")
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if base == "." || base == "/" || base == "" {
		base = "download"
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	path := filepath.Join(dir, base)
	for i := 1; ; i++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
			continue
		}
		if err != nil {
			log.Printf("caution: save %s: %v", name, err)
			return
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			log.Printf("caution: save %s: %v", name, err)
		}
		return
	}
}
