package ui

import (
	"math"
	"strconv"
	"time"

	"github.com/nullentropy/caution/terminal/gfx"
)

type TableColumn struct {
	Key    string
	Title  string
	Weight float32
	Width  float32
	// in-cell widget column: "" text (default), "button", "checkbox", "progress"
	// cells stay strings, the column says how to render them
	Kind string
}

var (
	headerFont = gfx.NewFont(12, gfx.FontOpts{Weight: 600})
	cellFont   = gfx.NewFont(13, gfx.FontOpts{})
)

// read live so a metrics patch re-lays the header and cells
func tblHeaderH() float32 { return Metrics.TableHeaderH }
func tblCellPad() float32 { return Metrics.TableCellPadX }

const (
	// rows requested past the visible edge so scrolling has runway
	tblOverscan = 12
	// trailing throttle for visible-range reports
	tblReportMs = 100
)

// virtualized: rows are data, not widgets. the client holds a sparse row cache
// and paints only the visible window, scrolling reports a throttled
// `visible-range` upstream and `rows` ops fill the cache. sort order and the
// full dataset live on the server, selection/hover/scroll are client-local
// with silent server sync
type TableView struct {
	Scroller
	Columns  []TableColumn
	RowCount int
	// per-node `rowHeight`, 0 defers to the row.height token
	RowHeight float32
	SortKey   string
	SortDir   string // "asc" | "desc"
	Selected  int
	// column whose cells are stable row keys, -1 for keyless
	// with keys, selection is the key (SelectedKey, "" = none)
	KeyCol      int
	SelectedKey string
	// node type "tree": rows carry hierarchy meta, column 0 indents by depth and
	// draws a disclosure, selection identity is the meta key, and toggles
	// round-trip to the server (it owns expansion and reflattens)
	Tree bool

	OnVisibleRange func(start, end int)
	OnSort         func(key string, asc bool)
	// key is "" when keyless
	OnRowSelect func(row int, key string)
	// double-click or Enter, same identity as OnRowSelect (activation implies selection)
	OnRowActivate func(row int, key string)
	OnToggle      func(row int, key string)
	// button click or checkbox flip, value carries the proposed "true"/"false"
	OnCellActivate func(row int, key, col, value string)
	OnColResize    func(key string, width float32)

	// client-local widths from header divider drags, by column key
	// like scroll position it survives prop re-application but not a remount
	colOverrides map[string]float32
	colDragIdx   int
	colDragX     float32
	colDragW     float32
	hoverDivider int

	cache    map[int][]string
	meta     map[int]RowMeta
	hoverRow int
	// row of the last selection/keyboard move, what the arrows step from
	cursorRow int
	// a double-click must hit the same row twice (the router already checks
	// timing and radius)
	lastDownRow int
	lastSent    [2]int
	hasSent     bool
	lastSentAt  time.Time
}

type RowMeta struct {
	Key        string
	Depth      int
	Expandable bool
	Expanded   bool
}

const (
	treeIndent = 14 // px per depth level
	treeGlyphW = 18 // the disclosure glyph's slot
)

func NewTableView() *TableView {
	t := &TableView{Selected: -1, KeyCol: -1, hoverRow: -1, cursorRow: -1,
		lastDownRow: -1, colDragIdx: -1, hoverDivider: -1,
		colOverrides: map[string]float32{},
		cache:        map[int][]string{}, meta: map[int]RowMeta{}}
	t.self = t
	t.Clips = true
	t.viewportFn = func() gfx.Rect {
		b := t.Bounds
		return gfx.R(b.X, b.Y+tblHeaderH(), b.W, max(0, b.H-tblHeaderH()))
	}
	return t
}

// rowHeight prop when set, else the row.height token
func (t *TableView) rowH() float32 {
	if t.RowHeight > 0 {
		return t.RowHeight
	}
	return Metrics.RowHeight
}

func (t *TableView) Interactive() bool { return true }
func (t *TableView) Focusable() bool   { return true }

