// caution design: a visual editor for .ui.json documents
//
//	go run ./cmd/design [-addr :9900] path/to/design.ui.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/nullentropy/caution/go"
	"github.com/nullentropy/caution/go/dev"
)

var paletteTypes = []string{
	"panel", "label", "button", "checkbox", "textfield", "select", "image",
	"hstack", "vstack", "scroll", "grid", "split", "dock", "shader",
}

func main() {
	addr := flag.String("addr", ":9900", "listen address")
	flag.Parse()
	path := flag.Arg(0)
	if path == "" {
		log.Fatal("usage: design [-addr :9900] <file.ui.json>")
	}
	dev.ServeClient("../src/terminal.ts")
	log.Fatal(caution.Serve(*addr, func(s *caution.Session) *caution.Node {
		return newDesigner(s, path).root
	}))
}

type dragState struct {
	node         *caution.Node
	x, y         float64 // the node's frame origin at pick
	pickX, pickY float64 // glass-relative pick point
	moved        bool
}

type designer struct {
	path       string
	root       *caution.Node
	design     *caution.Node // the document's live root, hosted on the canvas
	designSlot *caution.Node // its host - undo swaps the subtree here
	selected   *caution.Node

	flat   []*caution.Node // outline rows -> nodes
	labels []string

	tree      *caution.Node
	inspBody  *caution.Node
	status    *caution.Node
	undoStack [][]byte
	drag      *dragState
}

func newDesigner(s *caution.Session, path string) *designer {
	d := &designer{path: path}

	loadErr := ""
	if data, err := os.ReadFile(path); err == nil {
		if ui, uerr := caution.LoadUI(data); uerr == nil {
			d.design = ui.Root
		} else {
			loadErr = uerr.Error()
		}
	}
	if d.design == nil {
		d.design = caution.Panel().Bg("$panel").Radius(12)
	}
	ensureFill(d.design)

	d.status = caution.Label("").FontSize(12).Color("$inkFaint").
		Anchor(caution.A{Right: caution.Px(200), CenterY: caution.Px(0)})

	d.tree = caution.Table([]caution.Col{{Key: "node", Title: "Document", Weight: 1}}, 0).
		RowHeight(26).
		RowsFunc(func(start, end int) [][]string {
			out := make([][]string, 0, end-start+1)
			for i := start; i <= end && i < len(d.labels); i++ {
				if i < 0 {
					continue
				}
				out = append(out, []string{d.labels[i]})
			}
			return out
		}).
		OnRowSelect(func(i int) {
			if i >= 0 && i < len(d.flat) {
				d.selectNode(d.flat[i])
			}
		}).
		Anchor(caution.A{Left: caution.Px(8), Top: caution.Px(8), Right: caution.Px(8), Bottom: caution.Px(8)})

	d.inspBody = caution.VStack().Gap(10).Align("stretch").
		Anchor(caution.A{Left: caution.Px(12), Top: caution.Px(10), Right: caution.Px(12)})

	// The canvas: the document subtree in a swappable slot, and the glass on
	// top - clicks select, drags move, and the document itself never sees
	// the pointer while it's being edited.
	d.designSlot = caution.Panel().
		Anchor(caution.A{Left: caution.Px(0), Top: caution.Px(0), Right: caution.Px(0), Bottom: caution.Px(0)})
	d.designSlot.Add(d.design)
	glass := caution.Glass().
		Anchor(caution.A{Left: caution.Px(0), Top: caution.Px(0), Right: caution.Px(0), Bottom: caution.Px(0)}).
		OnPick(d.canvasPick).
		OnDragTo(d.canvasDrag).
		OnDrop(d.canvasDrop)
	canvasHost := caution.Panel().Bg("$bg").Radius(8).Border("$edgeSoft", 1).
		Anchor(caution.A{Left: caution.Px(12), Top: caution.Px(12), Right: caution.Px(12), Bottom: caution.Px(12)})
	canvasHost.Kids(d.designSlot, glass)

	d.root = caution.Panel()
	shell := caution.DockPanel().
		Anchor(caution.A{Left: caution.Px(0), Right: caution.Px(0), Top: caution.Px(0), Bottom: caution.Px(0)})
	d.root.Add(shell)
	shell.Kids(
		// toolbar
		caution.Panel().Dock("top").H(48).Bg("$panelAlt").Kids(
			caution.Label("caution design - "+path).FontSize(14).Weight(600).
				Anchor(caution.A{Left: caution.Px(16), CenterY: caution.Px(0), Right: caution.Px(320)}),
			d.status,
			caution.Button("Undo").
				Anchor(caution.A{Right: caution.Px(104), CenterY: caution.Px(0)}).
				OnClick(d.undo),
			caution.Button("Save").Primary().
				Anchor(caution.A{Right: caution.Px(12), CenterY: caution.Px(0)}).
				OnClick(d.save),
		),
		// outline | canvas | inspector
		caution.HSplit().SplitPos(280).SplitMin(200, 560).Kids(
			caution.Panel().Bg("$panelAlt").Border("$edgeSoft", 1).Kids(d.tree),
			caution.HSplit().SplitPos(620).SplitMin(320, 300).Kids(
				caution.Panel().Kids(canvasHost),
				caution.Panel().Bg("$panelAlt").Border("$edgeSoft", 1).Kids(
					caution.Scroll().InsetBottom(16).
						Anchor(caution.A{Left: caution.Px(1), Top: caution.Px(1), Right: caution.Px(1), Bottom: caution.Px(1)}).
						Kids(d.inspBody),
				),
			),
		),
	)

	s.OnKey("cmd+z", d.undo)

	if loadErr != "" {
		d.setStatus("load error: " + loadErr)
	} else {
		d.setStatus("ready")
	}
	d.refreshTree()
	d.selectNode(d.design)
	return d
}

