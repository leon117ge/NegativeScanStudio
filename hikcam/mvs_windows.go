//go:build windows

package hikcam

/*
#cgo LDFLAGS: -lkernel32
#include "mvs_windows.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"sync"
	"time"
	"unsafe"
)

const mvsLayer = 0x00000001 | 0x00000004 // GigE | USB3

func cErr(code int, detail string) error {
	if detail != "" {
		return fmt.Errorf("MVS error 0x%08X: %s", uint32(code), detail)
	}
	return fmt.Errorf("MVS error 0x%08X", uint32(code))
}

// SDKAvailable reports whether MvCameraControl.dll is loaded.
func SDKAvailable() bool {
	if C.hik_available() != 0 {
		return true
	}
	return C.hik_load() == 0
}

// SDKStatus is a one-line description of the runtime loader.
func SDKStatus() string {
	if SDKAvailable() {
		return "Hikrobot MVS SDK loaded (MvCameraControl.dll)"
	}
	msg := C.GoString(C.hik_load_error())
	if msg == "" {
		return ErrNoSDK.Error()
	}
	return msg
}

// Enumerate lists USB3 and GigE MVS cameras. It never panics if the DLL
// is missing: the result carries ErrNoSDK.
func Enumerate() ListResult {
	if C.hik_load() != 0 {
		return ListResult{SDK: SDKStatus(), Err: ErrNoSDK}
	}
	errbuf := make([]byte, 256)
	ret := int(C.hik_enum(C.uint(mvsLayer), (*C.char)(unsafe.Pointer(&errbuf[0])), C.int(len(errbuf))))
	if ret != 0 {
		msg := C.GoString((*C.char)(unsafe.Pointer(&errbuf[0])))
		if msg == "" {
			msg = fmt.Sprintf("MV_CC_EnumDevices 0x%08X", uint32(ret))
		}
		return ListResult{SDK: SDKStatus(), Err: fmt.Errorf("%s", msg)}
	}
	n := int(C.hik_count())
	out := make([]Info, 0, n)
	for i := 0; i < n; i++ {
		var model, serial, vendor, iface [128]C.char
		if C.hik_dev_info(C.int(i), &model[0], 128, &serial[0], 128, &vendor[0], 128, &iface[0], 128) != 0 {
			continue
		}
		out = append(out, Info{
			Index:     i,
			Model:     C.GoString(&model[0]),
			Serial:    C.GoString(&serial[0]),
			Vendor:    C.GoString(&vendor[0]),
			Interface: C.GoString(&iface[0]),
		})
	}
	return ListResult{Cameras: out, SDK: SDKStatus()}
}

type realDev struct {
	mu     sync.Mutex
	h      unsafe.Pointer
	info   Info
	set    Settings
	grab   bool
	closed bool
}

// OpenReal opens the enumerated camera at info.Index.
func OpenReal(info Info) (Device, error) {
	if C.hik_load() != 0 {
		return nil, ErrNoSDK
	}
	// Re-enumerate so the C-side device list matches info.Index.
	list := Enumerate()
	if list.Err != nil {
		return nil, list.Err
	}
	if info.Index < 0 || info.Index >= len(list.Cameras) {
		return nil, ErrNoCamera
	}
	errbuf := make([]byte, 256)
	var h unsafe.Pointer
	ret := int(C.hik_open(C.int(info.Index), &h, (*C.char)(unsafe.Pointer(&errbuf[0])), C.int(len(errbuf))))
	if ret != 0 {
		msg := C.GoString((*C.char)(unsafe.Pointer(&errbuf[0])))
		if msg == "" {
			msg = "open failed"
		}
		return nil, cErr(ret, msg)
	}
	d := &realDev{h: h, info: list.Cameras[info.Index], set: DefaultSettings()}
	if err := d.Apply(d.set); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *realDev) Info() Info {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.info
}

func (d *realDev) Settings() Settings {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.set
}

func (d *realDev) Apply(st Settings) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.h == nil {
		return ErrNotOpen
	}
	st = st.Clamp()
	restart := d.grab
	if restart {
		C.hik_stop(d.h)
		d.grab = false
	}
	if err := d.applyLocked(st); err != nil {
		if restart {
			if C.hik_start(d.h) == 0 {
				d.grab = true
			}
		}
		return err
	}
	d.set = st
	if restart {
		if ret := int(C.hik_start(d.h)); ret != 0 {
			return cErr(ret, "StartGrabbing after Apply")
		}
		d.grab = true
	}
	return nil
}

func (d *realDev) applyLocked(st Settings) error {
	// Manual exposure / gain, gamma off unless the user asked for it.
	_ = callEnum(d.h, "ExposureAuto", "Off")
	_ = callEnum(d.h, "GainAuto", "Off")
	gk := C.CString("GammaEnable")
	_ = C.hik_set_bool(d.h, gk, cBool(st.GammaEnable))
	C.free(unsafe.Pointer(gk))
	var last error
	for _, name := range st.PixelFormat.GenICamNames() {
		if err := callEnum(d.h, "PixelFormat", name); err != nil {
			last = err
			continue
		}
		last = nil
		break
	}
	if last != nil {
		return fmt.Errorf("PixelFormat: %w", last)
	}
	if err := callFloat(d.h, "ExposureTime", float32(st.ExposureUS)); err != nil {
		return err
	}
	if err := callFloat(d.h, "Gain", float32(st.GainDB)); err != nil {
		return err
	}
	if err := callFloat(d.h, "BlackLevel", float32(st.BlackLevel)); err != nil {
		// BlackLevel may be an integer node on some firmware.
		_ = callInt(d.h, "BlackLevel", int64(st.BlackLevel))
	}
	if st.SoftwareTrigger {
		if err := callEnum(d.h, "TriggerMode", "On"); err != nil {
			return err
		}
		if err := callEnum(d.h, "TriggerSource", "Software"); err != nil {
			return err
		}
	} else {
		_ = callEnum(d.h, "TriggerMode", "Off")
	}
	if st.FullFrame {
		// Restore the sensor's maximum image. Offset must go to 0 first
		// so Width/Height can grow back to the full frame.
		_ = callInt(d.h, "OffsetX", 0)
		_ = callInt(d.h, "OffsetY", 0)
		if w, _, max, err := getInt(d.h, "Width"); err == nil && max > 0 {
			_ = callInt(d.h, "Width", max)
			st.Width = int(w)
			if max > 0 {
				st.Width = int(max)
			}
		}
		if _, _, max, err := getInt(d.h, "Height"); err == nil && max > 0 {
			_ = callInt(d.h, "Height", max)
			st.Height = int(max)
		}
	} else {
		_ = callInt(d.h, "OffsetX", 0)
		_ = callInt(d.h, "OffsetY", 0)
		if st.Width > 0 {
			if err := callInt(d.h, "Width", int64(st.Width)); err != nil {
				return err
			}
		}
		if st.Height > 0 {
			if err := callInt(d.h, "Height", int64(st.Height)); err != nil {
				return err
			}
		}
		if err := callInt(d.h, "OffsetX", int64(st.OffsetX)); err != nil {
			return err
		}
		if err := callInt(d.h, "OffsetY", int64(st.OffsetY)); err != nil {
			return err
		}
	}
	if st.QualityPreset {
		applyQualityNodes(d.h)
	}
	return nil
}

// applyQualityNodes tries optional noise-relevant GenICam features.
// Every call is best-effort: firmware that lacks a node is left unchanged.
func applyQualityNodes(h unsafe.Pointer) {
	_ = callEnum(h, "ExposureAuto", "Off")
	_ = callEnum(h, "GainAuto", "Off")
	_ = callEnum(h, "BlackLevelAuto", "Off")
	_ = setBoolNode(h, "LUTEnable", false)
	_ = setBoolNode(h, "GammaEnable", false)
	_ = callInt(h, "DigitalShift", 0)
	_ = callFloat(h, "Sharpness", 0)
	_ = callEnum(h, "SharpnessEnable", "Off")
	_ = callEnum(h, "Sharpness", "Off")
	_ = callEnum(h, "NoiseReduction", "Off")
	_ = callEnum(h, "NR", "Off")
	_ = callEnum(h, "ADCBitDepth", "Bits_12")
	_ = callInt(h, "ADCBitDepth", 12)
}

func setBoolNode(h unsafe.Pointer, key string, v bool) error {
	ck := C.CString(key)
	defer C.free(unsafe.Pointer(ck))
	if ret := int(C.hik_set_bool(h, ck, cBool(v))); ret != 0 {
		return cErr(ret, key)
	}
	return nil
}

func (d *realDev) StartGrab() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.h == nil {
		return ErrNotOpen
	}
	if d.grab {
		return nil
	}
	if ret := int(C.hik_start(d.h)); ret != 0 {
		return cErr(ret, "StartGrabbing")
	}
	d.grab = true
	return nil
}

func (d *realDev) StopGrab() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.h == nil || !d.grab {
		d.grab = false
		return nil
	}
	C.hik_grab_free(d.h)
	if ret := int(C.hik_stop(d.h)); ret != 0 {
		return cErr(ret, "StopGrabbing")
	}
	d.grab = false
	return nil
}

func (d *realDev) Grab(timeout time.Duration) (*Frame, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.h == nil {
		return nil, ErrNotOpen
	}
	if !d.grab {
		return nil, ErrNotGrab
	}
	if d.set.SoftwareTrigger {
		ck := C.CString("TriggerSoftware")
		ret := int(C.hik_command(d.h, ck))
		C.free(unsafe.Pointer(ck))
		if ret != 0 {
			return nil, cErr(ret, "TriggerSoftware")
		}
	}
	ms := timeout.Milliseconds()
	if ms <= 0 {
		ms = 2000
	}
	var data *C.uchar
	var w, h C.int
	var pixfmt, length C.uint
	ret := int(C.hik_grab(d.h, C.uint(ms), &data, &w, &h, &pixfmt, &length))
	if ret != 0 {
		return nil, cErr(ret, "GetImageBuffer")
	}
	n := int(length)
	buf := C.GoBytes(unsafe.Pointer(data), C.int(n))
	C.hik_grab_free(d.h)
	pix, err := unpackMono(buf, int(w), int(h), uint32(pixfmt))
	if err != nil {
		return nil, err
	}
	return &Frame{
		W:           int(w),
		H:           int(h),
		Pix:         pix,
		PixelFormat: pixelTypeName(uint32(pixfmt)),
		ExposureUS:  d.set.ExposureUS,
		GainDB:      d.set.GainDB,
	}, nil
}

func (d *realDev) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.h == nil {
		d.closed = true
		return nil
	}
	C.hik_grab_free(d.h)
	if d.grab {
		C.hik_stop(d.h)
		d.grab = false
	}
	C.hik_close(d.h)
	d.h = nil
	d.closed = true
	return nil
}

func cBool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

func callEnum(h unsafe.Pointer, key, val string) error {
	ck, cv := C.CString(key), C.CString(val)
	defer C.free(unsafe.Pointer(ck))
	defer C.free(unsafe.Pointer(cv))
	if ret := int(C.hik_set_enum_str(h, ck, cv)); ret != 0 {
		return cErr(ret, key+"="+val)
	}
	return nil
}

func callFloat(h unsafe.Pointer, key string, v float32) error {
	ck := C.CString(key)
	defer C.free(unsafe.Pointer(ck))
	if ret := int(C.hik_set_float(h, ck, C.float(v))); ret != 0 {
		return cErr(ret, key)
	}
	return nil
}

func callInt(h unsafe.Pointer, key string, v int64) error {
	ck := C.CString(key)
	defer C.free(unsafe.Pointer(ck))
	if ret := int(C.hik_set_int(h, ck, C.longlong(v))); ret != 0 {
		return cErr(ret, key)
	}
	return nil
}

func getInt(h unsafe.Pointer, key string) (cur, min, max int64, err error) {
	ck := C.CString(key)
	defer C.free(unsafe.Pointer(ck))
	var c, mn, mx C.longlong
	if ret := int(C.hik_get_int(h, ck, &c, &mn, &mx)); ret != 0 {
		return 0, 0, 0, cErr(ret, key)
	}
	return int64(c), int64(mn), int64(mx), nil
}
