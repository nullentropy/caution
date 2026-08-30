#import <Cocoa/Cocoa.h>

void caution_custom_titlebar(void *nswindow, int style) {
	NSWindow *w = (__bridge NSWindow *)nswindow;
	w.styleMask |= NSWindowStyleMaskFullSizeContentView;
	w.titlebarAppearsTransparent = YES;
	w.titleVisibility = NSWindowTitleHidden;
	if (style > 0) {
		w.toolbar = [[NSToolbar alloc] initWithIdentifier:@"caution-chrome"];
		if (@available(macOS 11.0, *)) {
			w.toolbarStyle = style == 2 ? NSWindowToolbarStyleUnified : NSWindowToolbarStyleUnifiedCompact;
			w.titlebarSeparatorStyle = NSTitlebarSeparatorStyleNone;
		}
	}
}
