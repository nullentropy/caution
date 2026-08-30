package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/nullentropy/caution/go"
)

// Fixture scenes are deterministic on purpose: no animated shaders (the two
// captures see different clocks), no focused text field (caret blink phase
// differs), no images (async load). Wrapped labels break on explicit
// newlines with generous width slack, so the two font stacks - Go fonts
// natively, system fonts in the browser - cannot disagree about line counts.
// Panel geometry comes from anchors and fixed widths rather than text
// measurement wherever a font difference could move a box edge.

type scene struct {
	name  string
	theme string
	build func(s *caution.Session) *caution.Node
	// metrics restyles the scene's widget chrome (nil = terminal defaults).
	// The metrics scene runs off-default, so a terminal that ignores a token or
	// resolves it differently drifts here.
	metrics map[string]float64
}

var scenes = []scene{
	{"controls-midnight", "Midnight", controlsScene, nil},
	{"controls-light", "Light", controlsScene, nil},
	{"table-midnight", "Midnight", tableScene, nil},
	{"table-light", "Light", tableScene, nil},
	{"tree-midnight", "Midnight", treeScene, nil},
	{"tree-light", "Light", treeScene, nil},
	{"layout-midnight", "Midnight", layoutScene, nil},
	{"layout-light", "Light", layoutScene, nil},
	{"text-midnight", "Midnight", textScene, nil},
	{"text-light", "Light", textScene, nil},
	{"metrics-square", "Midnight", metricsScene, squareMetrics},
}

// squareMetrics is the anti-default look: sharp corners, chunky hairlines,
// compact geometry. Every value below must be read by both terminals from
// the same token, or the scene fails.
var squareMetrics = map[string]float64{
	"radius.control": 0,
	"radius.popover": 0,
	"radius.panel":   0,
	"border.width":   2,

	"control.height":     26,
	"control.padX":       10,
	"row.height":         22,
	"table.headerHeight": 24,
	"table.cellPadX":     8,
	"checkbox.size":      14,
	"slider.thumb":       6,
	"slider.track":       2,
	"dialog.titleHeight": 34,
	"space.pad":          6,
	"space.gap":          6,
}

// current is the scene the next mount builds. Captures run sequentially -
// the pointer only moves between them - but mounts happen on connection
// goroutines, so the handoff is atomic.
var current atomic.Pointer[scene]

func mount(s *caution.Session) *caution.Node {
	sc := current.Load()
	s.SetTheme(themes[sc.theme])
	if sc.metrics != nil {
		s.SetMetrics(sc.metrics)
	}
	s.SetTitle("goldens - " + sc.name)
	return sc.build(s)
}

// The demo's Midnight and Light token maps, copied: scenes must ship a full
// theme so both terminals resolve identical colors.
var themes = map[string]map[string]string{
	"Midnight": {
		"bg": "#16181d", "ink": "#e8eaf0", "inkDim": "#9aa3b2", "inkFaint": "#6b7280",
		"panel": "#262b33", "panelAlt": "#20252c", "panelInset": "#1b1f26",
		"edge": "#3a4150", "edgeSoft": "#333a46", "titlebar": "#2e343e",
		"control": "#2e343e", "controlEdge": "#4a5262", "accent": "#4f8cff",
	},
	"Light": {
		"bg": "#eef0f4", "ink": "#1b1e24", "inkDim": "#5b6472", "inkFaint": "#8a93a2",
		"panel": "#ffffff", "panelAlt": "#f7f8fa", "panelInset": "#eceef2",
		"edge": "#d4d9e0", "edgeSoft": "#e2e6ec", "titlebar": "#e8ebef",
		"control": "#f1f2f5", "controlEdge": "#c6cdd7", "accent": "#2563eb",
	},
}

