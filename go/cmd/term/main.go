// caution term: the native terminal as a standalone binary.
//
//	go run ./cmd/term                                  # ws://localhost:8787/ws
//	go run ./cmd/term -url ws://localhost:8788/ws      # the Clojure demo
//	go run ./cmd/term -url unix:/tmp/caution.sock      # no TCP port
//	go run ./cmd/term -scene                           # built-in renderer scene
//
// Headless verification (offscreen FBO -> PNG, no window shown):
//
//	go run ./cmd/term -shot out.png -settle 2000 -clicks "120,424@700"
//
// GL dialect: desktop OpenGL 4.1 by default; `-tags angle` renders through
// ANGLE's Metal backend instead (OpenGL ES 3) - set CAUTION_ANGLE to a
// directory holding libEGL.dylib/libGLESv2.dylib, or let it borrow the
// newest Chrome install's copy:
//
//	go build -tags angle ./cmd/term
package main

import (
	"flag"
	"log"
	"os"
	"runtime/pprof"

	"github.com/nullentropy/caution/go/terminal"
)

func main() {
	url := flag.String("url", "ws://localhost:8787/ws", "caution server: ws://, wss://, or unix:/path/to.sock")
	sceneFlag := flag.Bool("scene", false, "render the built-in renderer scene instead of connecting")
	shot := flag.String("shot", "", "render offscreen to this PNG and exit (headless)")
	settle := flag.Float64("settle", 1500, "shot mode: ms to run the session before capturing")
	clicks := flag.String("clicks", "", `shot mode: synthetic clicks, "x,y@ms;x,y@ms"`)
	width := flag.Int("w", 1180, "window width, logical px")
	height := flag.Int("h", 780, "window height, logical px")
	fps := flag.Int("fps", 0, "cap animation repaints (0 = vsync rate; input always paints immediately)")
	fullscreen := flag.Bool("fullscreen", false, "own the primary display at its current mode instead of opening a window")
	title := flag.String("title", "", "window title (default \"caution\")")
	appID := flag.String("appid", "", "app id advertised as WM_CLASS, matching an installed .desktop entry's StartupWMClass (default: slug of the title)")
	customTitlebar := flag.Bool("customtitlebar", false, "hide the system window chrome; the app draws its own titlebar (mark it with WindowDrag)")
	titlebarStyle := flag.String("titlebarstyle", "", `traffic-light placement with -customtitlebar: "" (top-left inset), "compact" (centered, ~40pt strip), "tall" (centered, ~66pt strip)`)
	menubar := flag.Bool("menubar", false, "realize menus as the in-window bar widget (the default off macOS)")
	cpuprofile := flag.String("cpuprofile", "", "write a pprof CPU profile here (profiles the whole run; pair with -shot -settle N)")
	flag.Parse()

	// Stopping the profile is explicit rather than deferred: the log.Fatal
	// below exits without running defers, and a profile that is never
	// stopped is written as a zero-byte file. (A panic during teardown
	// would do the same; terminal.wakeGate prevents that one.)
	stopProfile := func() {}
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal(err)
		}
		stopProfile = func() {
			pprof.StopCPUProfile()
			if err := f.Close(); err != nil {
				log.Println("cpuprofile:", err)
			}
		}
	}

	o := terminal.Options{
		Title: *title,
		URL:   *url, W: *width, H: *height, MaxFPS: *fps,
		Shot: *shot, SettleMs: *settle, Clicks: *clicks,
		InWindowMenu: *menubar, Fullscreen: *fullscreen,
		AppID: *appID, CustomTitlebar: *customTitlebar, TitlebarStyle: *titlebarStyle,
	}
	if *sceneFlag {
		o.Scene = scene
	}
	err := terminal.Run(o)
	stopProfile()
	if err != nil {
		log.Fatal(err)
	}
}
