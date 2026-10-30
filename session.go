package caution

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"reflect"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nullentropy/caution/wire"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

type MountFunc func(s *Session) *Node

// all widget mutation happens on the session goroutine: inside event
// handlers, or via Update from anywhere else (tickers, watchers, queues)
type Session struct {
	sid     int
	token   string
	conn    *websocket.Conn
	conns   chan adoptedConn
	nodes   map[int]*Node
	root    *Node
	nextID  int
	seq     int
	ops     []map[string]any
	updates chan func()
	done    chan struct{}
	closing chan struct{}
	closed  sync.Once
	grace   time.Duration
	mounted bool
	theme   map[string]string
	metrics map[string]float64
	title   string
	motion  Motion

	menu         []any
	menuHandlers map[int]func()

	keys         []any
	keyHandlers  map[int]func()
	onAux        func(button int)
	onFullscreen func(on bool)
	fullscreen   bool
	onFocus      func(focused bool)
	unfocused    bool
	onFileDrop   func(files []File, x, y float64)
	onFileDrag   func(over bool)
	drop         dropAssembly
	maxDropBytes int64

	preloads      []string
	soundPreloads []string
	sounds        map[string]string
	loops         []string

	// Set-Cookie strings not yet on the wire. unlike ops they survive a
	// disconnect, a resume can't rebuild them from the tree
	cookies []string

	// last node the app asked to focus, a mount names it so a reconnect that
	// remounts puts focus back. focusPending queues the cmd for the next flush
	focus        *Node
	focusPending bool

	vw, vh   float64
	onResize func(w, h float64)

	// event rate limiting so we can drop event floods
	evTokens float64
	evLast   time.Time

	identity any
	req      *http.Request
}

// whatever Options.Authorize produced for the original connection, nil
// without an Authorize hook
func (s *Session) Identity() any { return s.identity }

func (s *Session) Request() *http.Request { return s.req }

// zero value keeps safe defaults: same-origin upgrades, no auth, plain HTTP
type Options struct {
	// vets every connection, resumes included. return an identity, or an error
	// to reject with 403. on resume the identity must DeepEqual the original so
	// a leaked session token can't adopt someone else's session
	// nil allows everyone with a nil identity
	Authorize func(r *http.Request) (any, error)
	// for the WebSocket upgrade. empty means same-origin only, "*" allows any
	AllowedOrigins []string
	// both set switches to ListenAndServeTLS
	TLSCert, TLSKey string
	// how long a disconnected session waits for its client to resume before it
	// ends. 0 is 60s
	ResumeGrace time.Duration
	// total bytes per file drop, the client skips a larger one. 0 is 32MB
	MaxDropBytes int64
}

// reconnecting socket plus its reported viewport and the last seq the client
// applied (-1: none reported)
type adoptedConn struct {
	conn   *websocket.Conn
	vw, vh float64
	seq    int
}

type clientMsg struct {
	T     wire.Msg   `json:"t"`
	ID    int        `json:"id"`
	Ev    wire.Event `json:"ev"`
	Value any        `json:"value"`
	Seq   int        `json:"seq"`
	// socket it arrived on
	from *websocket.Conn
}

var (
	sessionCounter atomic.Int64
	registry       sync.Map // token -> *Session
)

func randToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// one WebSocket endpoint at /ws with default Options. ?resume=<sid>
// re-adopts the old session if it is still alive, anything else starts fresh
//
// addr is a TCP address (":8787") or "unix:/path/to.sock". a unix socket
// serves the same protocol with no port open, for a local native terminal
// (`cmd/term -url unix:/path`)
func Serve(addr string, mount MountFunc) error {
	return ServeOpts(addr, mount, Options{})
}

// a process may call both ServeOpts and ServeListener (dual web + native
// binary), the first registration's mount and options win
var wsOnce sync.Once

func registerWS(mount MountFunc, opts Options) {
	wsOnce.Do(func() { http.HandleFunc("/ws", wsHandler(mount, opts)) })
}

// caution/native uses this to run server and terminal in one process over
// an in-memory pipe
func ServeListener(l net.Listener, mount MountFunc, opts Options) error {
	registerWS(mount, opts)
	return http.Serve(l, nil)
}

