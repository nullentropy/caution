// Package ui is the terminal's retained widget tree, shared by the native and
// browser terminals
package ui

import "github.com/nullentropy/caution/terminal/gfx"

// tokens are pointers: widgets resolve "$token" props to the pointer once at
// inflate time, and a server theme op mutates the pointees so live widgets
// restyle instantly
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

// nil for unknown tokens
func Tok(name string) *gfx.Color { return Theme[name] }

// mutates colors in place (adding unknown tokens) so every widget holding a
// token pointer repaints in the new palette
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