// ensureFill: a root with no placement fills the canvas (parents that assign
// bounds ignore anchors).
func ensureFill(n *caution.Node) {
	if n.Prop("anchors") == nil && n.Prop("frame") == nil && n.Prop("width") == nil {
		n.SetProp("anchors", map[string]any{"left": 0.0, "right": 0.0, "top": 0.0, "bottom": 0.0})
	}
}

// -- canvas: pick, drag, drop -------------------------------------------------------

// canvasPick selects the design node under the click and arms a drag when
// the node is frame-placed (anchored/docked/stacked nodes are positioned by
// their parents - the inspector owns those).
func (d *designer) canvasPick(x, y float64, target *caution.Node) {
	n := d.designNodeFor(target)
	if n == nil {
		n = d.design
	}
	d.selectNode(n)
	d.drag = nil
	if n == d.design {
		return
	}
	if fx, fy, _, _, ok := frameOf(n); ok {
		d.drag = &dragState{node: n, x: fx, y: fy, pickX: x, pickY: y}
	} else {
		d.setStatus(n.Type() + " is placed by its parent - edit anchors/dock in the inspector")
	}
}

func (d *designer) canvasDrag(x, y float64) {
	dr := d.drag
	if dr == nil {
		return
	}
	if !dr.moved {
		d.pushUndo() // one snapshot per drag gesture, at the pre-move state
		dr.moved = true
	}
	_, _, w, h, _ := frameOf(dr.node)
	nx := snap(dr.x + (x - dr.pickX))
	ny := snap(dr.y + (y - dr.pickY))
	dr.node.SetProp("frame", []float64{nx, ny, w, h})
}

func (d *designer) canvasDrop(x, y float64) {
	dr := d.drag
	d.drag = nil
	if dr == nil {
		return
	}
	// The drop is the authoritative endpoint: intermediate drags coalesce
	// client-side and a quick drag can arrive as pick+drop alone. A release
	// within a couple of pixels of the pick is a click, not a move.
	if !dr.moved && math.Abs(x-dr.pickX)+math.Abs(y-dr.pickY) < 3 {
		return
	}
	if !dr.moved {
		d.pushUndo()
	}
	d.canvasDragTo(dr, x, y)
	fx, fy, _, _, _ := frameOf(dr.node)
	d.setStatus(fmt.Sprintf("moved %s to %g, %g", dr.node.Type(), fx, fy))
	d.rebuildInspector() // fresh frame values
}

