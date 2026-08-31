package proto

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/url"
	"sync"
	"time"

	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/ui"

	"github.com/gorilla/websocket"
)

type serverMsg struct {
	T         string             `json:"t"`
	Seq       int                `json:"seq"`
	Root      *nodeJSON          `json:"root"`
	SID       string             `json:"sid"`
	Theme     map[string]string  `json:"theme"`
	Metrics   map[string]float64 `json:"metrics"`
	Menu      []MenuSpec         `json:"menu"`
	Title     string             `json:"title"`
	Keys      []keySpec          `json:"keys"`
	Resources *resourceSpec      `json:"resources"`
	Ops       []patchOp          `json:"ops"`
}

// resourceSpec is the mount's preload block (the `resource` op inline);
// images today, fonts once the terminal has a font pipeline.
type resourceSpec struct {
	Images []string `json:"images"`
}

// keySpec is one session-registered shortcut.
type keySpec struct {
	ID  int    `json:"id"`
	Key string `json:"key"`
}

// MenuSpec is one top-level menu from the server. The shell realizes it
// natively (NSMenu on macOS).
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

// patchOp is the union of every op shape. Absent fields stay zero.
type patchOp struct {
	Op     string            `json:"op"`
	Parent int               `json:"parent"`
	Index  int               `json:"index"`
	Node   *nodeJSON         `json:"node"`
	ID     int               `json:"id"`
	P      props             `json:"p"`
	Start  int               `json:"start"`
	Reset  bool              `json:"reset"`
	Rows   [][]string        `json:"rows"`
	Tokens map[string]string `json:"tokens"`
	// Metrics is the metric-theme op's token map ("metrics" rather than
	// "tokens": theme already owns that key in this union, with string values).
	Metrics map[string]float64 `json:"metrics"`
	Menu    []MenuSpec         `json:"menu"`
	Title   string             `json:"title"`
	Keys    []keySpec          `json:"keys"`
	Images  []string           `json:"images"`
	Meta    []metaSpec         `json:"meta"`
}

// metaSpec is one tree row's wire meta, parallel to its cells in a rows op.
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
	T     string `json:"t"`
	ID    int    `json:"id"`
	Ev    string `json:"ev"`
	Value any    `json:"value,omitempty"`
	Seq   int    `json:"seq"`
}

// Config wires a Session to its transport and shell.
type Config struct {
	URL string
	// Dialer overrides websocket.DefaultDialer, for unix sockets and in-process
	// pipes route here via NetDial.
	Dialer *websocket.Dialer
	// Wake pokes the event loop (glfw.PostEmptyEvent).
	Wake func()
	// ViewSize reports the current logical window size (for the connect URL).
	ViewSize func() (int, int)
	// SetMenu realizes a server menu spec natively. Called on the Pump
	// thread (the main thread, since Cocoa requires it). nil ignores menus.
	SetMenu func(menus []MenuSpec)
	// SetTitle renames the window (server-driven `title`). Called on the
	// Pump thread. nil ignores titles.
	SetTitle func(title string)
	// Preload warms a resource cache entry (server-driven `resource` op -
	// images today). Called on the Pump thread. nil ignores preloads.
	Preload func(src string)
}

// Session is the native terminal's protocol client: connects, applies
// mount/patch to the widget tree, ships subscribed events back, reconnects
// with backoff, resumes within the process lifetime via the sid the server
// hands out (the browser keeps it in sessionStorage, here it lives in
// memory).
//
// Network I/O runs on goroutines, and everything that touches widgets is queued
// as a closure and drained by Pump() on the main (GL) thread. Wake pokes the
// event loop so a blocked WaitEvents notices.
type Session struct {
	ui     *ui.Ui
	url    string
	dialer *websocket.Dialer
	Wake   func()
	// ViewSize reports the current logical window size (for the connect URL).
	ViewSize func() (int, int)
	setMenu  func(menus []MenuSpec)
	setTitle func(title string)
	preload  func(src string)

	queue chan func()

	mu      sync.Mutex
	conn    *websocket.Conn
	lastSeq int
	sid     string

	mounted bool
	// everMounted flips once any mount happens (main thread only). From then
	// on the tree outlives the socket, disconnects show a banner instead of
	// the status screen.
	everMounted bool
	retry       time.Duration

	resizeAt  time.Time
	resizeW   int
	resizeH   int
	hasResize bool

	store *NodeStore
}

