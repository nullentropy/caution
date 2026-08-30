package text

import _ "embed"

// The emoji fallback face: Noto Emoji (monochrome outlines, OFL - see
// fonts/OFL.txt). Runes the text faces lack resolve here through the
// segmenter's fontmap, so emoji shape, ligate (ZWJ sequences, keycaps),
// and rasterize as tintable outlines - identically in both terminals,
// because this file compiles into the shared wasm engine too.
//
// Monochrome by explicit choice: the color Noto face is 10.7MB of CBDT
// bitmaps against this face's 1.9MB.
//
//go:embed fonts/NotoEmoji.ttf
var notoEmojiTTF []byte

func init() {
	emojiFace = mustParse(notoEmojiTTF)
}
