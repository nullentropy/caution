package terminal

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nullentropy/caution/go/internal/appicon"
	"github.com/nullentropy/caution/go/terminal/audio"
	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/pacer"
	"github.com/nullentropy/caution/go/terminal/proto"
	"github.com/nullentropy/caution/go/terminal/render"
	"github.com/nullentropy/caution/go/terminal/text"
	"github.com/nullentropy/caution/go/terminal/ui"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/gorilla/websocket"
	"github.com/nullentropy/caution/go/terminal/glx"
)

type Options struct {
	// URL of the caution server: ws://host/ws, wss://host/ws, or
	// unix:/path/to.sock
	URL string
	// NetDial overrides transport entirely
	NetDial func() (net.Conn, error)

	Title string
	W, H  int

	// MaxFPS caps animation-driven repaints. 0 lets vsync set the pace, so an
	// animated shader repaints at 120fps on a 120Hz display. Input-driven
	// repaints are never gated: a click or drag during the wait paints
	// immediately.
	MaxFPS int

	// InWindowMenu realizes the server's menu spec as the in-window menu bar
	// widget instead of the system menu bar. It is the only realization off
	// macOS, where it is implied. On macOS it is opt-in, and goldens forces it
	// so both terminals render the same bar.
	InWindowMenu bool

	// AppID is the application identifier the window advertises so a
	// desktop can match it to an installed .desktop entry (its
	// StartupWMClass) and give it the right icon and grouping. Under X11
	// it becomes WM_CLASS. Empty derives one from Title.
	//
	// On native Wayland this goes nowhere: GLFW 3.3 never calls
	// xdg_toplevel.set_app_id, so a compositor cannot match the window
	// (GLFW 3.4's GLFW_WAYLAND_APP_ID is the fix). Under XWayland the X11 path
	// applies as usual. See linux/README.md.
	AppID string

	// CustomTitlebar hides the system window chrome so the app draws its own.
	// The window stays titled, so traffic lights, rounded corners, shadow and
	// edge-resize all survive, but content extends under a transparent,
	// title-less titlebar. The app marks its own titlebar region with the
	// windowDrag node prop: presses there that no interactive widget claims move
	// the window, and a double-click zooms. This shell provides both, since the
	// GL view owns every pixel including the strip. Leave the top-left ~78x28
	// logical px free of controls, where the traffic lights float. macOS only.
	// Accepted and ignored elsewhere, and in shot mode and under Fullscreen.
	CustomTitlebar bool
	// TitlebarStyle picks where the traffic lights sit under CustomTitlebar.
	// "" keeps the standard 28pt inset, hugging the top-left corner. "compact"
	// and "tall" let AppKit center them in a ~40pt / ~66pt strip, using an
	// invisible empty NSToolbar in unifiedCompact / unified style. Match the
	// app's bar height to the strip. The toolbar comes out for the duration of
	// system fullscreen, where AppKit would otherwise park it in the
	// auto-hiding strip on top of the app's own titlebar.
	TitlebarStyle string

	// Fullscreen opens on the primary monitor at its current video mode instead
	// of a window (ignored in shot mode, which is offscreen). This is not the
	// system's own fullscreen, which the user drives with the green button and
	// which arrives at the app through Session.OnFullscreen.
	Fullscreen bool
	// HideCursor hides the pointer while it is over the window.
	HideCursor bool
	// ExitOnInput closes the window on any input, for screensavers and kiosks.
	// A short grace period after launch ignores the input burst that opened the
	// app, and mouse movement must travel a few pixels before it counts.
	ExitOnInput bool

	// Shot renders offscreen to this PNG and exits instead of opening a window.
	// This is the terminal's headless verification mode.
	Shot     string
	SettleMs float64 // shot: how long to run the session before capture
	Clicks   string  // shot: synthetic clicks, "x,y@ms;x,y@ms"

	// Scene renders a local display list instead of connecting (renderer
	// development; see cmd/term -scene).
	Scene func(w, h, time float32) *gfx.DisplayList
}