// canvasDragTo applies the final position (drags coalesce client-side; the
// drop always carries the true endpoint).
func (d *designer) canvasDragTo(dr *dragState, x, y float64) {
	_, _, w, h, _ := frameOf(dr.node)
	dr.node.SetProp("frame", []float64{snap(dr.x + (x - dr.pickX)), snap(dr.y + (y - dr.pickY)), w, h})
}

// snap quantizes drag positions to whole pixels in 2px steps - designer
// documents shouldn't collect fractional offsets from pointer noise.
func snap(v float64) float64 { return math.Round(v/2) * 2 }

// designNodeFor maps a glass pick target into the document: the target
// itself when it lives under the design root, nil otherwise (canvas dead
// space, designer chrome).
func (d *designer) designNodeFor(n *caution.Node) *caution.Node {
	for w := n; w != nil; w = w.Parent() {
		if w == d.design {
			return n
		}
	}
	return nil
}

// frameOf reads a node's frame prop ([]float64 fresh, []any after a load).
func frameOf(n *caution.Node) (x, y, w, h float64, ok bool) {
	switch f := n.Prop("frame").(type) {
	case []float64:
		if len(f) == 4 {
			return f[0], f[1], f[2], f[3], true
		}
	case []any:
		if len(f) == 4 {
			vals := make([]float64, 4)
			for i, v := range f {
				fv, isF := v.(float64)
				if !isF {
					return 0, 0, 0, 0, false
				}
				vals[i] = fv
			}
			return vals[0], vals[1], vals[2], vals[3], true
		}
	}
	return 0, 0, 0, 0, false
}

// -- undo ---------------------------------------------------------------------------

// pushUndo snapshots the document (its serialized form - the same bytes
// Save writes) before a mutation.
func (d *designer) pushUndo() {
	data, err := caution.SaveUI(d.design)
	if err != nil {
		return
	}
	d.undoStack = append(d.undoStack, data)
	if len(d.undoStack) > 100 {
		d.undoStack = d.undoStack[1:]
	}
}

func (d *designer) undo() {
	if len(d.undoStack) == 0 {
		d.setStatus("nothing to undo")
		return
	}
	data := d.undoStack[len(d.undoStack)-1]
	d.undoStack = d.undoStack[:len(d.undoStack)-1]
	ui, err := caution.LoadUI(data)
	if err != nil {
		d.setStatus("undo failed: " + err.Error())
		return
	}
	d.designSlot.Clear()
	d.design = ui.Root
	ensureFill(d.design)
	d.designSlot.Add(d.design)
	d.drag = nil
	d.selectNode(d.design)
	d.setStatus(fmt.Sprintf("undid (%d left)", len(d.undoStack)))
}

// -- document outline ------------------------------------------------------------

func (d *designer) refreshTree() {
	d.flat = d.flat[:0]
	d.labels = d.labels[:0]
	d.walk(d.design, 0)
	d.tree.SetRowCount(len(d.flat))
	sel := -1
	for i, n := range d.flat {
		if n == d.selected {
			sel = i
		}
	}
	d.tree.SetSelected(sel)
}

func (d *designer) walk(n *caution.Node, depth int) {
	label := strings.Repeat("      ", depth) + n.Type()
	if name, ok := n.Prop("name").(string); ok && name != "" {
		label += "  ·  " + name
	}
	d.flat = append(d.flat, n)
	d.labels = append(d.labels, label)
	for _, k := range n.Children() {
		d.walk(k, depth+1)
	}
}

// -- selection & inspector ----------------------------------------------------------

func (d *designer) selectNode(n *caution.Node) {
	if d.selected != nil && d.selected != n {
		d.selected.SetProp("outline", false)
	}
	d.selected = n
	if n != nil {
		n.SetProp("outline", true)
	}
	d.rebuildInspector()
	d.refreshTree()
}

func (d *designer) rebuildInspector() {
	d.inspBody.Clear()
	n := d.selected
	if n == nil {
		d.inspBody.Add(caution.Label("select a node in the outline or click it on the canvas").
			FontSize(13).Color("$inkDim"))
		return
	}

	d.inspBody.Add(caution.Label(n.Type()).FontSize(16).Weight(600))
	d.addTypedEditors(n)
	d.addFrameEditor(n)
	d.addJSONGrid(n)
	d.addPropAdder(n)
	d.addChildEditors(n)
}

