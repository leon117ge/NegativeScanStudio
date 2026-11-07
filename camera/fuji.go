package camera

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Fuji drives a Fujifilm X body in USB tether mode over raw PTP, following the
// sequences libgphoto2 uses (camera_fuji_capture, Fuji live view via 0x80000001).
type Fuji struct {
	p    *PTP
	log  *Logger
	info Info
	lv   bool

	// release strategy that last worked (index into releaseStrategies), -1 = none yet
	okStrategy int
	evLast     map[uint16]uint32
	pressed    bool           // S1 half-press sent and not cancelled
	ctlEnum    map[int64]bool // values the body accepts for 0xD208 (nil = unknown)

	// live view health
	lvLastSum   uint32
	lvLastNew   time.Time
	lvSame      int
	lvRearms    int
	lvDelErrs   int
	lvInfoErrs  int
	lvStaleNote bool
}

const lvHandle = 0x80000001

// OpenFuji wraps a transport, reads DeviceInfo and switches the body to USB control.
func OpenFuji(t Transport, log *Logger, driver string) (*Fuji, error) {
	f := &Fuji{p: &PTP{T: t, Log: log, Verbose: true}, log: log, okStrategy: -1}
	info, _, err := f.p.DeviceInfo()
	if err != nil {
		return nil, fmt.Errorf("GetDeviceInfo: %w", err)
	}
	info.Driver = driver
	f.info = info
	log.Printf("Connected: %s %s fw %s serial %s, vendor ext 0x%08X %q, %d ops, %d props, %d events",
		info.Manufacturer, info.Model, info.Version, info.Serial, info.VendorExt, info.VendorDesc, len(info.Ops), len(info.Props), len(info.Events))
	if info.VendorExt != VendorFuji && !strings.Contains(strings.ToLower(info.Manufacturer), "fuji") {
		log.Printf("WARNING: this is not a Fujifilm camera; capture sequences are Fuji-specific")
	}
	// libgphoto2 camera_prepare_capture: PC mode on, priority = USB control.
	if err := f.p.SetProp(PropFujiPCMode, 4, 1); err != nil {
		log.Printf("Set PC mode (0xD38C=1): %v (not fatal)", err)
	}
	if err := f.p.SetProp(PropFujiPriority, 4, 2); err != nil {
		log.Printf("Set priority USB (0xD207=2): %v (not fatal)", err)
	}
	if d, err := f.p.PropDesc(PropFujiCaptureCtl); err == nil && d.Form == 2 {
		f.ctlEnum = map[int64]bool{}
		for _, v := range d.Enum {
			f.ctlEnum[v] = true
		}
	}
	f.checkSettings()
	return f, nil
}

// checkSettings logs warnings for body settings that block tethered HDR
// capture (seen in the first X-T3 log: mode A, auto ISO, FNumber 0).
func (f *Fuji) checkSettings() []string {
	var w []string
	if v, err := f.p.PropValue(PropExposureProgram, 4); err == nil && v != 1 {
		name := map[int64]string{2: "P", 3: "A (aperture priority)", 4: "S (shutter priority)", 6: "P/other"}[v]
		w = append(w, fmt.Sprintf("Exposure mode is %s (0x500E=%d), not M: set the shutter dial to T (or a speed) so the shutter can be set over USB", name, v))
	}
	if v, err := f.p.PropValue(PropExposureIndex, 5); err == nil && v < 0 {
		w = append(w, fmt.Sprintf("ISO is AUTO%d (0x500F=%d): set the ISO dial to A with SHOOTING SETTING > ISO DIAL SETTING (A) = COMMAND so ISO 160 can be set over USB, or set ISO 160 on the dial", -v, v))
	}
	if v, err := f.p.PropValue(PropFNumber, 4); err == nil && v == 0 {
		w = append(w, "Lens reports no aperture (0x5007=0): adapted/manual lens. Turn SETUP > BUTTON/DIAL SETTING > SHOOT WITHOUT LENS = ON or the body will refuse to release")
	}
	if v, err := f.p.PropValue(0x500A, 4); err == nil && v != 1 {
		w = append(w, fmt.Sprintf("Focus mode is %d (not M): use the focus selector M (or AF-S with RELEASE priority) so the shot is not blocked by focus", v))
	}
	for _, x := range w {
		f.log.Printf("SETTINGS WARNING: %s", x)
	}
	return w
}

