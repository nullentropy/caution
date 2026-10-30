// caution goldens: render each fixture scene in the native terminal (-shot)
// and headless Chrome at 2x and diff them cell by cell (see diff.go)
// structural drift fails the run, composites land in -out
//
// both terminals build from the working tree, runs from any directory:
//
//	go run ./cmd/goldens                       # all scenes
//	go run ./cmd/goldens -scene table-light    # one scene
//	go run ./cmd/goldens -out /tmp/goldens
//	go run ./cmd/goldens a.png b.png           # just diff two existing shots
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	caution "github.com/nullentropy/caution"
	"github.com/nullentropy/caution/dev"
	"github.com/nullentropy/caution/internal/cdp"
)

func main() {
	sceneFlag := flag.String("scene", "", "run one scene by name (default: all)")
	out := flag.String("out", "goldens-out", "artifact directory")
	width := flag.Int("w", 1000, "viewport width, logical px")
	height := flag.Int("h", 700, "viewport height, logical px")
	settle := flag.Float64("settle", 1200, "native shot settle time, ms")
	chromeFlag := flag.String("chrome", "", "Chrome/Chromium binary (default: auto-discover)")
	flag.Parse()

	var err error
	if flag.NArg() == 2 {
		err = diffOnly(flag.Arg(0), flag.Arg(1), *out)
	} else {
		err = run(*sceneFlag, *out, *width, *height, *settle, *chromeFlag)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "goldens:", err)
		os.Exit(1)
	}
}

// for one-off investigations and testing the metric on known-different frames
func diffOnly(a, b, out string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	res, err := diffFrames("adhoc", a, b, out)
	if err != nil {
		return err
	}
	verdict := "ok"
	if !res.pass() {
		verdict = "DRIFT"
	}
	fmt.Printf("soft %.2f%%  hard %.2f%%  blob %d  p50 %.1f  p95 %.1f  max %.1f  -> %s\n%s\n",
		res.softPct, res.hardPct, res.blob, res.p50, res.p95, res.max, verdict, res.artifact)
	if verdict == "DRIFT" {
		return fmt.Errorf("frames drifted")
	}
	return nil
}

func run(sceneName, out string, w, h int, settle float64, chromeFlag string) error {
	root, err := dev.SourceRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}

	todo := scenes
	if sceneName != "" {
		todo = nil
		for _, sc := range scenes {
			if sc.name == sceneName {
				todo = []scene{sc}
			}
		}
		if todo == nil {
			return fmt.Errorf("unknown scene %q", sceneName)
		}
	}

	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := l.Addr().(*net.TCPAddr).Port
	dev.ServeClient("")
	caution.ServeAssets("/fonts/", filepath.Join(root, "terminal", "text", "testdata"))
	go func() {
		if err := caution.ServeListener(l, mount, caution.Options{}); err != nil {
			fmt.Fprintln(os.Stderr, "goldens: server:", err)
		}
	}()

	tmp, err := os.MkdirTemp("", "caution-goldens-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	termBin := filepath.Join(tmp, "term")
	build := exec.Command("go", "build", "-o", termBin, "./cmd/term")
	build.Dir = root // `go build ./cmd/...` needs the module, not our cwd
	if outB, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("building cmd/term: %v\n%s", err, outB)
	}

	chromePath, err := cdp.ChromeBin(chromeFlag)
	if err != nil {
		return err
	}
	chrome, err := cdp.Launch(chromePath, w, h)
	if err != nil {
		return err
	}
	defer chrome.Close()

	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
	pageURL := fmt.Sprintf("http://127.0.0.1:%d/", port)

	fmt.Printf("%-20s %8s %8s %6s %6s %6s %6s   %s\n",
		"scene", "soft%", "hard%", "blob", "p50", "p95", "max", "verdict")
	failed := 0
	for i := range todo {
		sc := todo[i]
		current.Store(&sc)

		nativePNG := filepath.Join(out, sc.name+"-native.png")
		// the browser always draws the in-window menu bar, native has to match
		shot := exec.Command(termBin,
			"-url", wsURL, "-shot", nativePNG,
			"-settle", fmt.Sprint(settle),
			"-w", fmt.Sprint(w), "-h", fmt.Sprint(h),
			"-menubar")
		if outB, err := shot.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: native shot: %v\n%s", sc.name, err, outB)
		}

		browserPNG := filepath.Join(out, sc.name+"-browser.png")
		if err := chrome.Capture(pageURL, browserPNG, w, h); err != nil {
			return fmt.Errorf("%s: browser shot: %v", sc.name, err)
		}

		res, err := diffFrames(sc.name, browserPNG, nativePNG, out)
		if err != nil {
			return fmt.Errorf("%s: diff: %v", sc.name, err)
		}
		verdict := "ok"
		if !res.pass() {
			verdict = "DRIFT"
			failed++
		}
		fmt.Printf("%-20s %7.2f%% %7.2f%% %6d %6.1f %6.1f %6.1f   %s\n",
			res.name, res.softPct, res.hardPct, res.blob, res.p50, res.p95, res.max, verdict)
	}

	fmt.Printf("\nartifacts in %s (browser | native | heatmap)\n", out)
	if failed > 0 {
		return fmt.Errorf("%d of %d scenes drifted (hard>%v%%, blob>%d, or soft>%v%%)",
			failed, len(todo), maxHard, maxBlob, maxSoft)
	}
	return nil
}
