package proto

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/nullentropy/caution/go/terminal/ui"
)

func TestSoundsOpInstallsTableAndDecodesEverySource(t *testing.T) {
	u := ui.New()
	var preloaded []string
	s := &Session{ui: u, preloadSound: func(src string) { preloaded = append(preloaded, src) }}
	s.applyOp(&patchOp{Op: "sounds", Tokens: map[string]string{"press": "/a.wav", "toggle": "/b.wav"}})
	if u.Sounds["press"] != "/a.wav" || u.Sounds["toggle"] != "/b.wav" {
		t.Fatalf("table = %v", u.Sounds)
	}
	if len(preloaded) != 2 {
		t.Fatalf("preloaded %v; want both sources", preloaded)
	}

	quiet := &Session{ui: ui.New()}
	quiet.applyOp(&patchOp{Op: "sounds", Tokens: map[string]string{"press": "/a.wav"}})
	if quiet.ui.Sounds["press"] != "/a.wav" {
		t.Fatal("table not installed without an audio hook")
	}
}

func TestPlayStopAndResourceOpsReachTheAudioHooks(t *testing.T) {
	var log, preloaded []string
	s := &Session{
		ui:           ui.New(),
		play:         func(src string, loop bool) { log = append(log, fmt.Sprintf("play %s loop=%v", src, loop)) },
		stop:         func(src string) { log = append(log, "stop "+src) },
		preloadSound: func(src string) { preloaded = append(preloaded, src) },
	}
	s.applyOp(&patchOp{Op: "play", Src: "/ding.wav"})
	s.applyOp(&patchOp{Op: "play", Src: "/alarm.wav", Loop: true})
	s.applyOp(&patchOp{Op: "stop", Src: "/alarm.wav"})
	s.applyOp(&patchOp{Op: "stop"})
	s.applyOp(&patchOp{Op: "resource", Sounds: []string{"/c.wav", "/d.wav"}})
	want := []string{"play /ding.wav loop=false", "play /alarm.wav loop=true", "stop /alarm.wav", "stop "}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("hooks saw %v; want %v", log, want)
	}
	if len(preloaded) != 2 || preloaded[1] != "/d.wav" {
		t.Fatalf("preloaded %v", preloaded)
	}
}

func TestMountStopsEverythingThenStartsItsLoops(t *testing.T) {
	var log []string
	s := &Session{
		ui:   ui.New(),
		play: func(src string, loop bool) { log = append(log, fmt.Sprintf("play %s loop=%v", src, loop)) },
		stop: func(src string) { log = append(log, "stop "+src) },
	}
	s.store = NewNodeStore(s)
	s.handle(&serverMsg{T: "mount", Loops: []string{"/alarm.wav", "/hum.wav"}})
	want := []string{"stop ", "play /alarm.wav loop=true", "play /hum.wav loop=true"}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("mount drove %v; want %v", log, want)
	}
}
