package terminal

import (
	"math"
	"strconv"
	"time"

	"github.com/go-gl/glfw/v3.3/glfw"

	"github.com/nullentropy/caution/terminal/ui"
)

type pointer struct{ x, y float32 }

func installInput(win *glfw.Window, u *ui.UI, exitOnInput bool) *pointer {
	p := &pointer{}
	inputExits, moveExits := exitTriggers(win, exitOnInput)

	var winDrag struct {
		active           bool
		startWX, startWY int
		startCX, startCY float64
		lastPress        time.Time
	}
	win.SetCursorPosCallback(func(_ *glfw.Window, x, y float64) {
		if winDrag.active {
			wx, wy := win.GetPos()
			sx, sy := float64(wx)+x, float64(wy)+y
			win.SetPos(winDrag.startWX+int(sx-winDrag.startCX),
				winDrag.startWY+int(sy-winDrag.startCY))
			return
		}
		if moveExits(float32(x), float32(y)) {
			return
		}
		p.x, p.y = float32(x), float32(y)
		u.PointerMove(p.x, p.y)
	})
	win.SetMouseButtonCallback(func(_ *glfw.Window, b glfw.MouseButton, a glfw.Action, mods glfw.ModifierKey) {
		if inputExits() {
			return
		}
		x, y := p.x, p.y
		bk := modKey(mods)
		switch {
		case b == glfw.MouseButtonRight:
			if a == glfw.Press {
				if !u.Button(3, true, x, y, bk) && !u.RightDown(x, y) {
					u.ContextClick(x, y)
				}
			} else if a == glfw.Release && !u.Button(3, false, x, y, bk) {
				u.RightUp(x, y)
			}
		case b == glfw.MouseButtonMiddle:
			u.Button(2, a == glfw.Press, x, y, bk)
		case b == glfw.MouseButton4 || b == glfw.MouseButton5:
			if a == glfw.Press {
				u.AuxDown(int(b) + 1) // GLFW counts from 0, the SDK from 1
			}
		case b == glfw.MouseButtonLeft && mods&glfw.ModControl != 0:
			if a == glfw.Press && !u.Button(1, true, x, y, bk) {
				u.ContextClick(x, y)
			} else if a == glfw.Release {
				u.Button(1, false, x, y, bk)
			}
		case b != glfw.MouseButtonLeft:
		case a == glfw.Press && mods&(glfw.ModShift|glfw.ModAlt) != 0 && u.Button(1, true, x, y, bk):
		case a == glfw.Press:
			if !u.WindowDragAt(x, y) {
				u.PointerDown(x, y)
				return
			}
			if time.Since(winDrag.lastPress) < 350*time.Millisecond {
				winDrag.lastPress = time.Time{}
				if win.GetAttrib(glfw.Maximized) == glfw.True {
					win.Restore()
				} else {
					win.Maximize()
				}
				return
			}
			winDrag.lastPress = time.Now()
			winDrag.active = true
			winDrag.startWX, winDrag.startWY = win.GetPos()
			cx, cy := win.GetCursorPos()
			winDrag.startCX = float64(winDrag.startWX) + cx
			winDrag.startCY = float64(winDrag.startWY) + cy
		case a == glfw.Release:
			if winDrag.active {
				winDrag.active = false
				return
			}
			if !u.Button(1, false, x, y, bk) {
				u.PointerUp(x, y)
			}
		}
	})
	win.SetScrollCallback(func(_ *glfw.Window, xoff, yoff float64) {
		if inputExits() {
			return
		}
		// GLFW reports scrolling up as positive y
		u.Wheel(p.x, p.y, float32(-xoff*12), float32(-yoff*12))
	})
	win.SetCharCallback(func(_ *glfw.Window, r rune) {
		if inputExits() {
			return
		}
		u.CharInput(r)
	})
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
		if action != glfw.Press && action != glfw.Repeat {
			return
		}
		if inputExits() {
			return
		}
		if k, ok := uiKey(key, scancode, mods); ok {
			u.KeyDown(k)
		}
	})
	return p
}

func exitTriggers(win *glfw.Window, enabled bool) (inputExits func() bool, moveExits func(x, y float32) bool) {
	if !enabled {
		return func() bool { return false }, func(x, y float32) bool { return false }
	}
	armed := time.Now().Add(500 * time.Millisecond)
	inputExits = func() bool {
		if time.Now().Before(armed) {
			return false
		}
		win.SetShouldClose(true)
		return true
	}
	var lastX, lastY, traveled float32
	tracking := false
	moveExits = func(x, y float32) bool {
		if !tracking {
			lastX, lastY, tracking = x, y, true
			return false
		}
		dx := float64(x - lastX)
		dy := float64(y - lastY)
		lastX, lastY = x, y
		if time.Now().Before(armed) {
			return false
		}
		traveled += float32(math.Abs(dx) + math.Abs(dy))
		if traveled < 12 {
			return false
		}
		win.SetShouldClose(true)
		return true
	}
	return inputExits, moveExits
}

func modKey(mods glfw.ModifierKey) ui.Key {
	return ui.Key{
		Meta:  mods&glfw.ModSuper != 0,
		Ctrl:  mods&glfw.ModControl != 0,
		Shift: mods&glfw.ModShift != 0,
		Alt:   mods&glfw.ModAlt != 0,
	}
}

var keyNames = map[glfw.Key]string{
	glfw.KeyTab:       "Tab",
	glfw.KeyEnter:     "Enter",
	glfw.KeyKPEnter:   "Enter",
	glfw.KeySpace:     " ",
	glfw.KeyEscape:    "Escape",
	glfw.KeyBackspace: "Backspace",
	glfw.KeyDelete:    "Delete",
	glfw.KeyLeft:      "Left",
	glfw.KeyRight:     "Right",
	glfw.KeyUp:        "Up",
	glfw.KeyDown:      "Down",
	glfw.KeyHome:      "Home",
	glfw.KeyEnd:       "End",
	glfw.KeyPageUp:    "PageUp",
	glfw.KeyPageDown:  "PageDown",
	glfw.KeyInsert:    "Insert",
}

func uiKey(key glfw.Key, scancode int, mods glfw.ModifierKey) (ui.Key, bool) {
	k := modKey(mods)
	if name, ok := keyNames[key]; ok {
		k.Name = name
		return k, true
	}
	// bare presses insert text through the char callback
	chord := mods&(glfw.ModSuper|glfw.ModControl|glfw.ModAlt) != 0
	switch {
	case chord && key >= glfw.KeyA && key <= glfw.KeyZ:
		k.Name = string(rune('a' + (key - glfw.KeyA)))
	case chord && key >= glfw.Key0 && key <= glfw.Key9:
		k.Name = string(rune('0' + (key - glfw.Key0)))
	case key >= glfw.KeyF1 && key <= glfw.KeyF12:
		k.Name = "F" + strconv.Itoa(int(key-glfw.KeyF1)+1)
	case chord:
		k.Name = glfw.GetKeyName(key, scancode)
	}
	return k, k.Name != ""
}
