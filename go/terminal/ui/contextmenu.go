package ui

import (
	"github.com/nullentropy/caution/go/terminal/gfx"
)

// ContextItem is one entry of a node's `context` prop (flat list; no
// nesting yet).
type ContextItem struct {
	ID    int
	Title string
	Sep   bool
}

var ctxFont = gfx.NewFont(13, gfx.FontOpts{})

// ctxRowH is one menu row - the row.height token.
func ctxRowH() float32 { return Metrics.RowHeight }

const (
	ctxSepH = 9
	ctxPad  = 6
)

// contextMenu is the client-local right-click menu; it lives on the Ui
// overlay layer, so click-away and Escape close it for free.
type contextMenu struct {
	Core
	items    []ContextItem
	onPick   func(id int)
	hoverRow int
}

// ContextClick opens the context menu of the deepest carrier under (x, y) -
// the shell routes right presses here.
func (u *Ui) contextClick(x, y float32) {
	u.clearTip()
	u.dismissOverlay() // a previous menu/popover yields to the new press
	var hit Widget
	if u.Root != nil {
		hit = u.Root.HitTest(x, y)
	}
	for w := hit; w != nil; w = w.Base().Parent {
		items := w.Base().ContextItems
		if len(items) == 0 {
			continue
		}
		pick := w.Base().OnContextPick
		if pick == nil {
			pick = func(int) {}
		}
		u.openContextMenu(items, pick, x, y)
		return
	}
}

func (u *Ui) openContextMenu(items []ContextItem, onPick func(id int), x, y float32) {
	m := &contextMenu{items: items, onPick: onPick, hoverRow: -1}
	m.self = m
	m.UI = u
	w := float32(140)
	h := float32(ctxPad * 2)
	for _, it := range items {
		if it.Sep {
			h += ctxSepH
			continue
		}
		h += ctxRowH()
		w = max(w, ceil32(u.Measure(ctxFont, it.Title).Width)+40)
	}
	w = min(w, 340)
	px := max(8, min(x, u.lastW-w-8))
	py := y
	if y+h > u.lastH-8 {
		py = max(8, y-h) // flip up at the bottom edge
	}
	m.Bounds = gfx.R(px, py, w, h)
	u.OpenOverlay(m)
	u.Sound("open")
}

func (m *contextMenu) Interactive() bool { return true }
func (m *contextMenu) Cursor() string    { return "pointer" }

func (m *contextMenu) PaintSelf(dl *gfx.DisplayList) {
	b := m.Bounds
	dl.Shadow(b, gfx.WithAlpha(gfx.Black, 0.45), 24, gfx.CornerRadius(Metrics.RadiusPopover), 0, 10)
	dl.Rect(b, *Theme["panel"], gfx.RectOpts{Radius: gfx.CornerRadius(Metrics.RadiusPopover), BorderWidth: Metrics.BorderWidth, BorderColor: Theme["edge"]})
	y := b.Y + ctxPad
	for i, it := range m.items {
		if it.Sep {
			dl.Fill(gfx.R(b.X+8, y+ctxSepH/2, b.W-16, Metrics.BorderWidth), *Theme["edgeSoft"], gfx.Corners{})
			y += ctxSepH
			continue
		}
		if i == m.hoverRow {
			dl.Fill(gfx.R(b.X+4, y, b.W-8, ctxRowH()), gfx.WithAlpha(*Theme["accent"], 0.25), gfx.CornerRadius(Metrics.RadiusControl))
		}
		mm := m.UI.Measure(ctxFont, it.Title)
		dl.Text(it.Title, b.X+14, y+(ctxRowH()-(mm.Ascent+mm.Descent))/2, ctxFont, *Theme["ink"])
		y += ctxRowH()
	}
}

func (m *contextMenu) OnPointerDown(_, y float32, _ int) {
	i := m.rowAt(y)
	if i < 0 {
		return
	}
	it := m.items[i]
	m.UI.dismissOverlay()
	m.sound("select")
	m.onPick(it.ID)
}

func (m *contextMenu) OnPointerHover(_, y float32) {
	if i := m.rowAt(y); i != m.hoverRow {
		m.hoverRow = i
		m.Invalidate()
	}
}

// rowAt is the item index at an absolute y (separators are dead space).
func (m *contextMenu) rowAt(absY float32) int {
	y := m.Bounds.Y + ctxPad
	for i, it := range m.items {
		h := float32(ctxRowH())
		if it.Sep {
			h = ctxSepH
		}
		if absY >= y && absY < y+h {
			if it.Sep {
				return -1
			}
			return i
		}
		y += h
	}
	return -1
}
