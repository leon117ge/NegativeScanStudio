package hdrctl

import (
	"os"
	"strings"
	"testing"
	"time"

	"basetool/camera"
	"basetool/filmdb"
	"basetool/hdrscan"
)

// Every film-stock compare string of the Windows meter, stockRecheck and
// capture code is in the port (the code around them is walk UI, so the
// strings are checked instead of whole declarations).
func TestStockCompareStringsMatchWindows(t *testing.T) {
	win, err := os.ReadFile("../../hdrpage.go")
	if err != nil {
		t.Fatal(err)
	}
	var mac string
	for _, f := range []string{"ctl.go", "filmdb.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		mac += string(b)
	}
	for _, s := range []string{
		`"\nFilm stock: None (no DB check)"`,
		`ent.Name+": no base to compare ("+berr.Error()+")", false`,
		`ent.Name + ": no base to compare (" + err.Error() + ")"`,
		`cmp, cmpOK = compareText(ent, b1, e, tol)`,
		`t, _ = compareText(ent, b1, e, tol)`,
		`if (len(warns) > 0 || !cmpOK) && ho == nil {`,
		`"Frame 0"`,
	} {
		if !strings.Contains(string(win), s) {
			t.Errorf("not in hdrpage.go any more: %s", s)
		}
		if !strings.Contains(mac, s) {
			t.Errorf("not in the port: %s", s)
		}
	}
	if strings.Contains(mac, "not on macOS yet") {
		t.Error("a 'not on macOS yet' placeholder is left")
	}
}

// fixedRig is a connected camera whose shots are a real RAW, set to Fixed
// frame 0.
func fixedRig(t *testing.T) (*testHost, *Ctl) {
	raf := testRAF(t)
	h, c := dbNew(t)
	h.scene = "x"
	c.Cfg.OutDir = t.TempDir()
	c.Cfg.Frame0 = 1
	cam := &fileCam{mockCam: newMock(), raf: raf}
	c.OpenSim = func(string, *camera.Logger) (camera.Device, error) { return cam, nil }
	c.ListDevices = func() ([]Device, error) { return nil, nil }
	c.Connect()
	h.pump(t, "connect", 5*time.Second, c.Connected)
	return h, c
}

func meter(t *testing.T, h *testHost, c *Ctl) {
	t.Helper()
	h.messages = nil
	c.Meter()
	h.pump(t, "lock", 60*time.Second, func() bool { return !c.Busy })
	if !c.Locked() {
		t.Fatalf("not locked: %q %q", c.ExpLbl, c.BaseLbl)
	}
}

// probe is the locked probe's base (the camera worker is idle).
func probe(t *testing.T, c *Ctl) [3]float64 {
	b1, _, err := c.sess.ProbeBase(leakPct)
	if err != nil {
		t.Fatal(err)
	}
	return b1
}

func waitBase(t *testing.T, h *testHost, c *Ctl, want string) {
	t.Helper()
	h.pump(t, "base readout "+want, 30*time.Second, func() bool { return c.BaseLbl == want })
}

