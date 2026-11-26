package ui

import "github.com/nullentropy/caution/terminal/gfx"

var dialogTitleFont = gfx.NewFont(15, gfx.FontOpts{Weight: 600})

func dialogTitleH() float32 { return Metrics.DialogTitleH }

type Dialog struct {
	Core
	Title     string
	CardW     float32
	CardH     float32
	OnDismiss func()
}

func NewDialog() *Dialog {
	d := &Dialog{CardW: 420, CardH: 200}
	d.self = d
	return d
}

func (d *Dialog) Interactive() bool { return true }

func (d *Dialog) cardRect() gfx.Rect {
	b := d.Bounds
	return gfx.R(b.X+(b.W-d.CardW)/2, b.Y+(b.H-d.CardH)/2, d.CardW, d.CardH)
}

func (d *Dialog) titleH() float32 {
	if d.Title == "" {
		return 0
	}
	return dialogTitleH()
}

func (d *Dialog) LayoutChildren() {
	d.adopt()
	card := d.cardRect()
	d.LayoutInto(gfx.R(card.X, card.Y+d.titleH(), card.W, card.H-d.titleH()))
}

func (d *Dialog) PaintSelf(dl *gfx.DisplayList) {
	dl.Fill(d.Bounds, gfx.WithAlpha(gfx.Black, 0.45), gfx.Corners{})
	card := d.cardRect()
	dl.Shadow(card, gfx.WithAlpha(gfx.Black, 0.5), 40, gfx.CornerRadius(Metrics.RadiusPanel), 0, 18)
	dl.Rect(card, *Theme["panel"], gfx.RectOpts{
		Radius: gfx.CornerRadius(Metrics.RadiusPanel), BorderWidth: Metrics.BorderWidth, BorderColor: Theme["edge"],
	})
	if d.Title == "" {
		return
	}
	m := d.UI.Measure(dialogTitleFont, d.Title)
	dl.Text(d.Title, card.X+20, card.Y+(dialogTitleH()-(m.Ascent+m.Descent))/2, dialogTitleFont, *Theme["ink"])
	hair := Metrics.BorderWidth
	dl.Fill(gfx.R(card.X, card.Y+dialogTitleH()-hair, card.W, hair), *Theme["edge"], gfx.Corners{})
}

func (d *Dialog) OnPointerDown(x, y float32, _ int) {
	if !Contains(d.cardRect(), x, y) && d.OnDismiss != nil {
		d.sound("close")
		d.OnDismiss()
	}
}
