//go:build darwin && cgo

package camera

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework ImageCaptureCore
#include <stdlib.h>
#include "icc_darwin.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"
)

// ICCAvailable reports whether this build has the ImageCaptureCore transport.
const ICCAvailable = true

const iccErrLen = 512

func iccErr(buf []C.char) error {
	s := C.GoString(&buf[0])
	if s == "" {
		s = "unknown ImageCaptureCore error"
	}
	return errors.New(s)
}

// ListICC lists the cameras ImageCaptureCore sees (USB, local). Call it from
// the main thread (mactether locks main to it) or with a running main run
// loop (a Cocoa app); the wait pumps the run loop when on the main thread.
func ListICC(timeout time.Duration) ([]ICCCamera, error) {
	const max = 16
	cams := make([]C.icc_cam, max)
	errb := make([]C.char, iccErrLen)
	n := C.icc_list(&cams[0], max, C.double(timeout.Seconds()), &errb[0], iccErrLen)
	if n < 0 {
		return nil, iccErr(errb)
	}
	out := make([]ICCCamera, 0, int(n))
	for i := 0; i < int(n); i++ {
		c := &cams[i]
		out = append(out, ICCCamera{Name: C.GoString(&c.name[0]), Serial: C.GoString(&c.serial[0]), UUID: C.GoString(&c.uuid[0]),
			VendorID: int(c.vid), ProductID: int(c.pid), CanPTP: c.canPTP != 0})
	}
	return out, nil
}

// ICCAuthorization reports ImageCaptureCore's control and contents
// authorization for this process (macOS privacy settings) without prompting.
func ICCAuthorization(timeout time.Duration) (control, contents string, err error) {
	a := make([]C.char, 64)
	b := make([]C.char, 64)
	errb := make([]C.char, iccErrLen)
	if C.icc_auth(&a[0], &b[0], 64, C.double(timeout.Seconds()), &errb[0], iccErrLen) != 0 {
		return "", "", iccErr(errb)
	}
	return C.GoString(&a[0]), C.GoString(&b[0]), nil
}

// OpenICC opens a camera by ICDevice UUID ("" = the first camera that
// accepts PTP commands) and returns a Transport for OpenFuji.
func OpenICC(uuid string, timeout time.Duration) (*ICCTransport, ICCCamera, error) {
	cams, err := ListICC(timeout)
	if err != nil {
		return nil, ICCCamera{}, err
	}
	var pick *ICCCamera
	for i := range cams {
		if (uuid == "" && cams[i].CanPTP) || (uuid != "" && strings.EqualFold(cams[i].UUID, uuid)) {
			pick = &cams[i]
			break
		}
	}
	if pick == nil {
		if uuid != "" {
			return nil, ICCCamera{}, fmt.Errorf("no camera with UUID %s (run: mactether list)", uuid)
		}
		if len(cams) > 0 {
			return nil, ICCCamera{}, errors.New("cameras found, but none accepts PTP commands over ImageCaptureCore")
		}
		return nil, ICCCamera{}, errors.New("no camera found: connect the X-T3 by USB, switch it on, set CONNECTION MODE to USB TETHER SHOOTING AUTO, and close Image Capture, Photos and X Acquire")
	}
	cu := C.CString(pick.UUID)
	defer C.free(unsafe.Pointer(cu))
	errb := make([]C.char, iccErrLen)
	h := C.icc_open(cu, C.double(timeout.Seconds()), &errb[0], iccErrLen)
	if h == nil {
		return nil, *pick, iccErr(errb)
	}
	return newICCTransport(&iccDarwin{h: h, timeout: 60 * time.Second}), *pick, nil
}

type iccDarwin struct {
	h       unsafe.Pointer
	timeout time.Duration
}

func cbytes(b []byte) *C.uchar {
	if len(b) == 0 {
		return nil
	}
	return (*C.uchar)(unsafe.Pointer(&b[0]))
}

// SendPTP passes one command (and optional data-out) through
// requestSendPTPCommand:outData:completion: and waits for the completion.
func (d *iccDarwin) SendPTP(cmd, out []byte) ([]byte, []byte, error) {
	if d.h == nil {
		return nil, nil, errors.New("session closed")
	}
	var resp, data *C.uchar
	var respLen C.int
	var dataLen C.longlong
	errb := make([]C.char, iccErrLen)
	hasOut := C.int(0)
	if out != nil {
		hasOut = 1
	}
	rc := C.icc_send(d.h, cbytes(cmd), C.int(len(cmd)), cbytes(out), C.longlong(len(out)), hasOut, C.double(d.timeout.Seconds()),
		&resp, &respLen, &data, &dataLen, &errb[0], iccErrLen)
	defer C.free(unsafe.Pointer(resp))
	defer C.free(unsafe.Pointer(data))
	if rc != 0 {
		return nil, nil, iccErr(errb)
	}
	r := C.GoBytes(unsafe.Pointer(resp), respLen)
	var in []byte
	if dataLen > 0 {
		in = C.GoBytes(unsafe.Pointer(data), C.int(dataLen))
	}
	return r, in, nil
}

func (d *iccDarwin) Close() error {
	if d.h == nil {
		return nil
	}
	C.icc_close(d.h, 5.0)
	d.h = nil
	return nil
}
