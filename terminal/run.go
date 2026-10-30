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

	"github.com/nullentropy/caution/internal/appicon"
	"github.com/nullentropy/caution/terminal/audio"
	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/glx"
	"github.com/nullentropy/caution/terminal/pacer"
	"github.com/nullentropy/caution/terminal/proto"
	"github.com/nullentropy/caution/terminal/render"
	"github.com/nullentropy/caution/terminal/text"
	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/gorilla/websocket"
)

type Options struct {
	// URL of the caution server: ws://host/ws, wss://host/ws, or
	// unix:/path/to.sock
	URL string
	// NetDial overrides transport entirely
	NetDial func() (net.Conn, error)

	Title string
	W, H  int

	// caps animation repaints only, input always paints immediately
	// 0 lets vsync pace it, so 120fps on a 120Hz display
	MaxFPS int

	// in-window menu bar widget instead of the system menu bar
	// always on off macOS. goldens forces it so both terminals render the same bar
	InWindowMenu bool

	// matched against a .desktop entry's StartupWMClass for icon and grouping
	// becomes WM_CLASS under X11. empty derives one from Title
	//
	// does nothing on native Wayland, GLFW 3.3 never calls
	// xdg_toplevel.set_app_id (3.4's GLFW_WAYLAND_APP_ID fixes it)
	// XWayland works. see linux/README.md
	AppID string

	// app draws its own titlebar. the window stays titled so traffic lights,
	// rounded corners, shadow and edge-resize survive, content runs under a
	// transparent titlebar
	// the app marks its titlebar with the windowDrag prop, unclaimed presses
	// there move the window and double-click zooms. the shell does both since
	// the GL view owns every pixel
	// keep the top-left ~78x28 logical px clear for the traffic lights
	// macOS only, ignored elsewhere, in shot mode and under Fullscreen
	CustomTitlebar bool
	// where the traffic lights sit under CustomTitlebar
	// "" is the standard 28pt inset in the corner
	// "compact" / "tall" center them in a ~40pt / ~66pt strip via an empty
	// NSToolbar (unifiedCompact / unified), match the app's bar height to it
	// the toolbar comes out during system fullscreen
	TitlebarStyle string

	// primary monitor at its current video mode, ignored in shot mode
	// not system fullscreen (green button), that one is Session.OnFullscreen
	Fullscreen bool
	HideCursor bool
	// for screensavers and kiosks. ignores the launch input burst, and the
	// mouse has to travel a few pixels before it counts
	ExitOnInput bool

	// render offscreen to this PNG and exit, headless verification mode
	Shot     string
	SettleMs float64 // shot: how long to run the session before capture
	Clicks   string  // shot: synthetic clicks, "x,y@ms;x,y@ms"

	// local display list instead of connecting, for renderer work (cmd/term -scene)
	Scene func(w, h, time float32) *gfx.DisplayList
}

// one scripted shot-mode input
//
//	x,y@ms             click
//	move:x,y@ms        hover, for tooltips
//	rclick:x,y@ms      right-click
//	drag:x1,y1,x2,y2@ms
//	wheel:x,y,dx,dy@ms
//	menu:id@ms
//	type:hello@ms
//	key:cmd+a@ms
//	aux:4@ms           4 = back, 5 = forward
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

// "cmd+shift+left"
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
		// placeholder host, the dial does the routing
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

