package caution

// Ready-made metric-token maps for Session.SetMetrics. They are plain data:
// copy one, change a value, pass your own map. Every name is a terminal token
// (go/terminal/ui/metrics.go has the table and the defaults). Tokens left out
// keep the terminal's default, and per-node props still override. The
// terminals' own defaults sit between these two.
var (
	// MetricsCompact is the dense look: tight rows, short controls, small
	// marks, for data-heavy screens.
	MetricsCompact = map[string]float64{
		"radius.control":     4,
		"radius.popover":     6,
		"radius.panel":       8,
		"control.height":     26,
		"control.padX":       10,
		"row.height":         22,
		"table.headerHeight": 26,
		"table.cellPadX":     8,
		"checkbox.size":      15,
		"slider.thumb":       7,
		"slider.track":       3,
		"dialog.titleHeight": 38,
		"space.pad":          7,
		"space.gap":          7,
	}

	// MetricsComfortable is the airy look: tall rows, generous padding, for
	// touch-friendly or presentation surfaces.
	MetricsComfortable = map[string]float64{
		"radius.control":     8,
		"radius.popover":     10,
		"radius.panel":       14,
		"control.height":     38,
		"control.padX":       20,
		"row.height":         34,
		"table.headerHeight": 38,
		"table.cellPadX":     16,
		"checkbox.size":      20,
		"slider.thumb":       10,
		"slider.track":       5,
		"dialog.titleHeight": 56,
		"space.pad":          13,
		"space.gap":          13,
	}
)
