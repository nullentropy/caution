package caution

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// These tests drive the full wire path - a real HTTP server, a real
// WebSocket, the session goroutine - because the behaviors under test are
// races between messages in flight, and shortcuts would test a different
// program.

type testClient struct {
	t   *testing.T
	c   *websocket.Conn
	url string
}

func dialSession(t *testing.T, mount MountFunc) *testClient {
	t.Helper()
	srv := httptest.NewServer(wsHandler(mount, Options{}))
	t.Cleanup(srv.Close)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?vw=800&vh=600"
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &testClient{t: t, c: c, url: url}
}

// redial reconnects the way a real client resumes: same server, the sid the
// mount handed out, and the last seq applied.
func (tc *testClient) redial(sid string, seq int) {
	tc.t.Helper()
	_ = tc.c.Close()
	c, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("%s&resume=%s&seq=%d", tc.url, sid, seq), nil)
	if err != nil {
		tc.t.Fatalf("redial: %v", err)
	}
	tc.t.Cleanup(func() { _ = c.Close() })
	tc.c = c
}

func (tc *testClient) read() map[string]any {
	tc.t.Helper()
	_ = tc.c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var m map[string]any
	if err := tc.c.ReadJSON(&m); err != nil {
		tc.t.Fatalf("read: %v", err)
	}
	return m
}

func (tc *testClient) event(id int, ev string, seq int) {
	tc.t.Helper()
	if err := tc.c.WriteJSON(map[string]any{"t": "ev", "id": id, "ev": ev, "seq": seq}); err != nil {
		tc.t.Fatalf("event write: %v", err)
	}
}

func (tc *testClient) eventValue(id int, ev string, value any, seq int) {
	tc.t.Helper()
	m := map[string]any{"t": "ev", "id": id, "ev": ev, "value": value, "seq": seq}
	if err := tc.c.WriteJSON(m); err != nil {
		tc.t.Fatalf("event write: %v", err)
	}
}

func seqOf(t *testing.T, m map[string]any) int {
	t.Helper()
	f, ok := m["seq"].(float64)
	if !ok {
		t.Fatalf("message has no seq: %v", m)
	}
	return int(f)
}

// kidIDs reads the root's child ids out of a mount message, the way a real
// client learns them.
func kidIDs(t *testing.T, m map[string]any) []int {
	t.Helper()
	root, ok := m["root"].(map[string]any)
	if !ok {
		t.Fatalf("message has no root: %v", m)
	}
	kids, _ := root["kids"].([]any)
	ids := make([]int, 0, len(kids))
	for _, k := range kids {
		km, _ := k.(map[string]any)
		id, _ := km["id"].(float64)
		ids = append(ids, int(id))
	}
	return ids
}

func expectFire(t *testing.T, ch chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("handler %q fired; want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %q to fire", want)
	}
}

func expectQuiet(t *testing.T, ch chan string) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("unexpected handler fire %q", got)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestClickDelivers(t *testing.T) {
	fired := make(chan string, 8)
	mount := func(s *Session) *Node {
		return Panel().Kids(Button("go").OnClick(func() { fired <- "go" }))
	}
	tc := dialSession(t, mount)
	m := tc.read()
	tc.event(kidIDs(t, m)[0], "click", seqOf(t, m))
	expectFire(t, fired, "go")
}

// A click races the patch that removes its target: the event names an id the
// server no longer knows, and must dissolve without side effects.
func TestClickRacingARemoveIsDropped(t *testing.T) {
	fired := make(chan string, 8)
	sessCh := make(chan *Session, 1)
	var doomed *Node
	mount := func(s *Session) *Node {
		sessCh <- s
		doomed = Button("doomed").OnClick(func() { fired <- "doomed" })
		return Panel().Kids(doomed, Button("safe").OnClick(func() { fired <- "safe" }))
	}
	tc := dialSession(t, mount)
	m := tc.read()
	ids := kidIDs(t, m)
	mountSeq := seqOf(t, m)
	sess := <-sessCh

	done := make(chan struct{})
	sess.Update(func() { doomed.Remove(); close(done) })
	<-done

	// The client hasn't applied the remove patch yet - it clicks the doomed
	// button, then (after applying) the safe one. Same socket, so the server
	// processes them in order: if "safe" fires and "doomed" never did, the
	// stale click was dropped.
	tc.event(ids[0], "click", mountSeq)
	patch := tc.read()
	tc.event(ids[1], "click", seqOf(t, patch))
	expectFire(t, fired, "safe")
	expectQuiet(t, fired)
}