// click is one scripted input in shot mode: a pointer click ("x,y@ms"), a
// bare pointer move ("move:x,y@ms", hover without pressing, for tooltips),
// a right-click ("rclick:x,y@ms", context menus), a press-drag-release
// ("drag:x1,y1,x2,y2@ms", sliders, dividers, splits), a wheel tick
// ("wheel:x,y,dx,dy@ms"), a menu pick ("menu:id@ms"), typed text
// ("type:hello@ms"), a key chord ("key:cmd+a@ms"), or one of the mouse's extra
// buttons ("aux:4@ms", 4 = back and 5 = forward).
type click struct {
	x, y   float32
	atMs   float64
	move   bool
	rclick bool
	drag   bool
	rdrag  bool
	wheel  bool
	x2, y2 float32
	menu   int
	aux    int
	typ    string
	key    string
}

func parseClicks(s string) ([]click, error) {
	var out []click
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		at := strings.LastIndex(part, "@")
		if at < 0 {
			return nil, fmt.Errorf("bad clicks entry %q (missing @ms)", part)
		}
		var c click
		if _, err := fmt.Sscanf(part[at+1:], "%f", &c.atMs); err != nil {
			return nil, fmt.Errorf("bad clicks entry %q (bad ms)", part)
		}
		body := part[:at]
		switch {
		case strings.HasPrefix(body, "menu:"):
			if _, err := fmt.Sscanf(body[5:], "%d", &c.menu); err != nil {
				return nil, fmt.Errorf("bad clicks entry %q (want menu:id@ms)", part)
			}
		case strings.HasPrefix(body, "aux:"):
			if _, err := fmt.Sscanf(body[4:], "%d", &c.aux); err != nil || c.aux < 4 || c.aux > 5 {
				return nil, fmt.Errorf("bad clicks entry %q (want aux:4@ms or aux:5@ms)", part)
			}
		case strings.HasPrefix(body, "type:"):
			c.typ = body[5:]
		case strings.HasPrefix(body, "key:"):
			c.key = body[4:]
		case strings.HasPrefix(body, "move:"):
			c.move = true
			if _, err := fmt.Sscanf(body[5:], "%f,%f", &c.x, &c.y); err != nil {
				return nil, fmt.Errorf("bad clicks entry %q (want move:x,y@ms)", part)
			}
		case strings.HasPrefix(body, "rclick:"):
			c.rclick = true
			if _, err := fmt.Sscanf(body[7:], "%f,%f", &c.x, &c.y); err != nil {
				return nil, fmt.Errorf("bad clicks entry %q (want rclick:x,y@ms)", part)
			}
		case strings.HasPrefix(body, "drag:"):
			c.drag = true
			if _, err := fmt.Sscanf(body[5:], "%f,%f,%f,%f", &c.x, &c.y, &c.x2, &c.y2); err != nil {
				return nil, fmt.Errorf("bad clicks entry %q (want drag:x1,y1,x2,y2@ms)", part)
			}
		case strings.HasPrefix(body, "rdrag:"):
			c.rdrag = true
			if _, err := fmt.Sscanf(body[6:], "%f,%f,%f,%f", &c.x, &c.y, &c.x2, &c.y2); err != nil {
				return nil, fmt.Errorf("bad clicks entry %q (want rdrag:x1,y1,x2,y2@ms)", part)
			}
		case strings.HasPrefix(body, "wheel:"):
			c.wheel = true
			if _, err := fmt.Sscanf(body[6:], "%f,%f,%f,%f", &c.x, &c.y, &c.x2, &c.y2); err != nil {
				return nil, fmt.Errorf("bad clicks entry %q (want wheel:x,y,dx,dy@ms)", part)
			}
		default:
			if _, err := fmt.Sscanf(body, "%f,%f", &c.x, &c.y); err != nil {
				return nil, fmt.Errorf("bad clicks entry %q (want x,y@ms)", part)
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// parseKeyChord turns "cmd+shift+left" into a ui.Key.
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

// menubarSpec converts the wire's menu spec into the ui package's local
// type (ui cannot import proto, because proto imports ui).
func menubarSpec(menus []proto.MenuSpec) []ui.MenuSpec {
	var conv func(items []proto.MenuItem) []ui.MenuItemSpec
	conv = func(items []proto.MenuItem) []ui.MenuItemSpec {
		out := make([]ui.MenuItemSpec, 0, len(items))
		for _, it := range items {
			out = append(out, ui.MenuItemSpec{
				ID: it.ID, Title: it.Title, Key: it.Key, Sep: it.Sep,
				Items: conv(it.Items),
			})
		}
		return out
	}
	out := make([]ui.MenuSpec, 0, len(menus))
	for _, m := range menus {
		out = append(out, ui.MenuSpec{Title: m.Title, Items: conv(m.Items)})
	}
	return out
}

// transport resolves Options into the pieces the session and image store
// need: a WebSocket URL + dialer, and an HTTP client + base for image srcs.
func transport(o Options) (wsURL string, dialer *websocket.Dialer, fetch func(string) ([]byte, error), err error) {
	d := *websocket.DefaultDialer
	var rt http.RoundTripper = http.DefaultTransport
	base := ""

	dialTo := o.NetDial
	if dialTo == nil && strings.HasPrefix(o.URL, "unix:") {
		sock := strings.TrimPrefix(o.URL, "unix:")
		dialTo = func() (net.Conn, error) { return net.Dial("unix", sock) }
	}

	if dialTo != nil {
		// A placeholder host: routing happens in the dial, not in DNS.
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

	client := &http.Client{Transport: rt, Timeout: 15 * time.Second}
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

// Run opens the terminal and blocks until its window closes (or the shot is
// written). Must be called from the process's main goroutine: GLFW event
// handling and the GL context live on the main OS thread, which this package
// pins at init (see mainthread.go) so app startup work can't drift off it.
func Run(o Options) error {
	// Redundant after the init-time lock (locks nest), but it keeps the
	// requirement visible at the call site.
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
	// Everything that pokes the event loop from another goroutine (the session's
	// read loop, the image store's load callback) goes through this gate, and the
	// gate closes before GLFW is torn down. Without it, a patch or a finished
	// image fetch arriving after Run returns calls PostEmptyEvent on a terminated
	// GLFW and panics.
	var gate wakeGate
	gate.open()
	defer func() {
		gate.close()
		glfw.Terminate()
	}()

	glHints() // desktop GL 4.1 core, or NoAPI + ANGLE under -tags angle
	glfw.WindowHint(glfw.CocoaRetinaFramebuffer, glfw.True)
	// WM_CLASS, so a desktop can tie the window to its installed entry
	// (cmd/linuxapp writes the matching StartupWMClass). Inert on macOS.
	appID := o.AppID
	if appID == "" {
		appID = appicon.Slug(o.Title)
	}
	glfw.WindowHintString(glfw.X11ClassName, appID)
	glfw.WindowHintString(glfw.X11InstanceName, appID)
	if o.Shot != "" {
		glfw.WindowHint(glfw.Visible, glfw.False)
	}

	// Fullscreen owns the primary monitor at its current mode (matching the
	// mode's color depth and refresh avoids a display mode switch).
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
	if o.CustomTitlebar && o.Shot == "" && !o.Fullscreen {
		applyCustomTitlebar(win, o.TitlebarStyle)
	}
	if err := glSetup(win, o.Shot != "", o.W, o.H); err != nil {
		return err
	}

	r, err := render.New()
	if err != nil {
		return fmt.Errorf("renderer: %w", err)
	}

	// -- local scene mode (no session) --------------------------------------------
	if o.Scene != nil {
		if o.Shot != "" {
			if err := RenderPNG(r, o.Scene(float32(o.W), float32(o.H), 1.0), gfx.Hex("#16181d"), o.W, o.H, o.Shot); err != nil {
				return err
			}
			fmt.Println("wrote", o.Shot)
			return nil
		}
		for !win.ShouldClose() {
			w, h := win.GetSize()
			fw, fh := win.GetFramebufferSize()
			r.Render(o.Scene(float32(w), float32(h), float32(glfw.GetTime())), gfx.Hex("#16181d"), render.Frame{
				ViewW: float32(w), ViewH: float32(h), DevW: int32(fw), DevH: int32(fh),
				Time: float32(glfw.GetTime()),
			})
			glSwap(win)
			glfw.PollEvents()
		}
		return nil
	}

	// -- session mode ----------------------------------------------------------------

	wsURL, dialer, fetch, err := transport(o)
	if err != nil {
		return err
	}

	u := ui.New()
	dirty := true
	animating := false
	u.OnInvalidate = func() {
		dirty = true
		gate.wake() // may arrive from an image-fetch goroutine
	}
	dprOf := func() float32 {
		fw, _ := win.GetFramebufferSize()
		w, _ := win.GetSize()
		if w == 0 {
			return 1
		}
		return float32(fw) / float32(w)
	}
	u.Measure = func(f gfx.Font, s string) *text.Run {
		return r.Shaper.Shape(f, dprOf(), s)
	}
	u.WriteClipboard = func(s string) { glfw.SetClipboardString(s) }
	u.ReadClipboard = glfw.GetClipboardString

	r.Images.Fetch = fetch
	r.Images.OnLoad = func() { u.Invalidate() }
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
			pick := func(id int) { sess.Event(0, "menu", id) }
			// macOS gets real NSMenus. Everywhere else (and under
			// -menubar) the in-window bar widget realizes the same spec.
			if o.InWindowMenu || runtime.GOOS != "darwin" {
				u.SetMenubar(menubarSpec(menus), pick)
				return
			}
			installMenu(menus, func(id int) { u.Sound("select"); pick(id) })
		},
		SetTitle:     win.SetTitle,
		Preload:      r.Images.Preload,
		PreloadSound: preloadSound,
		Play:         playSound,
		Stop:         stopSound,
	})
	watchFullscreen(win, func(on bool) { sess.NoteFullscreen(on) })

	// ExitOnInput: any input closes the window. A grace period swallows the
	// launch burst (the keystroke or click that started the app, a cursor already
	// crossing the window), and mouse movement must add up to a real gesture
	// before it counts.
	inputExits := func() bool { return false }
	moveExits := func(x, y float32) bool { return false }
	if o.ExitOnInput {
		armed := time.Now().Add(500 * time.Millisecond)
		inputExits = func() bool {
			if time.Now().Before(armed) {
				return false
			}
			win.SetShouldClose(true)
			return true
		}
		var lastX, lastY, traveled float32
		tracking := false
		moveExits = func(x, y float32) bool {
			if !tracking {
				lastX, lastY, tracking = x, y, true
				return false
			}
			dx := float64(x - lastX)
			dy := float64(y - lastY)
			lastX, lastY = x, y
			if time.Now().Before(armed) {
				return false
			}
			traveled += float32(math.Abs(dx) + math.Abs(dy))
			if traveled < 12 {
				return false
			}
			win.SetShouldClose(true)
			return true
		}
	}

	var mouseX, mouseY float32
	// Shell-side window dragging for windowDrag regions, the app's own titlebar
	// under CustomTitlebar. The math runs in screen space, window position plus
	// window-local cursor: SetPos moves the window under a stationary cursor, so
	// the local coordinates change every frame and would feed back.
	var winDrag struct {
		active           bool
		startWX, startWY int
		startCX, startCY float64
		lastPress        time.Time
	}
	win.SetCursorPosCallback(func(_ *glfw.Window, x, y float64) {
		if winDrag.active {
			wx, wy := win.GetPos()
			sx, sy := float64(wx)+x, float64(wy)+y
			win.SetPos(winDrag.startWX+int(sx-winDrag.startCX),
				winDrag.startWY+int(sy-winDrag.startCY))
			return
		}
		if moveExits(float32(x), float32(y)) {
			return
		}
		mouseX, mouseY = float32(x), float32(y)
		u.PointerMove(mouseX, mouseY)
	})
	win.SetMouseButtonCallback(func(_ *glfw.Window, b glfw.MouseButton, a glfw.Action, mods glfw.ModifierKey) {
		if inputExits() {
			return
		}
		// Right press: a glass subscribed to right gestures captures the button.
		// Otherwise, and always for ctrl-click, it opens a context menu.
		if b == glfw.MouseButtonRight {
			if a == glfw.Press {
				if !u.RightDown(mouseX, mouseY) {
					u.ContextClick(mouseX, mouseY)
				}
			} else if a == glfw.Release {
				u.RightUp(mouseX, mouseY)
			}
			return
		}
		if b == glfw.MouseButton4 || b == glfw.MouseButton5 {
			if a == glfw.Press {
				u.AuxDown(int(b) + 1) // GLFW counts from 0, the SDK from 1
			}
			return
		}
		if b == glfw.MouseButtonLeft && mods&glfw.ModControl != 0 {
			if a == glfw.Press {
				u.ContextClick(mouseX, mouseY)
			}
			return
		}
		if b != glfw.MouseButtonLeft {
			return
		}
		if a == glfw.Press {
			// A press on the app's own titlebar (windowDrag region that no
			// interactive widget claims) moves the window, and a quick second
			// press there zooms, the titlebar double-click convention.
			if u.WindowDragAt(mouseX, mouseY) {
				if time.Since(winDrag.lastPress) < 350*time.Millisecond {
					winDrag.lastPress = time.Time{}
					if win.GetAttrib(glfw.Maximized) == glfw.True {
						win.Restore()
					} else {
						win.Maximize()
					}
					return
				}
				winDrag.lastPress = time.Now()
				winDrag.active = true
				winDrag.startWX, winDrag.startWY = win.GetPos()
				cx, cy := win.GetCursorPos()
				winDrag.startCX = float64(winDrag.startWX) + cx
				winDrag.startCY = float64(winDrag.startWY) + cy
				return
			}
			u.PointerDown(mouseX, mouseY)
		} else if a == glfw.Release {
			if winDrag.active {
				winDrag.active = false
				return
			}
			u.PointerUp(mouseX, mouseY)
		}
	})
	win.SetScrollCallback(func(_ *glfw.Window, xoff, yoff float64) {
		if inputExits() {
			return
		}
		// GLFW says +y is scroll up. The widget contract wants +dy as content down.
		u.Wheel(mouseX, mouseY, float32(-xoff*12), float32(-yoff*12))
	})
	win.SetCharCallback(func(_ *glfw.Window, r rune) {
		if inputExits() {
			return
		}
		u.CharInput(r)
	})
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, mods glfw.ModifierKey) {
		if action != glfw.Press && action != glfw.Repeat {
			return
		}
		if inputExits() {
			return
		}
		k := ui.Key{
			Meta:  mods&glfw.ModSuper != 0,
			Ctrl:  mods&glfw.ModControl != 0,
			Shift: mods&glfw.ModShift != 0,
			Alt:   mods&glfw.ModAlt != 0,
		}
		switch key {
		case glfw.KeyTab:
			k.Name = "Tab"
		case glfw.KeyEnter, glfw.KeyKPEnter:
			k.Name = "Enter"
		case glfw.KeySpace:
			k.Name = " "
		case glfw.KeyEscape:
			k.Name = "Escape"
		case glfw.KeyBackspace:
			k.Name = "Backspace"
		case glfw.KeyDelete:
			k.Name = "Delete"
		case glfw.KeyLeft:
			k.Name = "Left"
		case glfw.KeyRight:
			k.Name = "Right"
		case glfw.KeyUp:
			k.Name = "Up"
		case glfw.KeyDown:
			k.Name = "Down"
		case glfw.KeyHome:
			k.Name = "Home"
		case glfw.KeyEnd:
			k.Name = "End"
		case glfw.KeyPageUp:
			k.Name = "PageUp"
		case glfw.KeyPageDown:
			k.Name = "PageDown"
		default:
			// Letters and digits route as keys only when a modifier chord is
			// held. Bare presses insert via the char callback. This is what
			// carries Cmd+A/C/V/X/Z and any app-registered combo.
			chord := mods&(glfw.ModSuper|glfw.ModControl|glfw.ModAlt) != 0
			switch {
			case chord && key >= glfw.KeyA && key <= glfw.KeyZ:
				k.Name = string(rune('a' + (key - glfw.KeyA)))
			case chord && key >= glfw.Key0 && key <= glfw.Key9:
				k.Name = string(rune('0' + (key - glfw.Key0)))
			default:
				return
			}
		}
		u.KeyDown(k)
	})
	// liveDraw, once assigned, paints synchronously from inside a window
	// callback. During an interactive resize macOS runs a modal event loop and
	// the frame loop below is parked in WaitEvents until the drag ends, so a
	// frame merely marked dirty here would paint only on release, with the
	// compositor stretching the last presented frame until then. Size and refresh
	// callbacks do fire from inside the modal loop, so painting here is what
	// keeps the tree laid out live under the drag. Nil in shot mode.
	var liveDraw func()
	win.SetSizeCallback(func(_ *glfw.Window, w, h int) {
		sess.NoteResize(w, h)
		dirty = true
		if liveDraw != nil {
			liveDraw()
		}
	})
	win.SetRefreshCallback(func(*glfw.Window) {
		dirty = true
		if liveDraw != nil {
			liveDraw()
		}
	})

	buildFrame := func() *gfx.DisplayList {
		w, h := win.GetSize()
		dl := &gfx.DisplayList{}
		// Text measures a hair differently per DPR, and widgets cache
		// measurements: tell the Ui which one it is looking at.
		u.NoteDPR(dprOf())
		u.BuildFrame(dl, float32(w), float32(h))
		return dl
	}

	if o.Shot != "" {
		cs, err := parseClicks(o.Clicks)
		if err != nil {
			return err
		}
		// Settle: pump the session, inject scripted input, and render real
		// frames offscreen (rendering is what requests images and reports
		// table ranges), then capture the last one.
		fbo, capture := shotTarget(o.W, o.H)
		start := glfw.GetTime()
		for glfw.GetTime()-start < o.SettleMs/1000 {
			sess.Pump()
			for len(cs) > 0 && (glfw.GetTime()-start)*1000 >= cs[0].atMs {
				c := cs[0]
				cs = cs[1:]
				switch {
				case c.menu > 0:
					// NSMenu first (darwin default), else the in-window bar.
					if !menuPerform(c.menu) &&
						(u.Menubar == nil || !u.Menubar.Perform(c.menu)) {
						fmt.Printf("menu probe: no item with id %d\n", c.menu)
					}
				case c.aux > 0:
					u.AuxDown(c.aux)
				case c.typ != "":
					for _, r := range c.typ {
						u.CharInput(r)
					}
				case c.key != "":
					u.KeyDown(parseKeyChord(c.key))
				case c.move:
					u.PointerMove(c.x, c.y)
				case c.rclick:
					u.PointerMove(c.x, c.y)
					u.ContextClick(c.x, c.y)
				case c.drag:
					u.PointerMove(c.x, c.y)
					u.PointerDown(c.x, c.y)
					u.PointerMove((c.x+c.x2)/2, (c.y+c.y2)/2)
					u.PointerMove(c.x2, c.y2)
					u.PointerUp(c.x2, c.y2)
				case c.rdrag:
					u.PointerMove(c.x, c.y)
					if !u.RightDown(c.x, c.y) {
						u.ContextClick(c.x, c.y)
					}
					u.PointerMove((c.x+c.x2)/2, (c.y+c.y2)/2)
					u.PointerMove(c.x2, c.y2)
					u.RightUp(c.x2, c.y2)
				case c.wheel:
					u.Wheel(c.x, c.y, c.x2, c.y2)
				default:
					u.PointerMove(c.x, c.y)
					u.PointerDown(c.x, c.y)
					u.PointerUp(c.x, c.y)
				}
			}
			r.Render(buildFrame(), *ui.Tok("bg"), render.Frame{
				ViewW: float32(o.W), ViewH: float32(o.H),
				DevW: int32(o.W * 2), DevH: int32(o.H * 2),
				FBO: fbo, Time: float32(glfw.GetTime() - start),
			})
			glfw.WaitEventsTimeout(0.02)
		}
		sess.Pump()
		r.Render(buildFrame(), *ui.Tok("bg"), render.Frame{
			ViewW: float32(o.W), ViewH: float32(o.H),
			DevW: int32(o.W * 2), DevH: int32(o.H * 2),
			FBO: fbo, Time: float32(o.SettleMs / 1000),
		})
		if err := capture(o.Shot); err != nil {
			return err
		}
		fmt.Println("wrote", o.Shot)
		return nil
	}

	var lastDraw time.Time
	// nextTick is when the nearest time-driven repaint comes due: a caret blink
	// flip, a tooltip show, a theme tween step. Those states are frozen in a
	// replayed list and invisible to the widget dirty bits, so when the tick
	// fires the loop rebuilds and NoteTick marks whichever widget the tick was
	// for.
	var nextTick time.Time
	fullDraw := func() {
		w, h := win.GetSize()
		fw, fh := win.GetFramebufferSize()
		dl := buildFrame()
		animating = dl.WantsAnimation()
		res := r.Render(dl, *ui.Tok("bg"), render.Frame{
			ViewW: float32(w), ViewH: float32(h),
			DevW: int32(fw), DevH: int32(fh),
			Time: float32(glfw.GetTime()),
			// This loop honors Painted below, which is what allows both the blit
			// skip and the swap skip.
			SkipUnpainted: true,
		})
		// When Render paints nothing it also skips its blit, so the back buffer
		// holds whatever was there before, which is undefined after the last swap.
		// Swapping anyway would present that garbage. Not swapping leaves the front
		// buffer showing the last good frame.
		if res.Painted {
			glSwap(win)
			framePresented.Add(1)
		}
		// lastDraw paces MaxFPS, so it timestamps the attempt rather than the
		// presentation: a run of skipped frames must not let the pacer think it is
		// starving and free-run.
		lastDraw = time.Now()
		if tick := u.TickIn(); tick > 0 {
			nextTick = lastDraw.Add(tick)
		} else {
			nextTick = time.Time{}
		}
	}
	liveDraw = func() {
		// Mirror the loop's own order, consuming the mark before painting, so
		// anything invalidated during the draw survives to the next frame.
		dirty = false
		fullDraw()
	}
	// partialDraw is the pure animation-continuation frame: no tree walk, no
	// display-list build. The renderer replays its retained list's damage
	// rects. False means this frame needs the full path.
	partialDraw := func() bool {
		w, h := win.GetSize()
		fw, fh := win.GetFramebufferSize()
		if !r.RenderPartial(*ui.Tok("bg"), render.Frame{
			ViewW: float32(w), ViewH: float32(h),
			DevW: int32(fw), DevH: int32(fh),
			Time: float32(glfw.GetTime()),
		}) {
			return false
		}
		glSwap(win)
		framePresented.Add(1)
		lastDraw = time.Now()
		return true
	}

	// The animation frame cap. Uncapped, vsync sets the pace and a 120Hz display
	// pays double a 60Hz one for the same animation. The cap only paces
	// animation-driven repaints: input arriving during the wait wakes the loop
	// and paints immediately.
	var minFrame time.Duration
	if o.MaxFPS > 0 {
		minFrame = time.Second / time.Duration(o.MaxFPS)
	}

	// frameStep is the work half of one loop iteration: pump the session, fire
	// due ticks, and paint what the frame decision asks for. The loop runs it
	// before every wait, and the modal-loop pacer runs the same step when the
	// loop cannot. NSMenu tracking parks the main thread in a modal run loop
	// where WaitEvents never returns and posted wakes queue undelivered, which
	// would freeze animation and patches until the menu closed. A
	// kCFRunLoopCommonModes timer still fires there, and its callback steps a
	// frame only when the loop is demonstrably starved, so the two never
	// double-paint.
	frameStep := func() {
		frameSteps.Add(1)
		sess.Pump()
		if !nextTick.IsZero() && !time.Now().Before(nextTick) {
			// A caret flip / tooltip show / tween step came due.
			u.NoteTick()
			dirty = true
		}
		// An animation whose damage is entirely off-screen paints nothing: skip it
		// and fall through to the idle wait below. The loop still wakes on input or
		// a patch, which is the only way such content comes back into view.
		paint := animating && !r.AnimationIsInvisible()
		if dirty {
			dirty = false
			fullDraw()
		} else if paint {
			if !partialDraw() {
				fullDraw()
			}
		}
	}
	starve := 25 * time.Millisecond
	if minFrame > starve {
		starve = minFrame + minFrame/2
	}
	pacer.Install(func() {
		if time.Since(lastDraw) < starve {
			return // the loop (or liveDraw) is keeping up - stand down
		}
		frameStep()
	})
	// The pacer is armed only while animation needs frames, so a quiet app pays
	// nothing for it. A caret-only app keeps its blink paused under an open menu,
	// and a patch arriving mid-menu over a static tree applies when it closes.
	pacerArmed := false
	for !win.ShouldClose() {
		frameStep()
		paint := animating && !r.AnimationIsInvisible()
		if paint != pacerArmed {
			pacerArmed = paint
			if paint {
				pacer.Arm(1.0 / 60.0)
			} else {
				pacer.Idle()
			}
		}
		if paint {
			if wait := minFrame - time.Since(lastDraw); wait > 0 {
				glfw.WaitEventsTimeout(wait.Seconds())
			} else {
				glfw.PollEvents()
			}
		} else if tick := u.TickIn(); tick > 0 {
			// Time-driven repaint without a game loop: sleep until the caret's
			// next blink phase.
			glfw.WaitEventsTimeout(tick.Seconds())
			u.NoteTick()
			dirty = true
		} else {
			glfw.WaitEvents()
		}
	}
	return nil
}

