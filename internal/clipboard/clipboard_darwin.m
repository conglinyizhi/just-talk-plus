#include "clipboard_darwin.h"

#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

int jt_clipboard_set(const char *text) {
	@autoreleasepool {
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		[pb clearContents];
		NSString *s = [NSString stringWithUTF8String:text == NULL ? "" : text];
		return [pb setString:s forType:NSPasteboardTypeString] ? 0 : -1;
	}
}

char *jt_clipboard_get(void) {
	@autoreleasepool {
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		NSString *s = [pb stringForType:NSPasteboardTypeString];
		if (s == nil) {
			return strdup("");
		}
		return strdup([s UTF8String]);
	}
}

char *jt_clipboard_types(void) {
	@autoreleasepool {
		NSArray<NSPasteboardItem *> *items = [[NSPasteboard generalPasteboard] pasteboardItems];
		if (items.count != 1) {
			return NULL;
		}
		NSArray<NSPasteboardType> *types = items[0].types;
		return strdup([[types componentsJoinedByString:@"\n"] UTF8String]);
	}
}

int jt_clipboard_item_count(void) {
	@autoreleasepool {
		return (int)[[NSPasteboard generalPasteboard] pasteboardItems].count;
	}
}

int jt_clipboard_read_data(const char *type, void **data, size_t *length) {
	@autoreleasepool {
		if (type == NULL || data == NULL || length == NULL) {
			return -1;
		}
		NSArray<NSPasteboardItem *> *items = [[NSPasteboard generalPasteboard] pasteboardItems];
		if (items.count != 1) {
			return -1;
		}
		NSData *value = [items[0] dataForType:[NSString stringWithUTF8String:type]];
		if (value == nil) {
			return -1;
		}
		*length = value.length;
		*data = malloc(value.length == 0 ? 1 : value.length);
		if (*data == NULL) {
			return -1;
		}
		if (value.length > 0) {
			memcpy(*data, value.bytes, value.length);
		}
		return 0;
	}
}

int jt_clipboard_clear(void) {
	@autoreleasepool {
		[[NSPasteboard generalPasteboard] clearContents];
		return 0;
	}
}

int jt_clipboard_set_data(const char *type, const void *data, size_t length) {
	@autoreleasepool {
		if (type == NULL) {
			return -1;
		}
		NSData *value = [NSData dataWithBytes:data length:length];
		return [[NSPasteboard generalPasteboard] setData:value forType:[NSString stringWithUTF8String:type]] ? 0 : -1;
	}
}