// Serve with deployment hardening, see Options
func ServeOpts(addr string, mount MountFunc, opts Options) error {
	registerWS(mount, opts)
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		if opts.TLSCert != "" || opts.TLSKey != "" {
			return errors.New("caution: TLS over a unix socket is unsupported (the filesystem is the access control)")
		}
		// a stale socket file from a previous run blocks Listen, a live one fails
		// the next Listen anyway, so removal is safe
		_ = os.Remove(path)
		l, err := net.Listen("unix", path)
		if err != nil {
			return err
		}
		log.Info().Msgf("caution: listening on unix socket %s", path)
		return http.Serve(l, nil)
	}
	log.Info().Msgf("caution: listening on %s", addr)
	if opts.TLSCert != "" && opts.TLSKey != "" {
		return http.ListenAndServeTLS(addr, opts.TLSCert, opts.TLSKey, nil)
	}
	return http.ListenAndServe(addr, nil)
}

func wsHandler(mount MountFunc, opts Options) http.HandlerFunc {
	up := websocket.Upgrader{CheckOrigin: originChecker(opts.AllowedOrigins)}
	return func(w http.ResponseWriter, r *http.Request) {
		grace := opts.ResumeGrace
		if grace == 0 {
			grace = 60 * time.Second
		}
		maxDrop := opts.MaxDropBytes
		if maxDrop == 0 {
			maxDrop = defaultMaxDropBytes
		}
		var identity any
		if opts.Authorize != nil {
			id, err := opts.Authorize(r)
			if err != nil {
				log.Info().Msgf("caution: connection rejected (%s): %v", r.RemoteAddr, err)
				deny(w, r, up)
				return
			}
			identity = id
		}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			log.Info().Msgf("caution: upgrade failed: %v", err)
			return
		}
		vw := queryFloat(r, "vw")
		vh := queryFloat(r, "vh")
		if tryResume(r, identity, adoptedConn{conn: conn, vw: vw, vh: vh, seq: -1}) {
			return
		}
		s := &Session{
			sid:          int(sessionCounter.Add(1)),
			token:        randToken(),
			conns:        make(chan adoptedConn, 1),
			nodes:        map[int]*Node{},
			updates:      make(chan func(), 64),
			done:         make(chan struct{}),
			closing:      make(chan struct{}),
			grace:        grace,
			maxDropBytes: maxDrop,
			vw:           vw,
			vh:           vh,
			identity:     identity,
			req:          r,
		}
		registry.Store(s.token, s)
		log.Info().Msgf("caution: session %d connected (%s)", s.sid, r.RemoteAddr)
		go s.run(mount, conn)
	}
}

// a browser can't see the status of a failed upgrade, so a plain 403 reads
// as an outage it retries forever. upgrade, say why, hang up
func deny(w http.ResponseWriter, r *http.Request, up websocket.Upgrader) {
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	_ = conn.WriteJSON(map[string]any{"t": wire.MsgDenied})
	_ = conn.Close()
}

// hands a to the session named by ?resume= if it can
// a resume token is not a credential, it only re-adopts a session minted for
// the same identity
func tryResume(r *http.Request, identity any, a adoptedConn) bool {
	v, ok := registry.Load(r.URL.Query().Get("resume"))
	if !ok {
		return false
	}
	s := v.(*Session)
	if !reflect.DeepEqual(s.identity, identity) {
		log.Info().Msgf("caution: session %d resume denied - identity mismatch", s.sid)
		return false
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("seq")); err == nil {
		a.seq = n
	}
	select {
	case s.conns <- a:
		return true
	case <-s.done:
		// expired between lookup and adoption
		return false
	}
}

// nil (empty list) defers to gorilla's default: Origin host must match the
// request host
func originChecker(allowed []string) func(*http.Request) bool {
	if len(allowed) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, a := range allowed {
		if a == "*" {
			return func(*http.Request) bool { return true }
		}
		set[a] = true
	}
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || set[origin]
	}
}

// runs fn on the session goroutine and flushes the ops as one patch
// safe from any goroutine, a closed session drops it
func (s *Session) Update(fn func()) {
	select {
	case s.updates <- fn:
	case <-s.done:
	}
}

// closed when the session ends (client gone past the resume grace, or Close)
// use it to stop tickers
func (s *Session) Done() <-chan struct{} { return s.done }

// ends the session now. the client reconnects into a fresh session and a
// fresh mount. safe from any goroutine
func (s *Session) Close() { s.closed.Do(func() { close(s.closing) }) }

// last-reported window size in logical px, 0,0 if never reported
// valid from mount on since the initial size rides the connect URL
func (s *Session) Viewport() (w, h float64) { return s.vw, s.vh }

