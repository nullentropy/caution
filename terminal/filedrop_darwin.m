#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>

void cautionFileDrag(int over);

static IMP origEntered;
static IMP origPerform;

static NSDragOperation entered(id self, SEL _cmd, id<NSDraggingInfo> info) {
	cautionFileDrag(1);
	return ((NSDragOperation (*)(id, SEL, id))origEntered)(self, _cmd, info);
}

// GLFW's view has no exit method, and a drop ends the drag without one
static void exited(id self, SEL _cmd, id<NSDraggingInfo> info) {
	cautionFileDrag(0);
}

static BOOL perform(id self, SEL _cmd, id<NSDraggingInfo> info) {
	cautionFileDrag(0);
	return ((BOOL (*)(id, SEL, id))origPerform)(self, _cmd, info);
}

void caution_watch_file_drag(void *nswindow) {
	NSWindow *w = (__bridge NSWindow *)nswindow;
	Class cls = [w.contentView class];
	Method m = class_getInstanceMethod(cls, @selector(draggingEntered:));
	Method p = class_getInstanceMethod(cls, @selector(performDragOperation:));
	if (!m || !p) {
		return;
	}
	origEntered = method_setImplementation(m, (IMP)entered);
	origPerform = method_setImplementation(p, (IMP)perform);
	class_addMethod(cls, @selector(draggingExited:), (IMP)exited, "v@:@");
}
