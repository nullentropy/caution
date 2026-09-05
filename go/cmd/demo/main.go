package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nullentropy/caution/go"
	"github.com/nullentropy/caution/go/dev"
	"github.com/nullentropy/caution/go/native"
)

// The counter window's structure is a UI document (a "nib"): designed as
// data
//
//go:embed window.ui.json
var windowUI []byte

type employee struct {
	id, name, email, role string
}

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

// Server-driven themes: full token maps pushed over the wire.
var themes = map[string]map[string]string{
	"Midnight": {
		"bg": "#16181d", "ink": "#e8eaf0", "inkDim": "#9aa3b2", "inkFaint": "#6b7280",
		"panel": "#262b33", "panelAlt": "#20252c", "panelInset": "#1b1f26",
		"edge": "#3a4150", "edgeSoft": "#333a46", "titlebar": "#2e343e",
		"control": "#2e343e", "controlEdge": "#4a5262", "accent": "#4f8cff",
	},
	"Violet": {
		"bg": "#151221", "ink": "#eae6f5", "inkDim": "#a49bc0", "inkFaint": "#746b91",
		"panel": "#251f38", "panelAlt": "#1f1a30", "panelInset": "#191428",
		"edge": "#3d3459", "edgeSoft": "#352d4e", "titlebar": "#2d2545",
		"control": "#2d2545", "controlEdge": "#4d4273", "accent": "#a78bfa",
	},
	"Light": {
		"bg": "#eef0f4", "ink": "#1b1e24", "inkDim": "#5b6472", "inkFaint": "#8a93a2",
		"panel": "#ffffff", "panelAlt": "#f7f8fa", "panelInset": "#eceef2",
		"edge": "#d4d9e0", "edgeSoft": "#e2e6ec", "titlebar": "#e8ebef",
		"control": "#f1f2f5", "controlEdge": "#c6cdd7", "accent": "#2563eb",
	},
}

var themeNames = []string{"Midnight", "Violet", "Light"}

// A static post-effect: the rows panel renders normally, then composites
// through this
const crtFrag = `
vec4 effect(vec2 uv) {
  vec2 c = uv * 2.0 - 1.0;
  c *= 1.0 + 0.06 * dot(c, c);          // barrel curvature
  vec2 suv = (c + 1.0) * 0.5;
  vec4 col = src(suv);
  float scan = 0.86 + 0.14 * sin(suv.y * u_res.y * 3.14159);
  col.rgb = mix(col.rgb, col.rgb * vec3(0.5, 1.1, 0.55), 0.6) * scan;
  col.rgb *= 1.0 - 0.3 * dot(c, c);     // vignette
  return col;
}`

// An animated shader pane: a raymarched torus, lit and spinning.
const torusFrag = `
float sdTorus(vec3 p, vec2 t) {
  vec2 q = vec2(length(p.xz) - t.x, p.y);
  return length(q) - t.y;
}
vec3 rot(vec3 p) {
  float ca = cos(u_time * 0.6 * u_speed), sa = sin(u_time * 0.6 * u_speed);
  float cb = cos(u_time * 0.37 * u_speed), sb = sin(u_time * 0.37 * u_speed);
  p.xz = mat2(ca, -sa, sa, ca) * p.xz;
  p.xy = mat2(cb, -sb, sb, cb) * p.xy;
  return p;
}
float map(vec3 p) { return sdTorus(rot(p), vec2(0.82, 0.34)); }
vec4 effect(vec2 uv) {
  vec2 sc = uv * 2.0 - 1.0;
  sc.x *= u_res.x / u_res.y;
  sc.y = -sc.y;
  vec3 ro = vec3(0.0, 0.0, -2.7);
  vec3 rd = normalize(vec3(sc, 1.7));
  float t = 0.0;
  float d = 1e9;
  for (int i = 0; i < 72; i++) {
    d = map(ro + rd * t);
    if (d < 0.001 || t > 7.0) break;
    t += d;
  }
  vec3 col = vec3(0.05, 0.055, 0.07);
  if (d < 0.01) {
    vec3 p = ro + rd * t;
    vec2 e = vec2(0.0025, 0.0);
    vec3 n = normalize(vec3(map(p + e.xyy) - map(p - e.xyy),
                            map(p + e.yxy) - map(p - e.yxy),
                            map(p + e.yyx) - map(p - e.yyx)));
    vec3 l = normalize(vec3(0.5, 0.8, -0.6));
    float dif = clamp(dot(n, l), 0.0, 1.0);
    float spe = pow(clamp(dot(reflect(-l, n), -rd), 0.0, 1.0), 24.0);
    col = vec3(0.31, 0.55, 1.0) * (0.12 + 0.88 * dif) + spe * 0.55;
  }
  return vec4(col, 1.0);
}`