// arrows/Page/Home/End move the cursor row, select it and reveal it
func (t *TableView) OnKey(k Key) bool {
	n := t.RowCount
	if n <= 0 {
		return false
	}
	page := max(1, int(t.viewport().H/t.rowH())-1)
	var to int
	switch k.Name {
	case "Down":
		to = min(n-1, t.cursorRow+1)
	case "Up":
		to = max(0, t.cursorRow-1)
	case "PageDown":
		to = min(n-1, t.cursorRow+page)
	case "PageUp":
		to = max(0, t.cursorRow-page)
	case "Home":
		to = 0
	case "End":
		to = n - 1
	case "Enter":
		// enter selects AND activates
		if t.cursorRow < 0 {
			return false
		}
		t.revealRow(t.cursorRow)
		t.selectRow(t.cursorRow)
		t.activateRow(t.cursorRow)
		return true
	case "Right":
		// tree: expand the cursor row, or step into it once open
		if !t.Tree || t.cursorRow < 0 {
			return false
		}
		if m, ok := t.meta[t.cursorRow]; ok && m.Expandable && !m.Expanded {
			t.toggleRow(t.cursorRow)
			return true
		}
		to = min(n-1, t.cursorRow+1)
	case "Left":
		// tree: collapse the cursor row, or jump to its parent
		if !t.Tree || t.cursorRow < 0 {
			return false
		}
		m := t.meta[t.cursorRow]
		if m.Expandable && m.Expanded {
			t.toggleRow(t.cursorRow)
			return true
		}
		p := t.cursorRow - 1
		for p >= 0 && t.meta[p].Depth >= m.Depth {
			p--
		}
		if p < 0 {
			return true
		}
		to = p
	default:
		return false
	}
	t.cursorRow = max(0, to)
	t.revealRow(t.cursorRow)
	t.selectRow(t.cursorRow)
	return true
}

func (t *TableView) toggleRow(row int) {
	if m, ok := t.meta[row]; ok && m.Expandable && t.OnToggle != nil {
		t.sound("toggle")
		t.OnToggle(row, m.Key)
	}
}

func (t *TableView) revealRow(i int) {
	v := t.viewport()
	top := float32(i) * t.rowH()
	bot := top + t.rowH()
	if top < t.ScrollY {
		t.ScrollY = top
	} else if bot > t.ScrollY+v.H {
		t.ScrollY = bot - v.H
	}
	t.clampScroll()
	t.Invalidate()
}

func (t *TableView) LayoutChildren() {
	t.ContentH = float32(t.RowCount) * t.rowH()
	var w float32
	for _, cw := range t.colWidths() {
		w += cw
	}
	t.ContentW = w
	t.clampScroll()
}

// from a server `rows` op, reset replaces. meta is nil for tables, parallel
// to rows for trees
func (t *TableView) ApplyRows(start int, rows [][]string, meta []RowMeta, reset bool) {
	if reset {
		clear(t.cache)
		clear(t.meta)
	}
	for i, r := range rows {
		t.cache[start+i] = r
	}
	for i, m := range meta {
		t.meta[start+i] = m
	}
	t.evictFarRows()
	t.Invalidate()
}

// bounds the cache, a long scroll through a huge table would otherwise pin
// every row ever seen. far rows re-fetch on return
func (t *TableView) evictFarRows() {
	const maxRows = 2000
	const keep = 600 // rows kept on each side of the viewport
	if len(t.cache) <= maxRows {
		return
	}
	first := int(t.ScrollY / t.rowH())
	last := int((t.ScrollY + t.viewport().H) / t.rowH())
	for k := range t.cache {
		if k < first-keep || k > last+keep {
			delete(t.cache, k)
			delete(t.meta, k)
		}
	}
}

