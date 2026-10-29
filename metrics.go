package caution

// ready-made maps for Session.SetMetrics, plain data so copy one and tweak it
// names are terminal tokens (terminal/ui/metrics.go has the table and defaults)
// tokens left out keep the terminal default, per-node props still override
// the terminal defaults sit between these two
var (
	// dense: tight rows, short controls, small marks, for data-heavy screens
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

	// airy: tall rows, generous padding, for touch or presentation surfaces
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
