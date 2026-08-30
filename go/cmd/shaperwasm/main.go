//go:build js && wasm

// caution shaperwasm: the native terminal's text stack and outline atlas
// rasterization over the embedded Go fonts compiled to WebAssembly. The browser
// terminal loads this at boot and from then on operates on text with
// the same code as the native one.
//
// Exports (on globalThis.__cautionText, all synchronous):
//
//	shape(size, weight, italic, mono, dpr, text) -> [width, ascent, descent,
//	    gid0, x0, cluster0, emoji0, ...]  (cluster = rune index into text;
//	    emoji = 1 when the glyph shaped with the emoji fallback face)
//	glyph(size, weight, italic, mono, dpr, gid, phase, emoji) -> [] (no ink)
//	    or [x0, y0, x1, y1, w, h, bx, by]  (atlas region, device px)
//	atlasTake(dst Uint8Array) -> bool   (copy the atlas when dirty)
//	reset()                             (DPR change: drop every glyph)
package main

import (
	"syscall/js"

	"github.com/nullentropy/caution/go/terminal/gfx"
	"github.com/nullentropy/caution/go/terminal/text"

	"github.com/go-text/typesetting/font"
)

var (
	shaper = text.NewShaper()
	atlas  = text.NewAtlas()
)

func fontOf(args []js.Value) gfx.Font {
	return gfx.NewFont(float32(args[0].Float()), gfx.FontOpts{
		Weight: args[1].Int(),
		Italic: args[2].Bool(),
		Mono:   args[3].Bool(),
	})
}

func shape(_ js.Value, args []js.Value) any {
	f := fontOf(args)
	dpr := float32(args[4].Float())
	run := shaper.Shape(f, dpr, args[5].String())
	out := make([]any, 0, 3+len(run.Glyphs)*4)
	out = append(out, run.Width, run.Ascent, run.Descent)
	for _, g := range run.Glyphs {
		emoji := 0
		if g.Emoji {
			emoji = 1
		}
		out = append(out, int(g.GID), g.X, g.Cluster, emoji)
	}
	return out
}

func glyph(_ js.Value, args []js.Value) any {
	f := fontOf(args)
	dpr := float32(args[4].Float())
	g := atlas.Get(f, dpr, font.GID(args[5].Int()), args[6].Int(), args[7].Bool())
	if g == nil {
		return []any{}
	}
	return []any{g.X0, g.Y0, g.X1, g.Y1, g.W, g.H, g.BX, g.BY}
}

func atlasTake(_ js.Value, args []js.Value) any {
	if !atlas.Dirty {
		return false
	}
	js.CopyBytesToJS(args[0], atlas.Pix)
	atlas.Dirty = false
	return true
}

func reset(_ js.Value, _ []js.Value) any {
	atlas.Reset()
	return nil
}

func main() {
	js.Global().Set("__cautionText", map[string]any{
		"shape":     js.FuncOf(shape),
		"glyph":     js.FuncOf(glyph),
		"atlasTake": js.FuncOf(atlasTake),
		"reset":     js.FuncOf(reset),
	})
	select {} // exports stay alive for the page's lifetime
}