func (t *TableView) PaintSelf(dl *gfx.DisplayList) {
	b := t.Bounds
	v := t.viewport()
	widths := t.colWidths()
	if t.UI.ShowFocusRing(t) {
		PaintFocusRing(dl, b, Metrics.RadiusControl)
	}

	// header
	dl.Fill(gfx.R(b.X, b.Y, b.W, tblHeaderH()), *Theme["titlebar"],
		gfx.Corners{TL: Metrics.RadiusControl, TR: Metrics.RadiusControl})
	dl.PushClip(gfx.R(b.X, b.Y, b.W, tblHeaderH()))
	hx := b.X - t.ScrollX
	for i, c := range t.Columns {
		w := widths[i]
		title := c.Title
		if c.Key == t.SortKey && t.SortKey != "" {
			if t.SortDir == "asc" {
				title += "  ↑"
			} else {
				title += "  ↓"
			}
		}
		m := t.UI.Measure(headerFont, title)
		dl.PushClip(gfx.R(hx, b.Y, w, tblHeaderH()))
		dl.Text(title, hx+tblCellPad(), b.Y+(tblHeaderH()-(m.Ascent+m.Descent))/2, headerFont, *Theme["inkDim"])
		dl.PopClip()
		hx += w
	}
	dl.PopClip()
	hair := Metrics.BorderWidth
	dl.Fill(gfx.R(b.X, b.Y+tblHeaderH()-hair, b.W, hair), *Theme["edge"], gfx.Corners{})

	if t.RowCount <= 0 || v.H <= 0 {
		return
	}

	first := max(0, int(t.ScrollY/t.rowH()))
	last := min(t.RowCount-1, int((t.ScrollY+v.H)/t.rowH()))
	fm := t.UI.Measure(cellFont, "M")
	cellTextH := fm.Ascent + fm.Descent

	dl.PushClip(v)
	for i := first; i <= last; i++ {
		y := v.Y + float32(i)*t.rowH() - t.ScrollY
		rowRect := gfx.R(b.X, y, b.W, t.rowH())
		data, haveData := t.cache[i]
		switch {
		case t.isSelected(i, data, haveData):
			dl.Fill(rowRect, gfx.WithAlpha(*Theme["accent"], 0.22), gfx.Corners{})
		case i == t.hoverRow:
			dl.Fill(rowRect, gfx.WithAlpha(*Theme["ink"], 0.05), gfx.Corners{})
		case i%2 == 1:
			dl.Fill(rowRect, gfx.WithAlpha(*Theme["ink"], 0.02), gfx.Corners{})
		}

		textY := y + (t.rowH()-cellTextH)/2
		x := b.X - t.ScrollX
		for ci := range t.Columns {
			w := widths[ci]
			if haveData {
				cell := ""
				if ci < len(data) {
					cell = data[ci]
				}
				// tree: column 0 indents by depth and carries the disclosure
				tx := x + tblCellPad()
				if t.Tree && ci == 0 {
					m := t.meta[i]
					tx += float32(m.Depth) * treeIndent
					if m.Expandable {
						paintDisclosure(dl, float32(math.Floor(float64(tx))),
							float32(math.Floor(float64(y+t.rowH()/2))), m.Expanded)
					}
					tx += treeGlyphW
				}
				dl.PushClip(gfx.R(x, y, w-4, t.rowH()))
				switch t.Columns[ci].Kind {
				case "progress":
					bw := max(24, w-2*tblCellPad()-(tx-x-tblCellPad()))
					bar := gfx.R(tx, float32(math.Floor(float64(y+t.rowH()/2-3))), bw, 6)
					edge := *Theme["edgeSoft"]
					dl.Rect(bar, *Theme["panelInset"], gfx.RectOpts{
						Radius: gfx.CornerRadius(3), BorderWidth: Metrics.BorderWidth, BorderColor: &edge})
					if v := parseCellFloat(cell); v > 0 {
						dl.Fill(gfx.R(bar.X, bar.Y, max(6, bar.W*v), 6), *Theme["accent"], gfx.CornerRadius(3))
					}
				case "checkbox":
					// in-cell marks follow radius.control but cap at their own scale, so a
					// rounded theme can't turn a 16px box into a pill
					cb := gfx.R(tx, float32(math.Floor(float64(y+t.rowH()/2-8))), 16, 16)
					edge := *Theme["controlEdge"]
					dl.Rect(cb, *Theme["control"], gfx.RectOpts{
						Radius: gfx.CornerRadius(min(Metrics.RadiusControl, 4)), BorderWidth: Metrics.BorderWidth, BorderColor: &edge})
					// the mark is geometry (an inset fill) so both terminals agree to the pixel
					if cellTruthy(cell) {
						dl.Fill(gfx.R(cb.X+4, cb.Y+4, 8, 8), *Theme["accent"], gfx.CornerRadius(min(Metrics.RadiusControl, 2)))
					}
				case "button":
					m := t.UI.Measure(cellFont, cell)
					bw := min(w-2*tblCellPad(), m.Width+24)
					bt := gfx.R(tx, y+5, max(28, bw), t.rowH()-10)
					edge := *Theme["controlEdge"]
					dl.Rect(bt, *Theme["control"], gfx.RectOpts{
						Radius: gfx.CornerRadius(Metrics.RadiusControl), BorderWidth: Metrics.BorderWidth, BorderColor: &edge})
					dl.Text(cell, bt.X+(bt.W-m.Width)/2, textY, cellFont, *Theme["ink"])
				default:
					dl.Text(cell, tx, textY, cellFont, *Theme["ink"])
				}
				dl.PopClip()
			} else {
				// skeleton placeholder while the window streams in
				dl.Fill(gfx.R(x+tblCellPad(), y+t.rowH()/2-4, max(24, w*0.55), 8),
					gfx.WithAlpha(*Theme["ink"], 0.06), gfx.CornerRadius(4))
			}
			x += w
		}
	}
	dl.PopClip()

	t.reportRange(max(0, first-tblOverscan), min(t.RowCount-1, last+tblOverscan))
}

