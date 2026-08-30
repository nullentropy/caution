#import <Cocoa/Cocoa.h>

// Enter a REAL menu-tracking modal session for `seconds`, then cancel it.
static NSMenu *probeMenu;

static void cancelCallout(CFRunLoopTimerRef t, void *info) {
	[probeMenu cancelTracking];
}

void caution_probe_menu(void *nswindow, double seconds) {
	NSWindow *w = (__bridge NSWindow *)nswindow;
	probeMenu = [[NSMenu alloc] initWithTitle:@"probe"];
	[probeMenu addItemWithTitle:@"item one" action:nil keyEquivalent:@""];
	[probeMenu addItemWithTitle:@"item two" action:nil keyEquivalent:@""];
	CFRunLoopTimerRef t = CFRunLoopTimerCreate(NULL,
		CFAbsoluteTimeGetCurrent() + seconds, 0, 0, 0, cancelCallout, NULL);
	CFRunLoopAddTimer(CFRunLoopGetMain(), t, kCFRunLoopCommonModes);
	// Blocks here inside the tracking run loop until cancelTracking.
	[probeMenu popUpMenuPositioningItem:nil
	                         atLocation:NSMakePoint(40, 40)
	                             inView:w.contentView];
	CFRunLoopRemoveTimer(CFRunLoopGetMain(), t, kCFRunLoopCommonModes);
	CFRelease(t);
}
