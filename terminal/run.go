package terminal

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nullentropy/caution/internal/appicon"
	"github.com/nullentropy/caution/terminal/audio"
	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/gpu"
	"github.com/nullentropy/caution/terminal/proto"
	"github.com/nullentropy/caution/terminal/render"
	"github.com/nullentropy/caution/terminal/text"
	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/gorilla/websocket"
)

type Options struct {
	URL     string
	NetDial func() (net.Conn, error)

	Title string
	W, H  int

	MaxFPS int

	InWindowMenu bool

	// it has no effect on native Wayland because GLFW 3.3 never calls
	// xdg_toplevel.set_app_id
	AppID string

	CustomTitlebar bool
	TitlebarStyle  string

	Fullscreen bool
	HideCursor bool

	Maximized   bool
	OnWindow    func(w, h int, maximized bool)
	ExitOnInput bool

	Shot     string
	SettleMs float64
	Clicks   string

	Scene func(w, h, time float32) *gfx.DisplayList
}

type inputKind int

const (
	inputClick inputKind = iota
	inputMove
	inputRightClick
	inputDrag
	inputRightDrag
	inputWheel
	inputMenu
	inputType
	inputKey
	inputAux
)

type shotInput struct {
	kind   inputKind
	atMs   float64
	x, y   float32
	x2, y2 float32
	menu   int
	aux    int
	typ    string
	key    string
}

func parseShotInputs(s string) ([]shotInput, error) {
	var out []shotInput
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		at := strings.LastIndex(part, "@")
		if at < 0 {
			return nil, fmt.Errorf("bad clicks entry %q (missing @ms)", part)
		}
		var in shotInput
		if _, err := fmt.Sscanf(part[at+1:], "%f", &in.atMs); err != nil {
			return nil, fmt.Errorf("bad clicks entry %q (bad ms)", part)
		}
		body := part[:at]
		scan := func(prefix, format, want string, args ...any) error {
			if _, err := fmt.Sscanf(body[len(prefix):], format, args...); err != nil {
				return fmt.Errorf("bad clicks entry %q (want %s)", part, want)
			}
			return nil
		}
		var err error
		switch {
		case strings.HasPrefix(body, "menu:"):
			in.kind = inputMenu
			err = scan("menu:", "%d", "menu:id@ms", &in.menu)
		case strings.HasPrefix(body, "aux:"):
			in.kind = inputAux
			if err = scan("aux:", "%d", "aux:4@ms or aux:5@ms", &in.aux); err == nil && (in.aux < 4 || in.aux > 5) {
				err = fmt.Errorf("bad clicks entry %q (want aux:4@ms or aux:5@ms)", part)
			}
		case strings.HasPrefix(body, "type:"):
			in.kind = inputType
			in.typ = body[5:]
		case strings.HasPrefix(body, "key:"):
			in.kind = inputKey
			in.key = body[4:]
		case strings.HasPrefix(body, "move:"):
			in.kind = inputMove
			err = scan("move:", "%f,%f", "move:x,y@ms", &in.x, &in.y)
		case strings.HasPrefix(body, "rclick:"):
			in.kind = inputRightClick
			err = scan("rclick:", "%f,%f", "rclick:x,y@ms", &in.x, &in.y)
		case strings.HasPrefix(body, "drag:"):
			in.kind = inputDrag
			err = scan("drag:", "%f,%f,%f,%f", "drag:x1,y1,x2,y2@ms", &in.x, &in.y, &in.x2, &in.y2)
		case strings.HasPrefix(body, "rdrag:"):
			in.kind = inputRightDrag
			err = scan("rdrag:", "%f,%f,%f,%f", "rdrag:x1,y1,x2,y2@ms", &in.x, &in.y, &in.x2, &in.y2)
		case strings.HasPrefix(body, "wheel:"):
			in.kind = inputWheel
			err = scan("wheel:", "%f,%f,%f,%f", "wheel:x,y,dx,dy@ms", &in.x, &in.y, &in.x2, &in.y2)
		default:
			err = scan("", "%f,%f", "x,y@ms", &in.x, &in.y)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, nil
}

func parseKeyChord(spec string) ui.Key {
	var k ui.Key
	parts := strings.Split(strings.ToLower(spec), "+")
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "cmd", "meta", "super", "command":
			k.Meta = true
		case "ctrl", "control":
			k.Ctrl = true
		case "shift":
			k.Shift = true
		case "opt", "option", "alt":
			k.Alt = true
		}
	}
	name := parts[len(parts)-1]
	names := map[string]string{
		"enter": "Enter", "return": "Enter", "backspace": "Backspace",
		"delete": "Delete", "left": "Left", "right": "Right", "up": "Up",
		"down": "Down", "home": "Home", "end": "End", "escape": "Escape",
		"pageup": "PageUp", "pagedown": "PageDown",
		"esc": "Escape", "tab": "Tab", "space": " ",
	}
	if n, ok := names[name]; ok {
		k.Name = n
	} else {
		k.Name = name
	}
	return k
}

