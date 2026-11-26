// mactether is the macOS hardware check for the X-T3 tether path (W6): the
// same Fuji PTP logic as the app (camera/fuji.go) over Apple ImageCaptureCore
// (camera/icc*.go) instead of Windows WPD.
//
//	mactether [flags] [command [args]]
//
// Commands (default: list):
//
//	list              cameras ImageCaptureCore sees (no camera is not an error)
//	info              open the camera: model, firmware, serial, exposure, vendor ops
//	lv [FILE]         save one live view JPEG (default lv.jpg); -n N times N frames
//	capture [DIR]     shoot once and download the RAF into DIR (default .)
//
// Flags:
//
//	-uuid U       camera UUID from "list" (default: the first PTP-capable camera)
//	-sim          use the built-in simulated X-T3 instead of ImageCaptureCore
//	-v            print the camera log (every PTP transaction) to stderr
//	-log DIR      also append the camera log to DIR/camera_log.txt
//	-n N          live view: fetch N frames and report frames per second
//	-prefix P     capture: file name prefix (default mactether_)
//	-timeout D    time to wait for ImageCaptureCore to find and open the camera (default 10s)
//	-outheader    send data-out phases with a PTP container header (hardware experiment)
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"basetool/camera"
)

// ImageCaptureCore calls back on the main run loop; keep main() on the main
// thread so the waits in camera/icc_darwin.m can pump it.
func init() { runtime.LockOSThread() }

type options struct {
	uuid, logDir, prefix string
	sim, verbose, outHdr bool
	n                    int
	timeout              time.Duration
}

func main() {
	var o options
	flag.StringVar(&o.uuid, "uuid", "", "camera UUID from list (default: first PTP-capable camera)")
	flag.BoolVar(&o.sim, "sim", false, "use the simulated X-T3")
	flag.BoolVar(&o.verbose, "v", false, "print the camera log to stderr")
	flag.StringVar(&o.logDir, "log", "", "append the camera log to DIR/camera_log.txt")
	flag.IntVar(&o.n, "n", 1, "live view: number of frames")
	flag.StringVar(&o.prefix, "prefix", "mactether_", "capture: file name prefix")
	flag.DurationVar(&o.timeout, "timeout", 10*time.Second, "camera discovery/open timeout")
	flag.BoolVar(&o.outHdr, "outheader", false, "send data-out with a PTP container header")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: mactether [-uuid U] [-sim] [-v] [-log DIR] [-n N] [-prefix P] [-timeout D] [-outheader] [list | info | lv [FILE] | capture [DIR]]")
		flag.PrintDefaults()
	}
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		args = []string{"list"}
	}
	if err := run(o, args); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}

func run(o options, args []string) error {
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "list":
		if len(rest) > 0 {
			return errors.New("list takes no arguments")
		}
		return list(o)
	case "info", "lv", "capture":
		if len(rest) > 1 {
			return fmt.Errorf("%s takes at most one argument", cmd)
		}
	default:
		return fmt.Errorf("unknown command %q (list, info, lv, capture)", cmd)
	}
	if o.n < 1 {
		return errors.New("-n must be at least 1")
	}
	log := camera.NewLogger(o.logDir)
	defer log.Close()
	if o.verbose {
		log.OnLine = func(s string) { fmt.Fprintln(os.Stderr, s) }
	}
	var t *camera.ICCTransport
	var cam camera.ICCCamera
	driver := "ICC"
	if o.sim {
		t, cam, driver = camera.OpenICCSim(), camera.SimICCCamera, "ICC (simulated)"
	} else {
		var err error
		if t, cam, err = camera.OpenICC(o.uuid, o.timeout); err != nil {
			return err
		}
	}
	t.DataOutHeader = o.outHdr
	f, err := camera.OpenFuji(t, log, driver)
	if err != nil {
		t.Close()
		return err
	}
	defer f.Close()
	fmt.Printf("camera: %s\n", cam)
	switch cmd {
	case "info":
		return info(f, t)
	case "lv":
		file := "lv.jpg"
		if len(rest) == 1 {
			file = rest[0]
		}
		return liveView(f, file, o.n)
	default:
		dir := "."
		if len(rest) == 1 {
			dir = rest[0]
		}
		return capture(f, dir, o.prefix)
	}
}