func (e employee) field(key string) string {
	switch key {
	case "name":
		return e.name
	case "email":
		return e.email
	case "role":
		return e.role
	default:
		return e.id
	}
}

func main() {
	nativeFlag := flag.Bool("native", false, "open as a native desktop window (in-process, no port) instead of serving HTTP")
	addr := flag.String("addr", ":8787", `listen address - ":8787" or "unix:/path/to.sock"`)
	shot := flag.String("shot", "", "native mode: render offscreen to this PNG and exit")
	clicks := flag.String("clicks", "", `native mode: synthetic clicks for -shot, "x,y@ms;..."`)
	flag.Parse()

	// The server serves its own terminal: host page + client runtime bundled
	// from TypeScript via esbuild's Go API. One process, no Node anywhere.
	dev.ServeClient("../src/terminal.ts")

	// A generated avatar, served like any asset and displayed by Image().
	// In native mode the same route is fetched over the in-process pipe.
	avatar := makeAvatar()
	http.HandleFunc("/assets/avatar.png", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(avatar)
	})
	// Generated tones, the same way: no binary assets in the repo.
	for name, wav := range makeTones() {
		wav := wav
		http.HandleFunc("/assets/sfx/"+name+".wav", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "audio/wav")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			_, _ = w.Write(wav)
		})
	}

	// Demo login: /login?u=ada sets the identity cookie and reloads. A real
	// app would validate a session/JWT here instead.
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("u")
		if u == "" {
			u = "nullentropy"
		}
		http.SetCookie(w, &http.Cookie{Name: "user", Value: u, Path: "/", HttpOnly: true})
		http.Redirect(w, r, "/", http.StatusFound)
	})

	opts := caution.Options{
		// Runs on every connection, resumes included - a resume token only
		// re-adopts a session minted for the same identity.
		Authorize: func(r *http.Request) (any, error) {
			if c, err := r.Cookie("user"); err == nil && c.Value != "" {
				return c.Value, nil
			}
			return "anonymous", nil
		},
	}

	if *nativeFlag || native.InBundle() {
		// One binary, two faces: `demo` is a web server, `demo -native` is a
		// desktop app - same mount, same routes, no port in native mode.
		// Inside a .app bundle (cmd/appbundle) there are no flags, so the
		// bundle itself is the switch.
		if err := native.Run(mount, native.Options{
			Title: "caution demo", Serve: opts,
			Shot: *shot, SettleMs: 2500, Clicks: *clicks,
		}); err != nil {
			log.Fatal(err)
		}
		return
	}

	log.Fatal(caution.ServeOpts(*addr, mount, opts))
}