func (f *Fuji) Info() Info { return f.info }

func (f *Fuji) Exposure() (Exposure, error) {
	iso, err := f.p.PropValue(PropExposureIndex, 5)
	if err != nil {
		return Exposure{}, fmt.Errorf("read ISO: %w", err)
	}
	sh, err := f.p.PropValue(PropExposureTime, 6)
	if err != nil {
		return Exposure{}, fmt.Errorf("read shutter: %w", err)
	}
	if iso < 0 { // AUTO1..AUTO3 are reported as -1..-3
		return Exposure{ISO: int(iso), Shutter: uint32(sh)}, nil
	}
	return Exposure{ISO: int(iso & 0x00FFFFFF), Shutter: uint32(sh)}, nil
}

// Status reads ISO, shutter and aperture without logging every transaction.
func (f *Fuji) Status() (Exposure, float64, error) {
	v := f.p.Verbose
	f.p.Verbose = false
	defer func() { f.p.Verbose = v }()
	e, err := f.Exposure()
	if err != nil {
		return e, 0, err
	}
	fn, err := f.p.PropValue(PropFNumber, 4)
	if err != nil {
		return e, 0, nil
	}
	return e, float64(fn) / 100, nil
}

func notWritable(err error) bool {
	for _, c := range []uint16{RespAccessDenied, RespInvalidDevPropVal, RespDevicePropNotSupp, RespGeneralError, RespOperationNotSupp} {
		if IsResp(err, c) {
			return true
		}
	}
	return false
}

// ExposureMode reads 0x500E (1 = M, 2 = P, 3 = A, 4 = S).
func (f *Fuji) ExposureMode() (bool, string, error) {
	v, err := f.p.PropValue(PropExposureProgram, 4)
	if err != nil {
		return true, "", err // unknown: do not block
	}
	name := map[int64]string{1: "M", 2: "P", 3: "A (aperture priority)", 4: "S (shutter priority)", 6: "P/other"}[v]
	if name == "" {
		name = fmt.Sprintf("mode %d", v)
	}
	return v == 1, name, nil
}

func (f *Fuji) notManual() error {
	if m, name, err := f.ExposureMode(); err == nil && !m {
		f.log.Printf("Exposure mode is %s, not M - the shutter cannot be set", name)
		return fmt.Errorf("%w - the camera is in %s", ErrNotManual, name)
	}
	return nil
}

func (f *Fuji) SetExposure(e Exposure) (Exposure, error) {
	f.StopLiveView()
	if err := f.notManual(); err != nil {
		got, _ := f.Exposure()
		return got, err
	}
	// v0.86: the X-T3 answers DeviceBusy for a while after idle/LV; skip
	// writes that are already in place and retry busy writes.
	if cur, cerr := f.Exposure(); cerr == nil && cur == e {
		f.log.Printf("Exposure already %s - nothing to write", cur)
		return cur, nil
	}
	f.waitUSBControl()
	if err := f.retryBusyFor(5*time.Second, "set ISO", func() error { return f.p.SetProp(PropExposureIndex, 5, int64(e.ISO)) }); err != nil {
		f.log.Printf("Set ISO %d failed: %v", e.ISO, err)
		if notWritable(err) {
			got, _ := f.Exposure()
			return got, fmt.Errorf("%w (ISO: %v)", ErrNotWritable, err)
		}
		return Exposure{}, err
	}
	if err := f.retryBusyFor(5*time.Second, "set shutter", func() error { return f.p.SetProp(PropExposureTime, 6, int64(e.Shutter)) }); err != nil {
		f.log.Printf("Set shutter %s failed: %v", ShutterLabel(e.Shutter), err)
		if notWritable(err) {
			got, _ := f.Exposure()
			return got, fmt.Errorf("%w (shutter: %v)", ErrNotWritable, err)
		}
		return Exposure{}, err
	}
	// Fuji applies property writes asynchronously: poll for up to 3 s.
	var got Exposure
	var err error
	for t0 := time.Now(); time.Since(t0) < 3*time.Second; time.Sleep(150 * time.Millisecond) {
		got, err = f.Exposure()
		if err == nil && got == e {
			f.log.Printf("Exposure set and verified: %s", got)
			return got, nil
		}
	}
	if err != nil {
		return got, err
	}
	f.log.Printf("Exposure write not applied: wanted %s, camera reports %s", e, got)
	if err := f.notManual(); err != nil {
		return got, err
	}
	return got, fmt.Errorf("%w: wanted %s, camera reports %s", ErrNotWritable, e, got)
}