// blocks until the window closes or the shot is written
// must be called from the main goroutine, see mainthread.go
func Run(o Options) error {
	// redundant after the init lock (locks nest), keeps the requirement visible
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
	// every off-thread wake goes through this gate, which closes before GLFW
	// teardown. otherwise a late patch or image fetch calls PostEmptyEvent on a
	// terminated GLFW and panics
	var gate wakeGate
	gate.open()
	defer func() {
		gate.close()
		glfw.Terminate()
	}()

	glHints() // desktop GL 4.1 core, or NoAPI + ANGLE under -tags angle
	glfw.WindowHint(glfw.CocoaRetinaFramebuffer, glfw.True)
	// WM_CLASS, cmd/linuxapp writes the matching StartupWMClass. inert on macOS
	appID := o.AppID
	if appID == "" {
		appID = appicon.Slug(o.Title)
	}
	glfw.WindowHintString(glfw.X11ClassName, appID)
	glfw.WindowHintString(glfw.X11InstanceName, appID)
	if o.Shot != "" {
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

	jar := openCookieJar(appicon.Slug(appID + " " + o.URL))
	wsURL, dialer, fetch, err := transport(o, jar)
	if err != nil {
		return err
	}

	u := ui.New()
	u.SetSystemReducedMotion(systemReducedMotion())
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
	r.Images.OnLoad = u.Invalidate
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
			// real NSMenus on macOS, the in-window bar everywhere else and under -menubar
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
	})
	watchFullscreen(win, sess.NoteFullscreen)

	// grace period swallows the launch burst (the key or click that started the
	// app, a cursor already crossing the window)
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
	// window drag for windowDrag regions
	// math is in screen space (window pos + local cursor), SetPos moves the
	// window under the cursor so local coords would feed back
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
		// a glass subscribed to right gestures captures it, otherwise context menu
		// ctrl-click is always a context menu
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
			// unclaimed press on the app's titlebar drags, double-click zooms
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
		// GLFW +y is scroll up, widgets want +dy as content down
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
			// letters and digits are keys only under a modifier chord (Cmd+A/C/V,
			// app combos), bare presses insert via the char callback
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
	// paints synchronously from inside a window callback
	// during a live resize macOS runs a modal loop and the frame loop sits in
	// WaitEvents until release, so marking dirty would just show the last frame
	// stretched. size and refresh callbacks still fire in the modal loop
	// nil in shot mode
	var liveDraw func()
	win.SetSizeCallback(func(_ *glfw.Window, w, h int) {
		sess.NoteResize(w, h)
		dirty = true
		if liveDraw != nil {
			liveDraw()
		}
	})
	win.SetFocusCallback(func(_ *glfw.Window, focused bool) {
		sess.NoteFocus(focused)
		if focused {
			// the user may have changed it in system settings while away
			u.SetSystemReducedMotion(systemReducedMotion())
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
		// text measures slightly differently per DPR and widgets cache measurements
		u.NoteDPR(dprOf())
		u.BuildFrame(dl, float32(w), float32(h))
		return dl
	}

	if o.Shot != "" {
		cs, err := parseClicks(o.Clicks)
		if err != nil {
			return err
		}
		// render real frames while settling, rendering is what requests images
		// and reports table ranges
		fbo, capture := shotTarget(o.W, o.H)
		start := glfw.GetTime()
		for glfw.GetTime()-start < o.SettleMs/1000 {
			sess.Pump()
			for len(cs) > 0 && (glfw.GetTime()-start)*1000 >= cs[0].atMs {
				c := cs[0]
				cs = cs[1:]
				switch {
				case c.menu > 0:
					// NSMenu first, else the in-window bar
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
	// next caret blink, tooltip show or theme tween step
	// those are invisible to widget dirty bits, so the tick rebuilds and
	// NoteTick marks the widget it was for
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
			// safe because we honor Painted below
			SkipUnpainted: true,
		})
		// no paint means no blit, the back buffer is undefined after a swap
		// so swapping would present garbage
		if res.Painted {
			glSwap(win)
			framePresented.Add(1)
		}
		// stamps the attempt not the present, so skipped frames don't make
		// the pacer think it's starving and free-run
		lastDraw = time.Now()
		if tick := u.TickIn(); tick > 0 {
			nextTick = lastDraw.Add(tick)
		} else {
			nextTick = time.Time{}
		}
	}
	liveDraw = func() {
		// clear before painting so anything invalidated during the draw survives
		dirty = false
		fullDraw()
	}
	// animation continuation, no tree walk or list build, just replays damage
	// rects from the retained list. false means take the full path
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

	// uncapped a 120Hz display pays double for the same animation
	// input during the wait still paints immediately
	var minFrame time.Duration
	if o.MaxFPS > 0 {
		minFrame = time.Second / time.Duration(o.MaxFPS)
	}

	// shared by the loop and the modal-loop pacer
	// NSMenu tracking parks the main thread in a modal run loop where
	// WaitEvents never returns and wakes queue up, freezing animation and
	// patches until the menu closes. a kCFRunLoopCommonModes timer still fires
	// there and steps only when the loop is starved, so no double paint
	frameStep := func() {
		frameSteps.Add(1)
		sess.Pump()
		if !nextTick.IsZero() && !time.Now().Before(nextTick) {
			u.NoteTick()
			dirty = true
		}
		// animation fully off-screen paints nothing, idle until input or a patch
		// brings it back into view
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
			return // loop or liveDraw is keeping up
		}
		frameStep()
	})
	// armed only while animating so a quiet app pays nothing
	// caret blink pauses under an open menu, a mid-menu patch on a static tree
	// applies when it closes
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
			// sleep until the next tick. frameStep checks nextTick so an early
			// wake isn't mistaken for one
			glfw.WaitEventsTimeout(tick.Seconds())
		} else {
			glfw.WaitEvents()
		}
	}
	return nil
}

// 2x offscreen framebuffer plus a PNG capture func
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

// offscreen at 2x, for cmd/term -scene
func RenderPNG(r *render.Renderer, dl *gfx.DisplayList, bg gfx.Color, w, h int, path string) error {
	fbo, capture := shotTarget(w, h)
	r.Render(dl, bg, render.Frame{
		ViewW: float32(w), ViewH: float32(h),
		DevW: int32(w * 2), DevH: int32(h * 2),
		FBO: fbo, Time: 1.0,
	})
	return capture(path)
}

// wakes come from the session read loop and image fetches, teardown from
// the main thread. the mutex means a wake never hits a terminated GLFW
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
