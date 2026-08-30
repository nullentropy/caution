#include <CoreFoundation/CoreFoundation.h>

extern void cautionPacerFire(void);

static CFRunLoopTimerRef pacerTimer;

static void pacerCallout(CFRunLoopTimerRef t, void *info) { cautionPacerFire(); }

void caution_pacer_install(void) {
	if (pacerTimer) return;
	// Created idle: first fire pushed to the far future until armed.
	pacerTimer = CFRunLoopTimerCreate(NULL, CFAbsoluteTimeGetCurrent() + 1e9,
		1.0 / 60.0, 0, 0, pacerCallout, NULL);
	CFRunLoopTimerSetTolerance(pacerTimer, 0.004);
	CFRunLoopAddTimer(CFRunLoopGetMain(), pacerTimer, kCFRunLoopCommonModes);
}

void caution_pacer_arm(double interval) {
	if (!pacerTimer) return;
	// Interval changes only take effect via next-fire-date on macOS
	CFRunLoopTimerSetNextFireDate(pacerTimer, CFAbsoluteTimeGetCurrent() + interval);
}

void caution_pacer_idle(void) {
	if (!pacerTimer) return;
	CFRunLoopTimerSetNextFireDate(pacerTimer, CFAbsoluteTimeGetCurrent() + 1e9);
}
