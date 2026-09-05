package ui

import (
	"testing"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

func soundUi() (*Ui, *[]string) {
	u := fakeUi()
	played := &[]string{}
	u.PlaySound = func(src string) { *played = append(*played, src) }
	u.SetSounds(map[string]string{
		"press": "/press.wav", "toggle": "/toggle.wav", "select": "/select.wav",
		"open": "/open.wav", "close": "/close.wav", "type": "/type.wav",
		"whoosh": "/whoosh.wav",
	})
	return u, played
}

func mounted(u *Ui, w Widget) {
	root := NewPanel()
	root.Add(w)
	u.Root = root
	layout(u, root, 400, 300)
}

func wantPlayed(t *testing.T, played *[]string, want ...string) {
	t.Helper()
	got := *played
	if len(got) != len(want) {
		t.Fatalf("played %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("played %v; want %v", got, want)
		}
	}
	*played = (*played)[:0]
}

func TestSoundTokensPerWidget(t *testing.T) {
	u, played := soundUi()

	btn := NewButton("go")
	btn.Frame = &gfx.Rect{X: 10, Y: 10, W: 100, H: 30}
	mounted(u, btn)
	btn.Activate()
	wantPlayed(t, played, "/press.wav")
	btn.OnPointerDown(20, 20, 1)
	btn.OnPointerUp(20, 20)
	wantPlayed(t, played, "/press.wav")
	btn.OnPointerDown(20, 20, 1)
	btn.OnPointerUp(300, 300) // released outside: no click, no sound
	wantPlayed(t, played)

	cb := NewCheckbox("opt")
	mounted(u, cb)
	cb.Activate()
	wantPlayed(t, played, "/toggle.wav")

	rg := NewRadioGroup()
	rg.Options = []string{"a", "b"}
	mounted(u, rg)
	rg.pick(1)
	wantPlayed(t, played, "/toggle.wav")

	tabs := NewTabs()
	tabs.Options = []string{"a", "b"}
	mounted(u, tabs)
	tabs.pick(1)
	wantPlayed(t, played, "/select.wav")
	tabs.pick(1) // already selected: nothing happens, nothing sounds
	wantPlayed(t, played)

	tf := NewTextField()
	mounted(u, tf)
	tf.OnChar('a')
	wantPlayed(t, played, "/type.wav")
	tf.OnChar(0x08)
	wantPlayed(t, played)
}

func TestSoundTokensForPopoversAndDialogs(t *testing.T) {
	u, played := soundUi()

	sel := NewSelect()
	sel.Options = []string{"one", "two"}
	sel.Frame = &gfx.Rect{X: 10, Y: 10, W: 120, H: 28}
	mounted(u, sel)
	sel.toggle()
	wantPlayed(t, played, "/open.wav")
	sel.Pick(1)
	wantPlayed(t, played, "/select.wav")
	sel.toggle()
	sel.toggle() // the second press on an open select closes it
	wantPlayed(t, played, "/open.wav", "/close.wav")

	sel.toggle()
	*played = (*played)[:0]
	u.KeyDown(Key{Name: "Escape"})
	wantPlayed(t, played, "/close.wav")

	sel.toggle()
	*played = (*played)[:0]
	u.PointerDown(390, 290) // click-away
	wantPlayed(t, played, "/close.wav")

	d := NewDialog()
	d.CardW, d.CardH = 200, 100
	d.Anchors = &Anchors{Left: f32p(0), Right: f32p(0), Top: f32p(0), Bottom: f32p(0)}
	d.OnDismiss = func() {}
	mounted(u, d)
	d.OnPointerDown(2, 2, 1) // on the scrim
	wantPlayed(t, played, "/close.wav")
	u.KeyDown(Key{Name: "Escape"})
	wantPlayed(t, played, "/close.wav")

	picked := 0
	u.openContextMenu([]ContextItem{{ID: 7, Title: "Cut"}}, func(int) { picked++ }, 50, 50)
	wantPlayed(t, played, "/open.wav")
	m := u.Overlay.(*contextMenu)
	m.OnPointerDown(m.Bounds.X+10, m.Bounds.Y+ctxPad+2, 1)
	wantPlayed(t, played, "/select.wav")
	if picked != 1 {
		t.Fatalf("context pick fired %d times", picked)
	}

	u.SetMenubar([]MenuSpec{{Title: "File", Items: []MenuItemSpec{{ID: 3, Title: "New"}}}}, func(int) {})
	u.Menubar.Perform(3)
	wantPlayed(t, played, "/select.wav")
	u.Menubar.Perform(99)
	wantPlayed(t, played)
}

func TestSoundTableRows(t *testing.T) {
	u, played := soundUi()
	tv := NewTableView()
	tv.Columns = []TableColumn{{Key: "a", Title: "A", Weight: 1}}
	tv.RowCount = 3
	tv.OnRowSelect = func(int, string) {}
	tv.OnRowActivate = func(int, string) {}
	tv.OnToggle = func(int, string) {}
	tv.Frame = &gfx.Rect{X: 0, Y: 0, W: 300, H: 200}
	mounted(u, tv)
	tv.ApplyRows(0, [][]string{{"x"}, {"y"}, {"z"}}, nil, true)

	tv.selectRow(1)
	wantPlayed(t, played, "/select.wav")
	tv.activateRow(1)
	wantPlayed(t, played, "/press.wav")

	tv.Tree = true
	tv.ApplyRows(0, [][]string{{"x"}, {"y"}, {"z"}}, []RowMeta{{Key: "k0", Expandable: true}, {Key: "k1"}, {Key: "k2"}}, true)
	tv.toggleRow(0)
	wantPlayed(t, played, "/toggle.wav")
	tv.toggleRow(1) // a leaf has nothing to toggle
	wantPlayed(t, played)
}

func TestSoundOverridesAndSilence(t *testing.T) {
	u, played := soundUi()
	btn := NewButton("send")
	mounted(u, btn)

	btn.SoundToken = "whoosh"
	btn.Activate()
	wantPlayed(t, played, "/whoosh.wav")

	btn.SoundToken = "none"
	btn.Activate()
	wantPlayed(t, played)

	btn.SoundToken = "nothing-in-the-table"
	btn.Activate()
	wantPlayed(t, played)

	// no table at all, or no hook, is silent and does not crash
	btn.SoundToken = ""
	u.SetSounds(nil)
	btn.Activate()
	wantPlayed(t, played)
	u.SetSounds(map[string]string{"press": "/press.wav"})
	u.PlaySound = nil
	btn.Activate()
}
