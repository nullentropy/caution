package ui

import (
	"testing"

	"github.com/nullentropy/caution/go/terminal/gfx"
)

func demoMenus(picked *[]int) ([]MenuSpec, func(id int)) {
	menus := []MenuSpec{
		{Title: "Rows", Items: []MenuItemSpec{
			{ID: 1, Title: "Add Row", Key: "n"},
			{Sep: true},
			{ID: 2, Title: "Clear Rows…", Key: "shift+cmd+k"},
			{Title: "Presets", Items: []MenuItemSpec{
				{ID: 3, Title: "Ten"},
				{ID: 4, Title: "Hundred", Key: "ctrl+opt+h"},
			}},
		}},
		{Title: "Theme", Items: []MenuItemSpec{
			{ID: 5, Title: "Midnight"},
		}},
	}
	return menus, func(id int) { *picked = append(*picked, id) }
}

func TestMenubarReservesHeightAndPushesTreeDown(t *testing.T) {
	u := fakeUi()
	root := NewPanel()
	u.Root = root

	var picked []int
	menus, pick := demoMenus(&picked)
	u.SetMenubar(menus, pick)
	dl := &gfx.DisplayList{}
	u.BuildFrame(dl, 800, 600)
	wantBounds(t, root, 0, MenubarH(), 800, 600-MenubarH())
	wantBounds(t, u.Menubar, 0, 0, 800, MenubarH())

	// Clearing the spec gives the tree the whole viewport back.
	u.SetMenubar(nil, pick)
	u.BuildFrame(&gfx.DisplayList{}, 800, 600)
	if u.Menubar != nil {
		t.Fatal("empty spec must clear the bar")
	}
	wantBounds(t, root, 0, 0, 800, 600)
}

func TestMenubarClickOpensAndPicks(t *testing.T) {
	u := fakeUi()
	u.Root = NewPanel()
	var picked []int
	menus, pick := demoMenus(&picked)
	u.SetMenubar(menus, pick)
	u.BuildFrame(&gfx.DisplayList{}, 800, 600)

	// Click the first title: its dropdown opens on the overlay layer.
	u.PointerDown(20, 13)
	u.PointerUp(20, 13)
	if u.Overlay == nil {
		t.Fatal("clicking a title must open its dropdown")
	}
	pop, ok := u.Overlay.(*menuPopover)
	if !ok {
		t.Fatalf("overlay is %T, want *menuPopover", u.Overlay)
	}
	// Rows: item(26) sep(9) item(26) section(26) item(26) item(26) + pad.
	// First row starts at bounds.Y + pad.
	y := pop.Bounds.Y + mbPad + 13 // middle of "Add Row"
	u.PointerDown(pop.Bounds.X+20, y)
	u.PointerUp(pop.Bounds.X+20, y)
	if len(picked) != 1 || picked[0] != 1 {
		t.Fatalf("picked = %v, want [1]", picked)
	}
	if u.Overlay != nil {
		t.Fatal("a pick must close the dropdown")
	}
	// The bar reconciles its highlight at next paint.
	u.BuildFrame(&gfx.DisplayList{}, 800, 600)
	if u.Menubar.openIdx != -1 {
		t.Fatalf("openIdx = %d after pick, want -1", u.Menubar.openIdx)
	}
}

func TestMenubarSectionRowsDoNotPick(t *testing.T) {
	u := fakeUi()
	u.Root = NewPanel()
	var picked []int
	menus, pick := demoMenus(&picked)
	u.SetMenubar(menus, pick)
	u.BuildFrame(&gfx.DisplayList{}, 800, 600)
	u.PointerDown(20, 13) // open "Rows"
	pop := u.Overlay.(*menuPopover)
	// Row offsets: item 0..26, sep 26..35, item 35..61, section 61..87.
	y := pop.Bounds.Y + mbPad + 26 + 9 + 26 + 13 // middle of the section header
	u.PointerDown(pop.Bounds.X+20, y)
	if len(picked) != 0 {
		t.Fatalf("section header picked %v; must be inert", picked)
	}
	if u.Overlay == nil {
		t.Fatal("clicking a section header must not close the menu")
	}
	// The nested item under it picks (id 3).
	y += 26
	u.PointerDown(pop.Bounds.X+30, y)
	if len(picked) != 1 || picked[0] != 3 {
		t.Fatalf("picked = %v, want [3]", picked)
	}
}

func TestMenubarKeyEquivalentsFirePicks(t *testing.T) {
	u := fakeUi()
	u.Root = NewPanel()
	var picked []int
	menus, pick := demoMenus(&picked)
	u.SetMenubar(menus, pick)
	u.BuildFrame(&gfx.DisplayList{}, 800, 600)

	// Bare "n" in the spec means Cmd+N, the platform default.
	if !u.KeyDown(Key{Name: "n", Meta: true}) {
		t.Fatal("Cmd+N must be consumed by the menu")
	}
	// Order-insensitive spec mods: "shift+cmd+k" fires on cmd+shift+k.
	if !u.KeyDown(Key{Name: "k", Meta: true, Shift: true}) {
		t.Fatal("Shift+Cmd+K must be consumed by the menu")
	}
	// Nested items' keys work too.
	if !u.KeyDown(Key{Name: "h", Ctrl: true, Alt: true}) {
		t.Fatal("Ctrl+Alt+H must be consumed by the menu")
	}
	if u.KeyDown(Key{Name: "z", Meta: true}) {
		t.Fatal("an unbound chord must not be consumed")
	}
	want := []int{1, 2, 4}
	if len(picked) != 3 || picked[0] != want[0] || picked[1] != want[1] || picked[2] != want[2] {
		t.Fatalf("picked = %v, want %v", picked, want)
	}
}

func TestCanonicalMenuComboAndLabel(t *testing.T) {
	cases := []struct{ spec, canonical, label string }{
		{"n", "cmd+n", "Cmd+N"},
		{"shift+cmd+k", "cmd+shift+k", "Shift+Cmd+K"},
		{"cmd+shift+k", "cmd+shift+k", "Shift+Cmd+K"},
		{"ctrl+opt+t", "ctrl+alt+t", "Ctrl+Alt+T"},
		{"alt+enter", "alt+enter", "Alt+Enter"},
	}
	for _, c := range cases {
		if got := CanonicalMenuCombo(c.spec); got != c.canonical {
			t.Fatalf("CanonicalMenuCombo(%q) = %q, want %q", c.spec, got, c.canonical)
		}
		if got := ComboLabel(c.spec); got != c.label {
			t.Fatalf("ComboLabel(%q) = %q, want %q", c.spec, got, c.label)
		}
	}
}

func TestMenubarPerformById(t *testing.T) {
	u := fakeUi()
	u.Root = NewPanel()
	var picked []int
	menus, pick := demoMenus(&picked)
	u.SetMenubar(menus, pick)
	if !u.Menubar.Perform(3) {
		t.Fatal("Perform(3) must find the nested item")
	}
	if u.Menubar.Perform(99) {
		t.Fatal("Perform(99) must report unknown ids")
	}
	if len(picked) != 1 || picked[0] != 3 {
		t.Fatalf("picked = %v, want [3]", picked)
	}
}
