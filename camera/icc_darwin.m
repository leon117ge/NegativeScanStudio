//go:build darwin && cgo

// ImageCaptureCore side of the W6 transport: list cameras, open a session,
// and pass raw PTP containers through
// -[ICCameraDevice requestSendPTPCommand:outData:completion:] (macOS 10.15+).
// All PTP logic stays in Go (icc.go, fuji.go); this file only moves bytes.
//
// Threading: ImageCaptureCore delivers delegate calls on the main thread;
// request completion blocks may run on any queue (ICDevice.h). Every result a
// wait looks at is an atomic property, set exactly once. When called on the
// main thread (mactether locks its main goroutine to it) the waits pump the
// run loop; on another thread they poll and rely on the app's main run loop.
//
// Authorization: the ICDeviceBrowser control/contents authorization API is
// iOS/iPadOS only (IC_UNAVAILABLE(macos) in the SDK), so nothing here asks for
// or checks it; on macOS access is not gated by an ImageCaptureCore prompt.

#import <Foundation/Foundation.h>
#import <ImageCaptureCore/ImageCaptureCore.h>
#include <limits.h>
#include <stdlib.h>
#include <string.h>
#include "icc_darwin.h"

static void setErr(char *err, int errlen, NSString *msg) {
	if (err == NULL || errlen <= 0) return;
	const char *s = [msg UTF8String];
	if (s == NULL) s = "error";
	strncpy(err, s, (size_t)errlen - 1);
	err[errlen - 1] = 0;
}

static void copyStr(char *dst, size_t n, NSString *s) {
	dst[0] = 0;
	if (s == nil) return;
	const char *c = [s UTF8String];
	if (c == NULL) return;
	strncpy(dst, c, n - 1);
	dst[n - 1] = 0;
}

// waitUntil returns YES when done() turns true before the timeout.
static BOOL waitUntil(double timeout, BOOL (^done)(void)) {
	NSDate *limit = [NSDate dateWithTimeIntervalSinceNow:timeout];
	while (!done()) {
		if ([limit timeIntervalSinceNow] <= 0) return NO;
		if ([NSThread isMainThread]) {
			CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.01, true);
		} else {
			[NSThread sleepForTimeInterval:0.002];
		}
	}
	return YES;
}

// runOnMain runs block on the main thread: inline when already there,
// otherwise asynchronously (the caller waits with waitUntil).
static void runOnMain(dispatch_block_t block) {
	if ([NSThread isMainThread]) {
		block();
	} else {
		dispatch_async(dispatch_get_main_queue(), block);
	}
}

@interface NSSICCBrowser : NSObject <ICDeviceBrowserDelegate>
@property(strong) ICDeviceBrowser *browser;
@property(strong) NSMutableArray<ICCameraDevice *> *cameras;
@property(atomic) BOOL enumerated;
@property(atomic) BOOL started;
@end

@implementation NSSICCBrowser
- (instancetype)init {
	if ((self = [super init])) {
		_cameras = [NSMutableArray array];
	}
	return self;
}
- (void)deviceBrowser:(ICDeviceBrowser *)browser didAddDevice:(ICDevice *)device moreComing:(BOOL)moreComing {
	if ([device isKindOfClass:[ICCameraDevice class]]) {
		@synchronized(self) {
			[self.cameras addObject:(ICCameraDevice *)device];
		}
	}
	if (!moreComing) self.enumerated = YES;
}
- (void)deviceBrowser:(ICDeviceBrowser *)browser didRemoveDevice:(ICDevice *)device moreGoing:(BOOL)moreGoing {
	@synchronized(self) {
		[self.cameras removeObject:(ICCameraDevice *)device];
	}
}
- (void)deviceBrowserDidEnumerateLocalDevices:(ICDeviceBrowser *)browser {
	self.enumerated = YES;
}
@end

static NSSICCBrowser *gBrowser;

