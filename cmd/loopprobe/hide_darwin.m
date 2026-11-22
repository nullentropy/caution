#import <Cocoa/Cocoa.h>

void caution_probe_hide(int hidden) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (hidden) {
			[NSApp hide:nil];
		} else {
			[NSApp unhide:nil];
			[NSApp activateIgnoringOtherApps:YES];
		}
	});
}
