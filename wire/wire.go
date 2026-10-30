package wire

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
)

type Type string

const (
	Panel     Type = "panel"
	Label     Type = "label"
	RichText  Type = "richtext"
	Button    Type = "button"
	Checkbox  Type = "checkbox"
	TextField Type = "textfield"
	TextArea  Type = "textarea"

	VStack Type = "vstack"
	HStack Type = "hstack"
	Dock   Type = "dock"
	Scroll Type = "scroll"
	Grid   Type = "grid"
	Split  Type = "split"

	Table Type = "table"
	Tree  Type = "tree"

	Select   Type = "select"
	Progress Type = "progress"
	Slider   Type = "slider"
	Radio    Type = "radio"
	Tabs     Type = "tabs"

	Dialog Type = "dialog"
	Image  Type = "image"
	Shader Type = "shader"
	Glass  Type = "glass"
)

var Types = []Type{
	Panel, Label, RichText, Button, Checkbox, TextField, TextArea,
	VStack, HStack, Dock, Scroll, Grid, Split,
	Table, Tree,
	Select, Progress, Slider, Radio, Tabs,
	Dialog, Image, Shader, Glass,
}

type Event string

const (
	EvClick        Event = "click"
	EvToggle       Event = "toggle"
	EvInput        Event = "input"
	EvCommit       Event = "commit"
	EvSubmit       Event = "submit"
	EvTab          Event = "tab"
	EvSelect       Event = "select"
	EvDismiss      Event = "dismiss"
	EvSort         Event = "sort"
	EvRowSelect    Event = "row-select"
	EvRowActivate  Event = "row-activate"
	EvCellActivate Event = "cell-activate"
	EvColResize    Event = "col-resize"
	EvSplitResize  Event = "split-resize"
	EvVisibleRange Event = "visible-range"
	EvContext      Event = "context"
	EvSpanClick    Event = "span-click"

	// glass pointer gestures, left button then right
	EvPick  Event = "pick"
	EvDrag  Event = "drag"
	EvDrop  Event = "drop"
	EvWheel Event = "wheel"
	EvRPick Event = "rpick"
	EvRDrag Event = "rdrag"
	EvRDrop Event = "rdrop"

	EvResize     Event = "resize"
	EvMenu       Event = "menu"
	EvKey        Event = "key"
	EvAux        Event = "aux"
	EvFullscreen Event = "fullscreen"
	EvFocus      Event = "focus"
	// a file drag entering or leaving the window, value is a bool
	EvFileDrag Event = "filedrag"
	// a drop's contents in DropPhase messages, see Session.OnFileDrop
	EvFileDrop Event = "filedrop"
)

// the value of a filedrop event. a drop is start, then data chunks in file
// order, then end. start carries x, y and files [{name, type, size}], data
// carries f (file index) and b (base64 of at most MaxChunk raw bytes)
type DropPhase string

const (
	DropStart DropPhase = "start"
	DropData  DropPhase = "data"
	DropEnd   DropPhase = "end"
)

// raw bytes per data chunk, base64 puts it at 512KB on the wire against the
// server's 1MB read limit
const MaxChunk = 384 << 10

const MaxDropFiles = 1024

type Op string

const (
	OpSet      Op = "set"
	OpInsert   Op = "insert"
	OpRemove   Op = "remove"
	OpMove     Op = "move"
	OpRows     Op = "rows"
	OpTheme    Op = "theme"
	OpMetrics  Op = "metrics"
	OpMenu     Op = "menu"
	OpTitle    Op = "title"
	OpKeys     Op = "keys"
	OpResource Op = "resource"
	OpPlay     Op = "play"
	OpStop     Op = "stop"
	OpSounds   Op = "sounds"
	OpFonts    Op = "fonts"
	OpCmd      Op = "cmd"
	OpMotion   Op = "motion"
	OpCookie   Op = "cookie"
)

// Session.SetMotion values, the mount's "motion" field and the motion op
type Motion string

const (
	MotionSystem  Motion = "system"
	MotionFull    Motion = "full"
	MotionReduced Motion = "reduced"
)

type Cmd string

const (
	CmdFocus  Cmd = "focus"
	CmdReveal Cmd = "reveal"
	CmdClear  Cmd = "clear"
	CmdValue  Cmd = "value"
	// value "1" or "-1", moves focus on from the node as Tab or Shift+Tab would
	CmdTabOut Cmd = "tabout"
)

type Msg string

const (
	MsgMount  Msg = "mount"
	MsgPatch  Msg = "patch"
	MsgResume Msg = "resume"
	MsgEvent  Msg = "ev"
	// Options.Authorize refused the connection, the client stops retrying
	MsgDenied Msg = "denied"
)

// payload of a data: URI, base64 or percent-encoded. images, sounds and fonts
// all accept one in place of a URL
func DataURI(src string) ([]byte, error) {
	comma := strings.IndexByte(src, ',')
	if comma < 0 {
		return nil, errors.New("malformed data URI")
	}
	meta, payload := src[5:comma], src[comma+1:]
	if strings.HasSuffix(meta, ";base64") {
		return base64.StdEncoding.DecodeString(payload)
	}
	unescaped, err := url.PathUnescape(payload)
	if err != nil {
		return nil, err
	}
	return []byte(unescaped), nil
}