// runs on the session goroutine, reports are client-debounced (~250ms)
func (s *Session) OnResize(fn func(w, h float64)) { s.onResize = fn }

func queryFloat(r *http.Request, key string) float64 {
	f, _ := strconv.ParseFloat(r.URL.Query().Get(key), 64)
	return f
}

// design tokens ("bg", "ink", "accent", ...) as color strings
func (s *Session) SetTheme(tokens map[string]string) {
	s.theme = tokens
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpTheme, "tokens": tokens})
	}
}

// metric tokens ("radius.control", "row.height", ...) in logical px, the
// geometry half of theming. restyles the widget vocabulary (square vs
// rounded, compact vs comfortable) without per-widget props
// terminals ignore unknown tokens, per-node props still override
// MetricsCompact and MetricsComfortable are ready-made maps
func (s *Session) SetMetrics(tokens map[string]float64) {
	s.metrics = tokens
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpMetrics, "metrics": tokens})
	}
}

// browser tab title, native window titlebar
func (s *Session) SetTitle(title string) {
	s.title = title
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpTitle, "title": title})
	}
}

// how much decorative motion the client plays: the fade a newly inserted
// widget gets, the slide of widgets a reflow moved, the theme crossfade.
// app shaders and effects always animate
type Motion = wire.Motion

const (
	// full unless the OS asks for reduced motion (macOS Reduce motion, Windows
	// "Show animations", GNOME and KDE animation settings, the browser's
	// prefers-reduced-motion). the default
	MotionSystem = wire.MotionSystem
	// full regardless of the OS preference
	MotionFull = wire.MotionFull
	// none: inserts appear, reflows snap, themes switch at once
	MotionReduced = wire.MotionReduced
)

func (s *Session) SetMotion(m Motion) {
	s.motion = m
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpMotion, "motion": m})
	}
}

// kept by the browser, or in a per-app file natively, and sent on every later
// connection so the next mount and Authorize see it. MaxAge < 0 deletes,
// Path defaults to /, HttpOnly is dropped since a page can't set it
func (s *Session) SetCookie(c *http.Cookie) {
	cc := *c
	cc.HttpOnly = false
	if cc.Path == "" {
		cc.Path = "/"
	}
	raw := cc.String()
	if raw == "" {
		log.Warn().Msgf("caution: session %d dropped invalid cookie %q", s.sid, c.Name)
		return
	}
	s.cookies = append(s.cookies, raw)
}

// warms client resource caches ahead of first use, images (URLs or data:
// URIs) for now. for images not in the tree yet like a next screen or a
// hover swap, mounted ones load at first paint anyway
// rides the mount and patches live like SetTheme. repeated srcs are dropped
// so calling it from a rebuild loop costs nothing
func (s *Session) Preload(srcs ...string) {
	var fresh []string
	for _, src := range srcs {
		if !slices.Contains(s.preloads, src) {
			s.preloads = append(s.preloads, src)
			fresh = append(fresh, src)
		}
	}
	if s.mounted && len(fresh) > 0 {
		s.ops = append(s.ops, map[string]any{"op": wire.OpResource, "images": fresh})
	}
}

// gesture table: which WAV plays on press, toggle, select, open, close or
// type, fired client-side at the moment of the gesture
func (s *Session) SetSounds(tokens map[string]string) {
	s.sounds = tokens
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpSounds, "tokens": tokens})
	}
}

// decodes WAV sources on the client ahead of their first Play
func (s *Session) PreloadSounds(srcs ...string) {
	var fresh []string
	for _, src := range srcs {
		if !slices.Contains(s.soundPreloads, src) {
			s.soundPreloads = append(s.soundPreloads, src)
			fresh = append(fresh, src)
		}
	}
	if s.mounted && len(fresh) > 0 {
		s.ops = append(s.ops, map[string]any{"op": wire.OpResource, "sounds": fresh})
	}
}

// src is a URL or data: URI
func (s *Session) Play(src string) {
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpPlay, "src": src})
	}
}

// repeats until Stop
func (s *Session) Loop(src string) {
	if slices.Contains(s.loops, src) {
		return
	}
	s.loops = append(s.loops, src)
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpPlay, "src": src, "loop": true})
	}
}

// every playing instance of src
func (s *Session) Stop(src string) {
	s.loops = slices.DeleteFunc(s.loops, func(l string) bool { return l == src })
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpStop, "src": src})
	}
}