func (f *Fuji) startLV() error {
	if !f.info.Supports(OpInitiateOpenCapture) {
		return errors.New("camera does not offer live view over USB (no InitiateOpenCapture)")
	}
	if err := f.p.Simple(OpInitiateOpenCapture, 0, 0); err != nil {
		return fmt.Errorf("start live view: %w", err)
	}
	f.lv = true
	f.p.Verbose = false // live view is chatty; problems are logged explicitly below
	f.lvLastNew = time.Now()
	f.lvSame = 0
	return nil
}

// LiveView returns one live view JPEG, following libgphoto2's Fuji preview
// (library.c ~3981-4048): GetObjectInfo/GetObject/DeleteObject on 0x80000001.
// The delete is what tells the body to render the next frame; if it fails or
// the body keeps serving the same frame, live view is re-armed
// (TerminateOpenCapture + InitiateOpenCapture) so the picture keeps moving.
func (f *Fuji) LiveView() ([]byte, error) {
	if !f.lv {
		if err := f.startLV(); err != nil {
			return nil, err
		}
		f.log.Printf("Live view started")
	}
	var jpg []byte
	var err error
	for t0 := time.Now(); time.Since(t0) < 2*time.Second; {
		if _, err = f.p.ObjectInfo(lvHandle); err != nil {
			if IsResp(err, RespInvalidObjectHandle) {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			if f.lvInfoErrs < 5 {
				f.lvInfoErrs++
				f.log.Printf("Live view GetObjectInfo(0x80000001): %v", err)
			}
			return nil, err
		}
		jpg, err = f.p.Object(lvHandle)
		if err != nil {
			if IsResp(err, RespDeviceBusy) || IsResp(err, RespAccessDenied) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			f.log.Printf("Live view GetObject(0x80000001): %v", err)
			return nil, err
		}
		if derr := f.p.Delete(lvHandle); derr != nil && f.lvDelErrs < 5 {
			f.lvDelErrs++
			f.log.Printf("Live view DeleteObject(0x80000001) failed: %v - the body may keep serving the same frame", derr)
		}
		break
	}
	if err != nil {
		return nil, err
	}
	if i := bytes.Index(jpg, []byte{0xFF, 0xD8}); i > 0 {
		jpg = jpg[i:]
	}
	if len(jpg) < 4 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		return nil, errors.New("live view frame is not a JPEG")
	}
	sum := crc32.ChecksumIEEE(jpg)
	if sum != f.lvLastSum {
		f.lvLastSum, f.lvLastNew, f.lvSame = sum, time.Now(), 0
	} else {
		f.lvSame++
		// A real sensor frame always differs (noise), so an identical frame
		// for 0.8 s means the body is not refreshing 0x80000001: re-arm.
		if time.Since(f.lvLastNew) > 800*time.Millisecond {
			f.lvRearms++
			if !f.lvStaleNote || f.lvRearms%50 == 0 {
				f.lvStaleNote = true
				f.log.Printf("Live view frame unchanged for %d polls (%.1f s) - re-arming live view (TerminateOpenCapture + InitiateOpenCapture), re-arm #%d",
					f.lvSame, time.Since(f.lvLastNew).Seconds(), f.lvRearms)
			}
			f.p.Simple(OpTerminateOpenCapture, 0)
			f.lv = false
			if err := f.startLV(); err != nil {
				return jpg, nil
			}
		}
	}
	return jpg, nil
}

func (f *Fuji) StopLiveView() error {
	if !f.lv {
		return nil
	}
	f.lv = false
	f.p.Verbose = true
	// The transaction ID of InitiateOpenCapture is owned by the Windows driver,
	// so try the common conventions.
	var err error
	for _, tid := range []uint32{0, 0xFFFFFFFF} {
		if err = f.p.Simple(OpTerminateOpenCapture, tid); err == nil || IsResp(err, 0x2018) {
			f.log.Printf("Live view stopped")
			return nil
		}
	}
	f.log.Printf("TerminateOpenCapture failed: %v", err)
	return err
}

