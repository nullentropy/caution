#import <Cocoa/Cocoa.h>

void cautionFullscreenChanged(int on);

static int toolbarStyle;

static void applyToolbar(NSWindow *w, int style) {
	if (style <= 0) {
		return;
	}
	w.toolbar = [[NSToolbar alloc] initWithIdentifier:@"caution-chrome"];
	if (@available(macOS 11.0, *)) {
		w.toolbarStyle = style == 2 ? NSWindowToolbarStyleUnified : NSWindowToolbarStyleUnifiedCompact;
		w.titlebarSeparatorStyle = NSTitlebarSeparatorStyleNone;
	}
}

void caution_custom_titlebar(void *nswindow, int style) {
	NSWindow *w = (__bridge NSWindow *)nswindow;
	w.styleMask |= NSWindowStyleMaskFullSizeContentView;
	w.titlebarAppearsTransparent = YES;
	w.titleVisibility = NSWindowTitleHidden;
	toolbarStyle = style;
	applyToolbar(w, style);
}

void caution_watch_fullscreen(void *nswindow) {
	NSWindow *w = (__bridge NSWindow *)nswindow;
	NSNotificationCenter *nc = NSNotificationCenter.defaultCenter;
	[nc addObserverForName:NSWindowWillEnterFullScreenNotification
	                object:w
	                 queue:nil
	            usingBlock:^(NSNotification *note) {
		// the toolbar only exists to center the traffic lights
		// in fullscreen AppKit parks it in the auto-hide strip over our
		// titlebar and there are no traffic lights, so drop it
		w.toolbar = nil;
		cautionFullscreenChanged(1);
	}];
	[nc addObserverForName:NSWindowDidExitFullScreenNotification
	                object:w
	                 queue:nil
	            usingBlock:^(NSNotification *note) {
		applyToolbar(w, toolbarStyle);
		cautionFullscreenChanged(0);
	}];
}
