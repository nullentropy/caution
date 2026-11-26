package proto

import (
	"strconv"
	"strings"

	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"
)

func applyTermGrid(t *ui.TermGrid, id int, p props, sink EventSink) {
	setOr(p, "size", &t.FontSize, 14)
	set(p, "font", &t.Family)
	set(p, "pad", &t.Pad)
	if c := color(p["fg"]); c != nil {
		t.Fg = *c
	}
	if c := color(p["bg"]); c != nil {
		t.Bg = *c
	}
	if o, ok := p["cursor"].(props); ok {
		f := func(k string) int { n, _ := num(o[k]); return int(n) }
		t.Caret.X, t.Caret.Y = f("x"), f("y")
		x, y := t.Caret.X, t.Caret.Y
		t.Caret.Visible, _ = boolean(o["v"])
		t.Caret.Blink, _ = boolean(o["blink"])
		t.Caret.Shape, _ = str(o["shape"])
		t.Caret.Ch, _ = str(o["ch"])
		t.Caret.Fg, t.Caret.Bg = color(o["f"]), color(o["b"])
		if t.Caret.X != x || t.Caret.Y != y {
			t.Touch()
		}
	}
	for k, v := range p {
		row, ok := strings.CutPrefix(k, "line:")
		if !ok {
			continue
		}
		y, err := strconv.Atoi(row)
		if err != nil {
			continue
		}
		arr, _ := v.([]any)
		runs := make([]ui.TermRun, 0, len(arr))
		for _, rv := range arr {
			o, _ := rv.(props)
			var r ui.TermRun
			x, _ := num(o["x"])
			r.X = int(x)
			r.Text, _ = str(o["t"])
			r.Fg, r.Bg = color(o["f"]), color(o["b"])
			r.Bold, _ = boolean(o["bo"])
			r.Italic, _ = boolean(o["it"])
			r.Under, _ = boolean(o["u"])
			r.Strike, _ = boolean(o["s"])
			runs = append(runs, r)
		}
		t.Lines[y] = runs
	}
	if subscribed(p, wire.EvTermInput) {
		t.OnInput = func(v map[string]any) { sink.Event(id, wire.EvTermInput, v) }
	}
	if subscribed(p, wire.EvTermMouse) {
		t.OnMouse = func(v map[string]any) { sink.Event(id, wire.EvTermMouse, v) }
	}
	if subscribed(p, wire.EvGridSize) {
		t.OnGridSize = func(c, r int) { sink.Event(id, wire.EvGridSize, map[string]any{"cols": c, "rows": r}) }
	}
}
