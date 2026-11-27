package ui

import "github.com/nullentropy/caution/terminal/gfx"

type GridTrack struct {
	Kind   SizeKind
	Px     float32
	Weight float32
	Align  Align
}

type gridPlaced struct {
	child    Widget
	row, col int
	span     int
}

type Grid struct {
	Core
	Columns []GridTrack
	ColGap  float32
	RowGap  float32
}

func NewGrid() *Grid { g := &Grid{ColGap: 12, RowGap: 10}; g.self = g; return g }

func (g *Grid) IntrinsicSize() Size {
	g.adopt()
	if len(g.Columns) == 0 || len(g.Kids) == 0 {
		return Size{}
	}
	placed := g.place()
	widths := g.contentWidths(placed)
	rows := g.rowHeights(placed)
	var w, h float32
	for _, cw := range widths {
		w += cw
	}
	w += g.ColGap * float32(len(g.Columns)-1)
	for _, rh := range rows {
		h += rh
	}
	if len(rows) > 1 {
		h += g.RowGap * float32(len(rows)-1)
	}
	return Size{w, h}
}

func (g *Grid) HeightForWidth(w float32) (float32, bool) {
	g.adopt()
	if len(g.Columns) == 0 || len(g.Kids) == 0 {
		return 0, true
	}
	_, rows := g.cells(g.place(), w)
	var h float32
	for _, rh := range rows {
		h += rh
	}
	if len(rows) > 1 {
		h += g.RowGap * float32(len(rows)-1)
	}
	return h, true
}

func (g *Grid) LayoutChildren() {
	g.adopt()
	if len(g.Columns) == 0 || len(g.Kids) == 0 {
		return
	}
	cells, rows := g.cells(g.place(), g.Bounds.W)
	ys := make([]float32, len(rows)+1)
	for i := 0; i < len(rows); i++ {
		ys[i+1] = ys[i] + rows[i] + g.RowGap
	}
	for _, c := range cells {
		rowY := g.Bounds.Y + ys[c.row]
		c.child.Base().Bounds = gfx.R(g.Bounds.X+c.x, rowY+(rows[c.row]-c.h)/2, c.w, c.h)
		layoutSubtree(c.child)
	}
}

type gridCell struct {
	gridPlaced
	x, w, h float32
}

func (g *Grid) cells(placed []gridPlaced, gw float32) ([]gridCell, []float32) {
	n := len(g.Columns)
	widths := g.columnWidths(placed, gw)
	xs := make([]float32, n+1)
	for i := 0; i < n; i++ {
		xs[i+1] = xs[i] + widths[i] + g.ColGap
	}
	out := make([]gridCell, 0, len(placed))
	var rows []float32
	for _, p := range placed {
		cellW := -g.ColGap
		for i := 0; i < p.span; i++ {
			if p.col+i < n {
				cellW += widths[p.col+i]
			}
			cellW += g.ColGap
		}
		align := g.Columns[p.col].Align
		if align == "" {
			align = AlignStart
		}
		if p.span > 1 {
			align = AlignStretch
		}
		sz := sizeOf(p.child)
		w := min(sz.W, cellW)
		if align == AlignStretch {
			w = cellW
		}
		x := xs[p.col]
		switch align {
		case AlignCenter:
			x += (cellW - w) / 2
		case AlignEnd:
			x += cellW - w
		}
		h := heightAt(p.child, w, sz.H)
		out = append(out, gridCell{p, x, w, h})
		for len(rows) <= p.row {
			rows = append(rows, 0)
		}
		rows[p.row] = max(rows[p.row], h)
	}
	return out, rows
}

// content columns shrink toward their widest word before fill columns go
// below theirs, otherwise a long caption starves the control column
func (g *Grid) columnWidths(placed []gridPlaced, gw float32) []float32 {
	n := len(g.Columns)
	widths := g.contentWidths(placed)
	need := g.ColGap * float32(n-1)
	var weightSum, give float32
	floors := make([]float32, n)
	for i, t := range g.Columns {
		switch t.Kind {
		case SizeFill:
			weightSum += weightOr1(t.Weight)
			floors[i] = g.minColumnWidth(placed, i)
			need += floors[i]
		case SizeContent:
			floors[i] = min(widths[i], g.minColumnWidth(placed, i))
			give += widths[i] - floors[i]
			need += widths[i]
		default:
			need += widths[i]
		}
	}
	if over := need - gw; over > 0 && give > 0 {
		k := min(1, over/give)
		for i, t := range g.Columns {
			if t.Kind == SizeContent {
				widths[i] -= (widths[i] - floors[i]) * k
			}
		}
	}

	used := g.ColGap * float32(n-1)
	for i, t := range g.Columns {
		if t.Kind != SizeFill {
			used += widths[i]
		}
	}
	free := max(0, gw-used)
	for i, t := range g.Columns {
		if t.Kind == SizeFill {
			widths[i] = free * weightOr1(t.Weight) / weightSum
		}
	}
	return widths
}

func (g *Grid) place() []gridPlaced {
	n := len(g.Columns)
	out := make([]gridPlaced, 0, len(g.Kids))
	row, col := 0, 0
	for _, child := range g.Kids {
		span := child.Base().GridSpan
		if span < 1 {
			span = 1
		}
		if span > n {
			span = n
		}
		if col+span > n {
			row++
			col = 0
		}
		out = append(out, gridPlaced{child, row, col, span})
		col += span
		if col >= n {
			row++
			col = 0
		}
	}
	return out
}

func (g *Grid) contentWidths(placed []gridPlaced) []float32 {
	widths := make([]float32, len(g.Columns))
	for i, t := range g.Columns {
		if t.Kind == SizeFixed {
			widths[i] = t.Px
			continue
		}
		var w float32
		for _, p := range placed {
			if p.col == i && p.span == 1 {
				w = max(w, sizeOf(p.child).W)
			}
		}
		widths[i] = w
	}
	return widths
}

func (g *Grid) rowHeights(placed []gridPlaced) []float32 {
	var rows []float32
	for _, p := range placed {
		for len(rows) <= p.row {
			rows = append(rows, 0)
		}
		rows[p.row] = max(rows[p.row], sizeOf(p.child).H)
	}
	return rows
}

func weightOr1(w float32) float32 {
	if w == 0 {
		return 1
	}
	return w
}

func minWidthOf(c Widget) float32 {
	if w := c.Base().Width; w != nil {
		return *w
	}
	if m, ok := c.(interface{ MinContentWidth() (float32, bool) }); ok {
		if w, ok := m.MinContentWidth(); ok {
			return w
		}
	}
	return sizeOf(c).W
}

func (g *Grid) minColumnWidth(placed []gridPlaced, col int) float32 {
	var w float32
	for _, p := range placed {
		if p.col == col && p.span == 1 {
			w = max(w, minWidthOf(p.child))
		}
	}
	return w
}
