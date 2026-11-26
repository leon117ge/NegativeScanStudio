//go:build darwin && cgo

// ImageCaptureCore side of the W6 transport: list cameras, open a session,
// and pass raw PTP containers through
// -[ICCameraDevice requestSendPTPCommand:outData:completion:] (macOS 10.15+).
// All PTP logic stays in Go (icc.go, fuji.go); this file only moves bytes.
//
// Threading: ImageCaptureCore delivers delegate calls and completions on the
// main run loop. When called on the main thread (mactether locks its main
// goroutine to it) the waits below pump the run loop; on another thread they
// block on a semaphore and rely on the app's running main run loop.

#import <Foundation/Foundation.h>
#import <ImageCaptureCore/ImageCaptureCore.h>
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
	static dispatch_once_t once;
	dispatch_once(&once, ^{
		gBrowser = [[NSSICCBrowser alloc] init];
	});
	NSSICCBrowser *b = gBrowser;
	if (!b.started) {
		b.started = YES;
		runOnMain(^{
			b.browser = [[ICDeviceBrowser alloc] init];
			b.browser.delegate = b;
			b.browser.browsedDeviceTypeMask = (ICDeviceTypeMask)(ICDeviceTypeMaskCamera | ICDeviceLocationTypeMaskLocal);
			[b.browser start];
		});
	}
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

// requestAuth asks for camera control and contents access (macOS 10.15+
// privacy prompts, shown for the terminal app on first use).
static NSString *requestAuth(double timeout) {
	NSSICCBrowser *b = gBrowser;
	__block NSString *control = nil;
	__block NSString *contents = nil;
	runOnMain(^{
		[b.browser requestControlAuthorizationWithCompletion:^(ICAuthorizationStatus st) {
			control = st;
		}];
		[b.browser requestContentsAuthorizationWithCompletion:^(ICAuthorizationStatus st) {
			contents = st;
		}];
	});
	waitUntil(timeout, ^BOOL {
		return control != nil && contents != nil;
	});
	if (control != nil && ![control isEqualToString:ICAuthorizationStatusAuthorized]) {
		return [NSString stringWithFormat:@"camera control access is %@: allow it in System Settings > Privacy & Security for the terminal app", control];
	}
	if (contents != nil && ![contents isEqualToString:ICAuthorizationStatusAuthorized]) {
		return [NSString stringWithFormat:@"camera contents access is %@: allow it in System Settings > Privacy & Security for the terminal app", contents];
	}
	return nil;
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

int icc_auth(char *control, char *contents, int n, double timeout, char *err, int errlen) {
	@autoreleasepool {
		if (!startBrowser(timeout, err, errlen)) return -1;
		copyStr(control, (size_t)n, [gBrowser.browser controlAuthorizationStatus]);
		copyStr(contents, (size_t)n, [gBrowser.browser contentsAuthorizationStatus]);
		return 0;
	}
}

@interface NSSICCSession : NSObject <ICCameraDeviceDelegate>
@property(strong) ICCameraDevice *camera;
@property(atomic) BOOL ready;
@property(atomic) BOOL removed;
@property(atomic) NSUInteger ptpEvents;
@end

@implementation NSSICCSession
// ICDeviceDelegate
- (void)didRemoveDevice:(ICDevice *)device {
	self.removed = YES;
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

void *icc_open(const char *uuid, double timeout, char *err, int errlen) {
	@autoreleasepool {
		if (!startBrowser(timeout, err, errlen)) return NULL;
		NSString *auth = requestAuth(timeout);
		if (auth != nil) {
			setErr(err, errlen, auth);
			return NULL;
		}
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
		__block BOOL opened = NO;
		__block NSError *openErr = nil;
		runOnMain(^{
			cam.delegate = s;
			[cam requestOpenSessionWithOptions:@{}
			                        completion:^(NSError *e) {
				                        openErr = e;
				                        opened = YES;
			                        }];
		});
		if (!waitUntil(timeout, ^BOOL {
			    return opened;
		    })) {
			setErr(err, errlen, @"opening the camera session timed out (close Image Capture, Photos and X Acquire, then retry)");
			return NULL;
		}
		if (openErr != nil) {
			setErr(err, errlen, [NSString stringWithFormat:@"open session: %@", openErr.localizedDescription]);
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
			return -1;
		}
		NSData *cmdData = [NSData dataWithBytes:cmd length:(NSUInteger)cmdlen];
		NSData *outData = hasOut ? [NSData dataWithBytes:out length:(NSUInteger)outlen] : nil;
		__block BOOL done = NO;
		__block NSData *gotData = nil;
		__block NSData *gotResp = nil;
		__block NSError *gotErr = nil;
		ICCameraDevice *cam = s.camera;
		runOnMain(^{
			[cam requestSendPTPCommand:cmdData
			                   outData:outData
			                completion:^(NSData *responseData, NSData *ptpResponseData, NSError *e) {
				                gotData = responseData;
				                gotResp = ptpResponseData;
				                gotErr = e;
				                done = YES;
			                }];
		});
		if (!waitUntil(timeout, ^BOOL {
			    return done;
		    })) {
			setErr(err, errlen, @"PTP command timed out");
			return -1;
		}
		if (gotErr != nil) {
			setErr(err, errlen, [NSString stringWithFormat:@"PTP command: %@ (%ld)", gotErr.localizedDescription, (long)gotErr.code]);
			return -1;
		}
		if (gotResp == nil || gotResp.length < 12) {
			setErr(err, errlen, [NSString stringWithFormat:@"PTP response container missing (%lu bytes)", (unsigned long)gotResp.length]);
			return -1;
		}
		*resp = malloc(gotResp.length);
		memcpy(*resp, gotResp.bytes, gotResp.length);
		*resplen = (int)gotResp.length;
		if (gotData.length > 0) {
			*data = malloc(gotData.length);
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
		__block BOOL done = NO;
		runOnMain(^{
			[cam requestCloseSessionWithOptions:@{}
			                         completion:^(NSError *e) {
				                         done = YES;
			                         }];
		});
		waitUntil(timeout, ^BOOL {
			return done;
		});
		runOnMain(^{
			cam.delegate = nil;
		});
	}
}