func list(o options) error {
	if o.sim {
		fmt.Printf("1 camera:\n  %s\n  uuid %s\n", camera.SimICCCamera, camera.SimICCCamera.UUID)
		return nil
	}
	cams, err := camera.ListICC(o.timeout / 2)
	if err != nil {
		return err
	}
	if ctl, cnt, err := camera.ICCAuthorization(o.timeout / 2); err == nil {
		fmt.Printf("ImageCaptureCore access: control %s, contents %s\n", ctl, cnt)
	}
	if len(cams) == 0 {
		fmt.Println("no cameras found (connect the X-T3 by USB, switch it on, CONNECTION MODE = USB TETHER SHOOTING AUTO)")
		return nil
	}
	fmt.Printf("%d camera(s):\n", len(cams))
	for _, c := range cams {
		fmt.Printf("  %s\n  uuid %s\n", c, c.UUID)
	}
	return nil
}

func info(f *camera.Fuji, t *camera.ICCTransport) error {
	i := f.Info()
	fmt.Printf("model: %s %s\nfirmware: %s\nserial: %s\ndriver: %s\n", i.Manufacturer, i.Model, i.Version, i.Serial, i.Driver)
	fmt.Printf("vendor extension: 0x%08X %q\noperations: %d, properties: %d, events: %d\n", i.VendorExt, i.VendorDesc, len(i.Ops), len(i.Props), len(i.Events))
	fmt.Printf("live view (InitiateOpenCapture): %v\n", i.Supports(camera.OpInitiateOpenCapture))
	if ops, err := t.SupportedVendorOps(); err == nil {
		var s []string
		for _, op := range ops {
			s = append(s, fmt.Sprintf("0x%04X", op))
		}
		fmt.Printf("vendor ops: %s\n", strings.Join(s, " "))
	}
	e, fn, err := f.Status()
	if err != nil {
		return fmt.Errorf("read exposure: %w", err)
	}
	fmt.Printf("exposure: %s, f-number %.1f (0 = manual lens)\n", e, fn)
	if m, name, err := f.ExposureMode(); err == nil {
		fmt.Printf("exposure mode: %s (manual: %v)\n", name, m)
	}
	return nil
}

func liveView(f *camera.Fuji, file string, n int) error {
	var jpg []byte
	t0 := time.Now()
	for k := 0; k < n; k++ {
		var err error
		if jpg, err = f.LiveView(); err != nil {
			f.StopLiveView()
			return fmt.Errorf("live view frame %d: %w", k+1, err)
		}
	}
	el := time.Since(t0)
	f.StopLiveView()
	if err := os.WriteFile(file, jpg, 0o644); err != nil {
		return err
	}
	fmt.Printf("live view: %d frame(s) in %d ms (%.1f fps), last frame %d bytes saved to %s\n", n, el.Milliseconds(), float64(n)/el.Seconds(), len(jpg), file)
	return nil
}

func capture(f *camera.Fuji, dir, prefix string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	t0 := time.Now()
	p, err := f.Capture(dir, prefix)
	if err != nil {
		return err
	}
	st, err := os.Stat(p)
	if err != nil {
		return err
	}
	kind := "not a RAF"
	if b, err := os.ReadFile(p); err == nil && bytes.HasPrefix(b, []byte("FUJIFILMCCD-RAW")) {
		kind = "RAF"
	}
	abs, _ := filepath.Abs(p)
	fmt.Printf("captured: %s (%d bytes, %s) in %d ms\n", abs, st.Size(), kind, time.Since(t0).Milliseconds())
	if kind != "RAF" {
		return errors.New("the downloaded file is not a Fujifilm RAF")
	}
	return nil
}
