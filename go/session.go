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

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// MountFunc builds a session's initial tree. It runs once per session, not
// once per connection: a session survives disconnects for resumeGrace and is
// re-adopted when the client reconnects with its session id. Captured
// variables are the app's per-session state. Handlers and Session.Update
// closures all run on the session's single goroutine, so that state needs no
// locking.
type MountFunc func(s *Session) *Node

const resumeGrace = 60 * time.Second

// Session is one live app instance. All widget mutation must happen on the
// session goroutine: synchronously inside event handlers, or via Update from
// anywhere else (tickers, watchers, message queues).
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
	mounted bool
	theme   map[string]string
	metrics map[string]float64
	title   string

	menu         []any
	menuHandlers map[int]func()

	keys         []any
	keyHandlers  map[int]func()
	onAux        func(button int)
	onFullscreen func(on bool)
	fullscreen   bool

	preloads      []string
	soundPreloads []string
	sounds        map[string]string
	loops         []string

	vw, vh   float64
	onResize func(w, h float64)

	// Event rate limiting: a token bucket sized well above any human rate, so
	// only floods trip it. See allowEvent.
	evTokens float64
	evLast   time.Time

	identity any
	req      *http.Request
}

// Identity returns whatever Options.Authorize produced for this session's
// original connection, or nil when no Authorize hook is configured.
func (s *Session) Identity() any { return s.identity }

// Request returns the HTTP request that opened the session, for cookies,
// headers, and the remote address. Read-only, and its response writer is
// already gone.
func (s *Session) Request() *http.Request { return s.req }

// Options harden Serve for real deployments. The zero value keeps safe
// defaults (same-origin upgrades, no auth, plain HTTP).
type Options struct {
	// Authorize vets every connection, including resumes, before it joins
	// a session. Return the connection's identity, or an error to reject
	// with 403. On resume the identity must DeepEqual the session's original
	// one, so a leaked session token can't adopt someone else's session.
	// nil allows everyone, with a nil identity.
	Authorize func(r *http.Request) (any, error)
	// AllowedOrigins for the WebSocket upgrade. Empty means same-origin only.
	// "*" allows any origin.
	AllowedOrigins []string
	// TLSCert/TLSKey switch to ListenAndServeTLS when both are set.
	TLSCert, TLSKey string
}

// adoptedConn is a reconnecting socket plus the viewport it reported and
// the last seq the client had applied (-1: none reported).
type adoptedConn struct {
	conn   *websocket.Conn
	vw, vh float64
	seq    int
}

