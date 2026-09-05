package proto

import (
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

	// shot mode has no audio hooks and the table still installs
	quiet := &Session{ui: ui.New()}
	quiet.applyOp(&patchOp{Op: "sounds", Tokens: map[string]string{"press": "/a.wav"}})
	if quiet.ui.Sounds["press"] != "/a.wav" {
		t.Fatal("table not installed without an audio hook")
	}
}

func TestPlayAndResourceOpsReachTheAudioHooks(t *testing.T) {
	var played, preloaded []string
	s := &Session{
		ui:           ui.New(),
		play:         func(src string) { played = append(played, src) },
		preloadSound: func(src string) { preloaded = append(preloaded, src) },
	}
	s.applyOp(&patchOp{Op: "play", Src: "/ding.wav"})
	s.applyOp(&patchOp{Op: "resource", Sounds: []string{"/c.wav", "/d.wav"}})
	if len(played) != 1 || played[0] != "/ding.wav" {
		t.Fatalf("played %v", played)
	}
	if len(preloaded) != 2 || preloaded[1] != "/d.wav" {
		t.Fatalf("preloaded %v", preloaded)
	}
}
