package render

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"sync/atomic"
	"testing"

	"github.com/nullentropy/caution/terminal/gpu/gputest"
)

func TestPreloadWhileUploadsDrain(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	s := newImageStore(gputest.New())
	s.Fetch = func(string) ([]byte, error) { return buf.Bytes(), nil }
	var preloading atomic.Bool
	preloading.Store(true)
	go func() {
		for i := 0; i < 2000; i++ {
			s.Preload(fmt.Sprintf("img%d", i))
		}
		preloading.Store(false)
	}()
	for preloading.Load() {
		s.drainUploads()
	}
}