// startBrowser creates the shared browser once (on the main thread) and
// waits for the initial enumeration of local devices.
static BOOL startBrowser(double timeout, char *err, int errlen) {
	// Create the browser object and queue the ICDeviceBrowser start exactly
	// once: two callers (the camera worker and a device-list refresh) can
	// arrive here together, and a check-then-set of a flag would let both
	// start a browser.
	static dispatch_once_t once;
	dispatch_once(&once, ^{
		NSSICCBrowser *nb = [[NSSICCBrowser alloc] init];
		gBrowser = nb;
		nb.started = YES;
		runOnMain(^{
			nb.browser = [[ICDeviceBrowser alloc] init];
			nb.browser.delegate = nb;
			nb.browser.browsedDeviceTypeMask = (ICDeviceTypeMask)(ICDeviceTypeMaskCamera | ICDeviceLocationTypeMaskLocal);
			[nb.browser start];
		});
	});
	NSSICCBrowser *b = gBrowser;
	// No callback arrives when there are no cameras on older systems: the
	// timeout then just means "nothing found".
	waitUntil(timeout, ^BOOL {
		return b.enumerated;
	});
	if (b.browser == nil) {
		setErr(err, errlen, @"ImageCaptureCore device browser did not start (is the main run loop running?)");
		return NO;
	}
	return YES;
}

int icc_list(icc_cam *out, int max, double timeout, char *err, int errlen) {
	@autoreleasepool {
		if (!startBrowser(timeout, err, errlen)) return -1;
		NSArray<ICCameraDevice *> *cams;
		@synchronized(gBrowser) {
			cams = [gBrowser.cameras copy];
		}
		int n = 0;
		for (ICCameraDevice *c in cams) {
			if (n >= max) break;
			icc_cam *o = &out[n++];
			memset(o, 0, sizeof(*o));
			copyStr(o->name, sizeof(o->name), c.name);
			copyStr(o->serial, sizeof(o->serial), c.serialNumberString);
			copyStr(o->uuid, sizeof(o->uuid), c.UUIDString);
			o->vid = (int)c.usbVendorID;
			o->pid = (int)c.usbProductID;
			o->canPTP = [c.capabilities containsObject:ICCameraDeviceCanAcceptPTPCommands] ? 1 : 0;
		}
		return n;
	}
}

// NSSICCResult holds one completion's outcome; the completion may run on any queue.
@interface NSSICCResult : NSObject
@property(atomic) BOOL done;
@property(atomic, strong) NSError *error;
@property(atomic, strong) NSData *data;
@property(atomic, strong) NSData *response;
@end
@implementation NSSICCResult
@end

@interface NSSICCSession : NSObject <ICCameraDeviceDelegate>
@property(strong) ICCameraDevice *camera;
@property(atomic) BOOL ready;
@property(atomic) BOOL removed;
@property(atomic) NSUInteger ptpEvents;
// Set by the required ICDeviceDelegate session callbacks.
@property(atomic) BOOL openSignalled;
@property(atomic, strong) NSError *openError;
@property(atomic) BOOL closeSignalled;
@property(atomic, strong) NSError *lastError;
@end

@implementation NSSICCSession
// ICDeviceDelegate (required)
- (void)device:(ICDevice *)device didOpenSessionWithError:(NSError *)error {
	self.openError = error;
	self.openSignalled = YES;
}
- (void)device:(ICDevice *)device didCloseSessionWithError:(NSError *)error {
	self.closeSignalled = YES;
}
- (void)didRemoveDevice:(ICDevice *)device {
	self.removed = YES;
}
// ICDeviceDelegate (optional)
- (void)device:(ICDevice *)device didEncounterError:(NSError *)error {
	if (error != nil) self.lastError = error;
}
- (void)deviceDidBecomeReady:(ICDevice *)device {
	self.ready = YES;
}
// ICCameraDeviceDelegate (all no-ops: Fuji polls, see fuji.go)
- (void)cameraDevice:(ICCameraDevice *)camera didAddItems:(NSArray<ICCameraItem *> *)items {
}
- (void)cameraDevice:(ICCameraDevice *)camera didRemoveItems:(NSArray<ICCameraItem *> *)items {
}
- (void)cameraDevice:(ICCameraDevice *)camera didReceiveThumbnail:(CGImageRef)thumbnail forItem:(ICCameraItem *)item error:(NSError *)error {
}
- (void)cameraDevice:(ICCameraDevice *)camera didReceiveMetadata:(NSDictionary *)metadata forItem:(ICCameraItem *)item error:(NSError *)error {
}
- (void)cameraDevice:(ICCameraDevice *)camera didRenameItems:(NSArray<ICCameraItem *> *)items {
}
- (void)cameraDeviceDidChangeCapability:(ICCameraDevice *)camera {
}
- (void)cameraDevice:(ICCameraDevice *)camera didReceivePTPEvent:(NSData *)eventData {
	self.ptpEvents++;
}
- (void)deviceDidBecomeReadyWithCompleteContentCatalog:(ICCameraDevice *)device {
	self.ready = YES;
}
- (void)cameraDeviceDidRemoveAccessRestriction:(ICDevice *)device {
}
- (void)cameraDeviceDidEnableAccessRestriction:(ICDevice *)device {
}
@end

