#import <Cocoa/Cocoa.h>

extern void cautionMenuPicked(long tag);

@interface CautionMenuTarget : NSObject
- (void)cautionPick:(NSMenuItem *)sender;
@end

@implementation CautionMenuTarget
- (void)cautionPick:(NSMenuItem *)sender {
	cautionMenuPicked(sender.tag);
}
@end

static CautionMenuTarget *cautionTarget(void) {
	static CautionMenuTarget *t = nil;
	if (!t) t = [[CautionMenuTarget alloc] init];
	return t;
}

// caution_menu_new returns a fresh main menu pre-populated with the standard
// app menu (About, Hide, Quit)
void *caution_menu_new(void) {
	NSString *app = [[NSProcessInfo processInfo] processName];
	NSMenu *root = [[NSMenu alloc] init];
	NSMenuItem *appItem = [root addItemWithTitle:app action:nil keyEquivalent:@""];
	NSMenu *appMenu = [[NSMenu alloc] initWithTitle:app];
	[appMenu addItemWithTitle:[@"About " stringByAppendingString:app]
	                   action:@selector(orderFrontStandardAboutPanel:)
	            keyEquivalent:@""];
	[appMenu addItem:[NSMenuItem separatorItem]];
	[appMenu addItemWithTitle:[@"Hide " stringByAppendingString:app]
	                   action:@selector(hide:)
	            keyEquivalent:@"h"];
	NSMenuItem *hideOthers = [appMenu addItemWithTitle:@"Hide Others"
	                                            action:@selector(hideOtherApplications:)
	                                     keyEquivalent:@"h"];
	hideOthers.keyEquivalentModifierMask = NSEventModifierFlagCommand | NSEventModifierFlagOption;
	[appMenu addItemWithTitle:@"Show All"
	                   action:@selector(unhideAllApplications:)
	            keyEquivalent:@""];
	[appMenu addItem:[NSMenuItem separatorItem]];
	[appMenu addItemWithTitle:[@"Quit " stringByAppendingString:app]
	                   action:@selector(terminate:)
	            keyEquivalent:@"q"];
	[appItem setSubmenu:appMenu];
	return root;
}

void *caution_menu_add_submenu(void *menuPtr, const char *title) {
	NSMenu *menu = (NSMenu *)menuPtr;
	NSString *t = [NSString stringWithUTF8String:title];
	NSMenuItem *item = [menu addItemWithTitle:t action:nil keyEquivalent:@""];
	// The menu bar shows the submenu's title, so it must be set here.
	NSMenu *sub = [[NSMenu alloc] initWithTitle:t];
	[item setSubmenu:sub];
	return sub;
}

void caution_menu_add_item(void *menuPtr, const char *title, const char *key,
                           unsigned long mods, long tag) {
	NSMenu *menu = (NSMenu *)menuPtr;
	NSMenuItem *item = [menu addItemWithTitle:[NSString stringWithUTF8String:title]
	                                   action:@selector(cautionPick:)
	                            keyEquivalent:[NSString stringWithUTF8String:key]];
	item.target = cautionTarget();
	item.keyEquivalentModifierMask = mods;
	item.tag = tag;
}

void caution_menu_add_sep(void *menuPtr) {
	[(NSMenu *)menuPtr addItem:[NSMenuItem separatorItem]];
}

void caution_menu_install(void *rootPtr) {
	[NSApp setMainMenu:(NSMenu *)rootPtr];
}

static NSMenuItem *cautionFindTag(NSMenu *menu, long tag) {
	for (NSMenuItem *item in menu.itemArray) {
		if (item.tag == tag && item.action == @selector(cautionPick:)) return item;
		if (item.submenu) {
			NSMenuItem *found = cautionFindTag(item.submenu, tag);
			if (found) return found;
		}
	}
	return nil;
}

int caution_menu_perform(long tag) {
	NSMenuItem *item = cautionFindTag([NSApp mainMenu], tag);
	if (!item) return 0;
	[item.menu performActionForItemAtIndex:[item.menu indexOfItem:item]];
	return 1;
}
