//go:build !darwin

package terminal

import "github.com/nullentropy/caution/go/terminal/proto"

func installMenu(_ []proto.MenuSpec, _ func(id int)) {}

func menuPerform(_ int) bool { return false }
