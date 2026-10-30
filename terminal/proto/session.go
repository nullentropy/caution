package proto

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/url"
	"sync"
	"time"

	"github.com/nullentropy/caution/terminal/gfx"
	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"

	"github.com/gorilla/websocket"
)

type serverMsg struct {
	T         wire.Msg           `json:"t"`
	Seq       int                `json:"seq"`
	Root      *nodeJSON          `json:"root"`
	SID       string             `json:"sid"`
	Focus     int                `json:"focus"`
	Theme     map[string]string  `json:"theme"`
	Sounds    map[string]string  `json:"sounds"`
	Loops     []string           `json:"loops"`
	Metrics   map[string]float64 `json:"metrics"`
	Menu      []MenuSpec         `json:"menu"`
	Title     string             `json:"title"`
	Keys      []keySpec          `json:"keys"`
	Resources *resourceSpec      `json:"resources"`
	Motion    wire.Motion        `json:"motion"`
	Cookies   []string           `json:"cookies"`
	Ops       []patchOp          `json:"ops"`
}

// the mount's preload block, the `resource` op inline
type resourceSpec struct {
	Images []string `json:"images"`
	Sounds []string `json:"sounds"`
}

type keySpec struct {
	ID  int    `json:"id"`
	Key string `json:"key"`
}

// top-level menu, the shell realizes it natively (NSMenu on macOS)
type MenuSpec struct {
	Title string     `json:"title"`
	Items []MenuItem `json:"items"`
}

type MenuItem struct {
	ID    int        `json:"id"`
	Title string     `json:"title"`
	Key   string     `json:"key"`
	Sep   bool       `json:"sep"`
	Items []MenuItem `json:"items"`
}

// union of every op shape, absent fields stay zero
type patchOp struct {
	Op     wire.Op           `json:"op"`
	Parent int               `json:"parent"`
	Index  int               `json:"index"`
	Node   *nodeJSON         `json:"node"`
	ID     int               `json:"id"`
	P      props             `json:"p"`
	Start  int               `json:"start"`
	Reset  bool              `json:"reset"`
	Rows   [][]string        `json:"rows"`
	Tokens map[string]string `json:"tokens"`
	// metric-theme tokens. not "tokens" because theme already owns that key in
	// this union, with string values
	Metrics map[string]float64 `json:"metrics"`
	Menu    []MenuSpec         `json:"menu"`
	Title   string             `json:"title"`
	Keys    []keySpec          `json:"keys"`
	Images  []string           `json:"images"`
	Sounds  []string           `json:"sounds"`
	Src     string             `json:"src"`
	Loop    bool               `json:"loop"`
	Meta    []metaSpec         `json:"meta"`
	Cmd     wire.Cmd           `json:"cmd"`
	Value   string             `json:"value"`
	Motion  wire.Motion        `json:"motion"`
	Cookie  string             `json:"cookie"`
}

// one tree row's meta, parallel to its cells in a rows op
type metaSpec struct {
	Key string `json:"key"`
	D   int    `json:"d"`
	K   bool   `json:"k"`
	X   bool   `json:"x"`
}

func rowMeta(in []metaSpec) []ui.RowMeta {
	if in == nil {
		return nil
	}
	out := make([]ui.RowMeta, len(in))
	for i, m := range in {
		out[i] = ui.RowMeta{Key: m.Key, Depth: m.D, Expandable: m.K, Expanded: m.X}
	}
	return out
}

type clientEvent struct {
	T     wire.Msg   `json:"t"`
	ID    int        `json:"id"`
	Ev    wire.Event `json:"ev"`
	Value any        `json:"value,omitempty"`
	Seq   int        `json:"seq"`
}

type Conn interface {
	ReadMessage() ([]byte, error)
	WriteMessage(data []byte) error
	Close() error
}