// controlsScene shows the widget vocabulary: one of everything that draws
// chrome, at rest, with fixed widths. It also carries a menu spec: the
// browser renders the in-window menu bar always, the native shot under
// -menubar - the closed bar is a parity surface (titles, heights, the
// tree pushed down by MenubarH).
func controlsScene(s *caution.Session) *caution.Node {
	s.SetMenu(
		caution.Menu{Title: "File", Items: []caution.MenuItem{
			{Title: "New Fixture", Key: "n"},
			{Sep: true},
			{Title: "Export…", Key: "shift+cmd+e"},
		}},
		caution.Menu{Title: "View", Items: []caution.MenuItem{
			{Title: "Midnight"},
			{Title: "Light"},
		}},
	)
	left := caution.VStack().Gap(14).
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(14), Right: caution.Px(16)}).Kids(
		caution.Label("Controls").FontSize(15).Weight(600),
		caution.HStack().Gap(10).Kids(
			caution.Button("Save").Primary().W(96),
			caution.Button("Cancel").W(96),
			caution.Button("Apply").W(96),
		),
		caution.Checkbox("Enable telemetry", true),
		caution.Checkbox("Auto-update", false),
		caution.Radio([]string{"Small", "Medium", "Large"}, 1),
		caution.TabBar([]string{"General", "Advanced", "About"}, 0),
		caution.Select([]string{"Midnight", "Violet", "Light"}, 0).W(200),
		caution.Slider(0, 100, 40).W(300),
		caution.Progress(0.65).W(300),
		caution.TextField("hello, golden frames").W(300),
		caution.Textarea("line one\nline two\nline three").Rows(4),
	)
	para := strings.Join([]string{
		"Server-driven UI: the terminal renders",
		"what the wire describes, and application",
		"code never touches the DOM.",
		"",
		"Every widget in this frame exists twice -",
		"once in WebGL, once in OpenGL - and this",
		"scene is the proof they agree.",
	}, "\n")
	right := caution.VStack().Gap(12).
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(14), Right: caution.Px(16)}).Kids(
		caution.Label("Text").FontSize(15).Weight(600),
		caution.Label(para).Wrap().Color("$inkDim"),
		caution.Label("mono 0123456789 => != <-").Mono().FontSize(13),
		caution.Label("faint footnote, 12px").Color("$inkFaint").FontSize(12),
		caution.Panel().W(320).H(64).Bg("$panelAlt").Radius(10).
			Border("$edge", 1).Shadow(14, 0, 4, "#00000055"),
	)
	return caution.DockPanel().Kids(
		caution.Panel().Dock("top").H(52).Bg("$panelAlt").Kids(
			caution.Label("caution golden fixtures - controls").FontSize(16).Weight(600).
				Anchor(caution.A{Left: caution.Px(18), CenterY: caution.Px(0)}),
		),
		caution.Panel().Dock("fill").Kids(
			caution.Panel().W(440).Bg("$panel").Radius(10).Border("$edgeSoft", 1).
				Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(12), Bottom: caution.Px(16)}).
				Kids(left),
			caution.Panel().Bg("$panelInset").Radius(10).Border("$edgeSoft", 1).
				Anchor(caution.A{Left: caution.Px(472), Top: caution.Px(12),
					Right: caution.Px(16), Bottom: caution.Px(16)}).
				Kids(right),
		),
	)
}

// tableScene: the virtualized table with a keyed selection, weighted and
// fixed columns, and enough rows for a scrollbar.
func tableScene(_ *caution.Session) *caution.Node {
	emps := makeEmployees(200)
	table := caution.Table([]caution.Col{
		{Key: "id", Title: "ID", Width: 90},
		{Key: "name", Title: "Name", Weight: 1},
		{Key: "email", Title: "Email", Weight: 1.6},
		{Key: "role", Title: "Role", Width: 110},
		// In-cell widget columns: all three renderers, deterministic values.
		{Key: "load", Title: "Load", Width: 120, Kind: "progress"},
		{Key: "ok", Title: "OK", Width: 50, Kind: "checkbox"},
		{Key: "act", Title: "", Width: 90, Kind: "button"},
	}, len(emps)).RowKey(0).SetSelectedKey("E00007").
		RowsFunc(func(start, end int) [][]string {
			out := make([][]string, 0, end-start+1)
			for i := start; i <= end && i < len(emps); i++ {
				e := emps[i]
				out = append(out, []string{e.id, e.name, e.email, e.role,
					fmt.Sprintf("0.%d", (i*7)%10), map[bool]string{true: "true", false: "false"}[i%3 == 0], "View"})
			}
			return out
		}).
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(12),
			Right: caution.Px(16), Bottom: caution.Px(16)})
	return caution.DockPanel().Kids(
		caution.Panel().Dock("top").H(40).Bg("$panelAlt").Kids(
			caution.Label("Employees - 200 rows, virtualized").FontSize(14).Weight(600).
				Anchor(caution.A{Left: caution.Px(18), CenterY: caution.Px(0)}),
		),
		caution.Panel().Dock("fill").Kids(table),
	)
}