func NewSession(u *ui.Ui, cfg Config) *Session {
	dialer := cfg.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	s := &Session{
		ui:       u,
		url:      cfg.URL,
		dialer:   dialer,
		Wake:     cfg.Wake,
		ViewSize: cfg.ViewSize,
		setMenu:  cfg.SetMenu,
		setTitle: cfg.SetTitle,
		preload:  cfg.Preload,
		queue:    make(chan func(), 256),
		retry:    500 * time.Millisecond,
	}
	s.store = NewNodeStore(s)
	u.OnAux = func(button int) { s.Event(0, "aux", button) }
	s.showStatus(fmt.Sprintf("connecting to %s …", cfg.URL))
	go s.connect()
	return s
}

// Pump runs queued protocol work on the caller's thread (the GL thread).
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
		// Present the last seq we applied: if nothing changed server-side,
		// the resume comes back as a bare ack and our world stays put.
		q.Set("seq", fmt.Sprint(s.lastSeq))
	}
	s.mu.Unlock()
	u.RawQuery = q.Encode()

	conn, _, err := s.dialer.Dial(u.String(), nil)
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
		// A tree we've shown stays up, visible and locally interactive
		// (scroll, selection), under a reconnect banner. Only a session
		// that never mounted falls back to the full status screen.
		if s.everMounted {
			s.showBanner("connection lost - reconnecting…")
		} else {
			s.showStatus(fmt.Sprintf("server unreachable at %s - retrying", s.url))
		}
	})
	time.AfterFunc(d, s.connect)
}

func (s *Session) readLoop(conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			s.mu.Lock()
			current := s.conn == conn
			if current {
				s.conn = nil
			}
			s.mu.Unlock()
			if current {
				s.scheduleRetry()
			}
			return
		}
		var msg serverMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Println("caution: bad server message:", err)
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
	case "mount":
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
		if msg.Menu != nil && s.setMenu != nil {
			s.setMenu(msg.Menu)
		}
		if msg.Title != "" && s.setTitle != nil {
			s.setTitle(msg.Title)
		}
		if msg.Keys != nil {
			s.applyKeys(msg.Keys)
		}
		if msg.Resources != nil {
			s.applyResources(msg.Resources.Images)
		}
		clear(s.store.ByID)
		if msg.Root != nil {
			root := s.store.Build(*msg.Root)
			s.mounted = true
			s.everMounted = true
			s.ui.SetRoot(root)
			s.hideBanner()
		}
	case "resume":
		// Nothing changed while we were away: the tree we're showing is
		// still right, keep everything.
		s.mounted = true
		s.everMounted = true
		s.hideBanner()
	case "patch":
		if !s.mounted {
			return
		}
		// Ops that name the node they touch mark just that node (the paint
		// layer splices everything around it), while ops that reshape the tree or
		// the client's chrome fall back to rebuilding the whole frame.
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