func (s *Session) StopAll() {
	s.loops = nil
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpStop})
	}
}

// session-wide shortcut: "cmd+k", "ctrl+shift+p", "alt+enter", any of
// cmd/ctrl/alt/shift plus a key. fires only when the focused widget declines
// the chord, so a text field's cmd+z stays undo
// in the native terminal a menu item's key equivalent for the same chord
// wins (the OS routes it first)
func (s *Session) OnKey(combo string, fn func()) {
	if s.keyHandlers == nil {
		s.keyHandlers = map[int]func(){}
	}
	id := len(s.keyHandlers) + 1
	s.keys = append(s.keys, map[string]any{"id": id, "key": normalizeCombo(combo)})
	s.keyHandlers[id] = fn
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpKeys, "keys": s.keys})
	}
}

// mouse buttons 4 (back) and 5 (forward). both terminals swallow the press
// either way so the browser never navigates history out from under the app
func (s *Session) OnAux(fn func(button int)) { s.onAux = fn }

// the system's own fullscreen: green button and cmd+ctrl+f on macOS, the
// fullscreen API on the web. a custom titlebar usually restyles here since
// fullscreen has no traffic lights to leave room for
func (s *Session) OnFullscreen(fn func(on bool)) { s.onFullscreen = fn }

// last known client state
func (s *Session) Fullscreen() bool { return s.fullscreen }

// the client window gaining or losing focus. a hidden browser tab counts as
// unfocused
func (s *Session) OnFocus(fn func(focused bool)) { s.onFocus = fn }

// last known client state, true until the client says otherwise
func (s *Session) Focused() bool { return !s.unfocused }

// canonical modifier order and case so the client's event-derived string
// always matches: cmd+ctrl+alt+shift+<key>
func normalizeCombo(combo string) string {
	var meta, ctrl, alt, shift bool
	key := ""
	for _, part := range strings.Split(strings.ToLower(combo), "+") {
		switch part {
		case "cmd", "meta", "super", "command":
			meta = true
		case "ctrl", "control":
			ctrl = true
		case "alt", "opt", "option":
			alt = true
		case "shift":
			shift = true
		default:
			key = part
		}
	}
	out := ""
	if meta {
		out += "cmd+"
	}
	if ctrl {
		out += "ctrl+"
	}
	if alt {
		out += "alt+"
	}
	if shift {
		out += "shift+"
	}
	return out + key
}

func (s *Session) run(mount MountFunc, first *websocket.Conn) {
	// runs last, so a client that reconnects on the close finds no session to
	// resume
	defer func() {
		if s.conn != nil {
			_ = s.conn.Close()
		}
	}()
	defer close(s.done)
	defer registry.Delete(s.token)
	defer log.Info().Msgf("caution: session %d closed", s.sid)

	s.safely("mount", func() { s.root = mount(s) })
	if s.root == nil {
		s.root = crashTree("the app's mount function panicked - see the server log")
	}
	s.attach(s.root)
	s.mounted = true

	msgs := make(chan clientMsg, 16)
	dead := make(chan *websocket.Conn, 4)
	pump := func(c *websocket.Conn) {
		go func() {
			for {
				var m clientMsg
				if err := c.ReadJSON(&m); err != nil {
					dead <- c
					return
				}
				m.from = c
				msgs <- m
			}
		}()
	}
	// a resuming client whose last-applied seq still matches gets a bare ack,
	// its tree, scroll, focus and row caches are intact and a remount would
	// discard them. anything else gets a full mount
	adopt := func(c *websocket.Conn, resumeSeq int) {
		if s.conn != nil {
			_ = s.conn.Close()
		}
		// events are small, a message near this limit is a bug or an attack
		c.SetReadLimit(1 << 20)
		s.conn = c
		s.drop.reset() // a drop cut by the old socket can't finish on this one
		if resumeSeq >= 0 && resumeSeq == s.seq {
			if err := c.WriteJSON(map[string]any{"t": wire.MsgResume, "seq": s.seq}); err != nil {
				log.Info().Msgf("caution: session %d resume ack failed: %v", s.sid, err)
			}
		} else {
			s.sendMount()
		}
		pump(c)
	}

	adopt(first, -1)
	var expire <-chan time.Time
	for {
		select {
		case m := <-msgs:
			s.safely("event handler", func() { s.dispatch(m) })
			s.flush()
		case fn := <-s.updates:
			s.safely("update", fn)
			s.flush()
		case a := <-s.conns:
			expire = nil
			log.Info().Msgf("caution: session %d resumed", s.sid)
			adopt(a.conn, a.seq)
			s.flush() // cookies set while the client was away
			// window may have changed size while the client was away
			if a.vw > 0 && (a.vw != s.vw || a.vh != s.vh) {
				s.vw, s.vh = a.vw, a.vh
				if s.onResize != nil {
					s.onResize(s.vw, s.vh)
					s.flush()
				}
			}
		case c := <-dead:
			if c != s.conn {
				continue // pump of an already-replaced connection
			}
			_ = s.conn.Close()
			s.conn = nil
			s.drop.reset()
			expire = time.After(s.grace)
			log.Info().Msgf("caution: session %d disconnected - resumable for %s", s.sid, s.grace)
		case <-expire:
			return
		case <-s.closing:
			return
		}
	}
}

