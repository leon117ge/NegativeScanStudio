//go:build darwin && cgo

package rgblight

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <stdlib.h>
#include <string.h>
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/IOKitLib.h>
#include <IOKit/serial/IOSerialKeys.h>
#include <IOKit/IOBSD.h>

typedef struct {
	char path[256];
	int vid, pid; // -1 when no USB parent has the key
} nss_serial_entry;

static int nss_num_prop(io_object_t svc, const char *key) {
	CFStringRef k = CFStringCreateWithCString(kCFAllocatorDefault, key, kCFStringEncodingUTF8);
	CFTypeRef v = IORegistryEntrySearchCFProperty(svc, kIOServicePlane, k, kCFAllocatorDefault,
		kIORegistryIterateRecursively | kIORegistryIterateParents);
	CFRelease(k);
	int out = -1;
	if (v) {
		if (CFGetTypeID(v) == CFNumberGetTypeID()) {
			int n = 0;
			if (CFNumberGetValue((CFNumberRef)v, kCFNumberIntType, &n)) out = n & 0xFFFF;
		}
		CFRelease(v);
	}
	return out;
}

// Lists every IOSerialBSDClient with its callout path and the idVendor /
// idProduct of the nearest USB ancestor. Returns the count, or -1 on error.
static int nss_list_serial(nss_serial_entry *out, int max) {
	CFMutableDictionaryRef match = IOServiceMatching(kIOSerialBSDServiceValue);
	if (!match) return -1;
	CFDictionarySetValue(match, CFSTR(kIOSerialBSDTypeKey), CFSTR(kIOSerialBSDAllTypes));
	io_iterator_t it = 0;
	// 0 is kIOMainPortDefault (kIOMasterPortDefault before macOS 12).
	if (IOServiceGetMatchingServices(0, match, &it) != KERN_SUCCESS) return -1;
	int n = 0;
	io_object_t svc;
	while ((svc = IOIteratorNext(it)) != 0) {
		if (n < max) {
			CFTypeRef p = IORegistryEntryCreateCFProperty(svc, CFSTR(kIOCalloutDeviceKey), kCFAllocatorDefault, 0);
			if (p) {
				if (CFGetTypeID(p) == CFStringGetTypeID() &&
					CFStringGetCString((CFStringRef)p, out[n].path, sizeof(out[n].path), kCFStringEncodingUTF8)) {
					out[n].vid = nss_num_prop(svc, "idVendor");
					out[n].pid = nss_num_prop(svc, "idProduct");
					n++;
				}
				CFRelease(p);
			}
		}
		IOObjectRelease(svc);
	}
	IOObjectRelease(it);
	return n;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func init() { enumerate = enumerateIOKit }

// EnumMethod names the enumerator compiled in (for logs and the CLI).
const EnumMethod = "IOKit"

const maxSerial = 128

// enumerateIOKit lists IOSerialBSDClient nodes through IOKit. If IOKit
// finds no USB identity for a usbmodem node it falls back to ioreg.
func enumerateIOKit() ([]serialEntry, error) {
	buf := (*[maxSerial]C.nss_serial_entry)(C.calloc(maxSerial, C.size_t(unsafe.Sizeof(C.nss_serial_entry{}))))
	if buf == nil {
		return nil, errors.New("out of memory")
	}
	defer C.free(unsafe.Pointer(buf))
	n := int(C.nss_list_serial(&buf[0], maxSerial))
	if n < 0 {
		return enumerateIoreg()
	}
	out := make([]serialEntry, 0, n)
	missing := false
	for i := 0; i < n; i++ {
		e := serialEntry{Path: C.GoString(&buf[i].path[0])}
		if buf[i].vid >= 0 && buf[i].pid >= 0 {
			e.VID, e.PID, e.USB = uint16(buf[i].vid), uint16(buf[i].pid), true
		} else if isUSBModem(e.Path) {
			missing = true
		}
		out = append(out, e)
	}
	if missing {
		if alt, err := enumerateIoreg(); err == nil {
			out = mergeEntries(out, alt)
		}
	}
	return out, nil
}