// A resume whose last-applied seq still matches gets a bare ack - the
// client's world is intact, so no remount. Events keep flowing after.
func TestResumeUnchangedAcksWithoutRemount(t *testing.T) {
	fired := make(chan string, 8)
	mount := func(s *Session) *Node {
		return Panel().Kids(Button("go").OnClick(func() { fired <- "go" }))
	}
	tc := dialSession(t, mount)
	m := tc.read()
	sid, _ := m["sid"].(string)
	seq := seqOf(t, m)
	id := kidIDs(t, m)[0]

	tc.redial(sid, seq)
	ack := tc.read()
	if ack["t"] != "resume" {
		t.Fatalf("first message after unchanged resume = %v; want a resume ack", ack["t"])
	}
	if seqOf(t, ack) != seq {
		t.Fatalf("ack seq = %d; want %d", seqOf(t, ack), seq)
	}
	// The kept tree still works: click the button we never re-learned about.
	tc.event(id, "click", seq)
	expectFire(t, fired, "go")
}

// A resume after server-side changes remounts - including changes whose
// patches were dropped while no client was connected.
func TestResumeAfterChangesRemounts(t *testing.T) {
	sessCh := make(chan *Session, 1)
	var lbl *Node
	mount := func(s *Session) *Node {
		sessCh <- s
		lbl = Label("a")
		return Panel().Kids(lbl)
	}
	tc := dialSession(t, mount)
	m := tc.read()
	sid, _ := m["sid"].(string)
	seq := seqOf(t, m)
	sess := <-sessCh

	// Mutate while we're away (the client closes first, so the patch is
	// dropped server-side - and still counts toward the seq).
	_ = tc.c.Close()
	done := make(chan struct{})
	sess.Update(func() { lbl.SetText("b"); close(done) })
	<-done

	tc.redial(sid, seq)
	m2 := tc.read()
	if m2["t"] != "mount" {
		t.Fatalf("first message after changed resume = %v; want a full mount", m2["t"])
	}
	if seqOf(t, m2) <= seq {
		t.Fatalf("remount seq = %d; want > %d", seqOf(t, m2), seq)
	}
}

// A keyed table's row-select carries {row, key}: the key is the selection's
// identity (echoed into selectedKey), the row is where it sat at click time.
func TestKeyedRowSelect(t *testing.T) {
	got := make(chan string, 8)
	var table *Node
	mount := func(s *Session) *Node {
		table = Table([]Col{{Key: "id", Title: "ID"}}, 100).
			RowKey(0).
			OnRowSelectKey(func(key string, row int) { got <- fmt.Sprintf("%s@%d", key, row) })
		return Panel().Kids(table)
	}
	tc := dialSession(t, mount)
	m := tc.read()
	id := kidIDs(t, m)[0]
	if err := tc.c.WriteJSON(map[string]any{
		"t": "ev", "id": id, "ev": "row-select",
		"value": map[string]any{"row": 14, "key": "E00015"}, "seq": seqOf(t, m),
	}); err != nil {
		t.Fatal(err)
	}
	expectFire(t, got, "E00015@14")
	// The echo is silent server state; prop reads happen on the session
	// goroutine like any other tree access.
	done := make(chan string, 1)
	table.sess.Update(func() {
		k, _ := table.Prop("selectedKey").(string)
		done <- k
	})
	if k := <-done; k != "E00015" {
		t.Fatalf("selectedKey echo = %q; want E00015", k)
	}
}

// A click races a SetRoot remount: the id space resets, so the stale click
// arrives wearing an id that now belongs to a different widget. The birth-seq
// guard must drop it; the same click re-fired against the new tree delivers.
func TestClickRacingARemountIsDropped(t *testing.T) {
	fired := make(chan string, 8)
	sessCh := make(chan *Session, 1)
	mount := func(s *Session) *Node {
		sessCh <- s
		return Panel().Kids(Button("refresh").OnClick(func() { fired <- "refresh" }))
	}
	tc := dialSession(t, mount)
	m := tc.read()
	oldID := kidIDs(t, m)[0]
	oldSeq := seqOf(t, m)
	sess := <-sessCh

	done := make(chan struct{})
	sess.Update(func() {
		sess.SetRoot(Panel().Kids(Button("delete everything").OnClick(func() { fired <- "delete" })))
		close(done)
	})
	<-done

	m2 := tc.read()
	newID := kidIDs(t, m2)[0]
	newSeq := seqOf(t, m2)
	if newID != oldID {
		t.Fatalf("test premise broken: expected the remount to recycle id %d, got %d", oldID, newID)
	}

	tc.event(newID, "click", oldSeq) // fired against the old tree - must drop
	tc.event(newID, "click", newSeq) // fired against the new tree - must land
	expectFire(t, fired, "delete")
	expectQuiet(t, fired)
}

