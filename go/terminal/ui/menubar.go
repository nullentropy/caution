package ui

import (
	"strings"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

// The in-window menu bar: the native terminal's realization of the server's
// menu spec on platforms without a system menu bar (Linux; forced on macOS
// via -menubar), and the mirror of the browser terminal's only realization.
// Client-local like the banner, never part of the server tree. Clicking a
// title opens its dropdown on the overlay layer, and while one is open, hovering
// another title switches to it. Picks flow through onPick as session-level
// `menu` events like NSMenu picks.
//
// Mirrored in the browser terminal (src/ui/menubar.ts): geometry and
// painting are identical so goldens can gate the bar.

// MenuSpec is one top-level menu, the ui-local copy of proto.MenuSpec, since
// this package cannot import proto.
type MenuSpec struct {
	Title string
	Items []MenuItemSpec
}

// MenuItemSpec is one entry: a separator, a submenu (Items), or a pickable
// item (ID + Title, optionally Key).
type MenuItemSpec struct {
	ID    int
	Title string
	Key   string
	Sep   bool
	Items []MenuItemSpec
}

// MenubarH is the bar's reserved height in logical px, the row.height token.
// The Ui's layout slot re-reads it every frame, so a metrics patch moves the
// whole tree under the bar.
func MenubarH() float32 { return Metrics.RowHeight }

// mbRowH is a dropdown row, the same token as the bar.
func mbRowH() float32 { return Metrics.RowHeight }

var barFont = gfx.NewFont(13, gfx.FontOpts{})
var sectionFont = gfx.NewFont(11, gfx.FontOpts{})

const (
	titlePad = 10
	mbSepH   = 9
	mbPad    = 6
)

// MenuBar is the bar widget itself. The Ui owns one in its Menubar slot.
type MenuBar struct {
	Core
	menus    []MenuSpec
	onPick   func(id int)
	hoverIdx int
	openIdx  int
	popover  *menuPopover
}

// SetMenubar installs (or clears, with an empty spec) the in-window menu
// bar. The tree lays out below it from the next frame.
func (u *Ui) SetMenubar(menus []MenuSpec, onPick func(id int)) {
	withItems := menus[:0:0]
	for _, m := range menus {
		if len(m.Items) > 0 {
			withItems = append(withItems, m)
		}
	}
	if len(withItems) == 0 {
		u.Menubar = nil
	} else {
		mb := &MenuBar{menus: withItems, onPick: onPick, hoverIdx: -1, openIdx: -1}
		mb.self = mb
		mb.UI = u
		u.Menubar = mb
	}
	u.Invalidate()
}

// Combos returns the menu key equivalents, canonicalized like registered
// combos (a bare key means command, the platform default for menus).
func (m *MenuBar) Combos() map[string]int {
	out := map[string]int{}
	var walk func(items []MenuItemSpec)
	walk = func(items []MenuItemSpec) {
		for _, it := range items {
			if len(it.Items) > 0 {
				walk(it.Items)
			} else if it.ID != 0 && it.Key != "" {
				out[CanonicalMenuCombo(it.Key)] = it.ID
			}
		}
	}
	for _, menu := range m.menus {
		walk(menu.Items)
	}
	return out
}

// Perform fires a pick by id: the keyboard path, and the probe hook
// ("menu:N" in shot scripts when no NSMenu is installed).
func (m *MenuBar) Perform(id int) bool {
	found := false
	var walk func(items []MenuItemSpec)
	walk = func(items []MenuItemSpec) {
		for _, it := range items {
			if len(it.Items) > 0 {
				walk(it.Items)
			} else if it.ID == id {
				found = true
			}
		}
	}
	for _, menu := range m.menus {
		walk(menu.Items)
	}
	if found {
		m.onPick(id)
	}
	return found
}

func (m *MenuBar) Interactive() bool { return true }

// PaintSelf reconciles open state at paint: if the overlay is no longer our
// popover (click-away, Escape, a pick), the highlight clears.
func (m *MenuBar) PaintSelf(dl *gfx.DisplayList) {
	if m.popover != nil && (m.UI == nil || m.UI.Overlay != Widget(m.popover)) {
		m.popover = nil
		m.openIdx = -1
	}
	b := m.Bounds
	dl.Fill(b, *Theme["titlebar"], gfx.Corners{})
	hair := Metrics.BorderWidth
	dl.Fill(gfx.R(b.X, b.Y+b.H-hair, b.W, hair), *Theme["edgeSoft"], gfx.Corners{})
	x := b.X + titlePad
	for i, menu := range m.menus {
		w := m.titleW(menu.Title)
		active := i == m.openIdx
		if active || i == m.hoverIdx {
			c := *Theme["control"]
			if active {
				c = gfx.WithAlpha(*Theme["accent"], 0.25)
			}
			dl.Fill(gfx.R(x, b.Y+3, w, b.H-7), c, gfx.CornerRadius(Metrics.RadiusControl))
		}
		run := m.UI.Measure(barFont, menu.Title)
		ink := Theme["inkDim"]
		if active || i == m.hoverIdx {
			ink = Theme["ink"]
		}
		dl.Text(menu.Title, x+titlePad, b.Y+(b.H-1-(run.Ascent+run.Descent))/2, barFont, *ink)
		x += w
	}
}

func (m *MenuBar) OnPointerDown(x, _ float32, _ int) {
	i := m.titleAt(x)
	if i < 0 {
		return
	}
	if i == m.openIdx {
		m.UI.CloseOverlay() // toggling the open title closes it
		return
	}
	m.open(i)
}

func (m *MenuBar) OnPointerHover(x, _ float32) {
	i := m.titleAt(x)
	if i != m.hoverIdx {
		m.hoverIdx = i
		m.Invalidate()
	}
	// The menubar convention: while a menu is open, pointing at another
	// title switches to it without a press.
	if m.openIdx >= 0 && i >= 0 && i != m.openIdx {
		m.open(i)
	}
}

func (m *MenuBar) OnHoverChange(hovered bool) {
	if !hovered && m.hoverIdx != -1 {
		m.hoverIdx = -1
		m.Invalidate()
	}
}

func (m *MenuBar) open(i int) {
	m.openIdx = i
	m.popover = openMenuPopover(m.UI, m.menus[i].Items, m.onPick, m.titleX(i), m.Bounds.Y+m.Bounds.H)
	m.Invalidate()
}

func (m *MenuBar) titleW(title string) float32 {
	return ceil32(m.UI.Measure(barFont, title).Width) + titlePad*2
}

func (m *MenuBar) titleX(idx int) float32 {
	x := m.Bounds.X + titlePad
	for i := 0; i < idx; i++ {
		x += m.titleW(m.menus[i].Title)
	}
	return x
}

func (m *MenuBar) titleAt(absX float32) int {
	x := m.Bounds.X + titlePad
	for i := range m.menus {
		w := m.titleW(m.menus[i].Title)
		if absX >= x && absX < x+w {
			return i
		}
		x += w
	}
	return -1
}

// menuRow is one dropdown row, flattened at open time. Submenus render as an
// indented section under a non-interactive header, the same as context menus.
type menuRow struct {
	item    MenuItemSpec
	indent  int
	section bool
}

// menuPopover is the dropdown under an open title, the context menu's
// pattern plus right-aligned key equivalents and submenu sections.
type menuPopover struct {
	Core
	rows     []menuRow
	onPick   func(id int)
	hoverRow int
}

func openMenuPopover(u *Ui, items []MenuItemSpec, onPick func(id int), x, y float32) *menuPopover {
	var rows []menuRow
	var flatten func(list []MenuItemSpec, indent int)
	flatten = func(list []MenuItemSpec, indent int) {
		for _, it := range list {
			if len(it.Items) > 0 {
				rows = append(rows, menuRow{item: it, indent: indent, section: true})
				flatten(it.Items, indent+1)
			} else {
				rows = append(rows, menuRow{item: it, indent: indent})
			}
		}
	}
	flatten(items, 0)
	p := &menuPopover{rows: rows, onPick: onPick, hoverRow: -1}
	p.self = p
	p.UI = u
	w := float32(160)
	h := float32(mbPad * 2)
	for _, r := range rows {
		if r.item.Sep {
			h += mbSepH
			continue
		}
		h += mbRowH()
		label := ceil32(u.Measure(barFont, r.item.Title).Width) + float32(r.indent*12)
		key := float32(0)
		if r.item.Key != "" {
			key = ceil32(u.Measure(barFont, ComboLabel(r.item.Key)).Width) + 24
		}
		w = max(w, label+key+44)
	}
	w = min(w, 360)
	px := max(8, min(x, u.lastW-w-8))
	py := max(8, min(y, u.lastH-h-8))
	p.Bounds = gfx.R(px, py, w, h)
	u.OpenOverlay(p)
	return p
}

func (p *menuPopover) Interactive() bool { return true }
func (p *menuPopover) Cursor() string    { return "pointer" }

func (p *menuPopover) PaintSelf(dl *gfx.DisplayList) {
	b := p.Bounds
	dl.Shadow(b, gfx.WithAlpha(gfx.Black, 0.45), 24, gfx.CornerRadius(Metrics.RadiusPopover), 0, 10)
	dl.Rect(b, *Theme["panel"], gfx.RectOpts{Radius: gfx.CornerRadius(Metrics.RadiusPopover), BorderWidth: Metrics.BorderWidth, BorderColor: Theme["edge"]})
	y := b.Y + mbPad
	for i, r := range p.rows {
		it := r.item
		if it.Sep {
			dl.Fill(gfx.R(b.X+8, y+mbSepH/2, b.W-16, Metrics.BorderWidth), *Theme["edgeSoft"], gfx.Corners{})
			y += mbSepH
			continue
		}
		x := b.X + 14 + float32(r.indent*12)
		if r.section {
			mm := p.UI.Measure(sectionFont, it.Title)
			dl.Text(it.Title, x, y+(mbRowH()-(mm.Ascent+mm.Descent))/2, sectionFont, *Theme["inkFaint"])
			y += mbRowH()
			continue
		}
		if i == p.hoverRow {
			dl.Fill(gfx.R(b.X+4, y, b.W-8, mbRowH()), gfx.WithAlpha(*Theme["accent"], 0.25), gfx.CornerRadius(Metrics.RadiusControl))
		}
		mm := p.UI.Measure(barFont, it.Title)
		dl.Text(it.Title, x, y+(mbRowH()-(mm.Ascent+mm.Descent))/2, barFont, *Theme["ink"])
		if it.Key != "" {
			label := ComboLabel(it.Key)
			km := p.UI.Measure(barFont, label)
			dl.Text(label, b.X+b.W-14-km.Width, y+(mbRowH()-(km.Ascent+km.Descent))/2, barFont, *Theme["inkFaint"])
		}
		y += mbRowH()
	}
}

func (p *menuPopover) OnPointerDown(_, y float32, _ int) {
	i := p.rowAt(y)
	if i < 0 || p.rows[i].section || p.rows[i].item.ID == 0 {
		return
	}
	id := p.rows[i].item.ID
	p.UI.CloseOverlay()
	p.onPick(id)
}

func (p *menuPopover) OnPointerHover(_, y float32) {
	i := p.rowAt(y)
	if i >= 0 && p.rows[i].section {
		i = -1
	}
	if i != p.hoverRow {
		p.hoverRow = i
		p.Invalidate()
	}
}

func (p *menuPopover) rowAt(absY float32) int {
	y := p.Bounds.Y + mbPad
	for i, r := range p.rows {
		h := float32(mbRowH())
		if r.item.Sep {
			h = mbSepH
		}
		if absY >= y && absY < y+h {
			if r.item.Sep {
				return -1
			}
			return i
		}
		y += h
	}
	return -1
}

// CanonicalMenuCombo normalizes a menu key spec ("shift+cmd+k"; a bare key
// means Cmd+key, the platform default) into the registered-combo form:
// cmd+ctrl+alt+shift+key. Mirrored in the browser terminal.
func CanonicalMenuCombo(spec string) string {
	parts := strings.Split(strings.ToLower(spec), "+")
	key := parts[len(parts)-1]
	mods := map[string]bool{}
	for _, p := range parts[:len(parts)-1] {
		mods[p] = true
	}
	has := func(names ...string) bool {
		for _, n := range names {
			if mods[n] {
				return true
			}
		}
		return false
	}
	out := ""
	if len(mods) == 0 || has("cmd", "command", "super") {
		out += "cmd+"
	}
	if has("ctrl", "control") {
		out += "ctrl+"
	}
	if has("opt", "option", "alt") {
		out += "alt+"
	}
	if has("shift") {
		out += "shift+"
	}
	if key == " " {
		key = "space"
	}
	return out + key
}

// ComboLabel is the display form of a menu key: "shift+cmd+k" -> "Shift+Cmd+K".
// Mac modifier order, spelled out rather than drawn as symbols, since the
// embedded fonts carry no modifier glyphs.
func ComboLabel(spec string) string {
	canonical := CanonicalMenuCombo(spec)
	parts := strings.Split(canonical, "+")
	key := parts[len(parts)-1]
	set := map[string]bool{}
	for _, p := range parts[:len(parts)-1] {
		set[p] = true
	}
	out := ""
	for _, m := range [...][2]string{{"ctrl", "Ctrl"}, {"alt", "Alt"}, {"shift", "Shift"}, {"cmd", "Cmd"}} {
		if set[m[0]] {
			out += m[1] + "+"
		}
	}
	return out + strings.ToUpper(key[:1]) + key[1:]
}
