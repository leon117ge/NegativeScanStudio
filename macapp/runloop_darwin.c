//go:build darwin

// Run-loop probe for -selftest-runloop (see runloop_darwin.go). Kept out of
// the Go preamble because that file uses //export.

#include <CoreFoundation/CoreFoundation.h>
#include <dispatch/dispatch.h>
#include <pthread.h>

extern void nssProbeFired(int which, int onMain);

static void nssMainQueueCB(void *ctx) { nssProbeFired(0, pthread_main_np()); }

static void nssTimerCB(CFRunLoopTimerRef t, void *info) { nssProbeFired(1, pthread_main_np()); }

// nssArmProbe queues one block on the main dispatch queue and one timer on
// the main CFRunLoop, the two ways ImageCaptureCore and AppKit deliver
// callbacks. Both must fire while Gio's app.Main owns the main thread.
void nssArmProbe(void) {
	dispatch_async_f(dispatch_get_main_queue(), NULL, nssMainQueueCB);
	CFRunLoopTimerRef t = CFRunLoopTimerCreate(NULL, CFAbsoluteTimeGetCurrent() + 0.2, 0, 0, 0, nssTimerCB, NULL);
	CFRunLoopAddTimer(CFRunLoopGetMain(), t, kCFRunLoopCommonModes);
	CFRelease(t);
}