// makeAvatar renders a little concentric-ring mark so the demo has an image
// without shipping binary assets.
func makeAvatar() []byte {
	const size = 128
	center := float64(size) / 2
	rings := []color.NRGBA{
		{R: 0x4f, G: 0x8c, B: 0xff, A: 0xff},
		{R: 0xa7, G: 0x8b, B: 0xfa, A: 0xff},
		{R: 0x22, G: 0xc5, B: 0x5e, A: 0xff},
		{R: 0x16, G: 0x18, B: 0x1d, A: 0xff},
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)+0.5-center, float64(y)+0.5-center)
			if d > center {
				continue // transparent corners; Radius() rounds the rest
			}
			img.SetNRGBA(x, y, rings[int(d/(center/4))%len(rings)])
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// makeTones synthesizes the demo's sound effects: short decaying sines at
// distinct pitches, so each gesture is recognizable.
func makeTones() map[string][]byte {
	tone := func(hz float64, ms int) []byte {
		const rate = 48000
		n := rate * ms / 1000
		var pcm bytes.Buffer
		for i := 0; i < n; i++ {
			t := float64(i) / rate
			env := math.Exp(-t*18) * math.Min(1, float64(i)/200) // fast attack, exponential decay
			binary.Write(&pcm, binary.LittleEndian, int16(math.Sin(2*math.Pi*hz*t)*env*9000))
		}
		var out bytes.Buffer
		out.WriteString("RIFF")
		binary.Write(&out, binary.LittleEndian, uint32(36+pcm.Len()))
		out.WriteString("WAVEfmt ")
		for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16)} {
			binary.Write(&out, binary.LittleEndian, v)
		}
		out.WriteString("data")
		binary.Write(&out, binary.LittleEndian, uint32(pcm.Len()))
		out.Write(pcm.Bytes())
		return out.Bytes()
	}
	return map[string][]byte{
		"press":  tone(880, 60),
		"toggle": tone(1320, 70),
		"select": tone(660, 80),
		"open":   tone(523, 90),
		"close":  tone(392, 90),
		"ding":   tone(1046, 320),
	}
}