// -- interaction ---------------------------------------------------------------

func (t *TableView) HitTest(x, y float32) Widget {
	if Contains(t.Bounds, x, y) {
		return t
	}
	return nil
}

// divider under x in the header (boundary right of column i), or -1
func (t *TableView) dividerAt(x, y float32) int {
	if y >= t.Bounds.Y+tblHeaderH() {
		return -1
	}
	widths := t.colWidths()
	cx := t.Bounds.X - t.ScrollX
	for i := 0; i < len(t.Columns)-1; i++ {
		cx += widths[i]
		if abs32(x-cx) <= 4 {
			return i
		}
	}
	return -1
}

func (t *TableView) OnPointerDown(x, y float32, detail int) {
	if t.thumbDown(x, y) {
		return
	}
	b := t.Bounds
	if y < b.Y+tblHeaderH() {
		if div := t.dividerAt(x, y); div >= 0 {
			t.colDragIdx = div
			t.colDragX = x
			t.colDragW = t.colWidths()[div]
			return
		}
		widths := t.colWidths()
		cx := b.X - t.ScrollX
		for i, c := range t.Columns {
			cx += widths[i]
			if x < cx {
				asc := true
				if t.SortKey == c.Key {
					asc = t.SortDir != "asc"
				}
				t.SortKey = c.Key // local echo, server syncs silently
				t.SortDir = "asc"
				if !asc {
					t.SortDir = "desc"
				}
				t.Invalidate()
				if t.OnSort != nil {
					t.OnSort(c.Key, asc)
				}
				return
			}
		}
		return
	}
	row := t.rowAt(y)
	if row < 0 {
		return
	}
	// a click on the disclosure toggles without selecting
	if t.Tree {
		if m, ok := t.meta[row]; ok && m.Expandable {
			gx := b.X - t.ScrollX + tblCellPad() + float32(m.Depth)*treeIndent
			if x >= gx-4 && x < gx+treeGlyphW {
				t.lastDownRow = -1 // a toggle is not half of a double-click
				t.toggleRow(row)
				return
			}
		}
	}
	// in-cell widget columns act instead of selecting
	if ci := t.colIndexAt(x); ci >= 0 {
		if kind := t.Columns[ci].Kind; kind == "button" || kind == "checkbox" {
			if cells, ok := t.cache[row]; ok {
				t.lastDownRow = -1 // widget cells never count toward a double-click
				colKey := t.Columns[ci].Key
				if kind == "button" {
					if t.OnCellActivate != nil {
						t.OnCellActivate(row, t.rowKeyOf(row), colKey, "")
					}
					return
				}
				// local echo: flip the cached cell now, the server keeps it by updating its
				// data or vetoes with a refresh
				next := "true"
				if ci < len(cells) && cellTruthy(cells[ci]) {
					next = "false"
				}
				cp := append([]string(nil), cells...)
				for len(cp) <= ci {
					cp = append(cp, "")
				}
				cp[ci] = next
				t.cache[row] = cp
				t.Invalidate()
				if t.OnCellActivate != nil {
					t.OnCellActivate(row, t.rowKeyOf(row), colKey, next)
				}
				return
			}
		}
	}
	// the router counts same-widget multi-clicks, the table adds same-row
	// exactly 2 since a triple starts over
	dbl := detail == 2 && row == t.lastDownRow
	t.lastDownRow = row
	t.selectRow(row)
	if dbl {
		t.activateRow(row)
	}
}

