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
	fullscreen := flag.Bool("fullscreen", true, "-present: own the display, which the gate assumes. false opens a window instead")
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