type clientMsg struct {
	T     string `json:"t"`
	ID    int    `json:"id"`
	Ev    string `json:"ev"`
	Value any    `json:"value"`
	Seq   int    `json:"seq"`
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

// Serve runs the caution server with default Options: one WebSocket endpoint
// at /ws. A connection carrying ?resume=<sid> re-adopts its old session if it
// is still alive. Anything else starts a fresh session.
//
// addr is a TCP address (":8787") or "unix:/path/to.sock". A unix socket
// serves the same protocol with nothing listening on any port, for a local
// native terminal (`cmd/term -url unix:/path`).
func Serve(addr string, mount MountFunc) error {
	return ServeOpts(addr, mount, Options{})
}

// wsOnce guards /ws registration: a process may call both ServeOpts and
// ServeListener (a dual web + native binary), and the first registration's
// mount and options win.
var wsOnce sync.Once

// registerWS installs the WebSocket endpoint on the default mux.
func registerWS(mount MountFunc, opts Options) {
	wsOnce.Do(func() { http.HandleFunc("/ws", wsHandler(mount, opts)) })
}

// ServeListener serves the caution app on an existing listener. caution/native
// uses it to run server and terminal in one process over an in-memory pipe.
func ServeListener(l net.Listener, mount MountFunc, opts Options) error {
	registerWS(mount, opts)
	return http.Serve(l, nil)
}

// ServeOpts is Serve with deployment hardening. See Options.
func ServeOpts(addr string, mount MountFunc, opts Options) error {
	registerWS(mount, opts)
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		if opts.TLSCert != "" || opts.TLSKey != "" {
			return errors.New("caution: TLS over a unix socket is unsupported (the filesystem is the access control)")
		}
		// A stale socket file from a previous run blocks Listen, while a live one
		// fails the subsequent Listen anyway, so removal is safe.
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
		var identity any
		if opts.Authorize != nil {
			id, err := opts.Authorize(r)
			if err != nil {
				log.Info().Msgf("caution: connection rejected (%s): %v", r.RemoteAddr, err)
				http.Error(w, "forbidden", http.StatusForbidden)
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
		if tok := r.URL.Query().Get("resume"); tok != "" {
			if v, ok := registry.Load(tok); ok {
				s := v.(*Session)
				if !reflect.DeepEqual(s.identity, identity) {
					// A resume token is not a credential: it only re-adopts a
					// session minted for the same identity.
					log.Info().Msgf("caution: session %d resume denied - identity mismatch", s.sid)
				} else {
					clientSeq := -1
					if q := r.URL.Query().Get("seq"); q != "" {
						if n, err := strconv.Atoi(q); err == nil {
							clientSeq = n
						}
					}
					select {
					case s.conns <- adoptedConn{conn: conn, vw: vw, vh: vh, seq: clientSeq}:
						return // adopted by the existing session
					case <-s.done:
						// session expired between lookup and adoption, so fall through
					}
				}
			}
		}
		s := &Session{
			sid:      int(sessionCounter.Add(1)),
			token:    randToken(),
			conns:    make(chan adoptedConn, 1),
			nodes:    map[int]*Node{},
			updates:  make(chan func(), 64),
			done:     make(chan struct{}),
			vw:       vw,
			vh:       vh,
			identity: identity,
			req:      r,
		}
		registry.Store(s.token, s)
		log.Info().Msgf("caution: session %d connected (%s)", s.sid, r.RemoteAddr)
		go s.run(mount, conn)
	}
}

// originChecker builds the upgrade origin policy. nil (empty list) defers to
// gorilla's default, which requires the Origin host to match the request
// host, meaning same-origin.
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

// Update schedules fn onto the session goroutine and flushes the resulting
// ops as one patch. Safe to call from any goroutine. A closed session drops
// the update.
func (s *Session) Update(fn func()) {
	select {
	case s.updates <- fn:
	case <-s.done:
	}
}

// Done is closed when the session ends (client gone past the resume grace).
// Use it to stop tickers.
func (s *Session) Done() <-chan struct{} { return s.done }

// Viewport returns the client's last-reported window size in logical px
// (0,0 if the client never reported one). Valid from mount onward: the
// initial size rides the connect URL.
func (s *Session) Viewport() (w, h float64) { return s.vw, s.vh }

// OnResize registers a callback for viewport changes. It runs on the session
// goroutine, so widget mutation inside it is safe. Resize reports are
// client-debounced (~250ms).
func (s *Session) OnResize(fn func(w, h float64)) { s.onResize = fn }

func queryFloat(r *http.Request, key string) float64 {
	f, _ := strconv.ParseFloat(r.URL.Query().Get(key), 64)
	return f
}

// SetTheme pushes design tokens ("bg", "ink", "accent", ...) as color strings.
func (s *Session) SetTheme(tokens map[string]string) {
	s.theme = tokens
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": "theme", "tokens": tokens})
	}
}

// SetMetrics pushes metric tokens ("radius.control", "row.height", ...) as
// logical-px numbers, the geometry half of theming. It restyles the widget
// vocabulary (square vs rounded, compact vs comfortable) without per-widget
// props. Unknown tokens are ignored by terminals, and per-node props still
// override. MetricsCompact and MetricsComfortable are ready-made maps.
func (s *Session) SetMetrics(tokens map[string]float64) {
	s.metrics = tokens
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": "metrics", "metrics": tokens})
	}
}

// SetTitle names the window: the browser tab's title, the native window's
// titlebar.
func (s *Session) SetTitle(title string) {
	s.title = title
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": "title", "title": title})
	}
}

// Preload warms the client's resource caches ahead of first use. Today that
// means images (URLs or data: URIs). It is for images that are not in the tree
// yet, such as a next screen or a hover swap; anything already mounted starts
// loading at first paint on its own. Same contract as SetTheme: rides the
// mount, patches live. Repeated sources are dropped, so calling it from a
// rebuild loop costs nothing.
func (s *Session) Preload(srcs ...string) {
	var fresh []string
	for _, src := range srcs {
		if !slices.Contains(s.preloads, src) {
			s.preloads = append(s.preloads, src)
			fresh = append(fresh, src)
		}
	}
	if s.mounted && len(fresh) > 0 {
		s.ops = append(s.ops, map[string]any{"op": "resource", "images": fresh})
	}
}

// SetSounds sets the gesture table: which WAV plays when the user presses,
// toggles, selects, opens, closes or types, fired by the client at the moment
// of the gesture
func (s *Session) SetSounds(tokens map[string]string) {
	s.sounds = tokens
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": "sounds", "tokens": tokens})
	}
}