type Config struct {
	URL string
	// overrides websocket.DefaultDialer. unix sockets and in-process pipes
	// route through its NetDial
	Dialer *websocket.Dialer
	// replaces gorilla entirely, the browser dials through the page's WebSocket
	Dial func(url string) (Conn, error)
	// resumes a session from a previous page load, the browser keeps it in
	// sessionStorage
	SID string
	// fires on every mount with the server's session id
	OnMount func(sid string)
	// glfw.PostEmptyEvent
	Wake func()
	// logical window size, for the connect URL
	ViewSize func() (int, int)
	// called on the Pump thread (main, Cocoa requires it). nil ignores menus
	SetMenu func(menus []MenuSpec)
	// server-driven `title`, Pump thread. nil ignores titles
	SetTitle func(title string)
	// warms a resource cache entry (`resource` op, images today). Pump thread
	// nil ignores preloads
	Preload func(src string)
	// decode ahead of time, play once. nil is silent, as in shot mode
	PreloadSound func(src string)
	Play         func(src string, loop bool)
	Stop         func(src string)
	// stores a Set-Cookie string for every later request to the server. nil
	// ignores cookies
	SetCookie func(raw string)
}

// protocol client: applies mount/patch, ships events, reconnects with
// backoff and resumes via the server's sid (in memory natively, the browser
// keeps it in sessionStorage and passes it back through Config.SID)
//
// network I/O runs on goroutines. anything touching widgets is queued as a
// closure and drained by Pump() on the main (GL) thread, Wake pokes the
// event loop so a blocked WaitEvents notices
type Session struct {
	ui   *ui.UI
	url  string
	dial func(url string) (Conn, error)
	Wake func()
	// logical window size, for the connect URL
	ViewSize     func() (int, int)
	setMenu      func(menus []MenuSpec)
	setTitle     func(title string)
	preload      func(src string)
	preloadSound func(src string)
	play         func(src string, loop bool)
	stop         func(src string)
	setCookie    func(raw string)
	onMount      func(sid string)

	queue chan func()

	mu      sync.Mutex
	conn    Conn
	lastSeq int
	sid     string

	fullscreen bool
	unfocused  bool

	mounted bool
	// set by a denied message, under mu. the server refused us so retrying
	// can't help
	denied bool
	// main thread only. once set the tree outlives the socket and disconnects
	// show a banner instead of the status screen
	everMounted bool
	retry       time.Duration

	resizeAt  time.Time
	resizeW   int
	resizeH   int
	hasResize bool

	store *NodeStore
}

func NewSession(u *ui.UI, cfg Config) *Session {
	dial := cfg.Dial
	if dial == nil {
		dialer := cfg.Dialer
		if dialer == nil {
			dialer = websocket.DefaultDialer
		}
		dial = func(url string) (Conn, error) {
			c, _, err := dialer.Dial(url, nil)
			if err != nil {
				return nil, err
			}
			return gorillaConn{c}, nil
		}
	}
	s := &Session{
		ui:           u,
		url:          cfg.URL,
		dial:         dial,
		Wake:         cfg.Wake,
		ViewSize:     cfg.ViewSize,
		setMenu:      cfg.SetMenu,
		setTitle:     cfg.SetTitle,
		preload:      cfg.Preload,
		preloadSound: cfg.PreloadSound,
		play:         cfg.Play,
		stop:         cfg.Stop,
		setCookie:    cfg.SetCookie,
		onMount:      cfg.OnMount,
		sid:          cfg.SID,
		queue:        make(chan func(), 256),
		retry:        500 * time.Millisecond,
	}
	s.store = NewNodeStore(s)
	u.OnAux = func(button int) { s.Event(0, wire.EvAux, button) }
	if cfg.Play != nil {
		u.PlaySound = func(src string) { cfg.Play(src, false) }
	}
	s.showStatus(fmt.Sprintf("connecting to %s …", cfg.URL))
	go s.connect()
	return s
}

// call on the GL thread
func (s *Session) Pump() {
	for {
		select {
		case fn := <-s.queue:
			fn()
		default:
			s.flushResize()
			return
		}
	}
}

