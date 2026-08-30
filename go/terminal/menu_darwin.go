//go:build darwin

package terminal

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void *caution_menu_new(void);
void *caution_menu_add_submenu(void *menu, const char *title);
void  caution_menu_add_item(void *menu, const char *title, const char *key, unsigned long mods, long tag);
void  caution_menu_add_sep(void *menu);
void  caution_menu_install(void *root);
int   caution_menu_perform(long tag);
*/
import "C"

import (
	"strings"
	"unsafe"

	"github.com/nullentropy/caution/go/terminal/proto"
)

var menuOnPick func(id int)

//export cautionMenuPicked
func cautionMenuPicked(tag C.long) {
	if menuOnPick != nil {
		menuOnPick(int(tag))
	}
}

// installMenu realizes a server menu spec as the macOS menu bar. Must run on
// the main thread.
func installMenu(menus []proto.MenuSpec, onPick func(id int)) {
	menuOnPick = onPick
	root := C.caution_menu_new()
	var add func(m unsafe.Pointer, items []proto.MenuItem)
	add = func(m unsafe.Pointer, items []proto.MenuItem) {
		for _, it := range items {
			switch {
			case it.Sep:
				C.caution_menu_add_sep(m)
			case len(it.Items) > 0:
				ct := C.CString(it.Title)
				sub := C.caution_menu_add_submenu(m, ct)
				C.free(unsafe.Pointer(ct))
				add(sub, it.Items)
			default:
				key, mods := parseMenuKey(it.Key)
				ct := C.CString(it.Title)
				ck := C.CString(key)
				C.caution_menu_add_item(m, ct, ck, C.ulong(mods), C.long(it.ID))
				C.free(unsafe.Pointer(ct))
				C.free(unsafe.Pointer(ck))
			}
		}
	}
	for _, menu := range menus {
		ct := C.CString(menu.Title)
		sub := C.caution_menu_add_submenu(root, ct)
		C.free(unsafe.Pointer(ct))
		add(sub, menu.Items)
	}
	C.caution_menu_install(root)
}

// menuPerform triggers a menu item by id through the real NSMenu action
// machinery - the shot-mode verification hook.
func menuPerform(id int) bool {
	return C.caution_menu_perform(C.long(id)) != 0
}

// NSEventModifierFlags.
const (
	modShift   = 1 << 17
	modControl = 1 << 18
	modOption  = 1 << 19
	modCommand = 1 << 20
)

// parseMenuKey turns "cmd+shift+k" into a key equivalent and modifier mask.
// A bare key means command - the platform default for menu shortcuts.
func parseMenuKey(spec string) (string, uint) {
	if spec == "" {
		return "", 0
	}
	parts := strings.Split(strings.ToLower(spec), "+")
	key := parts[len(parts)-1]
	var mods uint
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "cmd", "command", "super":
			mods |= modCommand
		case "shift":
			mods |= modShift
		case "ctrl", "control":
			mods |= modControl
		case "opt", "option", "alt":
			mods |= modOption
		}
	}
	if mods == 0 {
		mods = modCommand
	}
	return key, mods
}