// PreloadSounds decodes WAV sources on the client ahead of their first Play
func (s *Session) PreloadSounds(srcs ...string) {
	var fresh []string
	for _, src := range srcs {
		if !slices.Contains(s.soundPreloads, src) {
			s.soundPreloads = append(s.soundPreloads, src)
			fresh = append(fresh, src)
		}
	}
	if s.mounted && len(fresh) > 0 {
		s.ops = append(s.ops, map[string]any{"op": "resource", "sounds": fresh})
	}
}

// Play plays a WAV once on the client. src is a URL or data: URI
func (s *Session) Play(src string) {
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": "play", "src": src})
	}
}

// Loop plays src on repeat until Stop
func (s *Session) Loop(src string) {
	if slices.Contains(s.loops, src) {
		return
	}
	s.loops = append(s.loops, src)
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": "play", "src": src, "loop": true})
	}
}

// Stop stops every playing instance of src
func (s *Session) Stop(src string) {
	s.loops = slices.DeleteFunc(s.loops, func(l string) bool { return l == src })
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": "stop", "src": src})
	}
}

// StopAll stops every sound
func (s *Session) StopAll() {
	s.loops = nil
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": "stop"})
	}
}

// OnKey registers a session-wide keyboard shortcut: "cmd+k",
// "ctrl+shift+p", "alt+enter": one or more of cmd/ctrl/alt/shift plus a
// key. A combo fires only when the focused widget declines the chord, so a
// text field's Cmd+Z stays undo. In the native terminal, a menu item's key
// equivalent for the same chord wins (the OS routes it first). The handler
// runs on the session goroutine like any other.
func (s *Session) OnKey(combo string, fn func()) {
	if s.keyHandlers == nil {
		s.keyHandlers = map[int]func(){}
	}
	id := len(s.keyHandlers) + 1
	s.keys = append(s.keys, map[string]any{"id": id, "key": normalizeCombo(combo)})
	s.keyHandlers[id] = fn
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": "keys", "keys": s.keys})
	}
}

// OnAux handles the mouse's extra buttons, 4 (back) and 5 (forward). Both
// terminals swallow the press whether or not an app takes it, so the browser
// never navigates its own history out from under the app.
func (s *Session) OnAux(fn func(button int)) { s.onAux = fn }

// OnFullscreen handles the window entering and leaving the system's own
// fullscreen: the green button and Cmd+Ctrl+F on macOS, the browser's fullscreen
// API on the web. A custom titlebar usually wants to restyle there, since
// fullscreen has no traffic lights to leave room for.
func (s *Session) OnFullscreen(fn func(on bool)) { s.onFullscreen = fn }

// Fullscreen reports the client's last known fullscreen state.
func (s *Session) Fullscreen() bool { return s.fullscreen }

// normalizeCombo canonicalizes modifier order and case so the client's
// event-derived string always matches: cmd+ctrl+alt+shift+<key>.
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
				msgs <- m
			}
		}()
	}
	// adopt binds a connection. A resuming client whose last-applied seq still
	// matches ours gets a bare ack, since its tree, scroll, focus and row caches
	// are all still correct and a remount would discard them. Anything else gets
	// a full mount from the current tree.
	adopt := func(c *websocket.Conn, resumeSeq int) {
		if s.conn != nil {
			_ = s.conn.Close()
		}
		// Events are small, so a client message anywhere near this limit is a bug
		// or an attack.
		c.SetReadLimit(1 << 20)
		s.conn = c
		if resumeSeq >= 0 && resumeSeq == s.seq {
			if err := c.WriteJSON(map[string]any{"t": "resume", "seq": s.seq}); err != nil {
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
			// The window may have changed size while the client was away.
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
			expire = time.After(resumeGrace)
			log.Info().Msgf("caution: session %d disconnected - resumable for %s", s.sid, resumeGrace)
		case <-expire:
			return
		}
	}
}

// safely runs app code with a recover barrier: a panicking handler logs a
// stack trace and surfaces a crash dialog, but this session and every other
// one keeps running.
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
	// The bump happens even with no connection, because the tree has diverged
	// from anything a client ever saw
	s.seq++
	if s.conn == nil {
		return
	}
	msg := map[string]any{"t": "mount", "seq": s.seq, "sid": s.token, "root": s.root.toJSON()}
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
	if s.menu != nil {
		msg["menu"] = s.menu
	}
	if s.keys != nil {
		msg["keys"] = s.keys
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
	}
}

