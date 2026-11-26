package caution

import (
	"crypto/rand"
	"encoding/base64"
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
	onVisible    func(visible bool)
	hidden       bool
	onFileDrop   func(files []File, x, y float64)
	onFileDrag   func(over bool)
	drop         dropAssembly
	maxDropBytes int64

	preloads      []string
	soundPreloads []string
	fonts         []fontFamily
	fontUI        string
	fontMono      string
	fontFallback  []string
	sounds        map[string]string
	loops         []string

	cookies []string

	focus        *Node
	focusPending bool

	vw, vh   float64
	onResize func(w, h float64)

	evTokens float64
	evLast   time.Time

	identity any
	req      *http.Request
}

func (s *Session) Identity() any { return s.identity }

func (s *Session) Request() *http.Request { return s.req }

type Options struct {
	Authorize       func(r *http.Request) (any, error)
	AllowedOrigins  []string
	TLSCert, TLSKey string
	Instance        string
	ResumeGrace     time.Duration
	MaxDropBytes    int64
}

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
	from  *websocket.Conn
}

var (
	sessionCounter atomic.Int64
	registry       sync.Map
)

func newToken(instance string) string {
	if instance == "" {
		return randToken()
	}
	return instance + "." + randToken()
}

func ResumeInstance(r *http.Request) string {
	instance, _, ok := strings.Cut(r.URL.Query().Get("resume"), ".")
	if !ok {
		return ""
	}
	return instance
}

func randToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func Serve(addr string, mount MountFunc) error {
	return ServeOpts(addr, mount, Options{})
}

var wsOnce sync.Once

func registerWS(mount MountFunc, opts Options) {
	wsOnce.Do(func() { http.HandleFunc("/ws", wsHandler(mount, opts)) })
}

func ServeListener(l net.Listener, mount MountFunc, opts Options) error {
	registerWS(mount, opts)
	return http.Serve(l, nil)
}

func ServeOpts(addr string, mount MountFunc, opts Options) error {
	registerWS(mount, opts)
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		if opts.TLSCert != "" || opts.TLSKey != "" {
			return errors.New("caution: TLS over a unix socket is unsupported (the filesystem is the access control)")
		}
		// a stale socket file from a previous run blocks Listen
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
			token:        newToken(opts.Instance),
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

// a browser can't see the status of a failed upgrade
func deny(w http.ResponseWriter, r *http.Request, up websocket.Upgrader) {
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	_ = conn.WriteJSON(map[string]any{"t": wire.MsgDenied})
	_ = conn.Close()
}

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
		return false
	}
}

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

func (s *Session) Update(fn func()) {
	select {
	case s.updates <- fn:
	case <-s.done:
	}
}

func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) Close() { s.closed.Do(func() { close(s.closing) }) }

func (s *Session) Viewport() (w, h float64) { return s.vw, s.vh }

func (s *Session) OnResize(fn func(w, h float64)) { s.onResize = fn }

func queryFloat(r *http.Request, key string) float64 {
	f, _ := strconv.ParseFloat(r.URL.Query().Get(key), 64)
	return f
}

func (s *Session) SetTheme(tokens map[string]string) {
	s.theme = tokens
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpTheme, "tokens": tokens})
	}
}

func (s *Session) SetMetrics(tokens map[string]float64) {
	s.metrics = tokens
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpMetrics, "metrics": tokens})
	}
}

func (s *Session) SaveFile(name string, data []byte) {
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpSave, "name": name, "data": base64.StdEncoding.EncodeToString(data)})
	}
}

func (s *Session) WriteClipboard(text string) {
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpClip, "text": text})
	}
}

func (s *Session) SetTitle(title string) {
	s.title = title
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpTitle, "title": title})
	}
}

type Motion = wire.Motion

const (
	MotionSystem  = wire.MotionSystem
	MotionFull    = wire.MotionFull
	MotionReduced = wire.MotionReduced
)

