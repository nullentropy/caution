package ui

// Metrics is the geometry half of the design-token system, Theme's sibling:
// where Theme says what color a control edge is, Metrics says how round its
// corner is. It is a closed set of named numbers the server can set, not a
// stylesheet. Per-node props (Radius, RowHeight, explicit sizes) still
// override, and tokens are only the defaults widget chrome is built from.
//
// Widgets read fields at measure/paint time so a `metrics` op restyles
// live widgets, like theme colors.
var Metrics = defaultMetrics()

// MetricSet's fields are the wire tokens; applyMetricTokens maps the dotted
// names. Everything is logical px.
type MetricSet struct {
	RadiusControl float32 // "radius.control": buttons, fields, checkboxes, row highlights
	RadiusPopover float32 // "radius.popover": menus, dropdowns, floating lists
	RadiusPanel   float32 // "radius.panel": dialog cards
	BorderWidth   float32 // "border.width": control hairlines and separators (emphasis = x1.5)
	FocusRing     float32 // "focus.ring": the keyboard-focus ring's stroke width

	ControlHeight float32 // "control.height": buttons, selects, text fields (min); tabs +2
	ControlPadX   float32 // "control.padX": a control's horizontal text padding (buttons, tabs)
	RowHeight     float32 // "row.height": list rows - menus, select popups, radio rows, the menubar; the table's default
	TableHeaderH  float32 // "table.headerHeight"
	TableCellPadX float32 // "table.cellPadX"
	CheckboxSize  float32 // "checkbox.size": the checkbox box; radios are -2 (circles read bigger than squares)
	SliderThumb   float32 // "slider.thumb": thumb radius
	SliderTrack   float32 // "slider.track": track height
	DialogTitleH  float32 // "dialog.titleHeight"
	SpacePad      float32 // "space.pad": field/select inner text inset
	SpaceGap      float32 // "space.gap": mark<->label gap (checkbox, radio)
}

// CheckboxRadius is the corner radius the checkbox mark actually paints:
// radius.control capped at a third of checkbox.size. Shape carries meaning for
// marks, where a rounded square is a checkbox and a circle is a radio, and the
// default pair (18, 6) sits at the cap, so smaller boxes keep the default's
// proportion instead of growing rounder until the two stop being
// distinguishable. The table's 16px in-cell miniature caps for the same
// reason.
func (m MetricSet) CheckboxRadius() float32 {
	return min(m.RadiusControl, m.CheckboxSize/3)
}

func defaultMetrics() MetricSet {
	return MetricSet{
		RadiusControl: 6,
		RadiusPopover: 8,
		RadiusPanel:   12,
		BorderWidth:   1,
		FocusRing:     2,

		ControlHeight: 32,
		ControlPadX:   16,
		RowHeight:     28,
		TableHeaderH:  32,
		TableCellPadX: 12,
		CheckboxSize:  18,
		SliderThumb:   8,
		SliderTrack:   4,
		DialogTitleH:  48,
		SpacePad:      10,
		SpaceGap:      10,
	}
}

// applyMetricTokens mutates the table in place; unknown names are ignored (a
// newer server against an older terminal degrades to the defaults it ships).
// Callers outside the protocol layer want Ui.ApplyMetrics, which also drops
// size memos and rebuilds the frame.
func applyMetricTokens(tokens map[string]float64) {
	for name, v := range tokens {
		if p := metricSlot(name); p != nil {
			*p = float32(v)
		}
	}
}

func metricSlot(name string) *float32 {
	switch name {
	case "radius.control":
		return &Metrics.RadiusControl
	case "radius.popover":
		return &Metrics.RadiusPopover
	case "radius.panel":
		return &Metrics.RadiusPanel
	case "border.width":
		return &Metrics.BorderWidth
	case "focus.ring":
		return &Metrics.FocusRing
	case "control.height":
		return &Metrics.ControlHeight
	case "control.padX":
		return &Metrics.ControlPadX
	case "row.height":
		return &Metrics.RowHeight
	case "table.headerHeight":
		return &Metrics.TableHeaderH
	case "table.cellPadX":
		return &Metrics.TableCellPadX
	case "checkbox.size":
		return &Metrics.CheckboxSize
	case "slider.thumb":
		return &Metrics.SliderThumb
	case "slider.track":
		return &Metrics.SliderTrack
	case "dialog.titleHeight":
		return &Metrics.DialogTitleH
	case "space.pad":
		return &Metrics.SpacePad
	case "space.gap":
		return &Metrics.SpaceGap
	}
	return nil
}