func (f *Fuji) retryBusy(what string, fn func() error) error {
	var err error
	for t0 := time.Now(); time.Since(t0) < 10*time.Second; time.Sleep(100 * time.Millisecond) {
		err = fn()
		if err == nil || !(IsResp(err, RespDeviceBusy) || IsResp(err, RespAccessDenied)) {
			return err
		}
	}
	return fmt.Errorf("%s: camera stayed busy: %w", what, err)
}

// events reads the Fuji event list (GetDevicePropValue 0xD212).
func (f *Fuji) events() (int, error) {
	b, err := f.p.PropRaw(PropFujiCurrentState)
	if err != nil {
		return 0, err
	}
	r := &rd{b: b}
	n := int(r.u16())
	// The X-T3 repeats the same entry forever (0xD20E=0 in the first log),
	// so only changed values count as events.
	if f.evLast == nil {
		f.evLast = map[uint16]uint32{}
	}
	var parts []string
	for i := 0; i < n && r.err == nil; i++ {
		prop := r.u16()
		val := r.u32()
		if old, ok := f.evLast[prop]; ok && old == val {
			continue
		}
		f.evLast[prop] = val
		parts = append(parts, fmt.Sprintf("%s=%d", PropName(prop), val))
	}
	if len(parts) > 0 {
		f.log.Printf("Events: %s", strings.Join(parts, ", "))
	}
	return len(parts), nil
}

// drain waits for a 300 ms quiet window (2 s budget) so live view and the
// next capture are not disturbed by queued events.
func (f *Fuji) drain() {
	verbose := f.p.Verbose
	f.p.Verbose = false
	defer func() { f.p.Verbose = verbose }()
	quiet := time.Now()
	for t0 := time.Now(); time.Since(t0) < 2*time.Second; time.Sleep(50 * time.Millisecond) {
		n, err := f.events()
		if err != nil {
			return
		}
		if n > 0 {
			quiet = time.Now()
		} else if time.Since(quiet) >= 300*time.Millisecond {
			return
		}
	}
}

// release strategies, tried in this order (the one that worked last is tried
// first next time). References are libgphoto2 camlibs/ptp2.
type releaseStrategy struct {
	name string
	why  string
	run  func(f *Fuji) error
}

var releaseStrategies = []releaseStrategy{
	{"A: S1 half-press 0x0200 + AF-status poll, then 0x0304 shoot",
		"exact libgphoto2 sequence: library.c camera_fuji_capture (focus step 0x0200 + InitiateCapture, poll 0xD209 while 1, then 0x0304 + InitiateCapture)",
		func(f *Fuji) error {
			if err := f.halfPress(); err != nil {
				return err
			}
			return f.ctlCapture(0x0304, "shoot S1->S2->S0 (0x0304)")
		}},
	{"B: S1 half-press 0x0200, then 0x0104 shoot + S1 off",
		"Fuji SDK style split release; 0x0104 is in the X-T3 0xD208 enum (ptp.h D208 comment)",
		func(f *Fuji) error {
			if err := f.halfPress(); err != nil {
				return err
			}
			return f.ctlCapture(0x0104, "shoot + S1 off (0x0104)")
		}},
	{"C: direct 0x0304 shoot (v0.3 path)",
		"one-step SDK_ShootS2toS0 (ptp.h default value of 0xD208)",
		func(f *Fuji) error { return f.ctlCapture(0x0304, "shoot S1->S2->S0 (0x0304)") }},
	{"D: press-and-hold 0x0500, release 0x000C",
		"libgphoto2 bulb start/stop pair (config.c _put_Fuji_Bulb); some bodies accept it as a plain release",
		func(f *Fuji) error {
			if err := f.ctlCapture(0x0500, "press and hold (0x0500)"); err != nil {
				return err
			}
			hold := 300 * time.Millisecond
			if sh, err := f.p.PropValue(PropExposureTime, 6); err == nil && sh != 0xffffffff {
				hold += time.Duration(sh) * time.Microsecond
			}
			time.Sleep(hold)
			return f.ctlCapture(0x000C, "release (0x000C)")
		}},
}

var errNotInEnum = errors.New("value not in this body's 0xD208 list (skipped, nothing sent)")

func (f *Fuji) ctlOK(v int64) bool { return f.ctlEnum == nil || f.ctlEnum[v] }