// recover barrier: a panicking handler logs a stack and surfaces a crash
// dialog, this session and every other one keeps running
func (s *Session) safely(what string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Warn().Msgf("caution: session %d panic in %s: %v\n%s", s.sid, what, r, debug.Stack())
			s.showCrash(fmt.Sprint(r))
		}
	}()
	fn()
}

func (s *Session) showCrash(msg string) {
	if s.root == nil || !s.mounted {
		return
	}
	var dlg *Node
	dlg = Dialog("Internal error").CardSize(540, 190).OnDismiss(func() { dlg.Remove() })
	dlg.Kids(
		Label("a server-side handler panicked; the app may be in an inconsistent state").
			FontSize(13).Color("$inkDim").
			Anchor(A{Left: Px(20), Top: Px(14), Right: Px(20)}),
		Label(msg).FontSize(12).Mono().Color("#ff6b6b").Selectable().
			Anchor(A{Left: Px(20), Top: Px(44), Right: Px(20)}),
		Button("Dismiss").
			Anchor(A{Right: Px(20), Bottom: Px(16)}).
			OnClick(func() { dlg.Remove() }),
	)
	s.root.Add(dlg)
}

func crashTree(msg string) *Node {
	return Panel().Kids(
		Label("caution: the app failed to mount").FontSize(15).Weight(600).
			Anchor(A{Left: Px(24), Top: Px(24)}),
		Label(msg).FontSize(13).Mono().Color("#ff6b6b").Selectable().
			Anchor(A{Left: Px(24), Top: Px(54), Right: Px(24)}),
	)
}

func (s *Session) sendMount() {
	s.ops = nil
	// bump even with no connection, the tree has diverged from anything a
	// client ever saw
	s.seq++
	if s.conn == nil {
		return
	}
	msg := map[string]any{"t": wire.MsgMount, "seq": s.seq, "sid": s.token, "root": s.root.toJSON(), "fileCap": s.maxDropBytes}
	if s.focus != nil && s.focus.sess == s {
		msg["focus"] = s.focus.id
	}
	if s.theme != nil {
		msg["theme"] = s.theme
	}
	if s.sounds != nil {
		msg["sounds"] = s.sounds
	}
	if len(s.loops) > 0 {
		msg["loops"] = s.loops
	}
	if s.metrics != nil {
		msg["metrics"] = s.metrics
	}
	if s.title != "" {
		msg["title"] = s.title
	}
	if s.motion != "" {
		msg["motion"] = s.motion
	}
	if s.menu != nil {
		msg["menu"] = s.menu
	}
	if s.keys != nil {
		msg["keys"] = s.keys
	}
	if s.cookies != nil {
		msg["cookies"] = s.cookies
	}
	if s.preloads != nil || s.soundPreloads != nil {
		res := map[string]any{}
		if s.preloads != nil {
			res["images"] = s.preloads
		}
		if s.soundPreloads != nil {
			res["sounds"] = s.soundPreloads
		}
		msg["resources"] = res
	}
	if err := s.conn.WriteJSON(msg); err != nil {
		log.Info().Msgf("caution: session %d mount write failed: %v", s.sid, err)
		return
	}
	s.cookies = nil
}

// replaces the whole tree and remounts the client, for tree-as-data flows
// like loading a UI document or hot-reloading a design
// must run on the session goroutine (a handler or an Update closure)
func (s *Session) SetRoot(root *Node) {
	if s.root != nil {
		s.detach(s.root)
	}
	s.nodes = map[int]*Node{}
	s.nextID = 0
	s.root = root
	s.attach(root)
	s.sendMount()
}

