package audio

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"sync"
	"testing"
	"time"
)

// wav builds a RIFF/WAVE file around raw sample bytes. extensible wraps the
// format in a WAVE_FORMAT_EXTENSIBLE header the way pro tools export.
func wav(format, channels, rate, bits int, pcm []byte, extensible bool) []byte {
	var fmtChunk bytes.Buffer
	tag := format
	if extensible {
		tag = wavExtensible
	}
	binary.Write(&fmtChunk, binary.LittleEndian, uint16(tag))
	binary.Write(&fmtChunk, binary.LittleEndian, uint16(channels))
	binary.Write(&fmtChunk, binary.LittleEndian, uint32(rate))
	binary.Write(&fmtChunk, binary.LittleEndian, uint32(rate*channels*bits/8))
	binary.Write(&fmtChunk, binary.LittleEndian, uint16(channels*bits/8))
	binary.Write(&fmtChunk, binary.LittleEndian, uint16(bits))
	if extensible {
		binary.Write(&fmtChunk, binary.LittleEndian, uint16(22)) // cbSize
		binary.Write(&fmtChunk, binary.LittleEndian, uint16(bits))
		binary.Write(&fmtChunk, binary.LittleEndian, uint32(0)) // channel mask
		binary.Write(&fmtChunk, binary.LittleEndian, uint16(format))
		fmtChunk.Write(make([]byte, 14)) // rest of the GUID
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.Write(&out, binary.LittleEndian, uint32(0)) // size, unused by the decoder
	out.WriteString("WAVE")
	// A LIST chunk before fmt, with an odd size, checks chunk walking and the
	// pad byte.
	out.WriteString("LIST")
	binary.Write(&out, binary.LittleEndian, uint32(3))
	out.Write([]byte{1, 2, 3, 0})
	out.WriteString("fmt ")
	binary.Write(&out, binary.LittleEndian, uint32(fmtChunk.Len()))
	out.Write(fmtChunk.Bytes())
	out.WriteString("data")
	binary.Write(&out, binary.LittleEndian, uint32(len(pcm)))
	out.Write(pcm)
	return out.Bytes()
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func TestDecodeEveryEncoding(t *testing.T) {
	var pcm16 bytes.Buffer
	for _, v := range []int16{0, 16384, -32768, 32767} {
		binary.Write(&pcm16, binary.LittleEndian, v)
	}
	var pcm24 bytes.Buffer
	for _, v := range []int32{0, 4194304, -8388608} {
		pcm24.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16)})
	}
	var pcm32 bytes.Buffer
	for _, v := range []int32{0, 1 << 30, math.MinInt32} {
		binary.Write(&pcm32, binary.LittleEndian, v)
	}
	var f32 bytes.Buffer
	for _, v := range []float32{0, 0.25, -1} {
		binary.Write(&f32, binary.LittleEndian, v)
	}
	var f64 bytes.Buffer
	for _, v := range []float64{0, 0.5, -0.75} {
		binary.Write(&f64, binary.LittleEndian, v)
	}
	cases := []struct {
		name       string
		format     int
		bits       int
		pcm        []byte
		extensible bool
		want       []float32
	}{
		{"pcm8", wavPCM, 8, []byte{128, 255, 0, 192}, false, []float32{0, 127.0 / 128, -1, 0.5}},
		{"pcm16", wavPCM, 16, pcm16.Bytes(), false, []float32{0, 0.5, -1, 32767.0 / 32768}},
		{"pcm24", wavPCM, 24, pcm24.Bytes(), false, []float32{0, 0.5, -1}},
		{"pcm32", wavPCM, 32, pcm32.Bytes(), false, []float32{0, 0.5, -1}},
		{"float32", wavFloat, 32, f32.Bytes(), false, []float32{0, 0.25, -1}},
		{"float64", wavFloat, 64, f64.Bytes(), false, []float32{0, 0.5, -0.75}},
		{"extensible pcm16", wavPCM, 16, pcm16.Bytes(), true, []float32{0, 0.5, -1, 32767.0 / 32768}},
	}
	for _, c := range cases {
		clip, err := Decode(wav(c.format, 1, 22050, c.bits, c.pcm, c.extensible))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if clip.Rate != 22050 || clip.Channels != 1 {
			t.Fatalf("%s: rate/channels = %d/%d", c.name, clip.Rate, clip.Channels)
		}
		if len(clip.Samples) != len(c.want) {
			t.Fatalf("%s: %d samples, want %d", c.name, len(clip.Samples), len(c.want))
		}
		for i := range c.want {
			if !near(clip.Samples[i], c.want[i]) {
				t.Fatalf("%s: sample %d = %v, want %v", c.name, i, clip.Samples[i], c.want[i])
			}
		}
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	for _, bad := range [][]byte{nil, []byte("RIFF"), []byte("not a wav file at all, just text"),
		wav(99, 1, 44100, 16, []byte{0, 0}, false)} {
		if _, err := Decode(bad); err == nil {
			t.Fatalf("decoded %q", bad)
		}
	}
}

