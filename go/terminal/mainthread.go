package terminal

import "runtime"

// GLFW, AppKit, and every other desktop window system require window and
// event calls on the process's MAIN thread. Go's main goroutine starts on the main
// thread but may be migrated to another M whenever it yields (a GC assist
// during startup work, a blocking syscall, preemption). Calling
// runtime.LockOSThread() inside Run is therefore too late: by then the main
// goroutine can already be running on some other thread, and locking pins it
// to the wrong one. AppKit then traps deep inside window creation
// (NSUpdateCycleInitialize) with no hint about the cause.
//
// Locking here, during package init before main runs, is the documented
// remedy: init functions run on the main thread, so the main goroutine is
// pinned to it for the life of the process. Any app that does real work
// before opening a window (loading config, generating assets, warming a
// cache) is then safe by construction.
func init() {
	runtime.LockOSThread()
}
