package caution

import (
	"encoding/json"
	"testing"
)

func liveSession(root *Node) *Session {
	s := &Session{nodes: map[int]*Node{}}
	s.root = root
	s.attach(root)
	s.mounted = true
	return s
}

func opsJSON(t *testing.T, s *Session) string {
	t.Helper()
	b, err := json.Marshal(s.ops)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetPropStreamsSetOp(t *testing.T) {
	lbl := Label("a")
	s := liveSession(Panel().Kids(lbl))
	lbl.SetText("b")
	want := `[{"id":2,"op":"set","p":{"text":"b"}}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
}

func TestAddStreamsInsertWithBuiltSubtree(t *testing.T) {
	s := liveSession(Panel())
	child := VStack().Kids(Label("x"))
	s.root.Add(child)

	if child.id != 2 || child.kids[0].id != 3 {
		t.Fatalf("subtree ids = %d,%d; want 2,3", child.id, child.kids[0].id)
	}
	if s.nodes[2] != child || s.nodes[3] != child.kids[0] {
		t.Fatal("subtree not registered in the id map")
	}
	if child.born != s.seq+1 || child.kids[0].born != s.seq+1 {
		t.Fatalf("born = %d,%d; want %d (the next message sent)", child.born, child.kids[0].born, s.seq+1)
	}
	want := `[{"index":0,"node":{"id":2,"type":"vstack","kids":[{"id":3,"type":"label","p":{"text":"x"}}]},"op":"insert","parent":1}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
}

func TestRemoveStreamsRemoveAndRetiresIds(t *testing.T) {
	lbl := Label("a")
	s := liveSession(Panel().Kids(lbl))
	lbl.Remove()
	if _, ok := s.nodes[2]; ok {
		t.Fatal("removed node still registered")
	}
	if len(s.root.kids) != 0 {
		t.Fatal("removed node still parented")
	}
	want := `[{"id":2,"op":"remove"}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
}

func TestInsertThenSetConvergesOnNewestValue(t *testing.T) {
	s := liveSession(Panel())
	lbl := Label("old")
	s.root.Add(lbl)
	lbl.SetText("new")
	want := `[{"index":0,"node":{"id":2,"type":"label","p":{"text":"new"}},"op":"insert","parent":1},` +
		`{"id":2,"op":"set","p":{"text":"new"}}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
}

func TestClearStreamsRemovePerChild(t *testing.T) {
	s := liveSession(Panel().Kids(Label("a"), Label("b")))
	s.root.Clear()
	want := `[{"id":2,"op":"remove"},{"id":3,"op":"remove"}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
	if len(s.nodes) != 1 {
		t.Fatalf("id map holds %d nodes; want just the root", len(s.nodes))
	}
}

func TestSetValueNowShipsValueAndMarkerTogether(t *testing.T) {
	// The override must be one op: a value op and a marker op arriving as
	// separate patches could apply the value under local echo (dropped) and
	// then force nothing.
	f := TextField("a")
	s := liveSession(Panel().Kids(f))
	f.SetValueNow("$1,000")
	want := `[{"id":2,"op":"set","p":{"overrideSeq":1,"value":"$1,000"}}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
}

func TestSetThemePatchesLiveButRidesMountBefore(t *testing.T) {
	tokens := map[string]string{"accent": "#ff0000"}

	// Before mount: stored for the mount message, no op.
	pre := &Session{nodes: map[int]*Node{}}
	pre.SetTheme(tokens)
	if len(pre.ops) != 0 {
		t.Fatalf("pre-mount SetTheme queued %d ops; want 0", len(pre.ops))
	}
	if pre.theme == nil {
		t.Fatal("pre-mount SetTheme must store tokens for the mount")
	}

	// Mounted: one theme op.
	s := liveSession(Panel())
	s.SetTheme(tokens)
	want := `[{"op":"theme","tokens":{"accent":"#ff0000"}}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
}

func TestSetMetricsPatchesLiveButRidesMountBefore(t *testing.T) {
	tokens := map[string]float64{"radius.control": 0}

	// Before mount: stored for the mount message, no op.
	pre := &Session{nodes: map[int]*Node{}}
	pre.SetMetrics(tokens)
	if len(pre.ops) != 0 {
		t.Fatalf("pre-mount SetMetrics queued %d ops; want 0", len(pre.ops))
	}
	if pre.metrics == nil {
		t.Fatal("pre-mount SetMetrics must store tokens for the mount")
	}

	// Mounted: one metrics op.
	s := liveSession(Panel())
	s.SetMetrics(tokens)
	want := `[{"metrics":{"radius.control":0},"op":"metrics"}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
}

func TestTreeFlattensExpansionAndShipsMeta(t *testing.T) {
	items := []TreeItem{
		{Key: "a", Cells: []string{"a"}, Kids: []TreeItem{
			{Key: "a1", Cells: []string{"a1"}},
			{Key: "a2", Cells: []string{"a2"}, Kids: []TreeItem{{Key: "a2x", Cells: []string{"a2x"}}}},
		}},
		{Key: "b", Cells: []string{"b"}},
	}
	tree := Tree([]Col{{Key: "n", Title: "N"}}, items)
	s := liveSession(Panel().Kids(tree))
	if rc, _ := tree.Prop("rowCount").(int); rc != 2 {
		t.Fatalf("collapsed rowCount = %v; want 2 (roots only)", tree.Prop("rowCount"))
	}

	// With a client window reported, expanding refreshes rows + meta.
	tree.lastStart, tree.lastEnd, tree.hasRange = 0, 9, true
	s.ops = nil
	tree.Expand("a")
	if rc, _ := tree.Prop("rowCount").(int); rc != 4 {
		t.Fatalf("expanded rowCount = %v; want 4 (a, a1, a2, b)", tree.Prop("rowCount"))
	}
	want := `[{"id":2,"op":"set","p":{"rowCount":4}},` +
		`{"id":2,"meta":[{"d":0,"k":true,"key":"a","x":true},{"d":1,"key":"a1"},` +
		`{"d":1,"k":true,"key":"a2"},{"d":0,"key":"b"}],"op":"rows",` +
		`"reset":true,"rows":[["a"],["a1"],["a2"],["b"]],"start":0}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s\nwant %s", got, want)
	}

	// A client toggle collapses it again and fires the app callback.
	var toggled string
	tree.OnRowToggle(func(key string, expanded bool) {
		toggled = key
		if expanded {
			t.Fatalf("toggle of open %q reported expanded=true", key)
		}
	})
	tree.toggleTreeRow("a")
	if rc, _ := tree.Prop("rowCount").(int); rc != 2 || toggled != "a" {
		t.Fatalf("after toggle: rowCount %v, callback key %q; want 2, a", tree.Prop("rowCount"), toggled)
	}
}

func TestSetUniformMergesStoredAndShipsDelta(t *testing.T) {
	pane := Shader(FX{Frag: "vec4 effect(vec2 uv){return vec4(0.0);}",
		Uniforms: map[string]float64{"u_a": 1, "u_b": 2}})
	s := liveSession(Panel().Kids(pane))
	pane.SetUniform("u_a", 3)
	// The wire carries only the delta; the client merges.
	want := `[{"id":2,"op":"set","p":{"uniforms":{"u_a":3}}}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
	// The stored map stays complete, so a remount ships everything.
	u, _ := pane.Prop("uniforms").(map[string]float64)
	if u["u_a"] != 3 || u["u_b"] != 2 {
		t.Fatalf("stored uniforms = %v; want merged u_a:3 u_b:2", u)
	}
}

func TestPreloadDedupsAndFollowsMountContract(t *testing.T) {
	// Before mount: stored for the mount message, no op.
	pre := &Session{nodes: map[int]*Node{}}
	pre.Preload("/a.png")
	if len(pre.ops) != 0 {
		t.Fatalf("pre-mount Preload queued %d ops; want 0", len(pre.ops))
	}
	if len(pre.preloads) != 1 {
		t.Fatal("pre-mount Preload must store srcs for the mount")
	}

	// Mounted: one resource op carrying only the fresh srcs; an
	// all-duplicate call queues nothing.
	s := liveSession(Panel())
	s.Preload("/a.png", "/b.png")
	s.Preload("/a.png")
	want := `[{"images":["/a.png","/b.png"],"op":"resource"}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
}

func TestSetTitleFollowsTheThemeContract(t *testing.T) {
	pre := &Session{nodes: map[int]*Node{}}
	pre.SetTitle("early")
	if len(pre.ops) != 0 || pre.title != "early" {
		t.Fatal("pre-mount SetTitle must store for the mount, not queue an op")
	}

	s := liveSession(Panel())
	s.SetTitle("caution demo")
	want := `[{"op":"title","title":"caution demo"}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
}

func TestEventFloodTripsTheBucket(t *testing.T) {
	fired := 0
	btn := Button("x").OnClick(func() { fired++ })
	s := liveSession(Panel().Kids(btn))
	for i := 0; i < 400; i++ {
		s.dispatch(clientMsg{T: "ev", ID: btn.id, Ev: "click", Seq: 1})
	}
	// Burst capacity is 240 plus whatever refills during the loop - the
	// point is that hundreds of instant events do NOT all land.
	if fired < 200 || fired > 300 {
		t.Fatalf("flood delivered %d events; want ~240 (bucket burst)", fired)
	}
}

func TestFocusAndRevealQueueOneShotOps(t *testing.T) {
	b := Button("x")
	s := liveSession(Panel().Kids(b))
	b.Reveal()
	b.Focus()
	want := `[{"id":2,"op":"set","p":{"revealSeq":1}},{"id":2,"op":"set","p":{"focusSeq":1}}]`
	if got := opsJSON(t, s); got != want {
		t.Fatalf("ops = %s; want %s", got, want)
	}
}