// treeScene: the outline view - indents, disclosure triangles (open and
// closed), a selected row, and a second column staying aligned.
func treeScene(_ *caution.Session) *caution.Node {
	items := []caution.TreeItem{
		{Key: "src", Cells: []string{"src", "-"}, Kids: []caution.TreeItem{
			{Key: "ui", Cells: []string{"ui", "12 files"}, Kids: []caution.TreeItem{
				{Key: "widgets", Cells: []string{"widgets.go", "48 KB"}},
				{Key: "table", Cells: []string{"tableview.go", "31 KB"}},
			}},
			{Key: "proto", Cells: []string{"proto", "4 files"}},
			{Key: "gfx", Cells: []string{"gfx", "6 files"}},
		}},
		{Key: "docs", Cells: []string{"docs", "-"}, Kids: []caution.TreeItem{
			{Key: "design", Cells: []string{"README.md", "60 KB"}},
		}},
		{Key: "plan", Cells: []string{"TODO", "18 KB"}},
	}
	tree := caution.Tree([]caution.Col{
		{Key: "name", Title: "Name", Weight: 1},
		{Key: "info", Title: "Info", Width: 160},
	}, items).Expand("src", "ui").SetSelectedKey("table").
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(12),
			Right: caution.Px(16), Bottom: caution.Px(16)})
	return caution.DockPanel().Kids(
		caution.Panel().Dock("top").H(40).Bg("$panelAlt").Kids(
			caution.Label("Tree - indents, disclosures, keyed selection").FontSize(14).Weight(600).
				Anchor(caution.A{Left: caution.Px(18), CenterY: caution.Px(0)}),
		),
		caution.Panel().Dock("fill").Kids(tree),
	)
}

// layoutScene: split view, scroll view mid-content, grid form, docked bars -
// the containers, exercised together.
func layoutScene(_ *caution.Session) *caution.Node {
	// Stretch: rows carry height only. Without it a stack leaves them at their
	// intrinsic width, zero, and both terminals agree on an invisible row, which
	// parity diffing cannot flag.
	list := caution.VStack().Gap(8).Align("stretch").
		Anchor(caution.A{Left: caution.Px(12), Top: caution.Px(12), Right: caution.Px(12)})
	for i := 1; i <= 18; i++ {
		list.Add(caution.Panel().H(44).Bg("$panelAlt").Radius(8).Border("$edgeSoft", 1).Kids(
			caution.Label(fmt.Sprintf("Row %02d", i)).Weight(500).
				Anchor(caution.A{Left: caution.Px(12), CenterY: caution.Px(0)}),
			caution.Label("scrollable").FontSize(12).Color("$inkFaint").
				Anchor(caution.A{Right: caution.Px(12), CenterY: caution.Px(0)}),
		))
	}
	form := caution.Grid([]caution.GridCol{{Px: 110}, {Weight: 1}}).ColGap(12).RowGap(12).
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(16), Right: caution.Px(16)}).Kids(
		caution.Label("Name").Color("$inkDim"),
		caution.TextField("Ada Lovelace"),
		caution.Label("Email").Color("$inkDim"),
		caution.TextField("ada@example.com"),
		caution.Label("Role").Color("$inkDim"),
		caution.Select([]string{"Engineer", "Designer", "Manager"}, 0),
		caution.Label("Notes").Color("$inkDim"),
		caution.Textarea("Keeps the machines honest.\nPrefers punched cards.").Rows(3),
	)
	right := caution.DockPanel().Kids(
		caution.Panel().Dock("top").H(48).Bg("$panelAlt").Kids(
			caution.Button("New").W(84).
				Anchor(caution.A{Left: caution.Px(12), CenterY: caution.Px(0)}),
			caution.Button("Duplicate").W(104).
				Anchor(caution.A{Left: caution.Px(104), CenterY: caution.Px(0)}),
			caution.Button("Archive").W(96).
				Anchor(caution.A{Left: caution.Px(216), CenterY: caution.Px(0)}),
		),
		caution.Panel().Dock("bottom").H(32).Bg("$titlebar").Kids(
			caution.Label("ready · golden layout fixture").FontSize(12).Color("$inkDim").
				Anchor(caution.A{Left: caution.Px(12), CenterY: caution.Px(0)}),
		),
		caution.Panel().Dock("fill").Kids(form),
	)
	return caution.HSplit().SplitPos(340).Kids(
		caution.Panel().Bg("$panelInset").Kids(
			caution.Scroll().Anchor(caution.A{Left: caution.Px(0), Top: caution.Px(0),
				Right: caution.Px(0), Bottom: caution.Px(0)}).Kids(list),
		),
		right,
	)
}