func TestConvertSpreadsMonoAndResamples(t *testing.T) {
	// a constant mono signal at 44.1k
	mono := &Clip{Rate: 44100, Channels: 1, Samples: make([]float32, 441)}
	for i := range mono.Samples {
		mono.Samples[i] = 0.5
	}
	out := mono.Convert(48000, 2)
	if got := len(out) / 2; got != 480 {
		t.Fatalf("resampled to %d frames, want 480", got)
	}
	for i, v := range out {
		if !near(v, 0.5) {
			t.Fatalf("sample %d = %v after resampling a constant", i, v)
		}
	}
	// stereo stays stereo at the same rate, byte for byte
	stereo := &Clip{Rate: 48000, Channels: 2, Samples: []float32{1, -1, 0.5, -0.5}}
	if got := stereo.Convert(48000, 2); len(got) != 4 || got[0] != 1 || got[1] != -1 || got[3] != -0.5 {
		t.Fatalf("stereo passthrough = %v", got)
	}
	// six channels keep the first two
	surround := &Clip{Rate: 48000, Channels: 6, Samples: []float32{1, 2, 3, 4, 5, 6}}
	if got := surround.Convert(48000, 2); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("downmix = %v, want [1 2]", got)
	}
}

type fakeSink struct {
	mu    sync.Mutex
	plays chan int
	drain []int
	ended chan int
}

func newFakeSink() *fakeSink {
	return &fakeSink{plays: make(chan int, 16), ended: make(chan int, 16)}
}

func (f *fakeSink) Play(r io.Reader) {
	f.mu.Lock()
	id := len(f.drain)
	f.drain = append(f.drain, 0)
	f.mu.Unlock()
	f.plays <- id
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			f.mu.Lock()
			f.drain[id] += n
			f.mu.Unlock()
			if err != nil {
				f.ended <- id
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
}

func (f *fakeSink) drained(id int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.drain[id]
}

func (f *fakeSink) expect(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-f.plays:
		case <-time.After(2 * time.Second):
			t.Fatalf("play %d of %d never happened", i+1, n)
		}
	}
	select {
	case <-f.plays:
		t.Fatalf("played more than %d times", n)
	case <-time.After(50 * time.Millisecond):
	}
}