// token bucket, burst 240, refill 120/s. human input stays far below that
// (client debouncing keeps even scroll storms near 10/s) so only floods trip it
func (s *Session) allowEvent() bool {
	now := time.Now()
	if s.evLast.IsZero() {
		s.evTokens = 240
	} else {
		s.evTokens = math.Min(240, s.evTokens+now.Sub(s.evLast).Seconds()*120)
	}
	s.evLast = now
	if s.evTokens < 1 {
		return false
	}
	s.evTokens--
	return true
}

func (s *Session) dispatch(m clientMsg) {
	if m.T != wire.MsgEvent {
		return
	}
	// a drop is many chunks, the byte cap bounds it instead
	if m.Ev != wire.EvFileDrop && !s.allowEvent() {
		log.Debug().Msgf("caution: session %d event flood - dropping %s", s.sid, m.Ev)
		return
	}
	// session-level events carry node id 0, no widget owns them
	if m.ID == 0 {
		switch m.Ev {
		case wire.EvResize:
			v, _ := m.Value.(map[string]any)
			w, _ := v["w"].(float64)
			h, _ := v["h"].(float64)
			if w > 0 && h > 0 {
				s.vw, s.vh = w, h
				if s.onResize != nil {
					s.onResize(w, h)
				}
			}
		case wire.EvMenu:
			id, _ := m.Value.(float64)
			log.Trace().Msgf("caution: session %d <- menu pick %d", s.sid, int(id))
			if fn := s.menuHandlers[int(id)]; fn != nil {
				fn()
			}
		case wire.EvKey:
			id, _ := m.Value.(float64)
			log.Trace().Msgf("caution: session %d <- key combo %d", s.sid, int(id))
			if fn := s.keyHandlers[int(id)]; fn != nil {
				fn()
			}
		case wire.EvAux:
			b, _ := m.Value.(float64)
			log.Trace().Msgf("caution: session %d <- mouse button %d", s.sid, int(b))
			if s.onAux != nil {
				s.onAux(int(b))
			}
		case wire.EvFullscreen:
			on, _ := m.Value.(bool)
			s.fullscreen = on
			if s.onFullscreen != nil {
				s.onFullscreen(on)
			}
		case wire.EvFocus:
			focused, _ := m.Value.(bool)
			s.unfocused = !focused
			if s.onFocus != nil {
				s.onFocus(focused)
			}
		case wire.EvFileDrag:
			over, _ := m.Value.(bool)
			if s.onFileDrag != nil {
				s.onFileDrag(over)
			}
		case wire.EvFileDrop:
			if m.from != s.conn {
				break // queued before a reconnect, its drop was already discarded
			}
			v, _ := m.Value.(map[string]any)
			if files, x, y := s.drop.apply(v, s.maxDropBytes); files != nil && s.onFileDrop != nil {
				s.onFileDrop(files, x, y)
			}
		}
		return
	}
	n := s.nodes[m.ID]
	log.Trace().Msgf("caution: session %d <- %s node=%d value=%v", s.sid, m.Ev, m.ID, m.Value)
	if n == nil {
		return // stale event for a node removed by an in-flight patch
	}
	if m.Seq < n.born {
		// fired before the client ever saw this node: the id was recycled by a
		// remount (SetRoot resets the id space) and the event belongs to whatever
		// wore it in the old tree. delivering it would click a widget the user
		// never touched
		log.Debug().Msgf("caution: session %d dropped stale %s for node %d (event seq %d < node birth %d)",
			s.sid, m.Ev, m.ID, m.Seq, n.born)
		return
	}
	switch m.Ev {
	case wire.EvClick:
		if n.onClick != nil {
			n.onClick()
		}
	case wire.EvSpanClick:
		if n.onSpanClick != nil {
			action, _ := m.Value.(string)
			n.onSpanClick(action)
		}
	case wire.EvToggle:
		if v, ok := m.Value.(map[string]any); ok {
			// tree disclosure {row, key}. the node owns expansion: flip, reflatten,
			// refresh the client's window, then tell the app
			if key, _ := v["key"].(string); key != "" && n.typ == wire.Tree {
				n.toggleTreeRow(key)
			}
			break
		}
		checked, _ := m.Value.(bool)
		// local echo: the client already shows the new state, sync our copy
		// silently instead of patching it back
		n.props["checked"] = checked
		if n.onToggle != nil {
			n.onToggle(checked)
		}
	case wire.EvInput, wire.EvCommit:
		if f, ok := m.Value.(float64); ok {
			// numeric input/commit means a slider, same events, same echo rule
			n.props["value"] = f
			if m.Ev == wire.EvInput {
				if n.onSlide != nil {
					n.onSlide(f)
				}
			} else if n.onSlideEnd != nil {
				n.onSlideEnd(f)
			}
			break
		}
		v, _ := m.Value.(string)
		n.props["value"] = v // silent local-echo sync
		if m.Ev == wire.EvInput {
			if n.onInput != nil {
				n.onInput(v)
			}
		} else if n.onCommit != nil {
			n.onCommit(v)
		}
	case wire.EvSubmit:
		v, _ := m.Value.(string)
		n.props["value"] = v // silent local-echo sync
		if n.onSubmit != nil {
			n.onSubmit(v)
		}
	case wire.EvTab:
		v, _ := m.Value.(map[string]any)
		text, _ := v["value"].(string)
		shift, _ := v["shift"].(bool)
		n.props["value"] = text // silent local-echo sync
		if n.onTab != nil && !n.onTab(shift, text, toInt(v["caret"])) {
			dir := "1"
			if shift {
				dir = "-1"
			}
			s.queueCmd(n.id, wire.CmdTabOut, dir)
		}
	case wire.EvVisibleRange:
		v, _ := m.Value.(map[string]any)
		start, end := toInt(v["start"]), toInt(v["end"])
		n.lastStart, n.lastEnd, n.hasRange = start, end, true
		if end >= start {
			if rows, meta := n.rowsWindow(start, end); rows != nil {
				s.queueRows(n.id, start, rows, meta, false)
			}
		}
	case wire.EvSort:
		v, _ := m.Value.(map[string]any)
		key, _ := v["key"].(string)
		asc, _ := v["asc"].(bool)
		n.props["sortKey"] = key // silent local-echo sync
		if asc {
			n.props["sortDir"] = "asc"
		} else {
			n.props["sortDir"] = "desc"
		}
		if n.onSort != nil {
			n.onSort(key, asc)
		}
	case wire.EvCellActivate:
		v, _ := m.Value.(map[string]any)
		if n.onCellActivate != nil {
			key, _ := v["key"].(string)
			col, _ := v["col"].(string)
			val, _ := v["value"].(string)
			n.onCellActivate(toInt(v["row"]), key, col, val)
		}
	case wire.EvRowSelect:
		if v, ok := m.Value.(map[string]any); ok {
			// keyed table: the key is the selection identity, the index is just where
			// the row sat at click time
			idx := toInt(v["row"])
			key, _ := v["key"].(string)
			n.props["selectedKey"] = key // silent local-echo sync
			if n.onRowSelectKey != nil {
				n.onRowSelectKey(key, idx)
			}
			if n.onRowSelect != nil {
				n.onRowSelect(idx)
			}
			break
		}
		idx := toInt(m.Value)
		n.props["selected"] = idx // silent local-echo sync
		if n.onRowSelect != nil {
			n.onRowSelect(idx)
		}
	case wire.EvRowActivate:
		// no prop sync, the preceding row-select already did it
		if v, ok := m.Value.(map[string]any); ok {
			idx := toInt(v["row"])
			key, _ := v["key"].(string)
			if n.onRowActivateKey != nil {
				n.onRowActivateKey(key, idx)
			}
			if n.onRowActivate != nil {
				n.onRowActivate(idx)
			}
			break
		}
		if n.onRowActivate != nil {
			n.onRowActivate(toInt(m.Value))
		}
	case wire.EvSelect:
		idx := toInt(m.Value)
		n.props["selected"] = idx // silent local-echo sync
		if n.onSelect != nil {
			n.onSelect(idx)
		}
	case wire.EvDismiss:
		if n.onDismiss != nil {
			n.onDismiss()
		}
	case wire.EvContext:
		if fn := n.contextHandlers[toInt(m.Value)]; fn != nil {
			fn()
		}
	case wire.EvColResize:
		if v, ok := m.Value.(map[string]any); ok && n.onColResize != nil {
			key, _ := v["key"].(string)
			width, _ := v["width"].(float64)
			n.onColResize(key, width)
		}
	case wire.EvSplitResize:
		pos, _ := m.Value.(float64)
		n.props["pos"] = pos // silent local-echo sync
		if n.onSplitResize != nil {
			n.onSplitResize(pos)
		}
	case wire.EvPick:
		if v, ok := m.Value.(map[string]any); ok && n.onPick != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			// target resolves through the node table: the client sends the id, the app
			// gets the live node (nil when the id is 0 or gone)
			n.onPick(x, y, s.nodes[toInt(v["target"])])
		}
	case wire.EvDrag:
		if v, ok := m.Value.(map[string]any); ok && n.onDragTo != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			n.onDragTo(x, y)
		}
	case wire.EvDrop:
		if v, ok := m.Value.(map[string]any); ok && n.onDrop != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			n.onDrop(x, y)
		}
	case wire.EvWheel:
		if v, ok := m.Value.(map[string]any); ok && n.onWheel != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			dx, _ := v["dx"].(float64)
			dy, _ := v["dy"].(float64)
			n.onWheel(x, y, dx, dy)
		}
	case wire.EvRPick:
		if v, ok := m.Value.(map[string]any); ok && n.onRPick != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			n.onRPick(x, y)
		}
	case wire.EvRDrag:
		if v, ok := m.Value.(map[string]any); ok && n.onRDrag != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			n.onRDrag(x, y)
		}
	case wire.EvRDrop:
		if v, ok := m.Value.(map[string]any); ok && n.onRDrop != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			n.onRDrop(x, y)
		}
	}
}

