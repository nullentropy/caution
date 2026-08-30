package main

import (
	"fmt"
	"net"
	"os"
	"time"

	caution "github.com/nullentropy/caution/go"
	"github.com/nullentropy/caution/go/terminal"
)

// The presentation gate on a real display. The headless probes prove the
// renderer reports Painted honestly; this one proves the shell acts on it, on a
// window the compositor is really presenting, with vsync and a display refresh
// in the loop.
//
// Four scripted phases, each measured with the frame counters reset:
//
//	idle       a static tree            presents nothing
//	patch      one label at 10Hz        presents about once per patch
//	animate    a visible shader pane     presents at the MaxFPS cap
//	offscreen  the same pane moved off   presents nothing again
//
// Leave the pointer off the window while it runs. Input wakes the loop, which
// is fine (a wake that changes no pixel still presents nothing) but hovering
// something interactive is a real change and counts.

type presentPhase struct {
	name string
	// want is the range of presented frames the phase has to land in.
	lo, hi int
	dur    time.Duration
	// drive runs on the session goroutine at the start of the phase, and tick,
	// when set, runs every 100ms through it.
	drive func(sc *presentScene)
	tick  func(sc *presentScene, i int)
}

// presentScene is the mounted tree plus the two nodes the script drives.
type presentScene struct {
	s           *caution.Session
	clock, pane *caution.Node
}

const presentFPS = 30

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
		pane := caution.Shader(caution.FX{
			Frag:    "vec4 effect(vec2 uv) { return vec4(uv.x, uv.y, fract(u_time), 1.0); }",
			Animate: true,
		}).Frame(24, 92, 320, 200)
		root := caution.Panel().Kids(
			caution.Label("caution presentation gate").
				Anchor(caution.A{Left: caution.Px(24), Top: caution.Px(24)}),
			clock, pane,
		)
		select {
		case scenes <- &presentScene{s: s, clock: clock, pane: pane}:
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
			name: "offscreen", lo: 0, hi: 3, dur: dur,
			drive: func(sc *presentScene) { paneOffscreen(sc, true) },
		},
	}
	// One patch per 100ms, and a presented frame each: the loop must follow the
	// server, not the display.
	ticks := int(dur / (100 * time.Millisecond))
	phases[1].lo, phases[1].hi = ticks*3/4, ticks*2
	// The cap paces animation. A display slower than the cap presents fewer, so
	// the floor is generous and the ceiling is what actually gates.
	capped := int(dur.Seconds() * presentFPS)
	phases[2].lo, phases[2].hi = capped/2, capped*5/4

	go func() {
		sc := <-scenes
		time.Sleep(700 * time.Millisecond) // let the mount settle
		fmt.Printf("%-10s %8s %8s   %s\n", "phase", "steps", "present", "want")
		bad := 0
		for _, p := range phases {
			if p.drive != nil {
				done := make(chan struct{})
				sc.s.Update(func() { p.drive(sc); close(done) })
				<-done
				time.Sleep(250 * time.Millisecond) // the op has to land first
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
			if st.Presented < p.lo || st.Presented > p.hi {
				verdict = "FAIL"
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

// paneOffscreen parks the animated pane below the viewport or brings it back.
// Off the viewport its damage falls outside the frame, so the renderer reports
// the animation invisible and the loop skips the frame outright.
func paneOffscreen(sc *presentScene, off bool) {
	y := float64(92)
	if off {
		y = 5000
	}
	sc.pane.Frame(24, y, 320, 200)
}