func (s *Session) SetMotion(m Motion) {
	s.motion = m
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpMotion, "motion": m})
	}
}

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

type fontFamily struct {
	Family string   `json:"family"`
	Srcs   []string `json:"srcs"`
}

func (s *Session) AddFont(family string, srcs ...string) {
	i := slices.IndexFunc(s.fonts, func(f fontFamily) bool { return f.Family == family })
	if i < 0 {
		i = len(s.fonts)
		s.fonts = append(s.fonts, fontFamily{Family: family})
	}
	var fresh []string
	for _, src := range srcs {
		if !slices.Contains(s.fonts[i].Srcs, src) {
			s.fonts[i].Srcs = append(s.fonts[i].Srcs, src)
			fresh = append(fresh, src)
		}
	}
	if s.mounted && len(fresh) > 0 {
		s.ops = append(s.ops, map[string]any{"op": wire.OpResource, "fonts": []fontFamily{{Family: family, Srcs: fresh}}})
	}
}

func (s *Session) SetFonts(ui, mono string, fallback ...string) {
	s.fontUI, s.fontMono, s.fontFallback = ui, mono, fallback
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpFonts, "ui": ui, "mono": mono, "fallback": fallback})
	}
}

func (s *Session) SetSounds(tokens map[string]string) {
	s.sounds = tokens
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpSounds, "tokens": tokens})
	}
}

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

func (s *Session) Play(src string) {
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpPlay, "src": src})
	}
}

func (s *Session) Loop(src string) {
	if slices.Contains(s.loops, src) {
		return
	}
	s.loops = append(s.loops, src)
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpPlay, "src": src, "loop": true})
	}
}

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

func (s *Session) OnAux(fn func(button int)) { s.onAux = fn }

func (s *Session) OnFullscreen(fn func(on bool)) { s.onFullscreen = fn }

func (s *Session) Fullscreen() bool { return s.fullscreen }

func (s *Session) OnFocus(fn func(focused bool)) { s.onFocus = fn }

func (s *Session) Focused() bool { return !s.unfocused }

func (s *Session) OnVisible(fn func(visible bool)) { s.onVisible = fn }