// detach clears the camera's delegate on the main thread. ICDevice.delegate is
// assign (not weak), so the block keeps the session alive until it is cleared.
static void detach(ICCameraDevice *cam, NSSICCSession *s) {
	runOnMain(^{
		if (cam.delegate == s) cam.delegate = nil;
	});
}

void *icc_open(const char *uuid, double timeout, char *err, int errlen) {
	@autoreleasepool {
		if (!startBrowser(timeout, err, errlen)) return NULL;
		NSString *want = [NSString stringWithUTF8String:uuid];
		ICCameraDevice *cam = nil;
		@synchronized(gBrowser) {
			for (ICCameraDevice *c in gBrowser.cameras) {
				if ([c.UUIDString caseInsensitiveCompare:want] == NSOrderedSame) cam = c;
			}
		}
		if (cam == nil) {
			setErr(err, errlen, [NSString stringWithFormat:@"camera %@ is gone", want]);
			return NULL;
		}
		NSSICCSession *s = [[NSSICCSession alloc] init];
		s.camera = cam;
		NSSICCResult *r = [[NSSICCResult alloc] init];
		runOnMain(^{
			cam.delegate = s;
			[cam requestOpenSessionWithOptions:@{}
			                        completion:^(NSError *e) {
				                        r.error = e;
				                        r.done = YES;
			                        }];
		});
		// One wait for whichever arrives first: the completion block or the
		// required delegate call device:didOpenSessionWithError:. Both report
		// the same open; the other one is ignored.
		if (!waitUntil(timeout, ^BOOL {
			    return r.done || s.openSignalled;
		    })) {
			// The open may still finish later: ask for a close so no session is left behind.
			runOnMain(^{
				[cam requestCloseSessionWithOptions:@{}
				                         completion:^(NSError *e){
				                         }];
			});
			detach(cam, s);
			setErr(err, errlen, @"opening the camera session timed out (close Image Capture, Photos and X Acquire, then retry)");
			return NULL;
		}
		NSError *openErr = r.done ? r.error : s.openError;
		if (openErr != nil) {
			detach(cam, s);
			setErr(err, errlen, [NSString stringWithFormat:@"open session: %@ (%ld)", openErr.localizedDescription, (long)openErr.code]);
			return NULL;
		}
		// Ready (catalog enumerated) is not needed for PTP pass-through; give
		// it a short grace period so the first commands do not race it.
		waitUntil(timeout < 5 ? timeout : 5, ^BOOL {
			return s.ready;
		});
		if (![cam.capabilities containsObject:ICCameraDeviceCanAcceptPTPCommands]) {
			setErr(err, errlen, @"this camera does not accept PTP commands over ImageCaptureCore");
			runOnMain(^{
				[cam requestCloseSessionWithOptions:@{}
				                         completion:^(NSError *e){
				                         }];
			});
			detach(cam, s);
			return NULL;
		}
		return (__bridge_retained void *)s;
	}
}

