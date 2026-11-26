package terminal

import "sync/atomic"

type FrameStats struct {
	Steps, Presented int
}

var frameSteps, framePresented atomic.Int64

func Stats() FrameStats {
	return FrameStats{Steps: int(frameSteps.Load()), Presented: int(framePresented.Load())}
}

func ResetStats() {
	frameSteps.Store(0)
	framePresented.Store(0)
}