func (t *TableView) activateRow(row int) {
	if t.OnRowActivate == nil {
		return
	}
	if t.Tree || t.KeyCol >= 0 {
		key := t.rowKeyOf(row)
		if key == "" {
			return // skeleton row, no identity yet
		}
		t.sound("press")
		t.OnRowActivate(row, key)
		return
	}
	t.sound("press")
	t.OnRowActivate(row, "")
}

func (t *TableView) colIndexAt(x float32) int {
	widths := t.colWidths()
	cx := t.Bounds.X - t.ScrollX
	for i := range t.Columns {
		cx += widths[i]
		if x < cx {
			return i
		}
	}
	return -1
}

// the wire ships plain strings
func cellTruthy(v string) bool { return v == "true" || v == "1" || v == "yes" }

func parseCellFloat(v string) float32 {
	f, err := strconv.ParseFloat(v, 32)
	if err != nil || f < 0 {
		return 0
	}
	return min(1, float32(f))
}

func (t *TableView) isSelected(i int, data []string, haveData bool) bool {
	if t.Tree {
		return t.SelectedKey != "" && t.meta[i].Key == t.SelectedKey
	}
	if t.KeyCol >= 0 {
		return t.SelectedKey != "" && haveData && t.KeyCol < len(data) && data[t.KeyCol] == t.SelectedKey
	}
	return i == t.Selected
}

// tree meta key, or the key column's cell for keyed tables ("" when unknown or keyless)
func (t *TableView) rowKeyOf(row int) string {
	if t.Tree {
		return t.meta[row].Key
	}
	if t.KeyCol >= 0 {
		if data, ok := t.cache[row]; ok && t.KeyCol < len(data) {
			return data[t.KeyCol]
		}
	}
	return ""
}

func (t *TableView) selectRow(row int) {
	t.cursorRow = row // clicks and keyboard share the cursor
	if t.Tree || t.KeyCol >= 0 {
		key := t.rowKeyOf(row)
		if key == "" {
			return // skeleton row, no identity to select yet
		}
		t.SelectedKey = key // local echo
		t.sound("select")
		t.Invalidate()
		if t.OnRowSelect != nil {
			t.OnRowSelect(row, key)
		}
		return
	}
	t.Selected = row // local echo
	t.sound("select")
	t.Invalidate()
	if t.OnRowSelect != nil {
		t.OnRowSelect(row, "")
	}
}