func (s *Session) post(fn func()) {
	s.queue <- fn
	if s.Wake != nil {
		s.Wake()
	}
}

// -- connection ----------------------------------------------------------------------

func (s *Session) connect() {
	u, err := url.Parse(s.url)
	if err != nil {
		s.post(func() { s.showStatus("bad url: " + s.url) })
		return
	}
	q := u.Query()
	if s.ViewSize != nil {
		w, h := s.ViewSize()
		q.Set("vw", fmt.Sprint(w))
		q.Set("vh", fmt.Sprint(h))
	}
	s.mu.Lock()
	if s.sid != "" {
		q.Set("resume", s.sid)
		// last applied seq, if nothing changed server-side the resume is a
		// bare ack and our world stays put
		q.Set("seq", fmt.Sprint(s.lastSeq))
	}
	s.mu.Unlock()
	u.RawQuery = q.Encode()

	conn, err := s.dial(u.String())
	if err != nil {
		s.scheduleRetry()
		return
	}
	s.mu.Lock()
	s.conn = conn
	s.retry = 500 * time.Millisecond
	s.mu.Unlock()

	go s.readLoop(conn)
}

func (s *Session) scheduleRetry() {
	s.mu.Lock()
	d := s.retry
	s.retry = min(s.retry*8/5, 8*time.Second)
	s.mu.Unlock()
	s.post(func() {
		s.mounted = false
		// a tree we've shown stays up and locally interactive (scroll,
		// selection) under a reconnect banner. only a session that never
		// mounted gets the full status screen
		if s.everMounted {
			s.showBanner("connection lost - reconnecting…")
		} else {
			s.showStatus(fmt.Sprintf("server unreachable at %s - retrying", s.url))
		}
	})
	time.AfterFunc(d, s.connect)
}

func (s *Session) readLoop(conn Conn) {
	for {
		data, err := conn.ReadMessage()
		if err != nil {
			s.mu.Lock()
			current := s.conn == conn
			if current {
				s.conn = nil
			}
			retry := current && !s.denied
			s.mu.Unlock()
			if retry {
				s.scheduleRetry()
			}
			return
		}
		var msg serverMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Println("caution: bad server message:", err)
			continue
		}
		if msg.T == wire.MsgDenied {
			s.mu.Lock()
			s.denied = true
			s.mu.Unlock()
			s.post(func() {
				s.mounted = false
				s.hideBanner()
				s.showStatus("access denied")
			})
			continue
		}
		s.post(func() { s.handle(&msg) })
	}
}

// -- message handling (main thread) ----------------------------------------------------

