#import <Cocoa/Cocoa.h>

extern void cautionOcclusionChanged(int visible);

static int visibleNow(NSWindow *w) {
	return (w.occlusionState & NSWindowOcclusionStateVisible) != 0;
}

int caution_watch_occlusion(void *nswindow) {
	NSWindow *w = (__bridge NSWindow *)nswindow;
	[NSNotificationCenter.defaultCenter addObserverForName:NSWindowDidChangeOcclusionStateNotification
	                                                object:w
	                                                 queue:nil
	                                            usingBlock:^(NSNotification *note) {
		cautionOcclusionChanged(visibleNow(w));
	}];
	return visibleNow(w);
}
