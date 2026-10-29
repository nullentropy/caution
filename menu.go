package caution

import "github.com/nullentropy/caution/wire"

// the terminal supplies the standard app menu (About, Hide, Quit) itself
type Menu struct {
	Title string
	Items []MenuItem
}

// one of: a separator (Sep), a submenu (Items), or a pickable item
// (Title + OnPick, optionally Key)
type MenuItem struct {
	Title string
	// "n" (cmd+N, the command key is the platform default), "cmd+shift+k",
	// "ctrl+opt+t". letters and digits
	Key    string
	OnPick func()
	Sep    bool
	Items  []MenuItem
}

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
	specs := make([]any, 0, len(menus))
	for _, m := range menus {
		specs = append(specs, map[string]any{"title": m.Title, "items": conv(m.Items)})
	}
	s.menu = specs
	if s.mounted {
		s.ops = append(s.ops, map[string]any{"op": wire.OpMenu, "menu": specs})
	}
}