// Meter base (Fixed frame 0) and stockRecheck: no stock, a matching stock
// (pass), a different stock (fail, with the Frame 0 notice), the stock
// changed after the lock, and a probe that is gone.
func TestFixedFrame0StockCompare(t *testing.T) {
	h, c := fixedRig(t)

	// no stock
	meter(t, h, c)
	base := c.lockBase
	warned := strings.Contains(base, "\nWARNING: ")
	if base == "" || c.BaseLbl != base+"\nFilm stock: None (no DB check)" {
		t.Fatalf("no stock: lockBase %q label %q", base, c.BaseLbl)
	}
	if (len(h.messages) == 1) != warned {
		t.Fatalf("no stock notice %q (warnings %v)", h.messages, warned)
	}
	e, b1 := *c.lock, probe(t, c)
	c.DB.Put(filmdb.Entry{Name: "Match", R: b1[0], G: b1[1], B: b1[2], ISO: e.ISO, Shutter: e.Shutter, Aperture: 4}, false)
	c.DB.Put(filmdb.Entry{Name: "Other", R: b1[0] * 2, G: b1[1], B: b1[2] / 2, ISO: e.ISO, Shutter: e.Shutter, Aperture: 4}, false)
	match, other := c.DB.Find("Match"), c.DB.Find("Other")
	pass, ok := compareText(match, b1, e, c.tol())
	fail, bad := compareText(other, b1, e, c.tol())
	if !ok || bad || !strings.HasPrefix(pass, "Match: Film base OK vs DB: ") || !strings.HasPrefix(fail, "Other: Film base WARNING vs DB (") {
		t.Fatalf("fixtures %q %q", pass, fail)
	}

	// stock changed after the lock: rechecked from the probe, nothing reshot
	shots := c.Cfg.Counter
	c.SetStock(c.stockIndexOf("Match"))
	waitBase(t, h, c, base+"\n"+pass)
	c.SetStock(c.stockIndexOf("Other"))
	waitBase(t, h, c, base+"\n"+fail)
	c.SetStock(0)
	if c.BaseLbl != base+"\nFilm stock: None (no DB check)" {
		t.Fatalf("recheck none %q", c.BaseLbl)
	}
	if c.Cfg.Counter != shots || len(h.messages) > 1 {
		t.Fatalf("recheck shot or warned: %d %q", c.Cfg.Counter, h.messages)
	}

	// pass at Meter base: the line, the log, no notice for the compare
	c.SetStock(c.stockIndexOf("Match"))
	meter(t, h, c)
	if c.BaseLbl != c.lockBase+"\n"+pass {
		t.Fatalf("pass label %q", c.BaseLbl)
	}
	if (len(h.messages) == 1) != warned {
		t.Fatalf("pass notice %q", h.messages)
	}
	logHas(t, h, c, pass)

	// fail at Meter base: the line, the log and the in-window Frame 0 notice
	c.SetStock(c.stockIndexOf("Other"))
	meter(t, h, c)
	if c.BaseLbl != c.lockBase+"\n"+fail {
		t.Fatalf("fail label %q", c.BaseLbl)
	}
	if len(h.messages) != 1 || !strings.HasPrefix(h.messages[0], "Frame 0: ") || (!strings.HasSuffix(h.messages[0], "\n"+fail) && h.messages[0] != "Frame 0: "+fail) {
		t.Fatalf("fail notice %q", h.messages)
	}
	logHas(t, h, c, fail)

	// the probe is gone: "no base to compare"
	os.Remove(c.sess.LastProbePath)
	c.SetStock(c.stockIndexOf("Match"))
	h.pump(t, "no base", 30*time.Second, func() bool {
		return strings.HasPrefix(c.BaseLbl, c.lockBase+"\nMatch: no base to compare (")
	})

	// a metered (not Fixed) lock has no stock line and no recheck
	c.Cfg.Frame0 = 0
	c.clearLocked()
	if c.SetStock(0); c.lockBase != "" {
		t.Fatal("lockBase kept after clearLocked")
	}
	c.Disconnect()
}

// The capture result line: a clean Fixed frame 0 is compared with the
// selected stock; none with no stock, a dirty frame 0 or Metered frame 0.
func TestCaptureResultStockCompare(t *testing.T) {
	_, c := dbNew(t)
	e := camera.Exposure{ISO: 250, Shutter: 16667}
	rgb := [3]float64{0.2, 0.18, 0.07}
	c.DB.Put(filmdb.Entry{Name: "Match", R: rgb[0], G: rgb[1], B: rgb[2], ISO: 250, Shutter: 16667, Aperture: 4}, false)
	c.DB.Put(filmdb.Entry{Name: "Other", R: rgb[0] * 2, G: rgb[1], B: rgb[2], ISO: 160, Shutter: 16667, Aperture: 4}, false)
	c.Cfg.Frame0 = 1
	c.lastF0 = &frame0Base{RGB: rgb, N: 9, Exp: e, Pure: true}
	res := &hdrscan.Result{HDR: "/x/a_HDR.tiff", Frames: 2}

	if got := c.captureCompare(); got != "" {
		t.Fatalf("no stock %q", got)
	}
	c.Cfg.Stock = "Match"
	pass, _ := compareText(c.DB.Find("Match"), rgb, e, c.tol())
	if got := c.captureCompare(); got != "\n"+pass || !strings.Contains(pass, "Film base OK vs DB") {
		t.Fatalf("pass %q", got)
	}
	c.Cfg.Stock = "Other"
	fail, _ := compareText(c.DB.Find("Other"), rgb, e, c.tol())
	if got := c.captureCompare(); got != "\n"+fail || !strings.Contains(fail, "ISO mismatch: frame 0 ISO 250, DB ISO 160") {
		t.Fatalf("fail %q", got)
	}
	// the whole result text: resultText, then the compare line
	msg := resultText(res, c.processOptions(t.TempDir()).Metadata) + c.captureCompare()
	if !strings.HasSuffix(msg, "\n"+fail) || !strings.HasPrefix(msg, "Saved /x/a_HDR.tiff  (2 frames, ") {
		t.Fatalf("result %q", msg)
	}
	c.Cfg.Frame0 = 0
	if got := c.captureCompare(); got != "" {
		t.Fatalf("metered %q", got)
	}
	c.Cfg.Frame0 = 1
	c.lastF0.Pure = false
	if got := c.captureCompare(); got != "" {
		t.Fatalf("dirty frame 0 %q", got)
	}
}
