#import <Cocoa/Cocoa.h>
#include <libproc.h>

extern int cautionQuitAllowed(char *sender);

@interface CautionQuitFilter : NSObject
- (void)quit:(NSAppleEventDescriptor *)event reply:(NSAppleEventDescriptor *)reply;
@end

@implementation CautionQuitFilter
- (void)quit:(NSAppleEventDescriptor *)event reply:(NSAppleEventDescriptor *)reply {
	char name[2 * MAXCOMLEN + 1] = {0};
	NSAppleEventDescriptor *pid = [event attributeDescriptorForKeyword:keySenderPIDAttr];
	if (pid) proc_name([pid int32Value], name, sizeof name);
	if (!cautionQuitAllowed(name)) return;
	[NSApp terminate:nil];
	[NSApp postEvent:[NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint
	                               modifierFlags:0 timestamp:0 windowNumber:0 context:nil
	                                     subtype:0 data1:0 data2:0]
	         atStart:YES];
}
@end

void caution_filter_quit(void) {
	static CautionQuitFilter *filter = nil;
	if (!filter) filter = [[CautionQuitFilter alloc] init];
	[[NSAppleEventManager sharedAppleEventManager]
	    setEventHandler:filter
	        andSelector:@selector(quit:reply:)
	      forEventClass:kCoreEventClass
	         andEventID:kAEQuitApplication];
}
