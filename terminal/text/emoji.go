package text

import _ "embed"

// emoji fallback face, Noto Emoji (monochrome outlines, OFL, see
// fonts/OFL.txt). runes the text faces lack resolve here through the
// segmenter's fontmap, so emoji shape, ligate (ZWJ sequences, keycaps) and
// rasterize as tintable outlines. identical in both terminals since this
// compiles into the shared wasm engine too
//
// monochrome on purpose, the color Noto face is 10.7MB of CBDT bitmaps vs 1.9MB
//
//go:embed fonts/NotoEmoji.ttf
var notoEmojiTTF []byte

func init() {
	emojiFace = mustParse(notoEmojiTTF)
}