func transport(o Options, jar http.CookieJar) (wsURL string, dialer *websocket.Dialer, fetch func(string) ([]byte, error), err error) {
	d := *websocket.DefaultDialer
	d.Jar = jar
	var rt http.RoundTripper = http.DefaultTransport
	base := ""

	dialTo := o.NetDial
	if dialTo == nil && strings.HasPrefix(o.URL, "unix:") {
		sock := strings.TrimPrefix(o.URL, "unix:")
		dialTo = func() (net.Conn, error) { return net.Dial("unix", sock) }
	}

	if dialTo != nil {
		wsURL = "ws://caution/ws"
		base = "http://caution"
		d.NetDial = func(_, _ string) (net.Conn, error) { return dialTo() }
		rt = &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return dialTo()
		}}
	} else {
		wsURL = o.URL
		u, perr := url.Parse(o.URL)
		if perr != nil {
			return "", nil, nil, fmt.Errorf("bad url %q: %w", o.URL, perr)
		}
		scheme := "http"
		if u.Scheme == "wss" {
			scheme = "https"
		}
		base = scheme + "://" + u.Host
	}

	client := &http.Client{Transport: rt, Jar: jar, Timeout: 15 * time.Second}
	fetch = func(src string) ([]byte, error) {
		target := src
		if strings.HasPrefix(src, "/") {
			target = base + src
		}
		resp, err := client.Get(target)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s: %s", target, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	return wsURL, &d, fetch, nil
}

