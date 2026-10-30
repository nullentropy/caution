package caution

import (
	"encoding/base64"

	"github.com/nullentropy/caution/wire"

	"github.com/rs/zerolog/log"
)

type File struct {
	Name string
	// empty when the client could not tell
	Type string
	Data []byte
}

const defaultMaxDropBytes = 32 << 20

// fires once every byte is in, x, y in logical px. an over-cap drop or one cut
// by a disconnect never fires
func (s *Session) OnFileDrop(fn func(files []File, x, y float64)) { s.onFileDrop = fn }

// a drop reports false first. natively macOS only, glfw has no enter or leave
// elsewhere
func (s *Session) OnFileDrag(fn func(over bool)) { s.onFileDrag = fn }

type dropState int

const (
	dropIdle dropState = iota
	dropReceiving
)

// any protocol violation resets to idle and the rest of that drop is ignored
type dropAssembly struct {
	state dropState
	x, y  float64
	files []File
	sizes []int64
}

func (d *dropAssembly) reset() { *d = dropAssembly{} }

type dropFileSpec struct {
	Name string  `json:"name"`
	Type string  `json:"type"`
	Size float64 `json:"size"`
}

// returns the finished files on end, nil otherwise
func (d *dropAssembly) apply(v map[string]any, maxBytes int64) ([]File, float64, float64) {
	phase, _ := v["p"].(string)
	switch wire.DropPhase(phase) {
	case wire.DropStart:
		d.reset()
		px, _ := v["x"].(float64)
		py, _ := v["y"].(float64)
		raw, _ := v["files"].([]any)
		if len(raw) == 0 || len(raw) > wire.MaxDropFiles {
			log.Debug().Msgf("caution: drop of %d files refused", len(raw))
			return nil, 0, 0
		}
		var total int64
		fs := make([]File, len(raw))
		sizes := make([]int64, len(raw))
		for i, r := range raw {
			m, _ := r.(map[string]any)
			name, _ := m["name"].(string)
			typ, _ := m["type"].(string)
			size, _ := m["size"].(float64)
			if size < 0 || size > float64(maxBytes) {
				log.Debug().Msgf("caution: drop file %q size %v refused", name, size)
				return nil, 0, 0
			}
			total += int64(size)
			if total > maxBytes {
				log.Debug().Msgf("caution: drop over the %d byte cap refused", maxBytes)
				return nil, 0, 0
			}
			sizes[i] = int64(size)
			fs[i] = File{Name: name, Type: typ, Data: make([]byte, 0, int(size))}
		}
		*d = dropAssembly{state: dropReceiving, x: px, y: py, files: fs, sizes: sizes}
	case wire.DropData:
		if d.state != dropReceiving {
			return nil, 0, 0
		}
		f, _ := v["f"].(float64)
		b, _ := v["b"].(string)
		i := int(f)
		if i < 0 || i >= len(d.files) {
			d.reset()
			return nil, 0, 0
		}
		buf, err := base64.StdEncoding.DecodeString(b)
		if err != nil || int64(len(d.files[i].Data)+len(buf)) > d.sizes[i] {
			log.Debug().Msgf("caution: drop chunk refused: %v", err)
			d.reset()
			return nil, 0, 0
		}
		d.files[i].Data = append(d.files[i].Data, buf...)
	case wire.DropEnd:
		if d.state != dropReceiving {
			return nil, 0, 0
		}
		done, dx, dy := d.files, d.x, d.y
		for i, f := range done {
			if int64(len(f.Data)) != d.sizes[i] {
				log.Debug().Msgf("caution: drop file %q short, discarded", f.Name)
				d.reset()
				return nil, 0, 0
			}
		}
		d.reset()
		return done, dx, dy
	}
	return nil, 0, 0
}