func (s *Session) Visible() bool { return !s.hidden }

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
	adopt := func(c *websocket.Conn, resumeSeq int) {
		if s.conn != nil {
			_ = s.conn.Close()
		}
		c.SetReadLimit(1 << 20)
		s.conn = c
		s.drop.reset()
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
			s.flush()
			if a.vw > 0 && (a.vw != s.vw || a.vh != s.vh) {
				s.vw, s.vh = a.vw, a.vh
				if s.onResize != nil {
					s.onResize(s.vw, s.vh)
					s.flush()
				}
			}
		case c := <-dead:
			if c != s.conn {
				continue
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
	if s.fontUI != "" || s.fontMono != "" || len(s.fontFallback) > 0 {
		msg["fonts"] = map[string]any{"ui": s.fontUI, "mono": s.fontMono, "fallback": s.fontFallback}
	}
	if s.preloads != nil || s.soundPreloads != nil || s.fonts != nil {
		res := map[string]any{}
		if s.preloads != nil {
			res["images"] = s.preloads
		}
		if s.soundPreloads != nil {
			res["sounds"] = s.soundPreloads
		}
		if s.fonts != nil {
			res["fonts"] = s.fonts
		}
		msg["resources"] = res
	}
	if err := s.conn.WriteJSON(msg); err != nil {
		log.Info().Msgf("caution: session %d mount write failed: %v", s.sid, err)
		return
	}
	s.cookies = nil
}

func (s *Session) SetRoot(root *Node) {
	if s.root != nil {
		s.detach(s.root)
	}
	s.nodes = map[int]*Node{}
	s.nextID = 0
	s.root = root
	unnumber(root)
	s.attach(root)
	s.sendMount()
}

func unnumber(n *Node) {
	n.id = 0
	for _, k := range n.kids {
		unnumber(k)
	}
}

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
	if m.Ev != wire.EvFileDrop && !s.allowEvent() {
		log.Debug().Msgf("caution: session %d event flood - dropping %s", s.sid, m.Ev)
		return
	}
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
		case wire.EvVisible:
			visible, _ := m.Value.(bool)
			s.hidden = !visible
			if s.onVisible != nil {
				s.onVisible(visible)
			}
		case wire.EvFileDrag:
			over, _ := m.Value.(bool)
			if s.onFileDrag != nil {
				s.onFileDrag(over)
			}
		case wire.EvFileDrop:
			if m.from != s.conn {
				break
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
		return
	}
	if m.Seq < n.born {
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
			if key, _ := v["key"].(string); key != "" && n.typ == wire.Tree {
				n.toggleTreeRow(key)
			}
			break
		}
		checked, _ := m.Value.(bool)
		n.echo("checked", checked)
		if n.onToggle != nil {
			n.onToggle(checked)
		}
	case wire.EvInput, wire.EvCommit:
		if f, ok := m.Value.(float64); ok {
			n.echo("value", f)
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
		n.echo("value", v)
		if m.Ev == wire.EvInput {
			if n.onInput != nil {
				n.onInput(v)
			}
		} else if n.onCommit != nil {
			n.onCommit(v)
		}
	case wire.EvSubmit:
		v, _ := m.Value.(string)
		n.echo("value", v)
		if n.onSubmit != nil {
			n.onSubmit(v)
		}
	case wire.EvNav:
		if n.onNav != nil {
			n.onNav(toInt(m.Value))
		}
	case wire.EvTab:
		v, _ := m.Value.(map[string]any)
		text, _ := v["value"].(string)
		shift, _ := v["shift"].(bool)
		n.echo("value", text)
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
		n.echo("sortKey", key)
		if asc {
			n.echo("sortDir", "asc")
		} else {
			n.echo("sortDir", "desc")
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
			idx := toInt(v["row"])
			key, _ := v["key"].(string)
			n.echo("selectedKey", key)
			if n.onRowSelectKey != nil {
				n.onRowSelectKey(key, idx)
			}
			if n.onRowSelect != nil {
				n.onRowSelect(idx)
			}
			break
		}
		idx := toInt(m.Value)
		n.echo("selected", idx)
		if n.onRowSelect != nil {
			n.onRowSelect(idx)
		}
	case wire.EvRowActivate:
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
		n.echo("selected", idx)
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
	case wire.EvSelContext:
		if v, ok := m.Value.(map[string]any); ok {
			if fn := n.selectionHandlers[toInt(v["id"])]; fn != nil {
				fn(toInt(v["start"]), toInt(v["end"]))
			}
		}
	case wire.EvColResize:
		if v, ok := m.Value.(map[string]any); ok && n.onColResize != nil {
			key, _ := v["key"].(string)
			width, _ := v["width"].(float64)
			n.onColResize(key, width)
		}
	case wire.EvSplitResize:
		pos, _ := m.Value.(float64)
		n.echo("pos", pos)
		if n.onSplitResize != nil {
			n.onSplitResize(pos)
		}
	case wire.EvPick:
		if v, ok := m.Value.(map[string]any); ok && n.onPick != nil {
			x, _ := v["x"].(float64)
			y, _ := v["y"].(float64)
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
	case wire.EvTermInput, wire.EvTermMouse, wire.EvGridSize:
		n.dispatchTerm(m.Ev, m.Value)
	}
}

func (s *Session) flush() {
	s.queuePendingFocus()
	if len(s.ops) == 0 && (len(s.cookies) == 0 || s.conn == nil) {
		return
	}
	s.seq++
	if s.conn == nil {
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
		op["meta"] = meta
	}
	s.ops = append(s.ops, op)
}

func toInt(v any) int {
	f, _ := v.(float64)
	return int(f)
}
