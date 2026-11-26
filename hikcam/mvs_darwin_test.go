//go:build darwin && cgo

package hikcam

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

const realFramework = "/Library/Frameworks/MvCameraControl.framework/MvCameraControl"

// The loader keeps the first library it loads for the life of the process,
// so the missing-SDK checks run before the stub is loaded, in one test.
//
// Run with NSS_MVS_STUB=<path to the library from ci/macos/build_mvs_stub.sh>
// to also drive the whole capture path against the stand-in camera.
func TestDarwinLoader(t *testing.T) {
	stub := os.Getenv("NSS_MVS_STUB")
	t.Cleanup(func() {
		os.Unsetenv("NSS_MVS_LIB")
		os.Unsetenv("NSS_MVS_STUB_CAMERAS")
		os.Unsetenv("NSS_MVS_STUB_FAIL_OPEN")
	})

	t.Run("missing SDK degrades to ErrNoSDK", func(t *testing.T) {
		os.Setenv("NSS_MVS_LIB", "/nonexistent/nss-test/libMvCameraControl.dylib")
		if SDKAvailable() {
			t.Fatal("SDKAvailable true with a missing library")
		}
		res := Enumerate()
		if !errors.Is(res.Err, ErrNoSDK) || len(res.Cameras) != 0 {
			t.Fatalf("Enumerate = %+v, want ErrNoSDK and no cameras", res)
		}
		if msg := res.Message(); !strings.Contains(msg, "NSS_MVS_LIB") {
			t.Fatalf("Message %q does not explain the failed load", msg)
		}
		if d, err := OpenReal(Info{Index: 0}); !errors.Is(err, ErrNoSDK) || d != nil {
			t.Fatalf("OpenReal = %v, %v; want nil, ErrNoSDK", d, err)
		}
	})

	t.Run("default path without the SDK", func(t *testing.T) {
		os.Unsetenv("NSS_MVS_LIB")
		if _, err := os.Stat(realFramework); err == nil {
			t.Skip("Hikrobot MVS SDK is installed on this machine")
		}
		res := Enumerate()
		if !errors.Is(res.Err, ErrNoSDK) {
			t.Fatalf("Enumerate err = %v, want ErrNoSDK", res.Err)
		}
		if msg := res.Message(); !strings.Contains(msg, "MvCameraControl.framework") {
			t.Fatalf("Message %q does not name the framework", msg)
		}
	})

	if stub == "" {
		t.Skip("NSS_MVS_STUB not set; stub camera checks skipped")
	}
	os.Setenv("NSS_MVS_LIB", stub)

	t.Run("enumerate", func(t *testing.T) {
		os.Setenv("NSS_MVS_STUB_CAMERAS", "0")
		res := Enumerate()
		if res.Err != nil || len(res.Cameras) != 0 {
			t.Fatalf("no-camera Enumerate = %+v", res)
		}
		if !SDKAvailable() || !strings.Contains(res.Message(), "no camera found") {
			t.Fatalf("SDKAvailable=%v Message=%q", SDKAvailable(), res.Message())
		}
		os.Setenv("NSS_MVS_STUB_CAMERAS", "2")
		res = Enumerate()
		if res.Err != nil || len(res.Cameras) != 2 {
			t.Fatalf("Enumerate = %+v, want 2 cameras", res)
		}
		c := res.Cameras[1]
		if c.Index != 1 || c.Model != "MV-STUB-U3V" || c.Serial != "STUB0002" || c.Interface != "USB3" || c.Vendor != "Hikrobot (stub)" {
			t.Fatalf("camera 1 = %+v", c)
		}
		if !strings.Contains(SDKStatus(), "MvCameraControl.framework") {
			t.Fatalf("SDKStatus %q", SDKStatus())
		}
	})

	t.Run("open failure is reported", func(t *testing.T) {
		os.Setenv("NSS_MVS_STUB_FAIL_OPEN", "1")
		defer os.Unsetenv("NSS_MVS_STUB_FAIL_OPEN")
		d, err := OpenReal(Info{Index: 0})
		var he *Error
		if d != nil || !errors.As(err, &he) || he.Code != 0x80000203 {
			t.Fatalf("OpenReal = %v, %v; want *Error 0x80000203", d, err)
		}
		if _, err := OpenReal(Info{Index: 7}); !errors.Is(err, ErrNoCamera) {
			t.Fatalf("out-of-range OpenReal err = %v, want ErrNoCamera", err)
		}
	})

	t.Run("capture", func(t *testing.T) {
		os.Setenv("NSS_MVS_STUB_CAMERAS", "1")
		d, err := OpenReal(Info{Index: 0})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Grab(time.Second); !errors.Is(err, ErrNotGrab) {
			t.Fatalf("Grab before StartGrab err = %v, want ErrNotGrab", err)
		}
		// Scan path: Mono12, software trigger, full frame.
		if err := d.Apply(BestScanQuality()); err != nil {
			t.Fatal(err)
		}
		if st := d.Settings(); st.Width != 64 || st.Height != 48 {
			t.Fatalf("full frame read back as %dx%d, want 64x48", st.Width, st.Height)
		}
		if err := d.StartGrab(); err != nil {
			t.Fatal(err)
		}
		f, err := d.Grab(time.Second)
		if err != nil {
			t.Fatal(err)
		}
		checkRamp(t, f, "Mono12", 12, 1, func(v uint32) uint16 { return uint16(v << 4) })

		// Live view path: free-run Mono8, applied while grabbing.
		lv := BestScanQuality()
		lv.SoftwareTrigger = false
		lv.PixelFormat = PixelMono8
		if err := d.Apply(lv); err != nil {
			t.Fatal(err)
		}
		f, err = d.Grab(time.Second)
		if err != nil {
			t.Fatal(err)
		}
		checkRamp(t, f, "Mono8", 8, 2, func(v uint32) uint16 { return uint16(v<<8 | v) })

		if err := d.SetExposureGain(20000, 3); err != nil {
			t.Fatal(err)
		}
		if st := d.Settings(); st.ExposureUS != 20000 || st.GainDB != 3 {
			t.Fatalf("SetExposureGain read back %v us %v dB", st.ExposureUS, st.GainDB)
		}
		if err := d.StopGrab(); err != nil {
			t.Fatal(err)
		}
		if err := d.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Grab(time.Second); !errors.Is(err, ErrNotOpen) {
			t.Fatalf("Grab after Close err = %v, want ErrNotOpen", err)
		}
		if err := d.Close(); err != nil {
			t.Fatalf("second Close: %v", err)
		}
	})
}

// checkRamp compares a frame with the stub's pattern: code(x, y) =
// ((x + y*w)*37 + frameNum) mod 2^bits, unpacked by hikcam to 16 bits.
func checkRamp(t *testing.T, f *Frame, format string, bits uint, frameNum uint32, to16 func(uint32) uint16) {
	t.Helper()
	if f.W != 64 || f.H != 48 || f.PixelFormat != format || len(f.Pix) != 64*48 {
		t.Fatalf("frame %dx%d %s len %d, want 64x48 %s", f.W, f.H, f.PixelFormat, len(f.Pix), format)
	}
	mask := uint32(1)<<bits - 1
	for i, got := range f.Pix {
		want := to16((uint32(i)*37 + frameNum) & mask)
		if got != want {
			t.Fatalf("%s pixel %d = %d, want %d", format, i, got, want)
		}
	}
}
