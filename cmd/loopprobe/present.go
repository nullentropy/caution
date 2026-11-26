package main

import (
	"fmt"
	"net"
	"os"
	"slices"
	"time"

	caution "github.com/nullentropy/caution"
	"github.com/nullentropy/caution/terminal"
)

type presentPhase struct {
	name    string
	lo, hi  int
	stepsHi int
	dur     time.Duration
	drive   func(sc *presentScene)
	tick    func(sc *presentScene, i int)
	visible *bool
}

type presentScene struct {
	s                  *caution.Session
	clock, pane, field *caution.Node
}

const presentFPS = 30

var yes, no = true, false

func runPresent(fullscreen bool, seconds float64) error {
	dur := time.Duration(seconds * float64(time.Second))
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := l.Addr().(*net.TCPAddr).Port

	scenes := make(chan *presentScene, 1)
	mount := func(s *caution.Session) *caution.Node {
		s.SetTheme(map[string]string{"bg": "#101216"})
		clock := caution.Label("ticking: 0").
			Anchor(caution.A{Left: caution.Px(24), Top: caution.Px(52)})
		field := caution.TextField("").Frame(380, 52, 200, 28)
		pane := caution.Shader(caution.FX{
			Frag:    "vec4 effect(vec2 uv) { return vec4(uv.x, uv.y, fract(u_time), 1.0); }",
			Animate: true,
		}).Frame(24, 92, 320, 200)
		root := caution.Panel().Kids(
			caution.Label("caution presentation gate").
				Anchor(caution.A{Left: caution.Px(24), Top: caution.Px(24)}),
			clock, pane, field,
		)
		select {
		case scenes <- &presentScene{s: s, clock: clock, pane: pane, field: field}:
		default:
		}
		return root
	}
	go func() {
		if err := caution.ServeListener(l, mount, caution.Options{}); err != nil {
			fmt.Fprintln(os.Stderr, "loopprobe: server:", err)
		}
	}()

	phases := []presentPhase{
		{
			name: "idle", lo: 0, hi: 2, dur: dur,
			drive: func(sc *presentScene) { paneOffscreen(sc, true) },
		},
		{
			name: "patch", lo: 0, hi: 0, dur: dur,
			tick: func(sc *presentScene, i int) {
				sc.clock.SetText(fmt.Sprintf("ticking: %d", i))
			},
		},
		{
			name: "animate", lo: 0, hi: 0, dur: dur,
			drive: func(sc *presentScene) { paneOffscreen(sc, false) },
		},
		{
			name: "hidden", lo: 0, hi: 3, dur: dur, visible: &no,
			drive: func(*presentScene) { setAppHidden(true) },
			tick: func(sc *presentScene, i int) {
				sc.clock.SetText(fmt.Sprintf("hidden: %d", i))
			},
		},
		{
			name: "shown", lo: 0, hi: 0, dur: dur, visible: &yes,
			drive: func(*presentScene) { setAppHidden(false) },
		},
		{
			name: "offscreen", lo: 0, hi: 3, dur: dur,
			drive: func(sc *presentScene) { paneOffscreen(sc, true) },
		},
		{
			name: "caret", dur: dur,
			drive: func(sc *presentScene) { sc.field.Focus() },
		},
	}
	if !hideSupported {
		phases = slices.DeleteFunc(phases, func(p presentPhase) bool { return p.name == "hidden" || p.name == "shown" })
	}
	phase := func(name string) *presentPhase {
		return &phases[slices.IndexFunc(phases, func(p presentPhase) bool { return p.name == name })]
	}
	ticks := int(dur / (100 * time.Millisecond))
	phase("patch").lo, phase("patch").hi = ticks*3/4, ticks*2
	capped := int(dur.Seconds() * presentFPS)
	for _, name := range []string{"animate", "shown"} {
		if i := slices.IndexFunc(phases, func(p presentPhase) bool { return p.name == name }); i >= 0 {
			phases[i].lo, phases[i].hi = capped/2, capped*5/4
		}
	}
	blinks := int(dur / (530 * time.Millisecond))
	caret := phase("caret")
	caret.lo, caret.hi, caret.stepsHi = blinks/2, blinks+2, blinks*3+6

	go func() {
		sc := <-scenes
		time.Sleep(700 * time.Millisecond)
		fmt.Printf("%-10s %8s %8s   %s\n", "phase", "steps", "present", "want")
		bad := 0
		for _, p := range phases {
			if p.drive != nil {
				done := make(chan struct{})
				sc.s.Update(func() { p.drive(sc); close(done) })
				<-done
				time.Sleep(250 * time.Millisecond)
			}
			terminal.ResetStats()
			deadline := time.Now().Add(p.dur)
			for i := 0; time.Now().Before(deadline); i++ {
				if p.tick != nil {
					sc.s.Update(func() { p.tick(sc, i) })
				}
				time.Sleep(100 * time.Millisecond)
			}
			st := terminal.Stats()
			verdict := "ok"
			if st.Presented < p.lo || st.Presented > p.hi || (p.stepsHi > 0 && st.Steps > p.stepsHi) {
				verdict = "FAIL"
			}
			if p.visible != nil {
				seen := make(chan bool)
				sc.s.Update(func() { seen <- sc.s.Visible() })
				if v := <-seen; v != *p.visible {
					verdict = fmt.Sprintf("FAIL (session visible %v)", v)
				}
			}
			if verdict != "ok" {
				bad++
			}
			fmt.Printf("%-10s %8d %8d   %d..%d %s\n", p.name, st.Steps, st.Presented, p.lo, p.hi, verdict)
		}
		if bad > 0 {
			fmt.Fprintf(os.Stderr, "loopprobe: %d phase(s) outside their expected presentation counts\n", bad)
			os.Exit(1)
		}
		fmt.Println("present: ok - the loop presents on change and on animation, and stays quiet otherwise")
		os.Exit(0)
	}()

	return terminal.Run(terminal.Options{
		URL:        fmt.Sprintf("ws://127.0.0.1:%d/ws", port),
		Title:      "caution presentation gate",
		W:          900,
		H:          600,
		MaxFPS:     presentFPS,
		Fullscreen: fullscreen,
	})
}

func paneOffscreen(sc *presentScene, off bool) {
	y := float64(92)
	if off {
		y = 5000
	}
	sc.pane.Frame(24, y, 320, 200)
}