// ctlCapture writes 0xD208 and sends InitiateCapture(0,0), retrying while busy.
func (f *Fuji) ctlCapture(v int64, what string) error {
	if !f.ctlOK(v) {
		return fmt.Errorf("0x%04X: %w", v, errNotInEnum)
	}
	if err := f.retryBusyFor(3*time.Second, "0xD208="+what, func() error { return f.p.SetProp(PropFujiCaptureCtl, 4, v) }); err != nil {
		return fmt.Errorf("set 0xD208 %s: %w", what, err)
	}
	if err := f.retryBusyFor(5*time.Second, what, func() error { return f.p.Simple(OpInitiateCapture, 0, 0) }); err != nil {
		return fmt.Errorf("InitiateCapture after %s: %w", what, err)
	}
	f.log.Printf("  %s: accepted", what)
	return nil
}

// halfPress is libgphoto2's focus step: 0x0200 + InitiateCapture, then poll
// AFStatus 0xD209 while it is 1 (2 = ok, 3 = failed).
func (f *Fuji) halfPress() error {
	if err := f.ctlCapture(0x0200, "S1 half-press (0x0200)"); err != nil {
		return err
	}
	f.pressed = true
	st := int64(1)
	var err error
	t0 := time.Now()
	for ; time.Since(t0) < 6*time.Second; time.Sleep(50 * time.Millisecond) {
		st, err = f.p.PropValue(PropFujiAFStatus, 4)
		if err != nil {
			if IsResp(err, RespDeviceBusy) {
				continue
			}
			f.log.Printf("  AF status read failed: %v (continuing)", err)
			return nil
		}
		if st != 1 {
			break
		}
	}
	f.log.Printf("  AF status 0xD209 = %d after %d ms (1 busy, 2 ok, 3 failed)", st, time.Since(t0).Milliseconds())
	if st == 3 {
		return errors.New("half-press reported focus failure (0xD209=3): use focus mode M or AF-S with RELEASE priority")
	}
	return nil
}

// cancelPress is libgphoto2's recovery: 0xD208=0x0004 (cancel S1) + InitiateCapture.
func (f *Fuji) cancelPress() {
	f.pressed = false
	if err := f.p.SetProp(PropFujiCaptureCtl, 4, 0x0004); err != nil {
		f.log.Printf("  reset 0xD208=0x0004: %v", err)
		return
	}
	if err := f.p.Simple(OpInitiateCapture, 0, 0); err != nil {
		f.log.Printf("  reset InitiateCapture: %v", err)
	}
}

// waitUSBControl re-asserts PriorityMode=USB after live view; the X-T3 answers
// DeviceBusy for a while after TerminateOpenCapture (seen in the user's log).
func (f *Fuji) waitUSBControl() {
	t0 := time.Now()
	n := 0
	var err error
	for ; time.Since(t0) < 4*time.Second; time.Sleep(100 * time.Millisecond) {
		n++
		if err = f.p.SetProp(PropFujiPriority, 4, 2); err == nil || !IsResp(err, RespDeviceBusy) {
			break
		}
	}
	if err != nil {
		f.log.Printf("  PriorityMode 0xD207=2 after %d tries / %d ms: %v", n, time.Since(t0).Milliseconds(), err)
	} else if n > 1 {
		f.log.Printf("  PriorityMode 0xD207=2 accepted after %d tries / %d ms (camera was busy)", n, time.Since(t0).Milliseconds())
	}
}

