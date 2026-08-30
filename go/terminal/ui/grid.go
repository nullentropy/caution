package ui

import "github.com/nullentropy/caution/go/terminal/gfx"

// GridTrack is a column track: fixed px, content (max natural width of its
// cells), or fill (shares leftover space by weight). Align places each cell's
// child within the column ('start' default; 'stretch' gives it the full cell
// width).
type GridTrack struct {
	Kind   string // "fixed" | "content" | "fill"
	Px     float32
	Weight float32
	Align  string // "start" | "center" | "end" | "stretch"
}

type gridPlaced struct {
	child    Widget
	row, col int
	span     int
}

// Grid is NSGridView-style rows x columns for forms and inspectors. Children
// flow row-major; GridSpan spans columns. Rows auto-size to their tallest
// cell and children center vertically within the row - the classic form
// layout (right-aligned caption column, control column) needs no manual
// offsets.
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

func (g *Grid) LayoutChildren() {
	g.adopt()
	n := len(g.Columns)
	if n == 0 || len(g.Kids) == 0 {
		return
	}
	placed := g.place()
	widths := g.contentWidths(placed)

	// Fill columns share whatever the fixed/content columns leave behind.
	used := g.ColGap * float32(n-1)
	var weightSum float32
	for i, t := range g.Columns {
		if t.Kind == "fill" {
			weightSum += weightOr1(t.Weight)
		} else {
			used += widths[i]
		}
	}
	free := max(0, g.Bounds.W-used)
	for i, t := range g.Columns {
		if t.Kind == "fill" {
			ws := weightSum
			if ws == 0 {
				ws = 1
			}
			widths[i] = free * weightOr1(t.Weight) / ws
		}
	}

	xs := make([]float32, n+1)
	for i := 0; i < n; i++ {
		xs[i+1] = xs[i] + widths[i] + g.ColGap
	}
	rows := g.rowHeights(placed)
	ys := make([]float32, len(rows)+1)
	for i := 0; i < len(rows); i++ {
		ys[i+1] = ys[i] + rows[i] + g.RowGap
	}

	for _, p := range placed {
		cellW := -g.ColGap
		for i := 0; i < p.span; i++ {
			if p.col+i < n {
				cellW += widths[p.col+i]
			}
			cellW += g.ColGap
		}
		cellX := g.Bounds.X + xs[p.col]
		rowY := g.Bounds.Y + ys[p.row]
		rowH := rows[p.row]
		// Spanning cells always stretch across their columns (merged-cell rule).
		align := g.Columns[p.col].Align
		if align == "" {
			align = "start"
		}
		if p.span > 1 {
			align = "stretch"
		}
		sz := sizeOf(p.child)
		w := min(sz.W, cellW)
		if align == "stretch" {
			w = cellW
		}
		x := cellX
		switch align {
		case "center":
			x += (cellW - w) / 2
		case "end":
			x += cellW - w
		}
		p.child.Base().Bounds = gfx.R(x, rowY+(rowH-sz.H)/2, w, sz.H)
		layoutSubtree(p.child)
	}
}

// place is row-major flow with span-aware wrapping.
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
		if t.Kind == "fixed" {
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
