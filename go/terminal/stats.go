package terminal

import "sync/atomic"

// FrameStats counts what the presentation loop did. Presented is buffer swaps,
// the part of a frame that actually costs; Steps is loop iterations, so the
// difference is the frames the loop looked at and declined to present.
//
// Probe introspection: cmd/loopprobe -present drives a real window through
// scripted phases and asserts an idle app presents nothing, a patched one
// presents about once per patch, and an animation moved off the viewport
// presents nothing.
type FrameStats struct {
	Steps, Presented int
}

var frameSteps, framePresented atomic.Int64

// Stats reports the running terminal's frame counters. Safe from any goroutine.
func Stats() FrameStats {
	return FrameStats{Steps: int(frameSteps.Load()), Presented: int(framePresented.Load())}
}

// ResetStats zeroes the counters, for a probe measuring one phase at a time.
func ResetStats() {
	frameSteps.Store(0)
	framePresented.Store(0)
}