// shotTarget allocates a 2x offscreen framebuffer and returns it with a
// capture function that writes its current contents as PNG.
func shotTarget(w, h int) (uint32, func(path string) error) {
	devW, devH := int32(w*2), int32(h*2)
	var tex, fbo uint32
	glx.GenTextures(1, &tex)
	glx.ActiveTexture(glx.TEXTURE0)
	glx.BindTexture(glx.TEXTURE_2D, tex)
	glx.TexImage2D(glx.TEXTURE_2D, 0, glx.RGBA, devW, devH, 0, glx.RGBA, glx.UNSIGNED_BYTE, nil)
	glx.GenFramebuffers(1, &fbo)
	glx.BindFramebuffer(glx.FRAMEBUFFER, fbo)
	glx.FramebufferTexture2D(glx.FRAMEBUFFER, glx.COLOR_ATTACHMENT0, glx.TEXTURE_2D, tex, 0)
	return fbo, func(path string) error {
		return capturePNG(fbo, devW, devH, path)
	}
}

func capturePNG(fbo uint32, devW, devH int32, path string) error {
	pix := make([]byte, devW*devH*4)
	glx.BindFramebuffer(glx.FRAMEBUFFER, fbo)
	glx.ReadPixels(0, 0, devW, devH, glx.RGBA, glx.UNSIGNED_BYTE, glx.Ptr(pix))
	img := image.NewRGBA(image.Rect(0, 0, int(devW), int(devH)))
	stride := int(devW) * 4
	for y := 0; y < int(devH); y++ {
		src := pix[(int(devH)-1-y)*stride : (int(devH)-y)*stride]
		dst := img.Pix[y*stride : (y+1)*stride]
		copy(dst, src)
		for x := 3; x < stride; x += 4 {
			dst[x] = 0xff
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// RenderPNG renders one display list offscreen at 2x and writes it as PNG
// (used by cmd/term's -scene mode).
func RenderPNG(r *render.Renderer, dl *gfx.DisplayList, bg gfx.Color, w, h int, path string) error {
	fbo, capture := shotTarget(w, h)
	r.Render(dl, bg, render.Frame{
		ViewW: float32(w), ViewH: float32(h),
		DevW: int32(w * 2), DevH: int32(h * 2),
		FBO: fbo, Time: 1.0,
	})
	return capture(path)
}

// wakeGate serializes waking the event loop against GLFW teardown. Wakes
// arrive on the session's read-loop goroutine and on image-fetch goroutines,
// while teardown happens on the main thread as Run unwinds. Holding the mutex
// across both means a wake either happens while GLFW is alive or sees a closed
// gate and does nothing, never a call into terminated GLFW.
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
