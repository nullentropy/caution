# caution

A server-driven, GPU-rendered UI framework. You write your application as a
server, in Go or Clojure, against a widget SDK. Your user runs a generic
client (the "terminal") that renders the widget tree and sends interaction
events back. 

There's no HTML, CSS, or JavaScript anywhere in your app.

There are two terminals, and every app works in both without changes:

- the **browser terminal**: a fixed TypeScript runtime rendering into a
  WebGL2 canvas, bundled in your app
- the **native terminal**: a Go program (GLFW + OpenGL) that opens the same
  app as a desktop window, with no browser engine in the process

The two render identically. The test harness (`cmd/goldens`) renders the
same scenes through both and compares the frames.

## Quick start

```
go run -C go ./cmd/demo            # serve the demo app
open http://localhost:8787         # ... in a browser

go run -C go ./cmd/demo -native    # the same app as a desktop window
```

A complete app looks like this (see examples):

```go
package main

import (
    "log"
    "strconv"

    caution "github.com/nullentropy/caution/go"
    "github.com/nullentropy/caution/go/native"
)

func mount(s *caution.Session) *caution.Node {
    count := 0
    counter := caution.Label("0").FontSize(28).Weight(600)

    return caution.VStack().Pad(16).Gap(12).Kids(
        caution.Label("Counter").FontSize(15).Weight(600),
        counter,
        caution.HStack().Gap(8).Kids(
            caution.Button("−").OnClick(func() { count--; counter.SetText(strconv.Itoa(count)) }),
            caution.Button("+").Primary().OnClick(func() { count++; counter.SetText(strconv.Itoa(count)) }),
        ),
    )
}

func main() {
    caution.ServeClient()
    if native.InBundle() {
        log.Fatal(native.Run(mount, native.Options{Title: "Counter"}))
        return
    }
    log.Fatal(caution.Serve(":8787", mount))
}
```

One binary covers every deployment: serve browsers on a port, serve locally
over a unix socket (`caution.Serve("unix:/path.sock", mount)`), or open as a
desktop app with no listener (`native.Run`).

## How it works

The client owns everything that has to feel instant: pixels, layout,
hit-testing, hover and focus, text editing (caret, selection, IME),
scrolling, drag feedback, tooltips, table virtualization, etc.

The server owns everything else: which widgets exist, how they're arranged, 
what data they show, and what happens on semantic actions.

The wire between them is a WebSocket carrying JSON. The server sends a full
`mount`, then `patch` ops (`insert`, `set`, `move`, `remove`, plus `rows`
for table data). The client sends events the server subscribed to. 

Sessions survive reconnects: the server keeps the tree for a grace
period and a reload reattaches to the same session.

Frame render when something changes. The renderer diffs against the retained
frame and repaints only the damaged rectangles. Animated shaders repaint
continuously while visible.

## Widgets

The vocabulary is fixed. Each widget bundles rendering, layout behavior, and
local interaction, and declares which events it can send.

| Widget | Client-local behavior | Events |
|---|---|---|
| `Panel` | background, border, clipping, corner radius | |
| `Label` | wrapping, ellipsis, text selection, copy | |
| `Image` | async load, aspect fit, rounded corners | |
| `Button` | hover/press/focus, keyboard activation | `OnClick` |
| `TextField` | full editing: caret, selection, IME, undo, revert | `OnInput` (debounced), `OnCommit` |
| `Textarea` | multi-line editing, soft wrap | `OnInput`, `OnCommit` |
| `Checkbox`, `Radio` | toggle | `OnToggle`, `OnSelect` |
| `Slider` | drag and keyboard adjust, stepping | `OnSlide`, `OnSlideEnd` |
| `Progress` | determinate fill | |
| `Select` | popover, keyboard nav | `OnSelect` |
| `Scroll` | wheel, momentum, scrollbars | visible range (if subscribed) |
| `Table` | virtualized rows, sort, column resize, keyboard nav | `OnRowSelect`, `OnRowActivate`, `OnSort`, `OnCellActivate` |
| `Tree` | the table in tree mode, disclosure per row | table events + `OnRowToggle` |
| `TabBar`, `HSplit`/`VSplit`, `Dialog` | tab strip, draggable split panes, modal | `OnSelect`, `OnSplitResize`, `OnDismiss` |
| `Shader` | a pane painted by an app-supplied fragment shader | |
| `Glass` | transparent pointer capture for custom interaction | `OnPick`, `OnDragTo`, `OnDrop`, `OnWheel`, right-button set |

