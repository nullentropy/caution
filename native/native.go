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

	Serve caution.Options

	MaxFPS int

	AppID string

	CustomTitlebar bool
	TitlebarStyle  string

	Fullscreen  bool
	HideCursor  bool
	ExitOnInput bool

	Maximized bool
	OnWindow  func(w, h int, maximized bool)

	Shot     string
	SettleMs float64
	Clicks   string
}

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
		Maximized:      o.Maximized,
		OnWindow:       o.OnWindow,
		Shot:           o.Shot,
		SettleMs:       o.SettleMs,
		Clicks:         o.Clicks,
	})
}

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

func (l *memListener) Dial() (net.Conn, error) {
	server, client := net.Pipe()
	select {
	case l.ch <- server:
		return client, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
