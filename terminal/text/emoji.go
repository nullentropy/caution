package text

import _ "embed"

//go:embed fonts/NotoEmoji.ttf
var notoEmojiTTF []byte

func init() {
	emojiFace = mustParse(notoEmojiTTF)
}
