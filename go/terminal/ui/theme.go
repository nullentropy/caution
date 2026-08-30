// Package ui is the native terminal's widget tree: the same retained-tree,
// parent-interprets-layout-inputs model as the browser terminal's src/ui,
// ported to Go. Widgets embed Base and override the methods they care about;
// Base carries a back-reference to the outer widget (set by the New*
// constructors) for the few dispatch points Go embedding can't cover.
package ui

import "github.com/nullentropy/caution/go/terminal/gfx"

// Theme is the design-token table. Tokens are *pointers*: widgets resolve
// "$token" props to the pointer once at inflate time, and a server theme op
// mutates the pointees so live widgets restyle instantly.
var Theme = map[string]*gfx.Color{
	"bg":          hexP("#16181d"),
	"accent":      hexP("#4f8cff"),
	"ink":         hexP("#e8eaf0"),
	"inkDim":      hexP("#9aa3b2"),
	"inkFaint":    hexP("#6b7280"),
	"panel":       hexP("#262b33"),
	"panelAlt":    hexP("#20252c"),
	"panelInset":  hexP("#1b1f26"),
	"edge":        hexP("#3a4150"),
	"edgeSoft":    hexP("#333a46"),
	"titlebar":    hexP("#2e343e"),
	"control":     hexP("#2e343e"),
	"controlEdge": hexP("#4a5262"),
}

func hexP(s string) *gfx.Color {
	c := gfx.Hex(s)
	return &c
}

// Tok returns a theme token pointer (nil for unknown tokens).
func Tok(name string) *gfx.Color { return Theme[name] }

// ApplyTheme mutates token colors in place (adding unknown tokens), so every
// widget holding a token pointer repaints in the new palette.
func ApplyTheme(tokens map[string]string) {
	for k, v := range tokens {
		c := gfx.Hex(v)
		if existing := Theme[k]; existing != nil {
			*existing = c
		} else {
			Theme[k] = &c
		}
	}
}