// section is the inspector's group header.
func section(title string) *caution.Node {
	return caution.Label(title).FontSize(11).Color("$inkFaint")
}

// -- typed editors: the right control per prop, undo per commit ---------------------

func (d *designer) field(n *caution.Node, key, label string) *caution.Node {
	cur, _ := n.Prop(key).(string)
	return d.labeled(label, caution.TextField(cur).OnCommit(func(v string) {
		d.pushUndo()
		n.SetProp(key, v)
		d.refreshTree()
		d.setStatus(fmt.Sprintf("set %s.%s", n.Type(), key))
	}))
}

func (d *designer) check(n *caution.Node, key, label string) *caution.Node {
	cur, _ := n.Prop(key).(bool)
	return caution.Checkbox(label, cur).OnToggle(func(v bool) {
		d.pushUndo()
		n.SetProp(key, v)
		d.setStatus(fmt.Sprintf("set %s.%s", n.Type(), key))
	})
}

// slider edits a numeric prop with live preview: the first movement of a
// gesture snapshots the pre-drag document, later movements just retarget.
func (d *designer) slider(n *caution.Node, key, label string, minV, maxV, def float64) *caution.Node {
	cur := def
	if f, ok := propFloat(n, key); ok {
		cur = f
	}
	dirty := false
	s := caution.Slider(minV, maxV, cur).Step(1).
		OnSlide(func(v float64) {
			if !dirty {
				d.pushUndo()
				dirty = true
			}
			n.SetProp(key, v)
		}).
		OnSlideEnd(func(v float64) {
			dirty = false
			n.SetProp(key, v)
			d.setStatus(fmt.Sprintf("set %s.%s = %g", n.Type(), key, v))
		})
	return d.labeled(label, s)
}

func (d *designer) selectOf(n *caution.Node, key, label string, options []string, def string) *caution.Node {
	cur, _ := n.Prop(key).(string)
	if cur == "" {
		cur = def
	}
	idx := 0
	for i, o := range options {
		if o == cur {
			idx = i
		}
	}
	return d.labeled(label, caution.Select(options, idx).OnSelect(func(i int) {
		d.pushUndo()
		n.SetProp(key, options[i])
		d.setStatus(fmt.Sprintf("set %s.%s = %s", n.Type(), key, options[i]))
	}))
}

func (d *designer) labeled(label string, w *caution.Node) *caution.Node {
	return caution.Grid([]caution.GridCol{{Px: 84, Align: "end"}, {Weight: 1, Align: "stretch"}}).
		ColGap(10).Kids(caution.Label(label).FontSize(12).Color("$inkDim"), w)
}

func propFloat(n *caution.Node, key string) (float64, bool) {
	f, ok := n.Prop(key).(float64)
	return f, ok
}

func (d *designer) addTypedEditors(n *caution.Node) {
	rows := []*caution.Node{}
	add := func(ws ...*caution.Node) { rows = append(rows, ws...) }
	switch n.Type() {
	case "label":
		add(d.field(n, "text", "text"),
			d.slider(n, "fontSize", "size", 8, 40, 13),
			d.selectOf(n, "color", "color", []string{"$ink", "$inkDim", "$inkFaint", "$accent"}, "$ink"),
			d.slider(n, "weight", "weight", 300, 800, 400),
			d.check(n, "wrap", "wrap"))
	case "button":
		add(d.field(n, "label", "label"), d.check(n, "primary", "primary"))
	case "panel":
		add(d.field(n, "bg", "bg"),
			d.slider(n, "radius", "radius", 0, 24, 0),
			d.field(n, "border", "border"))
	case "checkbox":
		add(d.field(n, "label", "label"), d.check(n, "checked", "checked"))
	case "textfield":
		add(d.field(n, "value", "value"), d.field(n, "placeholder", "placeholder"))
	case "image":
		add(d.field(n, "src", "src"),
			d.selectOf(n, "fit", "fit", []string{"contain", "cover", "fill"}, "contain"),
			d.slider(n, "radius", "radius", 0, 24, 0))
	case "shader":
		cur, _ := n.Prop("frag").(string)
		add(caution.Textarea(cur).Rows(6).OnCommit(func(v string) {
			d.pushUndo()
			n.SetProp("frag", v)
			d.setStatus("set shader.frag")
		}), d.check(n, "animate", "animate"))
	case "hstack", "vstack":
		add(d.slider(n, "gap", "gap", 0, 32, 8),
			d.slider(n, "pad", "pad", 0, 32, 0),
			d.selectOf(n, "align", "align", []string{"start", "center", "end", "stretch"}, "start"))
	case "grid":
		add(d.slider(n, "colGap", "col gap", 0, 32, 8),
			d.slider(n, "rowGap", "row gap", 0, 32, 8))
	}
	if len(rows) == 0 {
		return
	}
	d.inspBody.Add(section(n.Type() + " props"))
	for _, r := range rows {
		d.inspBody.Add(r)
	}
}

