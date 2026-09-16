package ui

import (
	"fmt"
	"strings"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

// Semantic is what assistive technology should hear for a widget. Nil means
// decorative. A shell that has a DOM (the browser terminal) mirrors these
// into hidden elements screen readers can read and activate.
type Semantic struct {
	Role     string // text, button, checkbox, textbox, grid, image
	Label    string
	Checked  bool
	Value    string
	RowCount int
	Rows     []SemanticRow
}

// SemanticRow is one visible row of a grid.
type SemanticRow struct {
	Label    string
	Index    int
	Selected bool
	Bounds   gfx.Rect
}

// SemanticNode is one entry of the flattened tree the shell mirrors. IDs are
// stable per widget for its lifetime, so the shell can keep DOM focus on the
// same element across updates.
type SemanticNode struct {
	ID      int
	Widget  Widget
	Bounds  gfx.Rect
	Focused bool
	Semantic
}

// Semantics flattens the tree's meaning in paint order.
func (u *Ui) Semantics() []SemanticNode {
	var out []SemanticNode
	var walk func(w Widget)
	walk = func(w Widget) {
		b := w.Base()
		if s := w.Semantics(); s != nil {
			if b.semID == 0 {
				u.semSeq++
				b.semID = u.semSeq
			}
			out = append(out, SemanticNode{
				ID: b.semID, Widget: w, Bounds: b.Bounds, Focused: u.focused == w, Semantic: *s,
			})
		}
		for _, k := range b.Kids {
			walk(k)
		}
	}
	if u.Root != nil {
		walk(u.Root)
	}
	return out
}

// Activate is assistive technology pressing a widget, or one row of a grid
// when row is not negative.
func (u *Ui) Activate(w Widget, row int) {
	u.setFocus(w, false)
	if t, ok := w.(*TableView); ok && row >= 0 {
		t.selectRow(row)
		return
	}
	w.Activate()
}

func (l *Label) Semantics() *Semantic {
	if l.Text == "" {
		return nil
	}
	return &Semantic{Role: "text", Label: l.Text}
}

func (b *Button) Semantics() *Semantic {
	return &Semantic{Role: "button", Label: b.Label}
}

func (c *Checkbox) Semantics() *Semantic {
	return &Semantic{Role: "checkbox", Label: c.Label, Checked: c.Checked}
}

func (t *TextField) Semantics() *Semantic {
	return t.textboxSemantics("text field")
}

func (a *TextArea) Semantics() *Semantic {
	return a.textboxSemantics("text area")
}

func (t *TextField) textboxSemantics(fallback string) *Semantic {
	s := &Semantic{Role: "textbox", Label: t.Placeholder}
	if s.Label == "" {
		s.Label = fallback
	}
	if !t.Sensitive {
		s.Value = t.Value
	}
	return s
}

func (v *ImageView) Semantics() *Semantic {
	if v.Alt == "" {
		return nil
	}
	return &Semantic{Role: "image", Label: v.Alt}
}

func (p *Progress) Semantics() *Semantic {
	return &Semantic{Role: "text", Label: fmt.Sprintf("progress: %d%%", int(p.Value*100+0.5))}
}

func (s *Slider) Semantics() *Semantic {
	return &Semantic{Role: "text", Label: fmt.Sprintf("slider: %g", s.Value)}
}

func (r *RadioGroup) Semantics() *Semantic {
	label := "none"
	if r.Selected >= 0 && r.Selected < len(r.Options) {
		label = r.Options[r.Selected]
	}
	return &Semantic{Role: "text", Label: "radio group: " + label}
}

func (t *Tabs) Semantics() *Semantic {
	label := ""
	if t.Selected >= 0 && t.Selected < len(t.Options) {
		label = t.Options[t.Selected]
	}
	return &Semantic{Role: "text", Label: "tabs: " + label}
}

func (t *TableView) Semantics() *Semantic {
	v := t.viewport()
	var rows []SemanticRow
	if t.RowCount > 0 && v.H > 0 {
		rh := t.rowH()
		first := max(0, int(t.ScrollY/rh))
		last := min(t.RowCount-1, int((t.ScrollY+v.H)/rh))
		for i := first; i <= last; i++ {
			data, ok := t.cache[i]
			if !ok {
				continue
			}
			rows = append(rows, SemanticRow{
				Label:    strings.Join(data, " · "),
				Index:    i,
				Selected: t.isSelected(i, data, true),
				Bounds:   gfx.R(t.Bounds.X, v.Y+float32(i)*rh-t.ScrollY, t.Bounds.W, rh),
			})
		}
	}
	titles := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		titles[i] = c.Title
	}
	return &Semantic{
		Role:     "grid",
		Label:    fmt.Sprintf("table (%s), %d rows", strings.Join(titles, ", "), t.RowCount),
		RowCount: t.RowCount,
		Rows:     rows,
	}
}