func Run(o Options) error {
	// locking again is harmless because locks nest
	runtime.LockOSThread()
	if !onMainThread() {
		return errors.New("caution: terminal.Run must be called from the main goroutine " +
			"(the window system requires the process's main thread; calling from any " +
			"other goroutine traps inside AppKit)")
	}

	if o.W == 0 {
		o.W = 1180
	}
	if o.H == 0 {
		o.H = 780
	}
	if o.Title == "" {
		o.Title = "caution"
	}
	if o.URL == "" {
		o.URL = "ws://localhost:8787/ws"
	}
	if o.SettleMs == 0 {
		o.SettleMs = 1500
	}

	if err := glfw.Init(); err != nil {
		return fmt.Errorf("glfw: %w", err)
	}
	// a late PostEmptyEvent on a terminated GLFW panics
	var gate wakeGate
	gate.open()
	defer func() {
		gate.close()
		glfw.Terminate()
	}()

	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	glfw.WindowHint(glfw.CocoaRetinaFramebuffer, glfw.True)
	appID := o.AppID
	if appID == "" {
		appID = appicon.Slug(o.Title)
	}
	glfw.WindowHintString(glfw.X11ClassName, appID)
	glfw.WindowHintString(glfw.X11InstanceName, appID)
	customTitlebar := o.CustomTitlebar && o.Shot == "" && !o.Fullscreen
	if o.Shot != "" {
		glfw.WindowHint(glfw.Visible, glfw.False)
	} else if (o.Maximized || customTitlebar) && !o.Fullscreen {
		glfw.WindowHint(glfw.Visible, glfw.False)
	}

	// matching the current mode's depth and refresh avoids a mode switch
	var monitor *glfw.Monitor
	if o.Fullscreen && o.Shot == "" {
		monitor = glfw.GetPrimaryMonitor()
		if mode := monitor.GetVideoMode(); mode != nil {
			o.W, o.H = mode.Width, mode.Height
			glfw.WindowHint(glfw.RedBits, mode.RedBits)
			glfw.WindowHint(glfw.GreenBits, mode.GreenBits)
			glfw.WindowHint(glfw.BlueBits, mode.BlueBits)
			glfw.WindowHint(glfw.RefreshRate, mode.RefreshRate)
		}
	}

	win, err := glfw.CreateWindow(o.W, o.H, o.Title, monitor, nil)
	if err != nil {
		return fmt.Errorf("glfw window: %w", err)
	}
	if o.HideCursor && o.Shot == "" {
		win.SetInputMode(glfw.CursorMode, glfw.CursorHidden)
	}
	if customTitlebar {
		applyCustomTitlebar(win, o.TitlebarStyle, o.W, o.H)
	}
	if o.Maximized && o.Shot == "" && !o.Fullscreen {
		win.Maximize()
	}
	if (o.Maximized || customTitlebar) && o.Shot == "" && !o.Fullscreen {
		win.Show()
	}
	dev, err := OpenDevice(win)
	if err != nil {
		return err
	}

	r, err := render.New(dev)
	if err != nil {
		return fmt.Errorf("renderer: %w", err)
	}

	if o.Scene != nil {
		if o.Shot != "" {
			if err := RenderPNG(dev, r, o.Scene(float32(o.W), float32(o.H), 1.0), gfx.Hex("#16181d"), o.W, o.H, o.Shot); err != nil {
				return err
			}
			fmt.Println("wrote", o.Shot)
			return nil
		}
		for !win.ShouldClose() {
			w, h := win.GetSize()
			fw, fh := win.GetFramebufferSize()
			res := r.Render(o.Scene(float32(w), float32(h), float32(glfw.GetTime())), gfx.Hex("#16181d"), render.Frame{
				ViewW: float32(w), ViewH: float32(h), DevW: int32(fw), DevH: int32(fh),
				Time: float32(glfw.GetTime()),
			})
			if res.Oversized || res.Stopped || res.Empty {
				glfw.WaitEvents()
				continue
			}
			dev.Present()
			glfw.PollEvents()
		}
		return nil
	}

	jar := openCookieJar(appicon.Slug(appID + " " + o.URL))
	wsURL, dialer, fetch, err := transport(o, jar)
	if err != nil {
		return err
	}

	u := ui.New()
	fl := &frameLoop{win: win, dev: dev, out: presenter{dev: dev}, r: r, u: u, dirty: true}
	if o.MaxFPS > 0 {
		fl.minFrame = time.Second / time.Duration(o.MaxFPS)
	}
	u.SetSystemReducedMotion(systemReducedMotion())
	u.OnInvalidate = func() {
		fl.dirty = true
		gate.wake()
	}
	u.Measure = func(f gfx.Font, s string) *text.Run {
		return r.Shaper.Shape(f, fl.dpr(), s)
	}
	u.WriteClipboard = func(s string) { glfw.SetClipboardString(s) }
	u.SaveFile = saveToDownloads
	u.ReadClipboard = glfw.GetClipboardString

	r.Images.Fetch = fetch
	r.Images.OnLoad = u.InvalidateAll
	var playSound func(string, bool)
	var stopSound, preloadSound func(string)
	if o.Shot == "" {
		sounds := &audio.Store{Fetch: fetch}
		preloadSound = sounds.Preload
		playSound = func(src string, loop bool) {
			if loop {
				sounds.Loop(src)
			} else {
				sounds.Play(src)
			}
		}
		stopSound = func(src string) {
			if src == "" {
				sounds.StopAll()
			} else {
				sounds.Stop(src)
			}
		}
	}

	cursors := map[string]*glfw.Cursor{
		"":           glfw.CreateStandardCursor(glfw.ArrowCursor),
		"pointer":    glfw.CreateStandardCursor(glfw.HandCursor),
		"text":       glfw.CreateStandardCursor(glfw.IBeamCursor),
		"col-resize": glfw.CreateStandardCursor(glfw.HResizeCursor),
		"row-resize": glfw.CreateStandardCursor(glfw.VResizeCursor),
	}
	lastCursor := ""
	u.SetCursor = func(name string) {
		if _, ok := cursors[name]; !ok {
			name = ""
		}
		if name != lastCursor {
			lastCursor = name
			win.SetCursor(cursors[name])
		}
	}

	var sess *proto.Session
	sess = proto.NewSession(u, proto.Config{
		URL:      wsURL,
		Dialer:   dialer,
		Wake:     gate.wake,
		ViewSize: func() (int, int) { return win.GetSize() },
		SetMenu: func(menus []proto.MenuSpec) {
			pick := func(id int) { sess.Event(0, wire.EvMenu, id) }
			if o.InWindowMenu || runtime.GOOS != "darwin" {
				u.SetMenubar(proto.MenubarSpec(menus), pick)
				return
			}
			installMenu(menus, func(id int) { u.Sound("select"); pick(id) })
		},
		SetTitle:     win.SetTitle,
		SetCookie:    jar.set,
		Preload:      r.Images.Preload,
		PreloadSound: preloadSound,
		Play:         playSound,
		Stop:         stopSound,
		Fonts:        r.Fonts,
		Fetch:        fetch,
	})
	fl.sess = sess
	systemFullscreen := false
	noteWindow := func() {
		if o.OnWindow == nil || o.Shot != "" || o.Fullscreen || systemFullscreen {
			return
		}
		w, h := win.GetSize()
		o.OnWindow(w, h, win.GetAttrib(glfw.Maximized) == glfw.True)
	}
	win.SetMaximizeCallback(func(*glfw.Window, bool) { noteWindow() })
	watchFullscreen(win, func(on bool) {
		systemFullscreen = on
		noteWindow()
		sess.NoteFullscreen(on)
	})
	watchFileDrag(win, sess.NoteFileDrag)
	installQuitFilter()
	u.PasteImage = func(x, y float32) bool {
		data := clipboardImage()
		if data == nil {
			return false
		}
		sess.PasteImage(data, float64(x), float64(y))
		return true
	}
	var iconified, occluded bool
	noteVisible := func() {
		if h := iconified || occluded; h != fl.hidden {
			fl.hidden = h
			sess.NoteVisible(!h)
			gate.wake()
		}
	}
	win.SetIconifyCallback(func(_ *glfw.Window, on bool) {
		iconified = on
		noteVisible()
	})
	occluded = !watchOcclusion(win, func(visible bool) {
		occluded = !visible
		noteVisible()
	})
	noteVisible()

	ptr := installInput(win, u, o.ExitOnInput)
	win.SetSizeCallback(func(_ *glfw.Window, w, h int) {
		sess.NoteResize(w, h)
		noteWindow()
		fl.dirty = true
		fl.liveDraw()
	})
	win.SetFocusCallback(func(_ *glfw.Window, focused bool) {
		sess.NoteFocus(focused)
		if focused {
			u.SetSystemReducedMotion(systemReducedMotion())
		}
	})
	win.SetDropCallback(func(_ *glfw.Window, paths []string) {
		// glfw reports the drop point through the cursor callback first. ptr is
		// used because on macOS the polled cursor position is the pointer's, not the drag's
		sendDroppedFiles(sess, paths, float64(ptr.x), float64(ptr.y))
	})
	win.SetRefreshCallback(func(*glfw.Window) {
		fl.dirty = true
		fl.liveDraw()
	})

	if o.Shot != "" {
		return fl.shot(o)
	}
	fl.run()
	return nil
}

