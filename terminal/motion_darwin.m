#import <Cocoa/Cocoa.h>

int caution_reduce_motion(void) {
	return [[NSWorkspace sharedWorkspace] accessibilityDisplayShouldReduceMotion] ? 1 : 0;
}