func mount(s *caution.Session) *caution.Node {
	// Per-session app state: plain Go locals. Handlers and the ticker all run
	// on the session goroutine, so no locking is ever needed.
	count := 0
	auto := false
	events := 0
	rowN := 0
	start := time.Now()
	who, _ := s.Identity().(string) // from Options.Authorize (demo: cookie)

	s.SetTitle(fmt.Sprintf("caution demo - %s", who))

	// Interaction sounds are a token table the client fires itself, so a click
	// never waits on the server to sound. The ding is the other kind: the
	// server plays it when it has done something. Off until asked for, with
	// every source decoded up front so turning them on costs nothing.
	sfx := map[string]string{}
	var srcs []string
	for _, tok := range []string{"press", "toggle", "select", "open", "close"} {
		sfx[tok] = "/assets/sfx/" + tok + ".wav"
		srcs = append(srcs, sfx[tok])
	}
	const ding = "/assets/sfx/ding.wav"
	s.PreloadSounds(append(srcs, ding)...)
	soundsOn := false
	var soundsBox *caution.Node
	var installMenu func()
	setSounds := func(on bool) {
		events++
		soundsOn = on
		if on {
			s.SetSounds(sfx)
		} else {
			s.SetSounds(nil)
		}
		soundsBox.SetChecked(on) // the footer box and the menu item both drive this
		installMenu()
	}

	// Load the window nib and grab its outlets.
	win := caution.MustLoadUI(windowUI)
	counter := win.Node("counter")
	mirror := win.Node("mirror")
	prefix := "row"

	status := caution.Label("connected").FontSize(12).Color("$inkFaint").
		Anchor(caution.A{Left: caution.Px(32), CenterY: caution.Px(0)})
	soundsBox = caution.Checkbox("sounds", false).Tip("The gesture sound table, patched live").
		OnToggle(setSounds).
		Anchor(caution.A{Right: caution.Px(32), CenterY: caution.Px(0)})

	rowStack := caution.VStack().Gap(8).
		Anchor(caution.A{Left: caution.Px(20), Top: caution.Px(16), Right: caution.Px(20)})
	rowStack.Add(caution.Label("rows inserted by the server appear here").
		FontSize(13).Color("$inkDim"))
	var rows []*caution.Node

	// A multi-line composer: Enter inserts newlines, Post ships the note.
	// Clicking Post blurs the textarea, whose commit syncs the server's copy
	// of `value` before the click handler reads it.
	composer := caution.Textarea("").Rows(2).
		Placeholder("write a note… Enter makes a new line").
		OnCommit(func(string) { events++ }).
		Anchor(caution.A{Left: caution.Px(0), Top: caution.Px(0), Right: caution.Px(84), Bottom: caution.Px(0)})

	bump := func(delta int) func() {
		return func() {
			events++
			count += delta
			counter.SetText(strconv.Itoa(count))
		}
	}

	// -- virtualized table: 10k rows live here in Go; the client only ever
	// holds the visible window ------------------------------------------------
	emps := makeEmployees(10000)
	tableCaption := caution.Label("Employees - 10,000 rows, virtualized; click headers to sort server-side, click a row to select").
		FontSize(13).Color("$inkDim").
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(12), Right: caution.Px(16)})
	var table *caution.Node
	table = caution.Table([]caution.Col{
		{Key: "id", Title: "ID", Width: 90},
		{Key: "name", Title: "Name", Weight: 2},
		{Key: "email", Title: "Email", Weight: 3},
		{Key: "role", Title: "Role", Weight: 1},
	}, len(emps)).
		RowKey(0). // the ID column is the row's identity - selection survives sorts
		RowsFunc(func(start, end int) [][]string {
			if start < 0 {
				start = 0
			}
			if end >= len(emps) {
				end = len(emps) - 1
			}
			rows := make([][]string, 0, end-start+1)
			for i := start; i <= end; i++ {
				e := emps[i]
				rows = append(rows, []string{e.id, e.name, e.email, e.role})
			}
			return rows
		}).
		OnSort(func(key string, asc bool) {
			events++
			sort.SliceStable(emps, func(a, b int) bool {
				if asc {
					return emps[a].field(key) < emps[b].field(key)
				}
				return emps[a].field(key) > emps[b].field(key)
			})
			// No SetSelected(-1): selection is keyed, so the client's
			// highlight follows the selected employee to its new position.
			table.RefreshRows()
		}).
		OnColResize(func(string, float64) { events++ }).
		OnRowSelectKey(func(key string, row int) {
			events++
			if row >= 0 && row < len(emps) {
				e := emps[row]
				tableCaption.SetText(fmt.Sprintf("Employees - selected %s · %s · %s · %s (row %d)",
					e.id, e.name, e.email, e.role, row+1))
			}
		}).
		Anchor(caution.A{Left: caution.Px(16), Top: caution.Px(40), Right: caution.Px(16), Bottom: caution.Px(16)})

	// The torus pane: its spin speed is a server-pushed uniform, driven by a
	// slider and mirrored by a progress bar - widgets steering GLSL.
	torus := caution.Shader(caution.FX{
		Frag: torusFrag, Animate: true, Uniforms: map[string]float64{"u_speed": 1},
	}).Anchor(caution.A{Left: caution.Px(12), Top: caution.Px(32), Right: caution.Px(12), Bottom: caution.Px(40)})
	speedBar := caution.Progress(1.0 / 3).W(90).
		Anchor(caution.A{Right: caution.Px(14), Bottom: caution.Px(19)})

	// Root is a plain panel so dialogs (appended last) can cover everything,
	// including the docked header and footer.
	root := caution.Panel().Kids()
	shell := caution.DockPanel().
		Anchor(caution.A{Left: caution.Px(0), Right: caution.Px(0), Top: caution.Px(0), Bottom: caution.Px(0)})
	root.Add(shell)

	// Clear-rows confirmation: a server-driven modal flow.
	var confirm *caution.Node
	closeConfirm := func() {
		if confirm != nil {
			confirm.Remove()
			confirm = nil
		}
	}
	openConfirm := func() {
		if confirm != nil {
			return
		}
		confirm = caution.Dialog("Clear all rows?").CardSize(440, 168).OnDismiss(func() { events++; closeConfirm() })
		confirm.Kids(
			caution.Label("This removes every row the server inserted, notes included.").
				FontSize(13).Color("$inkDim").Wrap().
				Anchor(caution.A{Left: caution.Px(20), Top: caution.Px(16), Right: caution.Px(20)}),
			caution.HStack().Gap(10).Anchor(caution.A{Right: caution.Px(20), Bottom: caution.Px(20)}).Kids(
				caution.Button("Cancel").OnClick(func() { events++; closeConfirm() }),
				caution.Button("Clear rows").Primary().OnClick(func() {
					events++
					for _, r := range rows {
						r.Remove()
					}
					rows = nil
					rowN = 0
					closeConfirm()
				}),
			),
		)
		root.Add(confirm) // last child paints on top
	}

	// Bind behavior to the nib's named outlets
	win.Node("dec").Tip("Decrement - a full server round-trip").OnClick(bump(-1))
	win.Node("inc").Tip("Increment - a full server round-trip").OnClick(bump(+1))
	win.Node("theme").Tip("Themes are token maps pushed by the server")
	win.Node("auto").OnToggle(func(b bool) { events++; auto = b })
	win.Node("prefix").
		OnInput(func(v string) {
			events++
			mirror.SetText(fmt.Sprintf("server sees: %q (input)", v))
		}).
		OnCommit(func(v string) {
			events++
			if v == "panic" {
				// Deliberate resilience demo: the SDK recovers handler panics,
				// shows a crash dialog, and the session keeps running.
				panic("intentional demo panic - dismiss this dialog and keep going")
			}
			prefix = v
			mirror.SetText(fmt.Sprintf("server sees: %q (committed)", v))
		})
	applyTheme := func(i int) {
		events++
		if i >= 0 && i < len(themeNames) {
			s.SetTheme(themes[themeNames[i]])
			win.Node("theme").SetProp("selected", i) // keep the select in sync
		}
	}
	addRow := func() {
		events++
		rowN++
		row := caution.Label(fmt.Sprintf("%s %02d - inserted by the Go server at %s",
			prefix, rowN, time.Now().Format("15:04:05"))).FontSize(13).Selectable().Wrap()
		rows = append(rows, rowStack.Add(row))
		s.Play(ding)
	}
	postNote := func() {
		events++
		v, _ := composer.Prop("value").(string)
		if strings.TrimSpace(v) == "" {
			return
		}
		rowN++
		// Wrapped labels render the note's own newlines and re-wrap long
		// lines at the pane's width.
		rows = append(rows, rowStack.Add(caution.Label(v).FontSize(13).Selectable().Wrap()))
		composer.ClearValue()
		s.Play(ding)
	}
	win.Node("theme").OnSelect(applyTheme)
	win.Node("addRow").OnClick(addRow)
	win.Node("clearRows").OnClick(func() {
		events++
		openConfirm()
	})

	// A registered combo works in BOTH terminals, independent of menus.
	s.OnKey("cmd+j", addRow)

	installMenu = func() {
		soundItem := "Turn Sounds On"
		if soundsOn {
			soundItem = "Turn Sounds Off"
		}
		s.SetMenu(
			caution.Menu{Title: "Rows", Items: []caution.MenuItem{
				{Title: "Add Row", Key: "n", OnPick: addRow},
				{Sep: true},
				{Title: "Clear Rows…", Key: "shift+cmd+k", OnPick: func() { events++; openConfirm() }},
			}},
			caution.Menu{Title: "Theme", Items: []caution.MenuItem{
				{Title: "Midnight", OnPick: func() { applyTheme(0) }},
				{Title: "Violet", OnPick: func() { applyTheme(1) }},
				{Title: "Light", OnPick: func() { applyTheme(2) }},
			}},
			caution.Menu{Title: "Sound", Items: []caution.MenuItem{
				{Title: soundItem, Key: "shift+cmd+m", OnPick: func() { setSounds(!soundsOn) }},
			}},
		)
	}
	installMenu()

	shell.Kids(
		// header
		caution.Panel().Dock("top").H(84).Kids(
			caution.Label("caution - server-driven UI").FontSize(22).Weight(600).
				Anchor(caution.A{Left: caution.Px(32), Top: caution.Px(22)}),
			caution.Label("every widget on this screen was born in a Go process - the browser is a dumb terminal with excellent taste").
				FontSize(13).Color("$inkDim").Selectable().
				Anchor(caution.A{Left: caution.Px(32), Top: caution.Px(56), Right: caution.Px(120)}),
			caution.Image("/assets/avatar.png").Fit("cover").Radius(22).Alt("avatar for "+who).
				W(44).H(44).
				Anchor(caution.A{Right: caution.Px(32), CenterY: caution.Px(0)}),
		),
		// footer
		caution.Panel().Dock("bottom").H(34).Kids(status, soundsBox),
		// content
		caution.Panel().Dock("fill").Kids(
			// resizable panes: a vertical split (upper band / table), whose
			// upper pane is itself a horizontal split (window / rows)
			caution.VSplit().SplitPos(364).SplitMin(280, 160).
				Anchor(caution.A{Left: caution.Px(32), Top: caution.Px(12), Right: caution.Px(32), Bottom: caution.Px(12)}).
				OnSplitResize(func(float64) { events++ }).Kids(
				caution.HSplit().SplitPos(552).SplitMin(520, 200).
					OnSplitResize(func(float64) { events++ }).Kids(
					// counter window (pane A) - structure from window.ui.json
					win.Root,
					// pane B: rows terminal (CRT post-effect) with a note
					// composer docked beneath, over a shader pane
					caution.VSplit().SplitPos(210).SplitMin(120, 120).Kids(
						caution.DockPanel().DockGap(8).Kids(
							caution.Panel().Dock("bottom").H(56).Kids(
								composer,
								caution.Button("Post").OnClick(postNote).
									Tip("Post the note as a wrapped row").
									Anchor(caution.A{Right: caution.Px(0), CenterY: caution.Px(0)}),
							),
							caution.Panel().Bg("$panelAlt").Radius(12).Border("$edgeSoft", 1).
								Effect(caution.FX{Frag: crtFrag}).Dock("fill").
								Context(
									caution.ContextItem{Title: "Add row", OnPick: addRow},
									caution.ContextItem{Sep: true},
									caution.ContextItem{Title: "Clear rows…", OnPick: func() { events++; openConfirm() }},
								).Kids(
								caution.Scroll().InsetBottom(16).
									Anchor(caution.A{Left: caution.Px(1), Top: caution.Px(1), Right: caution.Px(1), Bottom: caution.Px(1)}).
									Kids(rowStack),
							),
						),
						caution.Panel().Bg("$panelAlt").Radius(12).Border("$edgeSoft", 1).Kids(
							caution.Label("raymarched in GLSL - u_time from the client clock, u_speed from a slider").
								FontSize(12).Color("$inkFaint").
								Anchor(caution.A{Left: caution.Px(14), Top: caution.Px(10), Right: caution.Px(14)}),
							torus,
							caution.Slider(0, 3, 1).Tip("Spin speed - a server-pushed shader uniform").
								OnSlide(func(v float64) {
									events++
									torus.SetUniform("u_speed", v) // one key on the wire; the client merges
									speedBar.SetProgress(v / 3)
								}).
								Anchor(caution.A{Left: caution.Px(12), Bottom: caution.Px(10), Right: caution.Px(120)}),
							speedBar,
						),
					),
				),
				// table panel (lower pane)
				caution.Panel().Bg("$panelAlt").Radius(12).Border("$edgeSoft", 1).Kids(tableCaption, table),
			),
		),
	)

	// Server push
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-s.Done():
				return
			case <-t.C:
				s.Update(func() {
					if auto {
						count++
						counter.SetText(strconv.Itoa(count))
					}
					status.SetText(fmt.Sprintf(
						"server time %s · session up %02ds · %d events handled · signed in as %s (/login?u=you) · one Go goroutine, zero JavaScript",
						time.Now().Format("15:04:05"), int(time.Since(start).Seconds()), events, who))
				})
			}
		}
	}()

	return root
}
