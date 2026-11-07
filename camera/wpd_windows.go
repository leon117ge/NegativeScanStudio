package camera

/*
#cgo LDFLAGS: -lole32 -loleaut32 -luuid
#include <stdlib.h>
#include "wpd_windows.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// WPDDevice is one entry from the Windows Portable Devices manager.
type WPDDevice struct {
	ID, Name, Manufacturer, Description string
}

func (d WPDDevice) String() string {
	s := strings.TrimSpace(d.Manufacturer + " " + d.Name)
	if d.Description != "" && d.Description != d.Name {
		s += " (" + d.Description + ")"
	}
	return s
}

// ListWPD enumerates portable devices (cameras, phones, players).
func ListWPD() ([]WPDDevice, error) {
	var out *C.wchar_t
	var errb [256]C.char
	n := C.wpd_list(&out, &errb[0], 256)
	if n < 0 {
		return nil, errors.New(C.GoString(&errb[0]))
	}
	defer C.wpd_free(unsafe.Pointer(out))
	var u []uint16
	for p := unsafe.Pointer(out); *(*uint16)(p) != 0; p = unsafe.Add(p, 2) {
		u = append(u, *(*uint16)(p))
	}
	var devs []WPDDevice
	for _, line := range strings.Split(syscall.UTF16ToString(u), "\n") {
		f := strings.Split(line, "\t")
		if len(f) == 4 {
			devs = append(devs, WPDDevice{f[0], f[1], f[2], f[3]})
		}
	}
	return devs, nil
}

type wpdTransport struct {
	dev unsafe.Pointer
}

// OpenWPD opens a device for raw PTP.
func OpenWPD(id string) (Transport, error) {
	u, err := syscall.UTF16FromString(id)
	if err != nil {
		return nil, err
	}
	var errb [256]C.char
	d := C.wpd_open((*C.wchar_t)(unsafe.Pointer(&u[0])), &errb[0], 256)
	if d == nil {
		return nil, errors.New(C.GoString(&errb[0]))
	}
	return &wpdTransport{dev: d}, nil
}

func (t *wpdTransport) Exec(op uint16, params []uint32, dataOut []byte, wantData bool) (uint16, []uint32, []byte, error) {
	if t.dev == nil {
		return 0, nil, nil, errors.New("device closed")
	}
	cp := (*C.uint)(C.malloc(C.size_t(4 * (len(params) + 1))))
	defer C.free(unsafe.Pointer(cp))
	ps := unsafe.Slice((*uint32)(unsafe.Pointer(cp)), len(params)+1)
	copy(ps, params)
	mode := C.int(0)
	var wd *C.uchar
	if dataOut != nil {
		mode = 2
		wd = (*C.uchar)(C.CBytes(dataOut))
		defer C.free(unsafe.Pointer(wd))
	} else if wantData {
		mode = 1
	}
	var r C.wpd_result
	rc := C.wpd_exec(t.dev, C.uint(op), cp, C.int(len(params)), mode, wd, C.ulonglong(len(dataOut)), &r)
	var data []byte
	if r.data != nil {
		data = C.GoBytes(unsafe.Pointer(r.data), C.int(r.datalen))
		C.wpd_free(unsafe.Pointer(r.data))
	}
	rp := make([]uint32, int(r.nrparams))
	for i := range rp {
		rp[i] = uint32(r.rparams[i])
	}
	if rc != 0 {
		return uint16(r.resp), rp, data, fmt.Errorf("WPD: %s", C.GoString(&r.err[0]))
	}
	return uint16(r.resp), rp, data, nil
}

func (t *wpdTransport) SupportedVendorOps() ([]uint16, error) {
	var ops [512]C.uint
	var errb [256]C.char
	n := C.wpd_vendor_ops(t.dev, &ops[0], 512, &errb[0], 256)
	if n < 0 {
		return nil, errors.New(C.GoString(&errb[0]))
	}
	out := make([]uint16, int(n))
	for i := range out {
		out[i] = uint16(ops[i])
	}
	return out, nil
}

func (t *wpdTransport) Close() error {
	if t.dev != nil {
		C.wpd_close(t.dev)
		t.dev = nil
	}
	return nil
}