// stateLine reads the capture-related properties in one quiet line.
func (f *Fuji) stateLine() string {
	v := f.p.Verbose
	f.p.Verbose = false
	defer func() { f.p.Verbose = v }()
	var parts []string
	for _, x := range []struct {
		code uint16
		typ  uint16
		name string
	}{{PropFujiPriority, 4, "D207"}, {PropFujiCaptureCtl, 4, "D208"}, {PropFujiAFStatus, 4, "D209"}, {PropFujiMediaRecord, 4, "D20C"},
		{PropFujiFreeSDRAM, 4, "D20E"}, {0xD201, 4, "D201"}, {PropExposureProgram, 4, "500E"}, {0x500A, 4, "500A"}, {PropFNumber, 4, "5007"},
		{PropExposureIndex, 5, "500F"}, {PropExposureTime, 6, "500D"}} {
		val, err := f.p.PropValue(x.code, x.typ)
		if err != nil {
			parts = append(parts, x.name+"=err")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", x.name, val))
	}
	if b, err := f.p.PropRaw(PropFujiCurrentState); err == nil {
		parts = append(parts, fmt.Sprintf("D212=[% X]", trunc(b, 26)))
	}
	return strings.Join(parts, " ")
}

func (f *Fuji) retryBusyFor(d time.Duration, what string, fn func() error) error {
	var err error
	for t0 := time.Now(); time.Since(t0) < d; time.Sleep(100 * time.Millisecond) {
		err = fn()
		if err == nil || !IsResp(err, RespDeviceBusy) {
			return err
		}
	}
	return fmt.Errorf("%s: camera stayed busy for %s: %w", what, d, err)
}

func (f *Fuji) Capture(dir, prefix string) (string, error) {
	f.StopLiveView()
	f.waitUSBControl()
	f.drain()
	before, err := f.p.ObjectHandles()
	if err != nil {
		return "", fmt.Errorf("list objects: %w", err)
	}
	known := map[uint32]bool{}
	for _, h := range before {
		known[h] = true
	}
	wait := 35 * time.Second
	if sh, err := f.p.PropValue(PropExposureTime, 6); err == nil && sh != 0xffffffff {
		wait += 2 * time.Duration(sh) * time.Microsecond
	}
	order := make([]int, 0, len(releaseStrategies))
	if f.okStrategy >= 0 {
		order = append(order, f.okStrategy)
	}
	for i := range releaseStrategies {
		if i != f.okStrategy {
			order = append(order, i)
		}
	}
	var fails []string
	released := false
	for k, i := range order {
		st := releaseStrategies[i]
		f.log.Printf("Release attempt %d/%d - %s", k+1, len(order), st.name)
		f.log.Printf("  why: %s", st.why)
		f.log.Printf("  state before: %s", f.stateLine())
		err := st.run(f)
		if err == nil {
			if f.okStrategy != i {
				f.log.Printf("  Release strategy %q works on this body - it will be used first from now on", st.name)
			}
			f.okStrategy = i
			f.pressed = false
			released = true
			break
		}
		f.log.Printf("  FAILED: %v", err)
		fails = append(fails, fmt.Sprintf("%s: %v", st.name[:1], err))
		if errors.Is(err, errNotInEnum) && !f.pressed {
			continue
		}
		f.cancelPress()
		time.Sleep(400 * time.Millisecond)
		f.waitUSBControl()
		if hs, err := f.p.ObjectHandles(); err == nil && len(hs) > len(before) {
			f.log.Printf("  note: %d new object(s) appeared despite the error - using them", len(hs)-len(before))
			released = true
			break
		}
	}
	if !released {
		w := f.checkSettings()
		msg := "the camera refused every release sequence (" + strings.Join(fails, "; ") + ")"
		if len(w) > 0 {
			msg += ".\nCheck on the camera:\n- " + strings.Join(w, "\n- ")
		} else {
			msg += ".\nCheck SHOOT WITHOUT LENS = ON (adapted lens), SHOOT WITHOUT CARD = ON or a card inserted, focus M, and that X Acquire is closed"
		}
		return "", errors.New(msg)
	}
	f.log.Printf("Shutter released, waiting up to %s for the image", wait.Round(time.Second))

	// Wait for new objects (events are unreliable on Fuji; compare handle lists).
	f.p.Verbose = false
	var found []uint32
	var foundAt time.Time
	t0 := time.Now()
	for time.Since(t0) < wait {
		f.events()
		hs, err := f.p.ObjectHandles()
		if err == nil {
			for _, h := range hs {
				if known[h] || h == lvHandle {
					continue
				}
				oi, err := f.p.ObjectInfo(h)
				if err != nil {
					continue // AccessDenied while still being written
				}
				known[h] = true
				if oi.Format == FmtAssociation {
					continue
				}
				f.log.Printf("New object 0x%08X %q format 0x%04X %d bytes", h, oi.Filename, oi.Format, oi.Size)
				found = append(found, h)
				foundAt = time.Now()
			}
		}
		if len(found) > 0 && time.Since(foundAt) > 1500*time.Millisecond {
			break // allow a RAW+JPEG pair to appear
		}
		time.Sleep(200 * time.Millisecond)
	}
	f.p.Verbose = true
	if len(found) == 0 {
		return "", fmt.Errorf("no image appeared within %s", wait.Round(time.Second))
	}
	var rawPath, anyPath string
	for _, h := range found {
		oi, err := f.p.ObjectInfo(h)
		if err != nil {
			return "", err
		}
		var data []byte
		if err := f.retryBusy("download", func() error {
			var e error
			data, e = f.p.Object(h)
			return e
		}); err != nil {
			return "", fmt.Errorf("download %s: %w", oi.Filename, err)
		}
		name := oi.Filename
		if name == "" {
			name = fmt.Sprintf("IMG_%08X.RAF", h)
		}
		dst := uniquePath(filepath.Join(dir, prefix+name))
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return "", err
		}
		f.log.Printf("Downloaded %s (%d bytes)", dst, len(data))
		if err := f.p.Delete(h); err != nil {
			f.log.Printf("Delete 0x%08X on camera failed: %v", h, err)
		} else {
			f.log.Printf("Deleted %q from camera", oi.Filename)
		}
		anyPath = dst
		if oi.Format != FmtJPEG && !strings.EqualFold(filepath.Ext(name), ".jpg") {
			rawPath = dst
		}
	}
	f.drain()
	if rawPath == "" {
		return anyPath, errors.New("camera delivered JPEG only; set IMAGE QUALITY to RAW")
	}
	return rawPath, nil
}