int icc_send(void *h, const unsigned char *cmd, int cmdlen, const unsigned char *out, long long outlen, int hasOut,
             double timeout, unsigned char **resp, int *resplen, unsigned char **data, long long *datalen, char *err,
             int errlen) {
	@autoreleasepool {
		*resp = NULL;
		*resplen = 0;
		*data = NULL;
		*datalen = 0;
		NSSICCSession *s = (__bridge NSSICCSession *)h;
		if (s.removed) {
			setErr(err, errlen, @"camera disconnected");
			return ICC_SEND_LOST;
		}
		NSData *cmdData = [NSData dataWithBytes:cmd length:(NSUInteger)cmdlen];
		NSData *outData = hasOut ? [NSData dataWithBytes:out length:(NSUInteger)outlen] : nil;
		NSSICCResult *r = [[NSSICCResult alloc] init];
		ICCameraDevice *cam = s.camera;
		runOnMain(^{
			[cam requestSendPTPCommand:cmdData
			                   outData:outData
			                completion:^(NSData *responseData, NSData *ptpResponseData, NSError *e) {
				                r.data = responseData;
				                r.response = ptpResponseData;
				                r.error = e;
				                r.done = YES;
			                }];
		});
		if (!waitUntil(timeout, ^BOOL {
			    return r.done || s.removed;
		    })) {
			NSError *last = s.lastError;
			setErr(err, errlen, last != nil ? [NSString stringWithFormat:@"PTP command timed out (last device error: %@)", last.localizedDescription] : @"PTP command timed out");
			// The request may still complete later: the session is out of step.
			return ICC_SEND_LOST;
		}
		if (!r.done) {
			setErr(err, errlen, @"camera disconnected");
			return ICC_SEND_LOST;
		}
		NSError *gotErr = r.error;
		NSData *gotResp = r.response;
		NSData *gotData = r.data;
		if (gotErr != nil) {
			setErr(err, errlen, [NSString stringWithFormat:@"PTP command: %@ (%ld)", gotErr.localizedDescription, (long)gotErr.code]);
			return -1;
		}
		if (gotResp == nil || gotResp.length < 12) {
			setErr(err, errlen, [NSString stringWithFormat:@"PTP response container missing (%lu bytes)", (unsigned long)gotResp.length]);
			return -1;
		}
		if (gotResp.length > INT_MAX) {
			setErr(err, errlen, [NSString stringWithFormat:@"PTP response container too large (%lu bytes)", (unsigned long)gotResp.length]);
			return -1;
		}
		*resp = malloc(gotResp.length);
		if (*resp == NULL) {
			setErr(err, errlen, [NSString stringWithFormat:@"out of memory for the PTP response (%lu bytes)", (unsigned long)gotResp.length]);
			return -1;
		}
		memcpy(*resp, gotResp.bytes, gotResp.length);
		*resplen = (int)gotResp.length;
		if (gotData.length > 0) {
			if (gotData.length > INT_MAX) {
				// The Go side copies the data phase with C.GoBytes (int length).
				setErr(err, errlen, [NSString stringWithFormat:@"PTP data phase too large (%lu bytes)", (unsigned long)gotData.length]);
				return -1;
			}
			*data = malloc(gotData.length);
			if (*data == NULL) {
				setErr(err, errlen, [NSString stringWithFormat:@"out of memory for the PTP data phase (%lu bytes)", (unsigned long)gotData.length]);
				return -1;
			}
			memcpy(*data, gotData.bytes, gotData.length);
			*datalen = (long long)gotData.length;
		}
		return 0;
	}
}

void icc_close(void *h, double timeout) {
	@autoreleasepool {
		NSSICCSession *s = (__bridge_transfer NSSICCSession *)h;
		ICCameraDevice *cam = s.camera;
		NSSICCResult *r = [[NSSICCResult alloc] init];
		runOnMain(^{
			[cam requestCloseSessionWithOptions:@{}
			                         completion:^(NSError *e) {
				                         r.error = e;
				                         r.done = YES;
			                         }];
		});
		// Completion or the required delegate call device:didCloseSessionWithError:.
		waitUntil(timeout, ^BOOL {
			return r.done || s.closeSignalled || s.removed;
		});
		detach(cam, s);
	}
}
