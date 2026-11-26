package ui

var Metrics = defaultMetrics()

type MetricSet struct {
	RadiusControl float32
	RadiusPopover float32
	RadiusPanel   float32
	BorderWidth   float32
	FocusRing     float32

	ControlHeight float32
	ControlPadX   float32
	RowHeight     float32
	TableHeaderH  float32
	TableCellPadX float32
	CheckboxSize  float32
	SliderThumb   float32
	SliderTrack   float32
	DialogTitleH  float32
	SpacePad      float32
	SpaceGap      float32
}

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
