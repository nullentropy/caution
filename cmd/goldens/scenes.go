package main

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/nullentropy/caution"

	"golang.org/x/image/font/gofont/gosmallcaps"
)

// scenes must be deterministic. no animated shaders (different clocks), no
// focused text field (caret phase), no images (async load)
// wrapped labels break on explicit newlines with width slack so the native and
// browser font stacks agree on line counts
// geometry comes from anchors and fixed widths, not text measurement

type scene struct {
	name  string
	theme string
	build func(s *caution.Session) *caution.Node
	// nil = terminal defaults
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
	{"richtext-midnight", "Midnight", richTextScene, nil},
	{"fonts-midnight", "Midnight", fontsScene, nil},
	{"metrics-square", "Midnight", metricsScene, squareMetrics},
}

// the anti-default look. both terminals must read every value from the same
// token or the scene fails
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

// captures run one at a time but mounts happen on connection goroutines
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

// copied from the demo. scenes ship a full theme so both terminals resolve
// identical colors
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

// one of everything that draws chrome, at rest, fixed widths
// carries a menu so the closed menu bar is diffed too (native under -menubar)
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

// keyed selection, weighted and fixed columns, enough rows for a scrollbar
func tableScene(_ *caution.Session) *caution.Node {
	emps := makeEmployees(200)
	table := caution.Table([]caution.Col{
		{Key: "id", Title: "ID", Width: 90},
		{Key: "name", Title: "Name", Weight: 1},
		{Key: "email", Title: "Email", Weight: 1.6},
		{Key: "role", Title: "Role", Width: 110},
		// in-cell widget columns, all three renderers
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

// indents, open and closed disclosure, a selected row, second column aligned
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

// containers together: split, scroll mid-content, grid form, docked bars
func layoutScene(_ *caution.Session) *caution.Node {
	// rows carry height only. without Stretch they get zero width and both
	// terminals agree on an invisible row, which diffing can't catch
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

// script itemization and bidi reordering. same engine and faces on both sides
// so even scripts the fonts lack (Hebrew, Arabic as .notdef boxes) must match
// pixel for pixel in the same order. Greek and Cyrillic check itemization
// doesn't disturb glyphs that exist
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

// every face, colour and background a span can carry, with a break inside a
// span and a word that straddles two
func richTextScene(_ *caution.Session) *caution.Node {
	body := caution.RichText(
		caution.Span{Text: "Plain, "},
		caution.Span{Text: "bold", Bold: true},
		caution.Span{Text: ", "},
		caution.Span{Text: "italic", Italic: true},
		caution.Span{Text: " and "},
		caution.Span{Text: "mono", Mono: true, Bg: "$panelInset"},
		caution.Span{Text: " share one line, then a "},
		caution.Span{Text: "clickable", Color: "$accent", Action: "go"},
		caution.Span{Text: " span and a strad"},
		caution.Span{Text: "dled word", Bold: true, Color: "$accent"},
		caution.Span{Text: ".\nA second paragraph keeps the "},
		caution.Span{Text: "base line height", Bold: true, Mono: true, Bg: "$edgeSoft"},
		caution.Span{Text: " when faces mix, and dims "},
		caution.Span{Text: "this run", Italic: true, Color: "$inkDim"},
		caution.Span{Text: ".\nItalic combines: "},
		caution.Span{Text: "bold italic", Bold: true, Italic: true},
		caution.Span{Text: ", "},
		caution.Span{Text: "mono italic", Mono: true, Italic: true},
		caution.Span{Text: ", "},
		caution.Span{Text: "bold mono italic", Bold: true, Mono: true, Italic: true},
		caution.Span{Text: "."},
	).FontSize(14).Selectable().
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(16), Right: caution.Px(16)})
	return caution.DockPanel().Kids(
		caution.Panel().Dock("top").H(40).Bg("$panelAlt").Kids(
			caution.Label("Rich text - one block, many faces").FontSize(14).Weight(600).
				Anchor(caution.A{Left: caution.Px(18), CenterY: caution.Px(0)}),
		),
		caution.Panel().Dock("fill").Kids(body),
	)
}

// every chrome the tokens touch, under squareMetrics. a terminal that misses a
// token or hardcodes a constant drifts here
// the dialog is here because it's the only radius.panel surface
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
			// per-node radius beats the token
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

// mirrors the demo's generator
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

// app fonts: a variable family over HTTP drawn across its weight axis, a
// static family as a data: URI, the session default, and every fallback (a
// family nobody registered, runes the app face lacks, mono with no mono
// default)
func fontsScene(s *caution.Session) *caution.Node {
	s.AddFont("Sweep", "/fonts/Estedad-VF.ttf")
	s.AddFont("Caps", "data:font/ttf;base64,"+base64.StdEncoding.EncodeToString(gosmallcaps.TTF))
	s.SetFonts("Sweep", "")
	row := func(title string, body *caution.Node) *caution.Node {
		return caution.Panel().H(56).Bg("$panelAlt").Radius(8).Border("$edgeSoft", 1).Kids(
			caution.Label(title).FontSize(11).Color("$inkFaint").
				Anchor(caution.A{Left: caution.Px(12), Top: caution.Px(7)}),
			body.Anchor(caution.A{Left: caution.Px(12), Bottom: caution.Px(8)}),
		)
	}
	sweep := caution.HStack().Gap(14)
	for w := 100; w <= 900; w += 100 {
		sweep.Kids(caution.Label("Weight").FontSize(18).Weight(w))
	}
	list := caution.VStack().Gap(10).Align("stretch").
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(14), Right: caution.Px(16)}).Kids(
		caution.Label("App fonts - the same files shape and rasterize in both terminals").FontSize(15).Weight(600),
		row("one variable file across its weight axis, session default family", sweep),
		row("per-node family: static small caps from a data: URI, then a family nobody registered",
			caution.HStack().Gap(14).Kids(
				caution.Label("Small capitals at fourteen").FontSize(14).Font("Caps"),
				caution.Label("unknown family draws with the Go face").FontSize(14).Font("Missing"),
			)),
		row("runes the app face lacks fall back per rune, mono keeps its own default",
			caution.HStack().Gap(14).Kids(
				caution.Label("latin αβγδ greek ✅ emoji").FontSize(14),
				caution.Label("0123 |ml. mono").FontSize(14).Mono(),
			)),
		row("rich text: spans name their own family and inherit the block's",
			caution.RichText(
				caution.Span{Text: "block family, "},
				caution.Span{Text: "bold at 700", Bold: true},
				caution.Span{Text: ", "},
				caution.Span{Text: "caps span", Font: "Caps"},
				caution.Span{Text: ", "},
				caution.Span{Text: "mono span", Mono: true},
				caution.Span{Text: " and "},
				caution.Span{Text: "italic with no italic file", Italic: true},
			).FontSize(14).W(900)),
	)
	return caution.DockPanel().Kids(
		caution.Panel().Dock("top").H(40).Bg("$panelAlt").Kids(
			caution.Label("Fonts - app families, variable weights, fallbacks").FontSize(14).Weight(600).
				Anchor(caution.A{Left: caution.Px(18), CenterY: caution.Px(0)}),
		),
		caution.Panel().Dock("fill").Kids(list),
	)
}