func (s *Session) flush() {
	s.queuePendingFocus()
	if len(s.ops) == 0 && (len(s.cookies) == 0 || s.conn == nil) {
		return
	}
	s.seq++
	if s.conn == nil {
		// disconnected: drop the ops (a resume that needs them remounts from the
		// current tree) but still count the batch, resume acks compare seqs to see
		// if anything changed while the client was away
		s.ops = nil
		return
	}
	for _, c := range s.cookies {
		s.ops = append(s.ops, map[string]any{"op": wire.OpCookie, "cookie": c})
	}
	msg := map[string]any{"t": wire.MsgPatch, "seq": s.seq, "ops": s.ops}
	s.ops = nil
	if err := s.conn.WriteJSON(msg); err != nil {
		log.Info().Msgf("caution: session %d write failed: %v", s.sid, err)
		return
	}
	s.cookies = nil
}

func (s *Session) attach(n *Node) {
	if n.id == 0 {
		s.nextID++
		n.id = s.nextID
	}
	// the next message sent carries this node to the client (s.seq only
	// advances on actual sends, so this holds across disconnects)
	n.born = s.seq + 1
	n.sess = s
	s.nodes[n.id] = n
	if n.wantsFocus {
		n.wantsFocus = false
		s.focus = n
		s.focusPending = true
	}
	for _, k := range n.kids {
		s.attach(k)
	}
}

