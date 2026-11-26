//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework AppKit -framework UniformTypeIdentifiers
#import <AppKit/AppKit.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#include <stdlib.h>
#include <string.h>

// nssPick runs an NSOpenPanel (kind 0 file, 2 folder) or NSSavePanel
// (kind 1) on the main thread and returns the chosen path (malloc'd) or
// NULL. It is called from a goroutine, never from the main thread, so
// dispatch_sync cannot deadlock; Gio's main run loop services the block.
static char *nssPick(int kind, const char *title, const char *start, const char *exts) {
	__block char *out = NULL;
	NSString *t = [NSString stringWithUTF8String:title];
	NSString *s = start ? [NSString stringWithUTF8String:start] : @"";
	NSString *e = exts ? [NSString stringWithUTF8String:exts] : @"";
	dispatch_sync(dispatch_get_main_queue(), ^{
		@autoreleasepool {
			NSSavePanel *p;
			if (kind == 1) {
				p = [NSSavePanel savePanel];
				p.canCreateDirectories = YES;
			} else {
				NSOpenPanel *o = [NSOpenPanel openPanel];
				o.canChooseFiles = kind == 0;
				o.canChooseDirectories = kind == 2;
				o.canCreateDirectories = kind == 2;
				o.allowsMultipleSelection = NO;
				p = o;
			}
			p.title = t;
			p.message = t;
			if (s.length > 0) {
				BOOL dir = NO;
				if ([[NSFileManager defaultManager] fileExistsAtPath:s isDirectory:&dir] && dir) {
					p.directoryURL = [NSURL fileURLWithPath:s isDirectory:YES];
				} else {
					p.directoryURL = [NSURL fileURLWithPath:[s stringByDeletingLastPathComponent] isDirectory:YES];
					if (kind == 1) p.nameFieldStringValue = [s lastPathComponent];
				}
			}
			if (e.length > 0 && kind != 2) {
				NSMutableArray<UTType *> *types = [NSMutableArray array];
				for (NSString *x in [e componentsSeparatedByString:@","]) {
					UTType *ut = [UTType typeWithFilenameExtension:x];
					if (ut) [types addObject:ut];
				}
				if (types.count > 0) p.allowedContentTypes = types;
			}
			[NSApp activateIgnoringOtherApps:YES];
			if ([p runModal] == NSModalResponseOK && p.URL.path) {
				out = strdup(p.URL.path.fileSystemRepresentation);
			}
		}
	});
	return out;
}
*/
import "C"

import (
	"strings"
	"unsafe"
)

func nativePick(title string, kind pickKind, start string, exts []string) (string, bool) {
	ct, cs, ce := C.CString(title), C.CString(start), C.CString(strings.Join(exts, ","))
	defer C.free(unsafe.Pointer(ct))
	defer C.free(unsafe.Pointer(cs))
	defer C.free(unsafe.Pointer(ce))
	r := C.nssPick(C.int(kind), ct, cs, ce)
	if r == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(r))
	return C.GoString(r), true
}