func (s *Session) handle(msg *serverMsg) {
	s.mu.Lock()
	s.lastSeq = msg.Seq
	s.mu.Unlock()
	switch msg.T {
	case wire.MsgMount:
		if msg.SID != "" {
			s.mu.Lock()
			s.sid = msg.SID
			s.mu.Unlock()
		}
		if msg.Theme != nil {
			ui.ApplyTheme(msg.Theme)
		}
		if msg.Metrics != nil {
			s.ui.ApplyMetrics(msg.Metrics)
		}
		s.applySounds(msg.Sounds) // nil is silent and replaces a table a resume left behind
		if s.stop != nil {
			s.stop("")
		}
		if s.play != nil {
			for _, src := range msg.Loops {
				s.play(src, true)
			}
		}
		if msg.Menu != nil && s.setMenu != nil {
			s.setMenu(msg.Menu)
		}
		if msg.Title != "" && s.setTitle != nil {
			s.setTitle(msg.Title)
		}
		if msg.Keys != nil {
			s.applyKeys(msg.Keys)
		}
		s.ui.SetMotion(motion(msg.Motion)) // absent is system, replacing a resumed choice
		if s.setCookie != nil {
			for _, c := range msg.Cookies {
				s.setCookie(c)
			}
		}
		if msg.Resources != nil {
			s.applyResources(msg.Resources.Images, msg.Resources.Sounds)
		}
		s.store.Reset()
		if msg.Root != nil {
			root := s.store.Build(*msg.Root)
			s.mounted = true
			s.everMounted = true
			s.ui.SetRoot(root)
			if w, ok := s.store.ByID[msg.Focus]; ok {
				w.Base().RequestFocus()
			}
			s.hideBanner()
			if s.onMount != nil {
				s.onMount(msg.SID)
			}
		}
		s.mu.Lock()
		fs, unfocused := s.fullscreen, s.unfocused
		s.mu.Unlock()
		if fs {
			s.Event(0, wire.EvFullscreen, true)
		}
		// a server assumes focused, so only the exception needs saying
		if unfocused {
			s.Event(0, wire.EvFocus, false)
		}
	case wire.MsgResume:
		// nothing changed while we were away, keep everything
		s.mounted = true
		s.everMounted = true
		s.hideBanner()
	case wire.MsgPatch:
		if !s.mounted {
			return
		}
		// ops naming one node mark just that node (paint splices around it),
		// ops that reshape the tree or the client's chrome rebuild the whole frame
		full := false
		for i := range msg.Ops {
			if s.applyOp(&msg.Ops[i]) {
				full = true
			}
		}
		if full {
			s.ui.Invalidate()
		}
	}
}

// true when the frame has to be rebuilt whole
func (s *Session) applyOp(op *patchOp) bool {
	switch op.Op {
	case wire.OpSet:
		if w, ok := s.store.ByID[op.ID]; ok {
			s.store.Apply(w, op.ID, op.P)
			// targeted invalidation, we know which node a set op touched
			s.ui.Damage(w)
		}
	case wire.OpCmd:
		if w, ok := s.store.ByID[op.ID]; ok {
			s.command(w, op.Cmd, op.Value)
		}
	case wire.OpInsert:
		parent, ok := s.store.ByID[op.Parent]
		if !ok || op.Node == nil {
			return true
		}
		s.ui.NoteStructural()
		w := s.store.Build(*op.Node)
		w.Base().Parent = parent
		insertKid(parent, w, op.Index)
		return true
	case wire.OpRemove:
		s.ui.NoteStructural()
		s.store.Remove(op.ID)
		return true
	case wire.OpRows:
		if w, ok := s.store.ByID[op.ID].(*ui.TableView); ok {
			w.ApplyRows(op.Start, op.Rows, rowMeta(op.Meta), op.Reset) // marks itself
		}
	case wire.OpTheme:
		// a patched theme is a switch the user watched, so fade it. the mount's
		// theme still snaps since first paint has no before
		s.ui.AnimateTheme(op.Tokens) // every widget's colors, whole frame
	case wire.OpMetrics:
		// geometry snaps (no tween) and invalidates measurement caches with the
		// frame, see UI.ApplyMetrics
		s.ui.ApplyMetrics(op.Metrics)
	case wire.OpMenu:
		if s.setMenu != nil {
			s.setMenu(op.Menu)
		}
		return true
	case wire.OpTitle:
		if s.setTitle != nil {
			s.setTitle(op.Title)
		}
	case wire.OpKeys:
		s.applyKeys(op.Keys)
	case wire.OpMotion:
		s.ui.SetMotion(motion(op.Motion))
	case wire.OpCookie:
		if s.setCookie != nil {
			s.setCookie(op.Cookie)
		}
	case wire.OpResource:
		s.applyResources(op.Images, op.Sounds)
	case wire.OpPlay:
		if s.play != nil {
			s.play(op.Src, op.Loop)
		}
	case wire.OpStop:
		if s.stop != nil {
			s.stop(op.Src)
		}
	case wire.OpSounds:
		s.applySounds(op.Tokens)
	case wire.OpMove:
		w, okW := s.store.ByID[op.ID]
		parent, okP := s.store.ByID[op.Parent]
		if !okW || !okP || isAncestor(w, parent) {
			return true
		}
		s.ui.NoteStructural()
		if old := w.Base().Parent; old != nil {
			ob := old.Base()
			for i, c := range ob.Kids {
				if c == w {
					ob.Kids = append(ob.Kids[:i], ob.Kids[i+1:]...)
					break
				}
			}
		}
		w.Base().Parent = parent
		insertKid(parent, w, op.Index)
		return true
	}
	return false
}