// addFrameEditor: numeric x/y/w/h for frame-placed nodes (the drag's exact
// values, editable); a placement hint otherwise.
func (d *designer) addFrameEditor(n *caution.Node) {
	if n == d.design {
		return
	}
	x, y, w, h, ok := frameOf(n)
	if !ok {
		return
	}
	d.inspBody.Add(section("frame - drag on the canvas, or edit"))
	mk := func(label string, cur float64, set func(v float64, f [4]float64) [4]float64) *caution.Node {
		return d.labeled(label, caution.TextField(fmt.Sprintf("%g", cur)).OnCommit(func(s string) {
			var v float64
			if _, err := fmt.Sscanf(strings.TrimSpace(s), "%g", &v); err != nil {
				d.setStatus("frame: not a number: " + s)
				return
			}
			fx, fy, fw, fh, _ := frameOf(n)
			nf := set(v, [4]float64{fx, fy, fw, fh})
			d.pushUndo()
			n.SetProp("frame", nf[:])
			d.setStatus(fmt.Sprintf("frame = %g,%g %gx%g", nf[0], nf[1], nf[2], nf[3]))
		}))
	}
	d.inspBody.Add(mk("x", x, func(v float64, f [4]float64) [4]float64 { f[0] = v; return f }))
	d.inspBody.Add(mk("y", y, func(v float64, f [4]float64) [4]float64 { f[1] = v; return f }))
	d.inspBody.Add(mk("w", w, func(v float64, f [4]float64) [4]float64 { f[2] = v; return f }))
	d.inspBody.Add(mk("h", h, func(v float64, f [4]float64) [4]float64 { f[3] = v; return f }))
}

// addJSONGrid: every prop, raw wire-form JSON - the fallback that can
// express anything the typed editors don't.
func (d *designer) addJSONGrid(n *caution.Node) {
	keys := n.PropKeys()
	shown := []string{}
	for _, k := range keys {
		if k == "on" || k == "outline" {
			continue
		}
		shown = append(shown, k)
	}
	if len(shown) == 0 {
		return
	}
	d.inspBody.Add(section("all props (wire form)"))
	grid := caution.Grid([]caution.GridCol{{Align: "end"}, {Weight: 1, Align: "stretch"}}).
		ColGap(10).RowGap(8)
	for _, k := range shown {
		k := k
		val, _ := json.Marshal(n.Prop(k))
		grid.Kids(
			caution.Label(k).FontSize(12).Color("$inkDim"),
			caution.TextField(string(val)).OnCommit(func(v string) {
				d.commitProp(n, k, v)
			}),
		)
	}
	d.inspBody.Add(grid)
}

func (d *designer) addPropAdder(n *caution.Node) {
	newKey, newVal := "", ""
	d.inspBody.Add(section("add prop"))
	d.inspBody.Add(caution.TextField("").Placeholder("prop name").
		OnCommit(func(v string) { newKey = v }))
	d.inspBody.Add(caution.TextField("").Placeholder(`value - JSON: 24, "$accent", {"left":10}`).
		OnCommit(func(v string) { newVal = v }))
	d.inspBody.Add(caution.Button("Set prop").OnClick(func() {
		if k := strings.TrimSpace(newKey); k != "" {
			d.commitProp(n, k, newVal)
			d.rebuildInspector()
		}
	}))
}