// textScene: the shaping engine's parity fixture - script itemization and
// bidi run reordering. Both terminals shape with the same wasm-compiled
// engine and the same faces, so even scripts the embedded fonts lack
// (Hebrew, Arabic -> .notdef boxes) must land on identical pixels in an
// identical visual order. Real-coverage rows (Greek, Cyrillic) prove
// itemization doesn't perturb rendering where glyphs exist.
func textScene(_ *caution.Session) *caution.Node {
	row := func(title, body string) *caution.Node {
		return caution.Panel().H(52).Bg("$panelAlt").Radius(8).Border("$edgeSoft", 1).Kids(
			caution.Label(title).FontSize(11).Color("$inkFaint").
				Anchor(caution.A{Left: caution.Px(12), Top: caution.Px(7)}),
			caution.Label(body).FontSize(14).
				Anchor(caution.A{Left: caution.Px(12), Bottom: caution.Px(8)}),
		)
	}
	list := caution.VStack().Gap(10).Align("stretch").
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(14), Right: caution.Px(16)}).Kids(
		caution.Label("Itemization & bidi").FontSize(15).Weight(600),
		row("latin", "The quick brown fox jumps over the lazy dog - 0123456789"),
		row("greek + cyrillic + latin", "alpha αβγδε · кириллица работает · back to latin"),
		row("hebrew embedded in latin (rtl run reverses)", "order: abc אבגדה def - width still adds up"),
		row("arabic embedded in latin (rtl + joining)", "total: مرحبا بالعالم end"),
		row("mixed weights", "regular - medium - bold share one baseline"),
		row("mono digits", "0123456789 |ml. == != <- -> 0xFF"),
		row("emoji fallback (noto emoji outlines, tinted like text)", "status: ✅ done · ⚠ warn · 🚀 ship · 😀"),
	)
	return caution.DockPanel().Kids(
		caution.Panel().Dock("top").H(40).Bg("$panelAlt").Kids(
			caution.Label("Text - one engine, two terminals, every script").FontSize(14).Weight(600).
				Anchor(caution.A{Left: caution.Px(18), CenterY: caution.Px(0)}),
		),
		caution.Panel().Dock("fill").Kids(list),
	)
}