func TestTreeToggleRoundTrip(t *testing.T) {
	items := []TreeItem{
		{Key: "a", Cells: []string{"a"}, Kids: []TreeItem{{Key: "a1", Cells: []string{"a1"}}}},
		{Key: "b", Cells: []string{"b"}},
	}
	tc := dialSession(t, func(s *Session) *Node {
		return Panel().Kids(Tree([]Col{{Key: "n", Title: "N"}}, items))
	})
	m := tc.read()
	treeID := kidIDs(t, m)[0]
	seq := seqOf(t, m)

	// The client reports its window, then toggles "a" open.
	if err := tc.c.WriteJSON(map[string]any{
		"t": "ev", "id": treeID, "ev": "visible-range",
		"value": map[string]any{"start": 0, "end": 9}, "seq": seq,
	}); err != nil {
		t.Fatal(err)
	}
	rowsMsg := tc.read() // rows for the collapsed view
	if err := tc.c.WriteJSON(map[string]any{
		"t": "ev", "id": treeID, "ev": "toggle",
		"value": map[string]any{"row": 0, "key": "a"}, "seq": seqOf(t, rowsMsg),
	}); err != nil {
		t.Fatal(err)
	}

	// Expansion answers with rowCount 3 and a reset rows op carrying meta.
	patch := tc.read()
	ops, _ := patch["ops"].([]any)
	var sawCount, sawRows bool
	for _, ov := range ops {
		op, _ := ov.(map[string]any)
		switch op["op"] {
		case "set":
			p, _ := op["p"].(map[string]any)
			if rc, ok := p["rowCount"].(float64); ok && int(rc) == 3 {
				sawCount = true
			}
		case "rows":
			meta, _ := op["meta"].([]any)
			if len(meta) != 3 {
				t.Fatalf("rows meta = %v; want 3 entries", op["meta"])
			}
			m0, _ := meta[0].(map[string]any)
			m1, _ := meta[1].(map[string]any)
			if m0["key"] != "a" || m0["x"] != true || m1["key"] != "a1" || m1["d"] != float64(1) {
				t.Fatalf("meta wrong: %v", meta)
			}
			sawRows = true
		}
	}
	if !sawCount || !sawRows {
		t.Fatalf("toggle patch missing rowCount/rows: %v", patch)
	}
}

func TestCellActivateRoundTrip(t *testing.T) {
	got := make(chan string, 1)
	tc := dialSession(t, func(s *Session) *Node {
		table := Table([]Col{
			{Key: "name", Weight: 1},
			{Key: "ok", Kind: "checkbox", Width: 60},
		}, 3).OnCellActivate(func(row int, _, col, value string) {
			got <- fmt.Sprintf("%d/%s/%s", row, col, value)
		})
		return Panel().Kids(table)
	})
	m := tc.read()
	id := kidIDs(t, m)[0]
	if err := tc.c.WriteJSON(map[string]any{"t": "ev", "id": id, "ev": "cell-activate",
		"value": map[string]any{"row": 2, "key": "", "col": "ok", "value": "true"},
		"seq":   seqOf(t, m)}); err != nil {
		t.Fatal(err)
	}
	expectFire(t, got, "2/ok/true")
}

func TestPreloadRidesMount(t *testing.T) {
	tc := dialSession(t, func(s *Session) *Node {
		s.Preload("/warm.png", "/hot.png") // pre-mount: must ride the mount message
		return Panel()
	})
	m := tc.read()
	if m["t"] != "mount" {
		t.Fatalf("first message = %v; want mount", m["t"])
	}
	res, ok := m["resources"].(map[string]any)
	if !ok {
		t.Fatalf("mount carries no resources block: %v", m)
	}
	imgs, _ := res["images"].([]any)
	if len(imgs) != 2 || imgs[0] != "/warm.png" || imgs[1] != "/hot.png" {
		t.Fatalf("mount resources.images = %v; want [/warm.png /hot.png]", imgs)
	}
}

func TestMetricsRideMount(t *testing.T) {
	tc := dialSession(t, func(s *Session) *Node {
		s.SetMetrics(map[string]float64{"radius.control": 0, "row.height": 24}) // pre-mount
		return Panel()
	})
	m := tc.read()
	if m["t"] != "mount" {
		t.Fatalf("first message = %v; want mount", m["t"])
	}
	mets, ok := m["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("mount carries no metrics block: %v", m)
	}
	if mets["radius.control"] != 0.0 || mets["row.height"] != 24.0 {
		t.Fatalf("mount metrics = %v; want radius.control 0, row.height 24", mets)
	}
}