func (d *designer) addChildEditors(n *caution.Node) {
	typeIdx := 0
	d.inspBody.Add(section("children"))
	d.inspBody.Add(caution.Select(paletteTypes, 0).OnSelect(func(i int) { typeIdx = i }))
	d.inspBody.Add(caution.Button("Add child").OnClick(func() {
		d.pushUndo()
		child := paletteNode(paletteTypes[typeIdx])
		n.Add(child)
		d.selectNode(child)
		d.setStatus("added " + child.Type())
	}))
	d.inspBody.Add(caution.Button("Delete node").OnClick(func() {
		if n == d.design {
			d.setStatus("can't delete the document root")
			return
		}
		d.pushUndo()
		parent := n.Parent()
		n.Remove()
		d.selectNode(parent)
		d.setStatus("deleted " + n.Type())
	}))
}

func (d *designer) commitProp(n *caution.Node, key, raw string) {
	raw = strings.TrimSpace(raw)
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		v = raw // bare words are a convenience for strings
	}
	d.pushUndo()
	n.SetProp(key, v)
	d.refreshTree() // names and types show in the outline
	d.setStatus(fmt.Sprintf("set %s.%s", n.Type(), key))
}

// -- palette & save ----------------------------------------------------------------

// imagePlaceholder is a self-contained SVG so a freshly added image renders
// immediately - an empty src paints nothing, which on a canvas reads as a
// failed add. Replace src in the inspector. The explicit width/height matter:
// a viewBox-only SVG has no intrinsic size, and texImage2D rejects the 0x0
// decode ("bad image data").
const imagePlaceholder = "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' width='160' height='120' viewBox='0 0 4 3'>" +
	"<rect width='4' height='3' fill='%23445'/><circle cx='1.3' cy='.9' r='.4' fill='%23778'/>" +
	"<path d='M0 3 1.5 1.6l1 .9L3.3 1.7 4 2.5V3z' fill='%23667'/></svg>"

func paletteNode(t string) *caution.Node {
	switch t {
	case "label":
		return caution.Label("Label").Frame(20, 20, 120, 18)
	case "button":
		return caution.Button("Button").Frame(20, 20, 100, 32)
	case "checkbox":
		return caution.Checkbox("Checkbox", false).Frame(20, 20, 180, 20)
	case "textfield":
		return caution.TextField("").Placeholder("text…").Frame(20, 20, 200, 32)
	case "select":
		return caution.Select([]string{"One", "Two"}, 0).Frame(20, 20, 140, 32)
	case "image":
		return caution.Image(imagePlaceholder).Alt("image").Frame(20, 20, 160, 120)
	case "hstack":
		return caution.HStack().Frame(20, 20, 260, 40)
	case "vstack":
		return caution.VStack().Frame(20, 20, 180, 140)
	case "scroll":
		return caution.Scroll().Frame(20, 20, 220, 160)
	case "grid":
		return caution.Grid([]caution.GridCol{{Align: "end"}, {Weight: 1, Align: "stretch"}}).
			Frame(20, 20, 260, 100)
	case "split":
		return caution.HSplit().Frame(20, 20, 320, 160)
	case "dock":
		return caution.DockPanel().Frame(20, 20, 320, 200)
	case "shader":
		return caution.Shader(caution.FX{
			Frag: "vec4 effect(vec2 uv) { return vec4(uv.x, uv.y, 1.0 - uv.x, 1.0); }",
		}).Frame(20, 20, 200, 140)
	default:
		return caution.Panel().Bg("$panelInset").Radius(8).Frame(20, 20, 220, 140)
	}
}

func (d *designer) save() {
	data, err := caution.SaveUI(d.design)
	if err != nil {
		d.setStatus("save failed: " + err.Error())
		return
	}
	if err := os.WriteFile(d.path, append(data, '\n'), 0o644); err != nil {
		d.setStatus("save failed: " + err.Error())
		return
	}
	d.setStatus("saved at " + time.Now().Format("15:04:05"))
}

func (d *designer) setStatus(msg string) {
	d.status.SetText(msg)
}
