package caution

// Menu is one top-level menu in the menu bar. The terminal supplies the
// standard app menu (About, Hide, Quit) itself
type Menu struct {
	Title string
	Items []MenuItem
}

// MenuItem is an entry in a Menu. One of the three shapes applies:
// a separator (Sep), a submenu (Items), or a pickable item (Title + OnPick,
// optionally Key).
type MenuItem struct {
	Title string
	// Key is a shortcut like "n" (Cmd+N; the command key is the platform default),
	// "cmd+shift+k", or "ctrl+opt+t". Letters and digits.
	Key    string
	OnPick func()
	Sep    bool
	Items  []MenuItem
}

// SetMenu replaces the session's menu bar.
func (s *Session) SetMenu(menus ...Menu) {
	s.menuHandlers = map[int]func(){}
	next := 0
	var conv func(items []MenuItem) []any
	conv = func(items []MenuItem) []any {
		out := make([]any, 0, len(items))
		for _, it := range items {
			switch {
			case it.Sep:
				out = append(out, map[string]any{"sep": true})
			case len(it.Items) > 0:
				out = append(out, map[string]any{"title": it.Title, "items": conv(it.Items)})
			default:
				next++
				m := map[string]any{"id": next, "title": it.Title}
				if it.Key != "" {
					m["key"] = it.Key
				}
				if it.OnPick != nil {
					s.menuHandlers[next] = it.OnPick
				}
				out = append(out, m)
			}
		}
		return out
	}
	wire := make([]any, 0, len(menus))
	for _, m := range menus {
		wire = append(wire, map[string]any{"title": m.Title, "items": conv(m.Items)})
	}
	s.menu = wire
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": "menu", "menu": wire})
	}
}