// SetRoot replaces the session's entire tree and remounts the client, for
// tree-as-data workflows like loading a new UI document or hot-reloading a
// design. Must run on the session goroutine: inside an event handler or an
// Update closure.
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

// allowEvent is the session's token bucket: burst 240, refill 120/s. Human
// interaction stays far below that (client-side debouncing keeps even scroll
// storms near 10/s), so only floods trip it.
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
	if m.T != "ev" {
		return
	}
	if !s.allowEvent() {
		log.Debug().Msgf("caution: session %d event flood - dropping %s", s.sid, m.Ev)
		return
	}
	// Session-level events carry node id 0 because no widget owns them.
	if m.ID == 0 {
		switch m.Ev {
		case "resize":
			v, _ := m.Value.(map[string]any)
			w, _ := v["w"].(float64)
			h, _ := v["h"].(float64)
			if w > 0 && h > 0 {
				s.vw, s.vh = w, h
				if s.onResize != nil {
					s.onResize(w, h)
				}
			}
		case "menu":
			id, _ := m.Value.(float64)
			log.Trace().Msgf("caution: session %d <- menu pick %d", s.sid, int(id))
			if fn := s.menuHandlers[int(id)]; fn != nil {
				fn()
			}
		case "key":
			id, _ := m.Value.(float64)
			log.Trace().Msgf("caution: session %d <- key combo %d", s.sid, int(id))
			if fn := s.keyHandlers[int(id)]; fn != nil {
				fn()
			}
		case "aux":
			b, _ := m.Value.(float64)
			log.Trace().Msgf("caution: session %d <- mouse button %d", s.sid, int(b))
			if s.onAux != nil {
				s.onAux(int(b))
			}
		case "fullscreen":
			on, _ := m.Value.(bool)
			s.fullscreen = on
			if s.onFullscreen != nil {
				s.onFullscreen(on)
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
		// The client fired this before it ever saw this node: the id was
		// recycled by a remount (SetRoot resets the id space), and the event
		// belongs to whatever wore the id in the previous tree. Delivering it
		// would click a widget the user never touched.
		log.Debug().Msgf("caution: session %d dropped stale %s for node %d (event seq %d < node birth %d)",
			s.sid, m.Ev, m.ID, m.Seq, n.born)
		return
	}
	switch m.Ev {
	case "click":
		if n.onClick != nil {
			n.onClick()
		}
	case "toggle":
		if v, ok := m.Value.(map[string]any); ok {
			// Tree disclosure: {row, key}. The node owns expansion: flip,
			// reflatten, refresh the client's window, then tell the app.
			if key, _ := v["key"].(string); key != "" && n.typ == "tree" {
				n.toggleTreeRow(key)
			}
			break
		}
		checked, _ := m.Value.(bool)
		// Local echo: the client already shows the new state, so sync our copy
		// silently rather than patching it back.
		n.props["checked"] = checked
		if n.onToggle != nil {
			n.onToggle(checked)
		}
	case "input", "commit":
		if f, ok := m.Value.(float64); ok {
			// Numeric input/commit: a slider. Same events, same echo rule.
			n.props["value"] = f
			if m.Ev == "input" {
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
		if m.Ev == "input" {
			if n.onInput != nil {
				n.onInput(v)
			}
		} else if n.onCommit != nil {
			n.onCommit(v)
		}
	case "visible-range":
		v, _ := m.Value.(map[string]any)
		start, end := toInt(v["start"]), toInt(v["end"])
		n.lastStart, n.lastEnd, n.hasRange = start, end, true
		if end >= start {
			if rows, meta := n.rowsWindow(start, end); rows != nil {
				s.queueRows(n.id, start, rows, meta, false)
			}
		}
	case "sort":
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
	case "cell-activate":
		v, _ := m.Value.(map[string]any)
		if n.onCellActivate != nil {
			key, _ := v["key"].(string)
			col, _ := v["col"].(string)
			val, _ := v["value"].(string)
			n.onCellActivate(toInt(v["row"]), key, col, val)
		}
	case "row-select":
		if v, ok := m.Value.(map[string]any); ok {
			// Keyed table: selection identity is the key, and the index is just
			// where the row sat at click time.
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
	case "row-activate":
		// No prop sync here: the row-select that preceded it already did.
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
	case "select":
		idx := toInt(m.Value)
		n.props["selected"] = idx // silent local-echo sync
		if n.onSelect != nil {
			n.onSelect(idx)
		}
	case "dismiss":
		if n.onDismiss != nil {
			n.onDismiss()
		}
	case "context":
		if fn := n.contextHandlers[toInt(m.Value)]; fn != nil {
			fn()
		}
	case "col-resize":
		if v, ok := m.Value.(map[string]any); ok && n.onColResize != nil {
			key, _ := v["key"].(string)
			width, _ := v["width"].(float64)
			n.onColResize(key, width)
		}
	case "split-resize":
		pos, _ := m.Value.(float64)
		n.props["pos"] = pos // silent local-echo sync
		if n.onSplitResize != nil {
			n.onSplitResize(pos)
		}
	case "pick":
		if v, ok := m.Value.(map[string]any); ok && n.onPick != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			// target resolves through the session's node table: the client
			// reports the id, the app gets the live node (nil when the id is
			// 0 or already gone).
			n.onPick(x, y, s.nodes[toInt(v["target"])])
		}
	case "drag":
		if v, ok := m.Value.(map[string]any); ok && n.onDragTo != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			n.onDragTo(x, y)
		}
	case "drop":
		if v, ok := m.Value.(map[string]any); ok && n.onDrop != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			n.onDrop(x, y)
		}
	case "wheel":
		if v, ok := m.Value.(map[string]any); ok && n.onWheel != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			dx, _ := v["dx"].(float64)
			dy, _ := v["dy"].(float64)
			n.onWheel(x, y, dx, dy)
		}
	case "rpick":
		if v, ok := m.Value.(map[string]any); ok && n.onRPick != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			n.onRPick(x, y)
		}
	case "rdrag":
		if v, ok := m.Value.(map[string]any); ok && n.onRDrag != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			n.onRDrag(x, y)
		}
	case "rdrop":
		if v, ok := m.Value.(map[string]any); ok && n.onRDrop != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
			n.onRDrop(x, y)
		}
	}
}