func uniquePath(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 2; ; i++ {
		q := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(q); err != nil {
			return q
		}
	}
}

// Diagnostics logs the device's capabilities and a read-only probe of the
// properties the HDR workflow uses.
func (f *Fuji) Diagnostics() error {
	l := f.log
	i := f.info
	l.Printf("==== Camera diagnostics ====")
	l.Printf("Driver: %s", i.Driver)
	l.Printf("Manufacturer: %s | Model: %s | Firmware: %s | Serial: %s", i.Manufacturer, i.Model, i.Version, i.Serial)
	l.Printf("Vendor extension: 0x%08X %q", i.VendorExt, i.VendorDesc)
	l.Printf("Operations (%d):", len(i.Ops))
	for _, o := range i.Ops {
		l.Printf("  op %s", OpName(o))
	}
	l.Printf("Events (%d): %s", len(i.Events), hexList(i.Events))
	l.Printf("Capture formats: %s | Image formats: %s", hexList(i.CaptureFmts), hexList(i.ImageFmts))
	if v, err := f.p.T.SupportedVendorOps(); err == nil {
		l.Printf("Driver-reported vendor opcodes (%d): %s", len(v), hexList(v))
	} else {
		l.Printf("Driver-reported vendor opcodes: %v", err)
	}
	if ids, err := f.p.StorageIDs(); err == nil {
		l.Printf("Storage IDs: %X", ids)
		for _, id := range ids {
			_, b, err := f.p.do(OpGetStorageInfo, []uint32{id}, nil, true)
			if err != nil {
				l.Printf("  storage %08X: %v", id, err)
				continue
			}
			r := &rd{b: b}
			typ, fs, acc := r.u16(), r.u16(), r.u16()
			maxc, free := r.u64(), r.u64()
			freeImg := r.u32()
			desc, label := r.str(), r.str()
			l.Printf("  storage %08X: type %d fs %d access %d capacity %d MB free %d MB (%d images) %q %q",
				id, typ, fs, acc, maxc>>20, free>>20, int32(freeImg), desc, label)
		}
	} else {
		l.Printf("Storage IDs: %v", err)
	}
	if hs, err := f.p.ObjectHandles(); err == nil {
		l.Printf("Objects on camera: %d", len(hs))
	} else {
		l.Printf("Object handles: %v", err)
	}
	l.Printf("Capture state: %s", f.stateLine())
	l.Printf("Release strategy that worked last: %d (-1 = none yet)", f.okStrategy)
	f.checkSettings()
	verbose := f.p.Verbose
	f.p.Verbose = false
	defer func() { f.p.Verbose = verbose }()
	props := append([]uint16{}, i.Props...)
	for _, p := range []uint16{PropExposureTime, PropExposureIndex, PropFNumber, PropExposureProgram, PropFujiPriority,
		PropFujiCaptureCtl, PropFujiAFStatus, PropFujiCurrentState, PropFujiPCMode, PropFujiFreeSDRAM, PropFujiMediaRecord,
		PropFujiBattery, PropFujiFirmware, PropFujiShotCount, PropFujiLVSize, PropFujiLVQuality, PropFujiISO, PropFujiShutterOld, PropFujiAperture, PropFujiTetherRaw} {
		if !i.HasProp(p) {
			props = append(props, p)
		}
	}
	l.Printf("Device properties (%d advertised, probing %d):", len(i.Props), len(props))
	for _, p := range props {
		adv := ""
		if !i.HasProp(p) {
			adv = " [not advertised]"
		}
		d, err := f.p.PropDesc(p)
		if err != nil {
			raw, err2 := f.p.PropRaw(p)
			if err2 != nil {
				l.Printf("  %s%s: desc %v; value %v", PropName(p), adv, err, err2)
			} else {
				l.Printf("  %s%s: desc %v; raw value % X", PropName(p), adv, err, trunc(raw, 32))
			}
			continue
		}
		form := ""
		switch d.Form {
		case 1:
			form = fmt.Sprintf(" range %d..%d step %d", d.Min, d.Max, d.Step)
		case 2:
			e := d.Enum
			more := ""
			if len(e) > 64 {
				e, more = e[:64], fmt.Sprintf(" …(+%d)", len(d.Enum)-64)
			}
			form = fmt.Sprintf(" enum %v%s", e, more)
		}
		rw := "ro"
		if d.Writable {
			rw = "rw"
		}
		extra := ""
		switch p {
		case PropExposureTime:
			extra = " = " + ShutterLabel(uint32(d.Current))
		case PropExposureIndex:
			if d.Current < 0 {
				extra = fmt.Sprintf(" = ISO AUTO%d", -d.Current)
			} else {
				extra = fmt.Sprintf(" = ISO %d (flags 0x%X)", d.Current&0xFFFFFF, uint32(d.Current)&0xFF000000)
			}
		case PropFNumber:
			extra = fmt.Sprintf(" = f/%.1f", float64(d.Current)/100)
		}
		l.Printf("  %s%s: type 0x%X %s current %d%s%s", PropName(p), adv, d.Type, rw, d.Current, extra, form)
	}
	// Full Fuji vendor property scan 0xD000-0xD3FF (read-only; unsupported
	// codes are only counted).
	l.Printf("Fuji vendor property scan 0xD000-0xD3FF (readable values only):")
	readable, unsupported := 0, 0
	for c := 0xD000; c <= 0xD3FF; c++ {
		p := uint16(c)
		if i.HasProp(p) {
			continue // already listed above
		}
		d, err := f.p.PropDesc(p)
		if err == nil {
			readable++
			l.Printf("  %s: type 0x%X rw=%v current %d%s", PropName(p), d.Type, d.Writable, d.Current, descForm(d))
			continue
		}
		raw, err2 := f.p.PropRaw(p)
		if err2 == nil {
			readable++
			l.Printf("  %s: (no desc: %v) raw % X", PropName(p), err, trunc(raw, 32))
			continue
		}
		unsupported++
	}
	l.Printf("Vendor scan: %d extra readable, %d not supported", readable, unsupported)
	l.Printf("==== end of diagnostics ====")
	return nil
}

func descForm(d PropDesc) string {
	switch d.Form {
	case 1:
		return fmt.Sprintf(" range %d..%d step %d", d.Min, d.Max, d.Step)
	case 2:
		e := d.Enum
		more := ""
		if len(e) > 64 {
			e, more = e[:64], fmt.Sprintf(" …(+%d)", len(d.Enum)-64)
		}
		return fmt.Sprintf(" enum %v%s", e, more)
	}
	return ""
}

func hexList(a []uint16) string {
	s := make([]string, len(a))
	for i, v := range a {
		s[i] = fmt.Sprintf("%04X", v)
	}
	return strings.Join(s, " ")
}

func trunc(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}

func (f *Fuji) Close() error {
	f.StopLiveView()
	f.p.SetProp(PropFujiPriority, 4, 1)
	f.log.Printf("Released USB control")
	return f.p.T.Close()
}

// SetVerbose turns PTP transaction logging on or off (Test shot logs everything).
func (f *Fuji) SetVerbose(on bool) { f.p.Verbose = on }
