package native

import (
	"errors"
	"log"
	"net"
	"os"
	"strings"
	"sync"

	caution "github.com/nullentropy/caution"
	"github.com/nullentropy/caution/terminal"
)

// running from inside a macOS .app bundle (the cmd/appbundle case)
// bundled apps launch with no arguments, so switch to native mode on this
// instead of a flag:
//
//	if flagNative || native.InBundle() { native.Run(mount, ...) }
func InBundle() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(exe, ".app/Contents/MacOS/")
}

type Options struct {
	Title string
	W, H  int

	// the app's caution.Options. Authorize runs against the in-process request
	// too so identity logic matches in both modes. origin and TLS mean nothing
	// over a pipe
	Serve caution.Options

	// caps animation-driven repaints, see terminal.Options.MaxFPS
	MaxFPS int

	// lets a Linux desktop match the window to its installed entry
	// (see terminal.Options.AppID)
	AppID string

	// hides the system chrome so the app draws its own, mark the replacement
	// region with Node.WindowDrag (see terminal.Options.CustomTitlebar)
	// TitlebarStyle places the traffic lights: "" standard top-left inset,
	// "compact"/"tall" centered in a ~40pt/~66pt strip. macOS only for now
	CustomTitlebar bool
	TitlebarStyle  string

	// kiosk/screensaver trio: own the primary display, hide the pointer, close
	// on any input (after a launch grace period). see terminal.Options
	Fullscreen  bool
	HideCursor  bool
	ExitOnInput bool

	// passed to the terminal's headless verification mode (render offscreen to
	// PNG and exit)
	Shot     string
	SettleMs float64
	Clicks   string
}

// blocks until the window closes. must be called from the main goroutine,
// the GL context lives on the main OS thread
func Run(mount caution.MountFunc, o Options) error {
	l := newMemListener()
	defer l.Close()
	go func() {
		if err := caution.ServeListener(l, mount, o.Serve); err != nil && !errors.Is(err, net.ErrClosed) {
			log.Println("caution native: server:", err)
		}
	}()
	return terminal.Run(terminal.Options{
		NetDial:        l.Dial,
		Title:          o.Title,
		W:              o.W,
		H:              o.H,
		MaxFPS:         o.MaxFPS,
		AppID:          o.AppID,
		CustomTitlebar: o.CustomTitlebar,
		TitlebarStyle:  o.TitlebarStyle,
		Fullscreen:     o.Fullscreen,
		HideCursor:     o.HideCursor,
		ExitOnInput:    o.ExitOnInput,
		Shot:           o.Shot,
		SettleMs:       o.SettleMs,
		Clicks:         o.Clicks,
	})
}

// connections are net.Pipe pairs: Dial hands the client end to the caller
// and the server end to Accept, the whole HTTP stack runs over it unmodified
type memListener struct {
	ch   chan net.Conn
	once sync.Once
	done chan struct{}
}

func newMemListener() *memListener {
	return &memListener{ch: make(chan net.Conn), done: make(chan struct{})}
}

func (l *memListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *memListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

type memAddr struct{}

func (memAddr) Network() string { return "mem" }
func (memAddr) String() string  { return "in-process" }

func (l *memListener) Addr() net.Addr { return memAddr{} }

// each call is a fresh in-memory connection
func (l *memListener) Dial() (net.Conn, error) {
	server, client := net.Pipe()
	select {
	case l.ch <- server:
		return client, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