func (s *Session) flush() {
	if len(s.ops) == 0 {
		return
	}
	if s.conn == nil {
		// Disconnected: drop the ops (a resume that needs them remounts from
		// current tree state) but still count the batch, because resume acks compare
		// seqs to decide whether anything changed while the client was away.
		s.seq++
		s.ops = nil
		return
	}
	s.seq++
	msg := map[string]any{"t": "patch", "seq": s.seq, "ops": s.ops}
	s.ops = nil
	if err := s.conn.WriteJSON(msg); err != nil {
		log.Info().Msgf("caution: session %d write failed: %v", s.sid, err)
	}
}

func (s *Session) attach(n *Node) {
	if n.id == 0 {
		s.nextID++
		n.id = s.nextID
	}
	// The message that will carry this node to the client is the next one
	// sent (s.seq only advances on actual sends, so this holds across
	// disconnects too).
	n.born = s.seq + 1
	n.sess = s
	s.nodes[n.id] = n
	for _, k := range n.kids {
		s.attach(k)
	}
}

func (s *Session) detach(n *Node) {
	delete(s.nodes, n.id)
	n.sess = nil
	for _, k := range n.kids {
		s.detach(k)
	}
}

func (s *Session) queueSet(id int, k string, v any) {
	s.ops = append(s.ops, map[string]any{"op": "set", "id": id, "p": map[string]any{k: v}})
}

func (s *Session) queueSetMulti(id int, p map[string]any) {
	s.ops = append(s.ops, map[string]any{"op": "set", "id": id, "p": p})
}

// queueInsert queues the subtree for the next flush. The op references the
// live node rather than a snapshot, so it serializes at flush time with the
// newest props and any sets queued after it in the same handler are just
// idempotent re-assertions. All mutation happens on the session goroutine, so
// nothing races the flush.
func (s *Session) queueInsert(parent, index int, n *Node) {
	s.ops = append(s.ops, map[string]any{"op": "insert", "parent": parent, "index": index, "node": n.toJSON()})
}

func (s *Session) queueRemove(id int) {
	s.ops = append(s.ops, map[string]any{"op": "remove", "id": id})
}

func (s *Session) queueRows(id, start int, rows [][]string, meta []map[string]any, reset bool) {
	op := map[string]any{"op": "rows", "id": id, "start": start, "reset": reset, "rows": rows}
	if meta != nil {
		op["meta"] = meta // trees only: {key, d(epth), k(ids), x(panded)} per row
	}
	s.ops = append(s.ops, op)
}

func toInt(v any) int {
	f, _ := v.(float64)
	return int(f)
}
