package wire

type Type string

const (
	Panel     Type = "panel"
	Label     Type = "label"
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
	Panel, Label, Button, Checkbox, TextField, TextArea,
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
)

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
	OpCmd      Op = "cmd"
)

type Cmd string

const (
	CmdFocus  Cmd = "focus"
	CmdReveal Cmd = "reveal"
	CmdClear  Cmd = "clear"
	CmdValue  Cmd = "value"
)

type Msg string

const (
	MsgMount  Msg = "mount"
	MsgPatch  Msg = "patch"
	MsgResume Msg = "resume"
	MsgEvent  Msg = "ev"
)
