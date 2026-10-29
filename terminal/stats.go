package terminal

import "sync/atomic"

// Presented is buffer swaps, the part that actually costs
// Steps is loop iterations, the difference is frames the loop skipped
//
// cmd/loopprobe -present asserts on these: idle presents nothing, a patch
// presents about once, an animation off the viewport presents nothing
type FrameStats struct {
	Steps, Presented int
}

var frameSteps, framePresented atomic.Int64

// safe from any goroutine
func Stats() FrameStats {
	return FrameStats{Steps: int(frameSteps.Load()), Presented: int(framePresented.Load())}
}

// for a probe measuring one phase at a time
func ResetStats() {
	frameSteps.Store(0)
	framePresented.Store(0)
}