func (f *fakeSink) waitEnded(t *testing.T, id int) {
	t.Helper()
	for {
		select {
		case got := <-f.ended:
			if got == id {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("voice %d never reached EOF", id)
		}
	}
}

func tone() []byte {
	var pcm bytes.Buffer
	for i := 0; i < 480; i++ {
		binary.Write(&pcm, binary.LittleEndian, int16(math.Sin(float64(i)/10)*16000))
	}
	return wav(wavPCM, 1, 48000, 16, pcm.Bytes(), false)
}

func TestStorePlaysAfterFetchAndFromCache(t *testing.T) {
	sink := newFakeSink()
	fetched := make(chan string, 8)
	st := &Store{
		Fetch:    func(src string) ([]byte, error) { fetched <- src; return tone(), nil },
		OpenSink: func() (Sink, error) { return sink, nil },
	}
	st.Play("/a.wav")
	sink.expect(t, 1)
	st.Play("/a.wav")
	sink.expect(t, 1)
	if len(fetched) != 1 {
		t.Fatalf("fetched %d times for one src", len(fetched))
	}
}

func TestStoreCollapsesPlaysQueuedBehindOneFetch(t *testing.T) {
	sink := newFakeSink()
	release := make(chan struct{})
	st := &Store{
		Fetch:    func(string) ([]byte, error) { <-release; return tone(), nil },
		OpenSink: func() (Sink, error) { return sink, nil },
	}
	st.Play("/slow.wav")
	st.Play("/slow.wav")
	st.Play("/slow.wav")
	close(release)
	sink.expect(t, 1)
}

func TestStorePreloadDoesNotPlayAndDoesNotOpenTheDevice(t *testing.T) {
	sink := newFakeSink()
	opened := false
	done := make(chan struct{})
	st := &Store{
		Fetch:    func(string) ([]byte, error) { defer close(done); return tone(), nil },
		OpenSink: func() (Sink, error) { opened = true; return sink, nil },
	}
	st.Preload("/warm.wav")
	<-done
	sink.expect(t, 0)
	if opened {
		t.Fatal("preloading opened the audio device")
	}
	st.Play("/warm.wav")
	sink.expect(t, 1)
}

func TestStoreSurvivesBadSourcesAndNoDevice(t *testing.T) {
	sink := newFakeSink()
	st := &Store{
		Fetch: func(src string) ([]byte, error) {
			if src == "/missing.wav" {
				return nil, errors.New("404")
			}
			return []byte("this is not audio"), nil
		},
		OpenSink: func() (Sink, error) { return sink, nil },
	}
	st.Play("/missing.wav")
	st.Play("/garbage.wav")
	sink.expect(t, 0)

	deaf := &Store{
		Fetch:    func(string) ([]byte, error) { return tone(), nil },
		OpenSink: func() (Sink, error) { return nil, errors.New("no output device") },
	}
	deaf.Play("/a.wav")
	deaf.Play("/a.wav")
	time.Sleep(50 * time.Millisecond) // nothing to observe but a crash
}

func TestDataURISources(t *testing.T) {
	sink := newFakeSink()
	st := &Store{OpenSink: func() (Sink, error) { return sink, nil }}
	st.Play("data:audio/wav;base64," + b64(tone()))
	sink.expect(t, 1)
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// TestOtoSinkDrains plays through the real output device, so it runs only when
// asked: CAUTION_AUDIO_DEVICE=1 go test ./terminal/audio -run Oto
func TestOtoSinkDrains(t *testing.T) {
	if os.Getenv("CAUTION_AUDIO_DEVICE") == "" {
		t.Skip("set CAUTION_AUDIO_DEVICE=1 to play through the real output device")
	}
	sink, err := openOto()
	if err != nil {
		t.Fatal(err)
	}
	clip, err := Decode(tone())
	if err != nil {
		t.Fatal(err)
	}
	samples := clip.Convert(Rate, Channels)
	sink.Play(&voice{samples: samples, fadeLeft: -1, done: func() {}})

	// and the same bytes through a player we can watch drain
	buf := make([]byte, len(samples)*4)
	for i, f := range samples {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	p := sink.(*otoSink).ctx.NewPlayer(bytes.NewReader(buf))
	p.Play()
	deadline := time.Now().Add(3 * time.Second)
	for p.IsPlaying() {
		if time.Now().After(deadline) {
			t.Fatal("the device never drained a 10ms clip")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := p.Err(); err != nil {
		t.Fatal(err)
	}
}

const onePass = 480 * Channels * 4

func TestOneShotEndsAndIsReaped(t *testing.T) {
	sink := newFakeSink()
	st := &Store{
		Fetch:    func(string) ([]byte, error) { return tone(), nil },
		OpenSink: func() (Sink, error) { return sink, nil },
	}
	st.Play("/a.wav")
	sink.expect(t, 1)
	sink.waitEnded(t, 0)
	if got := sink.drained(0); got != onePass {
		t.Fatalf("drained %d bytes, want one pass of %d", got, onePass)
	}
	st.mu.Lock()
	left := len(st.voices["/a.wav"])
	st.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d voices still tracked after the clip ended", left)
	}
}

func TestLoopRepeatsUntilStoppedAndFadesOut(t *testing.T) {
	sink := newFakeSink()
	st := &Store{
		Fetch:    func(string) ([]byte, error) { return tone(), nil },
		OpenSink: func() (Sink, error) { return sink, nil },
	}
	st.Loop("/a.wav")
	sink.expect(t, 1)
	time.Sleep(40 * time.Millisecond)
	before := sink.drained(0)
	if before < 3*onePass {
		t.Fatalf("drained only %d bytes after 40ms: not looping", before)
	}
	st.Stop("/a.wav")
	sink.waitEnded(t, 0)
	if extra := sink.drained(0) - before; extra > fadeSamples*4+4*4096 {
		t.Fatalf("%d bytes after Stop, want no more than the fade", extra)
	}

	st.Loop("/a.wav")
	sink.expect(t, 1)
	st.StopAll()
	sink.waitEnded(t, 1)
}

func TestLoopIsIdempotentWhilePlaying(t *testing.T) {
	sink := newFakeSink()
	st := &Store{
		Fetch:    func(string) ([]byte, error) { return tone(), nil },
		OpenSink: func() (Sink, error) { return sink, nil },
	}
	st.Loop("/a.wav")
	sink.expect(t, 1)
	st.Loop("/a.wav")
	st.Loop("/a.wav")
	sink.expect(t, 0)
	st.StopAll()
	sink.waitEnded(t, 0)
}

func TestStopBeforeDecodeCancelsThePlay(t *testing.T) {
	sink := newFakeSink()
	release := make(chan struct{})
	st := &Store{
		Fetch:    func(string) ([]byte, error) { <-release; return tone(), nil },
		OpenSink: func() (Sink, error) { return sink, nil },
	}
	st.Play("/slow.wav")
	st.Loop("/slow.wav")
	st.Stop("/slow.wav")
	close(release)
	sink.expect(t, 0)
}