Two props work on every widget: `Tip("...")` shows a tooltip after hover
idle, and `Context(items)` attaches a right-click menu.

## Layout

- **frame**: fixed x/y/w/h in the parent.
  `n.Frame(x, y, w, h)`
- **anchors**: pin any of left/right/top/bottom/centerX/centerY to the
  parent at offsets. Pin one horizontal edge and the width stays fixed, pin
  both and the child stretches.
  `n.Anchor(caution.A{Left: caution.Px(16), Right: caution.Px(16), Top: caution.Px(12)})`
- **dock**: children consume edges in order (toolbar top, status bar
  bottom, sidebar left), and the last child fills.
  `caution.DockPanel()`, `n.Dock("top")`
- **stack**: horizontal or vertical run with gap and alignment. Children
  are fixed, content-sized, or `Fill(weight)`.
  `caution.VStack()`, `caution.HStack()`
- **grid**: rows and columns for forms.
  `caution.Grid(cols)`

## Events and state

Build the tree in `mount`, then mutate nodes from handlers. Mutations are
the patches. Everything in one handler flushes as a single patch when it returns.

Handlers and `Session.Update` closures run on one goroutine per session, so
session state is plain local variables:

```go
s.Update(func() {              // server push, no client event involved
    clock.SetText(time.Now().Format("15:04:05"))
})
```

Text fields edit locally and stream debounced `OnInput`. The client is
authoritative for a focused field until `OnCommit`. To force a value from
the server use `ClearValue()` or `SetValueNow(v)`.

## Styling: two token tables

Widgets read named tokens from two tables the server can set at mount or patch live:

```go
s.SetTheme(map[string]string{     // colors
    "bg": "#16181d",
    "accent": "#4f8cff",
    "ink": "#e8eaf0",
})
s.SetMetrics(caution.MetricsCompact)  // geometry: radii, heights, padding
```