func TestGlassPickRoundTrip(t *testing.T) {
	// The glass reports {x, y, target}; the SDK resolves target through the
	// session's node table so the app receives the live *Node.
	got := make(chan string, 2)
	var inner *Node
	tc := dialSession(t, func(s *Session) *Node {
		inner = Label("hit me")
		glass := Glass().
			OnPick(func(x, y float64, target *Node) {
				name := "nil"
				if target == inner {
					name = "inner"
				} else if target != nil {
					name = target.Type()
				}
				got <- fmt.Sprintf("pick %g,%g %s", x, y, name)
			}).
			OnDrop(func(x, y float64) { got <- fmt.Sprintf("drop %g,%g", x, y) })
		return Panel().Kids(inner, glass)
	})
	m := tc.read()
	glassID := kidIDs(t, m)[1]
	innerID := kidIDs(t, m)[0]
	if err := tc.c.WriteJSON(map[string]any{"t": "ev", "id": glassID, "ev": "pick",
		"value": map[string]any{"x": 42.0, "y": 17.0, "target": innerID},
		"seq":   seqOf(t, m)}); err != nil {
		t.Fatal(err)
	}
	expectFire(t, got, "pick 42,17 inner")
	if err := tc.c.WriteJSON(map[string]any{"t": "ev", "id": glassID, "ev": "drop",
		"value": map[string]any{"x": 60.0, "y": 20.0},
		"seq":   seqOf(t, m)}); err != nil {
		t.Fatal(err)
	}
	expectFire(t, got, "drop 60,20")
}

func TestGlassPickUnknownTargetIsNil(t *testing.T) {
	got := make(chan string, 1)
	tc := dialSession(t, func(s *Session) *Node {
		return Panel().Kids(Glass().OnPick(func(_, _ float64, target *Node) {
			if target == nil {
				got <- "nil"
			} else {
				got <- target.Type()
			}
		}))
	})
	m := tc.read()
	glassID := kidIDs(t, m)[0]
	if err := tc.c.WriteJSON(map[string]any{"t": "ev", "id": glassID, "ev": "pick",
		"value": map[string]any{"x": 1.0, "y": 2.0, "target": 9999},
		"seq":   seqOf(t, m)}); err != nil {
		t.Fatal(err)
	}
	expectFire(t, got, "nil")
}

func TestAuxButtonsReachTheSession(t *testing.T) {
	fired := make(chan string, 8)
	mount := func(s *Session) *Node {
		s.OnAux(func(b int) { fired <- fmt.Sprintf("aux%d", b) })
		return Panel().Kids(Button("go").OnClick(func() { fired <- "go" }))
	}
	tc := dialSession(t, mount)
	m := tc.read()
	seq := seqOf(t, m)
	tc.eventValue(0, "aux", 4, seq)
	expectFire(t, fired, "aux4")
	tc.eventValue(0, "aux", 5, seq)
	expectFire(t, fired, "aux5")
}

func TestAuxWithoutAHandlerIsDropped(t *testing.T) {
	fired := make(chan string, 8)
	mount := func(_ *Session) *Node {
		return Panel().Kids(Button("go").OnClick(func() { fired <- "go" }))
	}
	tc := dialSession(t, mount)
	m := tc.read()
	seq := seqOf(t, m)
	tc.eventValue(0, "aux", 4, seq)
	expectQuiet(t, fired)
	tc.event(kidIDs(t, m)[0], "click", seq) // still alive
	expectFire(t, fired, "go")
}

func TestFullscreenReachesTheSession(t *testing.T) {
	fired := make(chan string, 8)
	var sess *Session
	mount := func(s *Session) *Node {
		sess = s
		s.OnFullscreen(func(on bool) { fired <- fmt.Sprintf("fullscreen=%v", on) })
		return Panel().Kids(Button("go").OnClick(func() { fired <- "go" }))
	}
	tc := dialSession(t, mount)
	m := tc.read()
	seq := seqOf(t, m)
	tc.eventValue(0, "fullscreen", true, seq)
	expectFire(t, fired, "fullscreen=true")
	tc.eventValue(0, "fullscreen", false, seq)
	expectFire(t, fired, "fullscreen=false")

	done := make(chan bool, 1)
	sess.Update(func() { done <- sess.Fullscreen() })
	if <-done {
		t.Fatal("Fullscreen() still true after the client left fullscreen")
	}
}
