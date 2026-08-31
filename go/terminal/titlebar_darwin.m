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
		// A toolbar survives into fullscreen, where AppKit parks it in the
		// auto-hiding strip across the top of the screen and it covers the
		// app's own titlebar. It is only here to make AppKit center the
		// traffic lights, and fullscreen has none, so it comes out.
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