Theme tokens are colors ("bg", "panel", "accent", "ink", ...). Live theme updates
patch with a cross-fade. Metrics tokens are about sixteen numbers ("radius.control",
"control.height", "row.height", "checkbox.size", ...) that reshape the whole
widget vocabulary: square vs rounded, compact vs comfortable. `MetricsCompact`
and `MetricsComfortable` ship as plain maps to copy or tweak. Per-node props
(an explicit `Radius`, a table's `RowHeight`) override tokens. Colors accept
`#rrggbb[aa]` or token references (`"$accent"`).

## Tables and trees

The client keeps a sparse cache and asks for windows as the user scrolls.

```go
t := caution.Table([]caution.Col{
    {Key: "id", Title: "ID", Width: 80},
    {Key: "name", Title: "Name", Weight: 1},
    {Key: "ok", Title: "OK", Width: 60, Kind: "checkbox"},
}, rowCount).RowKey(0)

t.RowsFunc(func(start, end int) [][]string { ... })
t.OnRowSelectKey(func(key string, row int) { ... })
t.OnRowActivateKey(func(key string, row int) { ... }) // double-click or Enter
t.OnSort(func(key string, asc bool) { ... })
```

`RowKey(col)` makes selection identity the key so it survives sort and data 
changes.  `Col.Kind` renders a cell as a button, checkbox, or progress bar while
cells stay strings.

Activations arrive as `OnCellActivate(row, key, col, value)`. 

## Menus and shortcuts

```go
s.SetMenu(caution.Menu{Title: "Rows", Items: []caution.MenuItem{
    {ID: 1, Title: "Add Row", Key: "n"},          // bare key = Cmd+N
    {Sep: true},
    {ID: 2, Title: "Clear Rows…", Key: "shift+cmd+k"},
}})
```

On macOS the native terminal uses NSMenus. The browser and linux render it within
the app. Picks come back as session events. Session-wide shortcuts outside menus
register through the `keys` list and fire after the focused widget declines
the chord.

The mouse's extra buttons are session-wide too. Button 4 is back and 5 is
forward on every mouse that has them:

```go
s.OnAux(func(button int) {
    switch button {
    case 4:
        goBack()
    case 5:
        goForward()
    }
})
```

Both terminals swallow the press whether or not the app handles it, so the
browser will not navigate its own history out from under the app.

Entering and leaving the system's own fullscreen is reported the same way. On
macOS that is the green button and Cmd+Ctrl+F, in the browser it is the
fullscreen API. It is worth handling under `CustomTitlebar`, because fullscreen
has no traffic lights and the gutter the app leaves for them is dead space:

```go
s.OnFullscreen(func(on bool) {
    bar.H(map[bool]float64{false: 38, true: 28}[on])
})
```

## Custom shaders

Apps can ship GLSL over the wire. Two forms:

- `n.Effect(caution.FX{Frag: src})`
  The widget's subtree renders offscreen and composites back through your shader.
  A caveat is that hit-testing still sees the undistorted widgets, which
  manifests as the cursor not being in quite the expected place so keep that
  in mind for heavy distortions.
- `caution.Shader(caution.FX{Frag: src, Animate: true})`
  A pane painted entirely by the shader.

Define `vec4 effect(vec2 uv)` (uv origin top-left); available
are `u_time` (seconds), `u_res` (logical px), your float uniforms, and, for
effects, `src(uv)`, the subtree texture. `SetUniform(k, v)` ships one key
and merges, so tweening a uniform is cheap. A static shader costs nothing
after first paint; `Animate: true` repaints while visible, confined to its
own damage. Compile errors log and fall back to passthrough.

For sprite animation, `Preload` image frames once and patch a single `src`
prop per tick (`examples/nyan` runs 12fps this way on ~40 bytes per frame).

## Glass

`caution.Glass()` is a transparent pointer-capture surface for custom
interaction (canvases, editors, cameras). While subscribed it reports
`OnPick(x, y, target)` with the deepest server node under the point,
coalesced `OnDragTo`, and `OnDrop` as the authoritative endpoint. `OnWheel`
claims scrolling over the glass.
`OnRightPick`/`OnRightDragTo`/`OnRightDrop` claim the right button for
orbit/measure gestures, otherwise right-click keeps its context-menu
meaning. 

## UI documents and the designer

A widget tree can be data. A `.ui.json` document in wire format plus `name`
markers for outlets. 

```go
//go:embed window.ui.json
var windowUI []byte

win := caution.MustLoadUI(windowUI)
counter := win.Node("counter")
win.Node("inc").OnClick(func() { ... })
```

## Native apps

`native.Run(mount, native.Options{...})` runs the server and the native
terminal in one process over an in-memory pipe; `go build` is the bundler.

Packaging:

- macOS: `go run -C go ./cmd/appbundle -pkg ./cmd/demo -name "My App"`
- Linux: `go run -C go ./cmd/linuxapp -pkg ./cmd/demo -name "My App" -install`

Window options (`native.Options` / `terminal.Options` / `cmd/term` flags):

- `Fullscreen`, `HideCursor`, `ExitOnInput`
- `MaxFPS`: cap animation repaints.
- `CustomTitlebar`: hide the macOS system title bar and draw your own.

```go
bar := caution.Panel().Bg("$titlebar").WindowDrag().H(40).Kids(
    caution.Label("my app"),
    caution.Button("settings").H(26).Anchor(caution.A{Right: caution.Px(8), Top: caution.Px(7)}),
)
```

## Accessibility

The browser terminal mirrors the widget tree into a hidden DOM layer for 
screen readers.

## Deployment

`caution.ServeOpts(addr, mount, caution.Options{...})`:

- `Authorize(r) (identity, error)` runs on every connection, including
  resumes. A resumed session must present the same identity, so a leaked
  resume token cannot adopt another user's session
- `AllowedOrigins` (same-origin by default), `TLSCert`/`TLSKey`
- mount, handlers, and `Update` closures run behind a recover barrier: a
  panicking handler logs a stack trace and shows an in-app crash dialog

`ServeAssets(prefix, dir)` serves static files. `Image()` also accepts
`data:` URIs. `s.Preload(srcs...)` warms image caches for screens not yet
mounted.

## Clojure SDK

`clj/` is the clojure implementation of the SDK. See `clj/README.md`.

## The wire protocol

### Server to client

A full tree on mount, then patches.

```jsonc
{"t": "mount", "seq": 1, "root": {"id": 1, "type": "vstack", "p": {...}, "kids": [...]}}

{"t": "patch", "seq": 2, "ops": [
  {"op": "insert", "parent": 12, "index": 0,
   "node": {"id": 47, "type": "button", "p": {"label": "Save", "on": ["click"]}}},
  {"op": "set",    "id": 31, "p": {"text": "3 items"}},
  {"op": "move",   "id": 44, "parent": 12, "index": 2},
  {"op": "remove", "id": 19}
]}
```

Table rows travel as an op too:

```jsonc
{"op": "rows", "id": 60, "start": 180, "reset": false,
 "rows": [["E00181", "Ada Lovelace", "ada@example.com", "Engineer"]]}
```

Theme, metrics, menu, shortcut, and preload ops follow the same pattern and
also ride the mount.

### Client to server

Each event carries the last patch seq the client applied.

```jsonc
{"t": "ev", "id": 47, "ev": "click", "seq": 2}
{"t": "ev", "id": 19, "ev": "commit", "value": "hello", "seq": 2}
{"t": "ev", "id": 60, "ev": "visible-range", "value": {"start": 180, "end": 260}, "seq": 2}
{"t": "ev", "id": 60, "ev": "row-select", "value": {"row": 14, "key": "E00015"}, "seq": 2}
{"t": "ev", "id": 0, "ev": "aux", "value": 4, "seq": 2}
{"t": "ev", "id": 0, "ev": "fullscreen", "value": true, "seq": 2}
```

Node id 0 means the session owns the event, not a widget: viewport resizes, menu
picks, key combos, and the mouse's extra buttons.

### Rules

- Events are subscription-based: the client only emits what the server
  declared its interest in, with the session-level ones (resize, aux,
  fullscreen) as the exception, since no node exists to carry the subscription
- Node ids are server-owned
- An event is delivered only if its target id is known and its seq is at least
  the id's birth seq (the mount or patch that introduced it), so an event fired
  against a replaced tree doesn't apply to an unrelated widget

## Developing in this repo

```
go run -C go ./cmd/demo              # demo, serving the client live from src/
cd go && go tool tsgo --noEmit -p .. # typecheck the TypeScript
go test ./...                        # Go suites (run from go/)
go run -C go ./cmd/tstest            # TypeScript unit suite
go run -C go ./cmd/goldens           # browser vs native pixel parity (needs Chrome)
go run -C go ./cmd/damageprobe       # partial-invalidation pixel identity
go run -C go ./cmd/loopprobe         # presentation-loop behavior (also -browser, -leaks, -menu)
go run -C go ./cmd/bundle -minify    # regenerate the embedded terminal bundle
```

The headless verification mode used throughout:

```
go run -C go ./cmd/term -shot out.png -settle 2000 -clicks "120,424@700;key:enter@900"
```

renders a live session offscreen to PNG with scripted input (clicks, drags,
right-drags, wheel, typed text, key chords, menu picks).

## Trade-offs

- Browser conveniences that don't apply inside the canvas: find, whole page text
  selection, spellcheck, autofill, etc
- Password managers partially work through the hidden input funnel
- Server-driven UI degrades with the connection
- Complex-script coverage: text shapes and reorders correctly (bidi,
  ligatures, marks), but RTL scripts render placeholder boxes
- Emoji render monochrome
- IME composition works in the browser terminal only
