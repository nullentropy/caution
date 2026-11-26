package terminal

import "runtime"

// window and event calls must happen on the process's main thread. the main
// goroutine starts there but can migrate whenever it yields, so locking inside
// Run is too late and AppKit traps in window creation with no hint why. init
// runs on the main thread, so locking here pins the main goroutine for good
func init() {
	runtime.LockOSThread()
}
