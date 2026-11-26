package caution

import (
	"strconv"

	"github.com/nullentropy/caution/wire"
)

func TermGrid() *Node { return newNode(wire.TermGrid) }

type TermRun struct {
	X      int    `json:"x"`
	Text   string `json:"t"`
	Fg     string `json:"f,omitempty"`
	Bg     string `json:"b,omitempty"`
	Bold   bool   `json:"bo,omitempty"`
	Italic bool   `json:"it,omitempty"`
	Under  bool   `json:"u,omitempty"`
	Strike bool   `json:"s,omitempty"`
}

type TermCursor struct {
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Visible bool   `json:"v"`
	Blink   bool   `json:"blink,omitempty"`
	Shape   string `json:"shape,omitempty"`
	Ch      string `json:"ch,omitempty"`
	Fg      string `json:"f,omitempty"`
	Bg      string `json:"b,omitempty"`
}

type TermInput struct {
	Key                    string
	Text                   string
	Paste                  bool
	Ctrl, Alt, Shift, Meta bool
}

type TermMouse struct {
	Kind         string
	X, Y, Button int
	Dy           int
	Shift, Ctrl  bool
}

type termHandlers struct {
	input func(TermInput)
	mouse func(TermMouse)
	size  func(cols, rows int)
}

func (n *Node) termH() *termHandlers {
	if n.term == nil {
		n.term = &termHandlers{}
	}
	return n.term
}

func (n *Node) OnTermInput(fn func(TermInput)) *Node {
	n.termH().input = fn
	return n.subscribe(wire.EvTermInput)
}

func (n *Node) OnTermMouse(fn func(TermMouse)) *Node {
	n.termH().mouse = fn
	return n.subscribe(wire.EvTermMouse)
}

func (n *Node) OnGridSize(fn func(cols, rows int)) *Node {
	n.termH().size = fn
	return n.subscribe(wire.EvGridSize)
}

func (n *Node) TermColors(fg, bg string) *Node {
	return n.setMany(map[string]any{"fg": fg, "bg": bg})
}

func (n *Node) SetTermLines(lines map[int][]TermRun) *Node {
	p := make(map[string]any, len(lines))
	for y, runs := range lines {
		if runs == nil {
			runs = []TermRun{}
		}
		p["line:"+strconv.Itoa(y)] = runs
	}
	return n.setMany(p)
}

func (n *Node) SetTermCursor(c TermCursor) *Node { return n.set("cursor", c) }

func (n *Node) setMany(p map[string]any) *Node {
	if len(p) == 0 {
		return n
	}
	for k, v := range p {
		n.props[k] = v
	}
	if n.sess != nil {
		n.sess.ops = append(n.sess.ops, map[string]any{"op": wire.OpSet, "id": n.id, "p": p})
	}
	return n
}

func (n *Node) dispatchTerm(ev wire.Event, value any) {
	h := n.term
	if h == nil {
		return
	}
	v, _ := value.(map[string]any)
	b := func(k string) bool { x, _ := v[k].(bool); return x }
	s := func(k string) string { x, _ := v[k].(string); return x }
	switch ev {
	case wire.EvTermInput:
		if h.input != nil {
			h.input(TermInput{
				Key: s("k"), Text: s("t"), Paste: b("paste"),
				Ctrl: b("ctrl"), Alt: b("alt"), Shift: b("shift"), Meta: b("meta"),
			})
		}
	case wire.EvTermMouse:
		if h.mouse != nil {
			h.mouse(TermMouse{
				Kind: s("kind"), X: toInt(v["x"]), Y: toInt(v["y"]),
				Button: toInt(v["button"]), Dy: toInt(v["dy"]),
				Shift: b("shift"), Ctrl: b("ctrl"),
			})
		}
	case wire.EvGridSize:
		if h.size != nil {
			if c, r := toInt(v["cols"]), toInt(v["rows"]); c > 0 && r > 0 {
				h.size(c, r)
			}
		}
	}
}