func shotTarget(dev gpu.Device, w, h int) (gpu.Texture, func(path string) error, error) {
	if max := dev.MaxTextureSize(); w <= 0 || h <= 0 || w*2 > max || h*2 > max {
		return nil, nil, fmt.Errorf("shot: %dx%d at 2x is outside the GPU's %d px texture limit", w, h, max)
	}
	t, err := dev.NewTexture(w*2, h*2, gpu.Nearest)
	if err != nil {
		return nil, nil, err
	}
	return t, func(path string) error { return capturePNG(dev, t, path) }, nil
}

func capturePNG(dev gpu.Device, t gpu.Texture, path string) error {
	w, h := t.Size()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	pix, err := dev.ReadPixels(t)
	if err != nil {
		return err
	}
	copy(img.Pix, pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0xff
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func shotFailed(res render.FrameResult) error {
	switch {
	case res.Stopped:
		return errors.New("shot: drawing stopped after a GPU error")
	case res.Oversized:
		return errors.New("shot: the frame is past the GPU's texture limit")
	case res.Empty:
		return errors.New("shot: the frame has no area")
	}
	return nil
}

func withShot(dev gpu.Device, w, h int, path string, draw func(target gpu.Texture) error) error {
	t, capture, err := shotTarget(dev, w, h)
	if err != nil {
		return err
	}
	defer dev.DeleteTexture(t)
	if err := draw(t); err != nil {
		return err
	}
	return capture(path)
}

func RenderPNG(dev gpu.Device, r *render.Renderer, dl *gfx.DisplayList, bg gfx.Color, w, h int, path string) error {
	return withShot(dev, w, h, path, func(t gpu.Texture) error {
		return shotFailed(r.Render(dl, bg, render.Frame{
			ViewW: float32(w), ViewH: float32(h),
			DevW: int32(w * 2), DevH: int32(h * 2),
			Target: t, Time: 1.0,
		}))
	})
}

type wakeGate struct {
	mu    sync.Mutex
	alive bool
}

func (g *wakeGate) open() {
	g.mu.Lock()
	g.alive = true
	g.mu.Unlock()
}

func (g *wakeGate) close() {
	g.mu.Lock()
	g.alive = false
	g.mu.Unlock()
}

func (g *wakeGate) wake() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.alive {
		glfw.PostEmptyEvent()
	}
}