// geometry like the select chevron: shrinking bars, pointing right when
// collapsed, down when open
func paintDisclosure(dl *gfx.DisplayList, x, cy float32, open bool) {
	for i := 0; i < 5; i++ {
		s := float32(9 - 2*i)
		if open {
			dl.Fill(gfx.R(x+(9-s)/2, cy-3+float32(i), s, 1), *Theme["inkDim"], gfx.Corners{})
		} else {
			dl.Fill(gfx.R(x+float32(i), cy-s/2, 1, s), *Theme["inkDim"], gfx.Corners{})
		}
	}
}

func (t *TableView) OnPointerDrag(x, y float32) {
	if t.colDragIdx >= 0 {
		col := t.Columns[t.colDragIdx]
		t.colOverrides[col.Key] = max(40, t.colDragW+(x-t.colDragX))
		t.Invalidate()
		return
	}
	t.thumbDrag(x, y)
}

func (t *TableView) OnPointerUp(_, _ float32) {
	if t.colDragIdx >= 0 {
		col := t.Columns[t.colDragIdx]
		t.colDragIdx = -1
		if w, ok := t.colOverrides[col.Key]; ok && t.OnColResize != nil {
			t.OnColResize(col.Key, float32(int(w+0.5)))
		}
		return
	}
	t.thumbUp()
}

func (t *TableView) Cursor() string {
	if t.hoverDivider >= 0 || t.colDragIdx >= 0 {
		return "col-resize"
	}
	return ""
}

func (t *TableView) OnPointerHover(x, y float32) {
	if div := t.dividerAt(x, y); div != t.hoverDivider {
		t.hoverDivider = div
		t.Invalidate()
	}
	row := -1
	if y >= t.Bounds.Y+tblHeaderH() {
		row = t.rowAt(y)
	}
	if row != t.hoverRow {
		t.hoverRow = row
		t.Invalidate()
	}
}

func (t *TableView) OnHoverChange(h bool) {
	if !h && t.hoverRow != -1 {
		t.hoverRow = -1
		t.Invalidate()
	}
}

// -- internals -------------------------------------------------------------------

func (t *TableView) rowAt(y float32) int {
	v := t.viewport()
	i := int((y - v.Y + t.ScrollY) / t.rowH())
	if i >= 0 && i < t.RowCount {
		return i
	}
	return -1
}

func (t *TableView) colWidths() []float32 {
	px := func(c TableColumn) float32 {
		if o, ok := t.colOverrides[c.Key]; ok {
			return o
		}
		return c.Width
	}
	var fixed, weightSum float32
	for _, c := range t.Columns {
		if w := px(c); w > 0 {
			fixed += w
		} else {
			weightSum += weightOr1(c.Weight)
		}
	}
	free := max(0, t.Bounds.W-fixed)
	ws := weightSum
	if ws == 0 {
		ws = 1
	}
	// weighted columns never collapse below a readable floor, when fixed columns
	// (resizes included) eat the width the table scrolls horizontally instead
	const minW = 80
	out := make([]float32, len(t.Columns))
	for i, c := range t.Columns {
		if w := px(c); w > 0 {
			out[i] = w
		} else {
			out[i] = max(minW, free*weightOr1(c.Weight)/ws)
		}
	}
	return out
}

// one report per tblReportMs. the browser uses a trailing timer, here a
// suppressed report re-invalidates so a later frame retries until the trailing
// value flushes. same convergence, no timer thread
func (t *TableView) reportRange(start, end int) {
	if t.OnVisibleRange == nil || end < start {
		return
	}
	if t.hasSent && start == t.lastSent[0] && end == t.lastSent[1] {
		return
	}
	if t.hasSent && time.Since(t.lastSentAt) < tblReportMs*time.Millisecond {
		t.Invalidate()
		return
	}
	t.lastSent = [2]int{start, end}
	t.hasSent = true
	t.lastSentAt = time.Now()
	t.OnVisibleRange(start, end)
}
