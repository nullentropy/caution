#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>

void *caution_clipboard_png(int *len) {
	@autoreleasepool {
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		// Finder puts the file's icon on the pasteboard next to its URL
		if ([pb availableTypeFromArray:@[NSPasteboardTypeFileURL]]) return NULL;
		NSData *png = [pb dataForType:NSPasteboardTypePNG];
		if (!png) {
			NSData *tiff = [pb dataForType:NSPasteboardTypeTIFF];
			if (!tiff) return NULL;
			NSBitmapImageRep *rep = [NSBitmapImageRep imageRepWithData:tiff];
			png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
			if (!png) return NULL;
		}
		void *out = malloc(png.length);
		if (!out) return NULL;
		memcpy(out, png.bytes, png.length);
		*len = (int)png.length;
		return out;
	}
}