// applyOp applies one op and reports whether the frame after it has to be
// rebuilt whole.
func (s *Session) applyOp(op *patchOp) bool {
	switch op.Op {
	case "set":
		if w, ok := s.store.ByID[op.ID]; ok {
			s.store.Apply(w, op.ID, op.P)
			// The protocol layer knows which node a set op touched,
			// so hand the paint layer a targeted invalidation.
			s.ui.Damage(w)
		}
	case "insert":
		parent, ok := s.store.ByID[op.Parent]
		if !ok || op.Node == nil {
			return true
		}
		s.ui.NoteStructural()
		w := s.store.Build(*op.Node)
		w.Base().Parent = parent
		pk := parent.Base()
		idx := min(op.Index, len(pk.Kids))
		pk.Kids = append(pk.Kids[:idx], append([]ui.Widget{w}, pk.Kids[idx:]...)...)
		return true
	case "remove":
		s.ui.NoteStructural()
		s.store.Remove(op.ID)
		return true
	case "rows":
		if w, ok := s.store.ByID[op.ID].(*ui.TableView); ok {
			w.ApplyRows(op.Start, op.Rows, rowMeta(op.Meta), op.Reset) // marks itself
		}
	case "theme":
		// a patched theme is a switch the user watched happen, so fade it. the
		// mount's theme still snaps, since first paint has no before
		s.ui.AnimateTheme(op.Tokens) // every widget's colors: whole frame
	case "metrics":
		// Geometry snaps (no tween) and invalidates measurement caches along
		// with the frame. See Ui.ApplyMetrics.
		s.ui.ApplyMetrics(op.Metrics)
	case "menu":
		if s.setMenu != nil {
			s.setMenu(op.Menu)
		}
		return true
	case "title":
		if s.setTitle != nil {
			s.setTitle(op.Title)
		}
	case "keys":
		s.applyKeys(op.Keys)
	case "resource":
		s.applyResources(op.Images)
	case "move":
		w, okW := s.store.ByID[op.ID]
		parent, okP := s.store.ByID[op.Parent]
		if !okW || !okP {
			return true
		}
		s.ui.NoteStructural()
		if old := w.Base().Parent; old != nil {
			ok := old.Base()
			for i, c := range ok.Kids {
				if c == w {
					ok.Kids = append(ok.Kids[:i], ok.Kids[i+1:]...)
					break
				}
			}
		}
		w.Base().Parent = parent
		pk := parent.Base()
		idx := min(op.Index, len(pk.Kids))
		pk.Kids = append(pk.Kids[:idx], append([]ui.Widget{w}, pk.Kids[idx:]...)...)
		return true
	}
	return false
}

// applyKeys installs the session's registered shortcuts on the Ui (main
// thread, since handle() runs on Pump).
func (s *Session) applyKeys(keys []keySpec) {
	m := make(map[string]int, len(keys))
	for _, k := range keys {
		m[k.Key] = k.ID
	}
	s.ui.KeyCombos = m
	s.ui.OnKeyCombo = func(id int) { s.Event(0, "key", id) }
}

func (s *Session) applyResources(images []string) {
	if s.preload == nil {
		return
	}
	for _, src := range images {
		s.preload(src)
	}
}

// -- upstream events -------------------------------------------------------------------

// Event implements EventSink. Safe from any goroutine (input debounce timers
// fire off-thread).
func (s *Session) Event(id int, ev string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return
	}
	msg := clientEvent{T: "ev", ID: id, Ev: ev, Value: value, Seq: s.lastSeq}
	if err := s.conn.WriteJSON(msg); err != nil {
		log.Println("caution: event send failed:", err)
	}
}

// NoteResize debounces window resizes into the session-level resize event
// (node id 0, because no widget owns the viewport).
func (s *Session) NoteResize(w, h int) {
	s.resizeW, s.resizeH = w, h
	s.resizeAt = time.Now().Add(250 * time.Millisecond)
	s.hasResize = true
}

func (s *Session) flushResize() {
	if s.hasResize && time.Now().After(s.resizeAt) {
		s.hasResize = false
		s.Event(0, "resize", map[string]any{"w": s.resizeW, "h": s.resizeH})
	}
}

// -- status screen ---------------------------------------------------------------------

// showBanner puts a small client-owned pill over the live tree while
// reconnecting. The tree beneath keeps working locally, and the banner never
// intercepts input.
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

// showStatus is the minimal client-owned screen for the disconnected state.
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