// metricsScene is the metric-theming fixture: every kind of widget chrome the
// tokens touch, rendered under squareMetrics, so sharp corners and 2px
// hairlines. Two independent implementations of the token table have to resolve
// identical pixels, and a terminal that misses one token or keeps a hardcoded
// constant drifts here. The dialog rides on top because it is the only
// radius.panel surface, and no other scene carries one.
func metricsScene(s *caution.Session) *caution.Node {
	s.SetMenu(
		caution.Menu{Title: "Metrics", Items: []caution.MenuItem{
			{Title: "Square", Key: "1"},
			{Sep: true},
			{Title: "Default", Key: "0"},
		}},
	)
	emps := makeEmployees(40)
	table := caution.Table([]caution.Col{
		{Key: "id", Title: "ID", Width: 80},
		{Key: "name", Title: "Name", Weight: 1},
		{Key: "load", Title: "Load", Width: 110, Kind: "progress"},
		{Key: "ok", Title: "OK", Width: 50, Kind: "checkbox"},
		{Key: "act", Title: "", Width: 84, Kind: "button"},
	}, len(emps)).RowKey(0).SetSelectedKey("E00003").
		RowsFunc(func(start, end int) [][]string {
			out := make([][]string, 0, end-start+1)
			for i := start; i <= end && i < len(emps); i++ {
				e := emps[i]
				out = append(out, []string{e.id, e.name,
					fmt.Sprintf("0.%d", (i*7)%10), map[bool]string{true: "true", false: "false"}[i%3 == 0], "View"})
			}
			return out
		}).
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(12),
			Right: caution.Px(16), Bottom: caution.Px(16)})
	controls := caution.VStack().Gap(14).
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(14), Right: caution.Px(16)}).Kids(
		caution.Label("Square chrome").FontSize(15).Weight(600),
		caution.HStack().Gap(10).Kids(
			caution.Button("Save").Primary().W(96),
			caution.Button("Cancel").W(96),
			// Per-node radius beats the (square) token - the override half
			// of the contract, pinned cross-terminal.
			caution.Button("pill").Radius(14).W(72).H(28),
		),
		caution.Checkbox("Sharp corners", true),
		caution.Checkbox("Chunky hairlines", false),
		caution.Radio([]string{"Square", "Round"}, 0),
		caution.TabBar([]string{"One", "Two", "Three"}, 1),
		caution.Select([]string{"Square", "Default"}, 0).W(180),
		caution.Slider(0, 100, 60).W(260),
		caution.Progress(0.4).W(260),
		caution.TextField("no radius here").W(260),
		caution.TextField("pill field").Radius(16).W(260),
		caution.Textarea("first line\nsecond line").Rows(3),
	)
	dlg := caution.Dialog("Metric theming").CardSize(340, 150).Kids(
		caution.Label("radius.panel = 0, border.width = 2").Color("$inkDim").
			Anchor(caution.A{Left: caution.Px(20), Top: caution.Px(14), Right: caution.Px(20)}),
		caution.Button("OK").Primary().
			Anchor(caution.A{Right: caution.Px(16), Bottom: caution.Px(14)}),
	)
	return caution.DockPanel().Kids(
		caution.Panel().Dock("fill").Kids(
			caution.Panel().W(420).Bg("$panel").Radius(10).Border("$edgeSoft", 1).
				Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(12), Bottom: caution.Px(16)}).
				Kids(controls),
			caution.Panel().Bg("$panelInset").Radius(10).Border("$edgeSoft", 1).
				Anchor(caution.A{Left: caution.Px(452), Top: caution.Px(12),
					Right: caution.Px(16), Bottom: caution.Px(16)}).
				Kids(table),
		),
		dlg,
	)
}

type employee struct {
	id, name, email, role string
}

// makeEmployees mirrors the demo's generator - deterministic rows.
func makeEmployees(n int) []employee {
	firsts := []string{"Ada", "Grace", "Alan", "Edsger", "Barbara", "Donald", "Ken", "Dennis", "Bjarne", "Guido", "Linus", "Margaret"}
	lasts := []string{"Lovelace", "Hopper", "Turing", "Dijkstra", "Liskov", "Knuth", "Thompson", "Ritchie", "Stroustrup", "Rossum", "Torvalds", "Hamilton"}
	roles := []string{"Engineer", "Designer", "Manager", "Analyst", "Support", "Ops"}
	out := make([]employee, n)
	for i := range out {
		f := firsts[i%len(firsts)]
		l := lasts[(i/len(firsts))%len(lasts)]
		out[i] = employee{
			id:    fmt.Sprintf("E%05d", i+1),
			name:  f + " " + l,
			email: strings.ToLower(f) + "." + strings.ToLower(l) + strconv.Itoa(i) + "@example.com",
			role:  roles[(i*7)%len(roles)],
		}
	}
	return out
}
