package terminal

import "runtime"

// window and event calls must happen on the process's MAIN thread
// the main goroutine starts there but can migrate to another M whenever it
// yields (GC assist, blocking syscall, preemption), so locking inside Run is
// too late and can pin the wrong thread. AppKit then traps deep in window
// creation (NSUpdateCycleInitialize) with no hint why
//
// init runs on the main thread, so locking here pins the main goroutine for
// the life of the process, even if the app does real work before Run
func init() {
	runtime.LockOSThread()
}
