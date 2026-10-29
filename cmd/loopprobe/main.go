// loopprobe: behavioral gates for the presentation loop
//
// a widget inserted by a server op fades in over 200ms while the only other
// animation is an animate:true composite. if the continuation gate stops
// checking geometry animation in flight, the fade freezes and nothing notices
//
// two scenes, both checked on pixels sampled while the loop runs free:
//
//	A: a plain solid panel inserted beside an animate:true shader pane
//	B: the overlay shape. an inserted panel that itself carries an
//	   animate:true effect layer (CRT-style) with an animated shader inside
//
// native copies the shell's loop shape (dirty -> full, else partial or full)
// -browser checks the damage timeline and final screenshot pixels
//
//	go run ./cmd/loopprobe            # native, offscreen
//	go run ./cmd/loopprobe -browser   # embedded client in headless Chrome
//	go run ./cmd/loopprobe -present   # a real window, counting presented frames
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
)

func init() { runtime.LockOSThread() }

func main() {
	browser := flag.Bool("browser", false, "probe the browser terminal (headless Chrome) instead of the native renderer")
	leaks := flag.Bool("leaks", false, "probe renderer resource bounds instead: resize storms and shader-mint storms must not grow the pools")
	menu := flag.Bool("menu", false, "probe modal-loop pacing instead: animation frames must flow while an NSMenu tracking session parks the main thread (darwin)")
	present := flag.Bool("present", false, "probe the presentation gate on a real display: a window runs scripted phases and the frames it presents are counted")
	fullscreen := flag.Bool("fullscreen", true, "-present: own the display, the shape the gate is written for. false opens a window instead")
	phase := flag.Float64("phase", 2, "-present: seconds to measure each phase for")
	chrome := flag.String("chrome", "", "chrome binary (default: $CAUTION_CHROME or a well-known install)")
	flag.Parse()
	var err error
	switch {
	case *present:
		err = runPresent(*fullscreen, *phase)
	case *menu:
		err = runMenu()
	case *leaks:
		err = runLeaks()
	case *browser:
		err = runBrowser(*chrome)
	default:
		err = runNative()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loopprobe:", err)
		os.Exit(1)
	}
}