func (s *Session) detach(n *Node) {
	delete(s.nodes, n.id)
	n.sess = nil
	if s.focus == n {
		s.focus = nil
	}
	for _, k := range n.kids {
		s.detach(k)
	}
}

func (s *Session) queueSet(id int, k string, v any) {
	s.ops = append(s.ops, map[string]any{"op": wire.OpSet, "id": id, "p": map[string]any{k: v}})
}

func (s *Session) queuePendingFocus() {
	if !s.focusPending {
		return
	}
	s.focusPending = false
	if s.focus != nil && s.focus.sess == s {
		s.queueCmd(s.focus.id, wire.CmdFocus, nil)
	}
}

func (s *Session) queueCmd(id int, cmd wire.Cmd, value any) {
	op := map[string]any{"op": wire.OpCmd, "id": id, "cmd": cmd}
	if value != nil {
		op["value"] = value
	}
	s.ops = append(s.ops, op)
}

func (s *Session) queueInsert(parent, index int, n *Node) {
	s.ops = append(s.ops, map[string]any{"op": wire.OpInsert, "parent": parent, "index": index, "node": n.toJSON()})
}

func (s *Session) queueRemove(id int) {
	s.ops = append(s.ops, map[string]any{"op": wire.OpRemove, "id": id})
}

func (s *Session) queueRows(id, start int, rows [][]string, meta []map[string]any, reset bool) {
	op := map[string]any{"op": wire.OpRows, "id": id, "start": start, "reset": reset, "rows": rows}
	if meta != nil {
		op["meta"] = meta // trees only: {key, d(epth), k(ids), x(panded)} per row
	}
	s.ops = append(s.ops, op)
}

func toInt(v any) int {
	f, _ := v.(float64)
	return int(f)
}
