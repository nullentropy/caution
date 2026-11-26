package ui

import (
	"github.com/nullentropy/caution/terminal/gfx"
)

type ContextItem struct {
	ID    int
	Title string
	Sep   bool
}

var ctxFont = gfx.NewFont(13, gfx.FontOpts{})

func ctxRowH() float32 { return Metrics.RowHeight }

const (
	ctxSepH = 9
	ctxPad  = 6
)

type contextMenu struct {
	Core
	items    []ContextItem
	onPick   func(id int)
	hoverRow int
}

func (u *UI) contextClick(x, y float32) {
	u.clearTip()
	u.dismissOverlay()
	var hit Widget
	if u.Root != nil {
		hit = u.Root.HitTest(x, y)
	}
	for w := hit; w != nil; w = w.Base().Parent {
		if s, e, ok := u.selectionWithin(w); ok && len(w.Base().SelectionItems) > 0 {
			pick := w.Base().OnSelectionPick
			if pick == nil {
				pick = func(int, int, int) {}
			}
			u.openContextMenu(w.Base().SelectionItems, func(id int) { pick(id, s, e) }, x, y)
			return
		}
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

func (u *UI) selectionWithin(w Widget) (start, end int, ok bool) {
	if u.sel == nil || u.sel.Start == u.sel.End {
		return 0, 0, false
	}
	for o := Widget(u.sel.Owner); o != nil; o = o.Base().Parent {
		if o == w {
			return min(u.sel.Start, u.sel.End), max(u.sel.Start, u.sel.End), true
		}
	}
	return 0, 0, false
}

func (u *UI) openContextMenu(items []ContextItem, onPick func(id int), x, y float32) {
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
		py = max(8, y-h)
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
		m.InvalidateWidget()
	}
}

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