// i is clamped to the valid range
func insertKid(parent, w ui.Widget, i int) {
	pk := parent.Base()
	i = max(0, min(i, len(pk.Kids)))
	pk.Kids = append(pk.Kids[:i], append([]ui.Widget{w}, pk.Kids[i:]...)...)
}

// a is n or one of n's ancestors. moving a node under itself would make a
// cycle
func isAncestor(a, n ui.Widget) bool {
	for ; n != nil; n = n.Base().Parent {
		if n == a {
			return true
		}
	}
	return false
}

func (s *Session) command(w ui.Widget, cmd wire.Cmd, value string) {
	switch cmd {
	case wire.CmdFocus:
		w.Base().RequestFocus()
	case wire.CmdReveal:
		w.Base().RequestReveal()
	case wire.CmdClear:
		switch t := w.(type) {
		case *ui.TextField:
			t.ForceClear()
		case *ui.TextArea:
			t.ForceClear()
		}
	case wire.CmdValue:
		switch t := w.(type) {
		case *ui.TextField:
			t.ForceValue(value)
		case *ui.TextArea:
			t.ForceValue(value)
		}
	}
}

// main thread, handle() runs on Pump
func (s *Session) applyKeys(keys []keySpec) {
	m := make(map[string]int, len(keys))
	for _, k := range keys {
		m[k.Key] = k.ID
	}
	s.ui.KeyCombos = m
	s.ui.OnKeyCombo = func(id int) { s.Event(0, wire.EvKey, id) }
}

// installs the gesture table and decodes every source now
func (s *Session) applySounds(tokens map[string]string) {
	s.ui.SetSounds(tokens)
	if s.preloadSound != nil {
		for _, src := range tokens {
			s.preloadSound(src)
		}
	}
}

func (s *Session) applyResources(images, sounds []string) {
	if s.preload != nil {
		for _, src := range images {
			s.preload(src)
		}
	}
	if s.preloadSound != nil {
		for _, src := range sounds {
			s.preloadSound(src)
		}
	}
}

// -- upstream events -------------------------------------------------------------------

// safe from any goroutine, input debounce timers fire off-thread
func (s *Session) Event(id int, ev wire.Event, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return
	}
	msg := clientEvent{T: wire.MsgEvent, ID: id, Ev: ev, Value: value, Seq: s.lastSeq}
	data, err := json.Marshal(msg)
	if err == nil {
		err = s.conn.WriteMessage(data)
	}
	if err != nil {
		log.Println("caution: event send failed:", err)
	}
}

type gorillaConn struct{ *websocket.Conn }

func (c gorillaConn) ReadMessage() ([]byte, error) {
	_, data, err := c.Conn.ReadMessage()
	return data, err
}

func (c gorillaConn) WriteMessage(data []byte) error {
	return c.Conn.WriteMessage(websocket.TextMessage, data)
}

func MenubarSpec(menus []MenuSpec) []ui.MenuSpec {
	var conv func(items []MenuItem) []ui.MenuItemSpec
	conv = func(items []MenuItem) []ui.MenuItemSpec {
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

// remembered so a client that reconnects while fullscreen tells the app at
// the next mount
func (s *Session) NoteFullscreen(on bool) {
	s.mu.Lock()
	s.fullscreen = on
	s.mu.Unlock()
	s.Event(0, wire.EvFullscreen, on)
}

// main thread
func (s *Session) NoteFocus(focused bool) {
	s.ui.SetWindowActive(focused)
	s.mu.Lock()
	changed := s.unfocused == focused
	s.unfocused = !focused
	s.mu.Unlock()
	if changed {
		s.Event(0, wire.EvFocus, focused)
	}
}

// debounced into the session-level resize event, node id 0 since no widget
// owns the viewport
func (s *Session) NoteResize(w, h int) {
	s.resizeW, s.resizeH = w, h
	s.resizeAt = time.Now().Add(250 * time.Millisecond)
	s.hasResize = true
}

func (s *Session) flushResize() {
	if s.hasResize && time.Now().After(s.resizeAt) {
		s.hasResize = false
		s.Event(0, wire.EvResize, map[string]any{"w": s.resizeW, "h": s.resizeH})
	}
}

// -- status screen ---------------------------------------------------------------------

// client-owned pill over the live tree while reconnecting. the tree keeps
// working locally and the banner never takes input
func (s *Session) showBanner(text string) {
	f := gfx.NewFont(12, gfx.FontOpts{Weight: 500})
	m := s.ui.Measure(f, text)
	const padL, dot, gap, padR = 12, 8, 8, 14
	root := ui.NewPanel() // transparent, full-viewport carrier
	pill := ui.NewPanel()
	root.Add(pill)
	pill.Width = f32p(float32(math.Ceil(float64(padL + dot + gap + m.Width + padR))))
	pill.Height = f32p(30)
	pill.Anchors = &ui.Anchors{CenterX: f32p(0), Top: f32p(10)}
	pill.Bg = ui.Tok("panelAlt")
	pill.Radius = gfx.CornerRadius(15)
	pill.BorderColor = ui.Tok("edge")
	pill.DropShadow = &ui.Shadow{Blur: 14, Color: gfx.WithAlpha(gfx.Black, 0.4), Dy: 3}
	dotW := ui.NewPanel()
	pill.Add(dotW)
	dotW.Width = f32p(dot)
	dotW.Height = f32p(dot)
	dotW.Radius = gfx.CornerRadius(dot / 2)
	dotW.Bg = ui.Tok("accent")
	dotW.Anchors = &ui.Anchors{Left: f32p(padL), CenterY: f32p(0)}
	label := ui.NewLabel(text, f, ui.Tok("inkDim"))
	pill.Add(label)
	label.Anchors = &ui.Anchors{Left: f32p(padL + dot + gap), CenterY: f32p(0)}
	s.ui.Banner = root
	s.ui.Invalidate()
}

func (s *Session) hideBanner() {
	if s.ui.Banner != nil {
		s.ui.Banner = nil
		s.ui.Invalidate()
	}
}

// minimal client-owned screen for the disconnected state
func (s *Session) showStatus(text string) {
	root := ui.NewPanel()
	card := ui.NewPanel()
	root.Add(card)
	card.Anchors = &ui.Anchors{CenterX: f32p(0), CenterY: f32p(-40)}
	card.Width = f32p(560)
	card.Height = f32p(120)
	card.Bg = ui.Tok("panelAlt")
	card.BorderColor = ui.Tok("edgeSoft")
	card.Radius = gfx.CornerRadius(12)
	title := ui.NewLabel("caution", gfx.NewFont(18, gfx.FontOpts{Weight: 600}), nil)
	title.Anchors = &ui.Anchors{Left: f32p(24), Top: f32p(24)}
	card.Add(title)
	detail := ui.NewLabel(text, gfx.NewFont(13, gfx.FontOpts{}), ui.Tok("inkDim"))
	detail.Anchors = &ui.Anchors{Left: f32p(24), Top: f32p(58)}
	detail.Selectable = true
	card.Add(detail)
	s.ui.SetRoot(root)
}

func f32p(v float32) *float32 { return &v }

func motion(m wire.Motion) ui.Motion {
	switch m {
	case wire.MotionFull:
		return ui.MotionFull
	case wire.MotionReduced:
		return ui.MotionReduced
	}
	return ui.MotionSystem
}
