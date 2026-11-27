package hdrctl

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"basetool/camera"
	"basetool/hdrscan"
	"basetool/macapp/appstate"
	"basetool/rawconv"
	"basetool/rgblight"
	"basetool/scanmeta"
)

// triCam is an X-T3 stand-in for Trichrome tests: every Capture writes a
// linear 16-bit TIFF of a synthetic negative lit by the simulated RGB
// light's current (device-side) state at the current exposure, and the
// calls are recorded.
type triCam struct {
	*mockCam
	light *rgblight.SimDevice
	base  camera.Exposure
	mu    sync.Mutex
	n     int
	log   []string
}

func newTriCam(light *rgblight.SimDevice) *triCam {
	m := newMock()
	return &triCam{mockCam: m, light: light, base: m.exp}
}

func (c *triCam) record(s string) {
	c.mu.Lock()
	c.log = append(c.log, s)
	c.mu.Unlock()
}

func (c *triCam) calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.log...)
}

func (c *triCam) SetExposure(e camera.Exposure) (camera.Exposure, error) {
	c.record("set " + e.String())
	return c.mockCam.SetExposure(e)
}

func (c *triCam) Capture(dir, prefix string) (string, error) {
	on, v := c.light.Lights()
	e := c.mockCam.exp
	c.mu.Lock()
	c.n++
	n := c.n
	c.log = append(c.log, fmt.Sprintf("capture %s light on=%v value=%v", e, on, v))
	c.mu.Unlock()
	k := hdrscan.ExposureValue(e) / hdrscan.ExposureValue(c.base)
	const w, h = 96, 64
	pix := make([]uint16, w*h*3)
	mask := [3]float64{0.9, 0.55, 0.3}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			l := 0.02 + 0.2*(0.5+0.5*math.Sin(float64(x)/9)*math.Cos(float64(y)/7))
			if x < 8 {
				l = 0.6 // clear film base
			}
			for ch := 0; ch < 3; ch++ {
				g := 0.002
				if on[ch] {
					g = float64(v[ch]) / 100
				}
				pix[3*(y*w+x)+ch] = uint16(math.Min(65535, math.Round(l*mask[ch]*g*k*65535)))
			}
		}
	}
	p := filepath.Join(dir, fmt.Sprintf("%sDSCF%04d.tif", prefix, n))
	return p, rawconv.WriteTIFF16(p, w, h, pix, false, "Mock", "Cam", "test")
}

// triRig is a controller with the simulated RGB light and a connected,
// frame-0-locked triCam.
type triRig struct {
	h   *testHost
	c   *Ctl
	l   *Light
	cam *triCam
	out string
}

func newTriRig(t *testing.T) *triRig {
	t.Helper()
	h, c, l := newLightCtl(t)
	connectSim(t, h, l)
	r := &triRig{h: h, c: c, l: l, cam: newTriCam(l.sim), out: t.TempDir()}
	h.scene = "mock.raf"
	c.ListDevices = func() ([]Device, error) { return nil, nil }
	c.OpenSim = func(string, *camera.Logger) (camera.Device, error) { return r.cam, nil }
	c.Cfg.OutDir, c.Cfg.Roll = r.out, "golden"
	l.SettleText = "20"
	c.Connect()
	h.pump(t, "camera connect", 30*time.Second, func() bool { return c.Connected() && !c.Busy })
	t.Cleanup(func() {
		c.Disconnect()
		for len(h.q) > 0 {
			(<-h.q)()
		}
	})
	return r
}

// lock sets frame 0 as a successful Meter base does (the worker writes
// the session, the UI goroutine its copy).
func (r *triRig) lock(t *testing.T, e camera.Exposure) {
	t.Helper()
	done := make(chan struct{})
	r.c.worker.do(func() { r.c.sess.Locked = &e; close(done) })
	<-done
	ee := e
	r.c.lock = &ee
}

func TestTriReasonsAndButtons(t *testing.T) {
	h, c, l := newLightCtl(t)
	tr := c.Tri
	b := tr.Buttons()
	if b.Capture || b.Stop || !b.Mode || b.Meter {
		t.Fatalf("idle buttons %+v", b)
	}
	if tr.WhyText != "Capture disabled: camera not connected" || tr.CamText != "Camera: not connected" ||
		tr.LockText != "Frame 0: not locked" || tr.LightText != "Light: not connected" || tr.ValText != "Scan values: -" {
		t.Fatalf("idle texts: %q %q %q %q %q", tr.WhyText, tr.CamText, tr.LockText, tr.LightText, tr.ValText)
	}
	if !strings.Contains(tr.LogText(), "Capture disabled: Camera not connected (Camera and Light > Camera > Connect).") {
		t.Fatal("reason change not logged:\n" + tr.LogText())
	}
	// mode / bracket: Bracket only in HDR mode; the label follows
	c.Cfg.ScanMode = 0
	tr.ModeChanged()
	if b := tr.Buttons(); b.Bracket || b.CapText != "Capture trichrome" {
		t.Fatalf("single: %+v", b)
	}
	c.Cfg.ScanMode, c.Cfg.TriBracket = 1, 1
	tr.ModeChanged()
	tr.BracketChanged()
	if b := tr.Buttons(); !b.Bracket || b.CapText != "Capture trichrome HDR" || !reflect.DeepEqual(tr.evs(), []int{0, 2, 3}) {
		t.Fatalf("hdr: %+v %v", b, tr.evs())
	}
	if !strings.Contains(tr.LogText(), "Bracket per channel: 0 / +2 / +3 EV") {
		t.Fatal(tr.LogText())
	}
	// persisted
	c2 := New(newHost())
	if c2.Cfg.ScanMode != 1 || c2.Cfg.TriBracket != 1 {
		t.Fatalf("not saved: mode %d bracket %d", c2.Cfg.ScanMode, c2.Cfg.TriBracket)
	}

	// camera connected, frame 0 not locked
	r := &triRig{h: h, c: c, l: l, cam: newTriCam(nil)}
	h.scene = "mock.raf"
	c.OpenSim = func(string, *camera.Logger) (camera.Device, error) { return r.cam, nil }
	c.Connect()
	h.pump(t, "connect", 30*time.Second, func() bool { return c.Connected() && !c.Busy })
	tr.Buttons()
	if tr.WhyText != "Capture disabled: frame 0 not locked" || !tr.Buttons().Meter || tr.CamText == "Camera: not connected" {
		t.Fatalf("unlocked: %q", tr.WhyText)
	}
	r.lock(t, camera.Exposure{ISO: 160, Shutter: 3906})
	tr.Buttons()
	if tr.WhyText != "Capture disabled: RGB light not connected" || !strings.HasPrefix(tr.LockText, "Frame 0: LOCKED  ISO 160, ") {
		t.Fatalf("locked, no light: %q %q", tr.WhyText, tr.LockText)
	}
	connectSim(t, h, l)
	b = tr.Buttons()
	if !b.Capture || tr.WhyText != "Ready. Press Capture for the next frame." || !strings.HasPrefix(tr.ValText, "Scan values: R ") {
		t.Fatalf("ready: %+v %q %q", b, tr.WhyText, tr.ValText)
	}
	if !strings.HasPrefix(tr.OutText, "Output: ") || !strings.Contains(tr.OutText, "next #0001") {
		t.Fatal(tr.OutText)
	}
	// a refused capture says why and does nothing
	l.busy = true
	tr.Capture()
	l.busy = false
	if tr.Running() || !strings.Contains(tr.LogText(), "Capture refused: RGB light busy (leveling or a light command).") {
		t.Fatal(tr.LogText())
	}
	c.Disconnect()
	h.pump(t, "disconnect", 30*time.Second, func() bool { return !c.Connected() })
}

var shotRE = regexp.MustCompile(`golden_\d{8}_\d{6}_\d{4}`)

func maskShot(s string) string { return shotRE.ReplaceAllString(s, "golden_SHOT") }

// windowsTriScan is TriUI.capture + finish + nextMerge (../tripage.go)
// transcribed literally onto the rig's session and light client, run
// synchronously: the reference the macOS page must match.
func windowsTriScan(t *testing.T, r *triRig, evs []int) (*hdrscan.TriShot, *hdrscan.TriResult, hdrscan.ProcessOptions) {
	t.Helper()
	h, lu := r.c, r.l
	dir, err := h.rollDir()
	if err != nil {
		t.Fatal(err)
	}
	var vals [3]int
	if cs := lu.cl.CachedState(); cs != nil {
		vals = cs.Value
	}
	settle := time.Duration(lu.cfg.SettleMS) * time.Millisecond
	if ms, perr := strconv.Atoi(strings.TrimSpace(lu.SettleText)); perr == nil && ms >= 0 && ms <= 5000 {
		settle = time.Duration(ms) * time.Millisecond
	}
	c := lu.cl
	s := h.sess
	// HDRUI.processOptions (../hdrpage.go)
	mo := hdrscan.DefaultOptions()
	mo.Align = appstate.AlignMode(h.Cfg)  // alignModes[alignCB.CurrentIndex()]
	mo.Method = appstate.MergeMode(h.Cfg) // mergeModes[mergeCB.CurrentIndex()]
	opts := hdrscan.ProcessOptions{Merge: mo, OutDir: dir, KeepTIFF: h.Cfg.KeepTIFF, DeleteRAW: h.Cfg.DeleteRAW, LZW: h.Cfg.LZW, Software: appVersion, Metadata: h.scanMetadata(), LeakPct: 20, Dust: h.Cfg.Dust}
	opts.Metadata.LightRGB = &vals
	opts.Metadata.LightModel = "CuteNewDesign RGB Light Air"
	opts.Metadata.Mode = "trichrome"
	opts.Metadata = withScanCamera(opts.Metadata).WithLeveling(h.metadataLeveling(), vals)
	opts.MeteredBase = s.Reading.RGB
	opts.BaseArea = s.Reading.Area
	if !s.Reading.Region.Rect.Empty() {
		reg := s.Reading.Region
		opts.BaseRegion = &reg
	}
	roll, last := h.Cfg.Roll, h.Cfg.Counter
	sw := &rgblight.ChannelSwitch{C: c, Values: vals}
	before, err := sw.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var num int
	shot, err := s.CaptureTrichrome(context.Background(), dir, evs, settle, sw, func(tm time.Time) string {
		b, k := hdrscan.NextShot(dir, roll, tm, last)
		num = k
		return b
	})
	if err != nil {
		t.Fatal(err)
	}
	if cs := c.CachedState(); cs == nil || cs.On != before.On || cs.Value != before.Value {
		t.Fatalf("reference: light not restored: %v vs %v", cs, before)
	}
	h.Cfg.Counter = num
	o := opts
	o.Ctx = context.Background()
	res, err := hdrscan.ProcessTrichrome(shot.Raws, shot.Exposures, o)
	if err != nil {
		t.Fatal(err)
	}
	return shot, res, opts
}

// TestTriGoldenMatchesWindows scans one HDR trichrome frame with the
// macOS page and with the literal Windows sequence on the same simulated
// light and camera, and compares the light / camera command sequence, the
// exposures, the file names and the merged TIFF (byte for byte, except
// the metadata creation time).
func TestTriGoldenMatchesWindows(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    int
		bracket int
		evs     []int
	}{{"single", 0, 0, []int{0}}, {"hdr-0-2-3", 1, 1, []int{0, 2, 3}}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTriRig(t)
			c, tr := r.c, r.c.Tri
			c.Cfg.ScanMode, c.Cfg.TriBracket = tc.mode, tc.bracket
			e0 := camera.Exposure{ISO: 160, Shutter: 3906}
			r.lock(t, e0)
			if b := tr.Buttons(); !b.Capture {
				t.Fatalf("not ready: %s", tr.WhyText)
			}
			lightBefore := append([]byte(nil), r.l.sim.Received()...)
			tr.Capture()
			if !tr.Running() || !c.Busy || !r.l.busy {
				t.Fatal("not running")
			}
			if b := tr.Buttons(); b.Capture || !b.Stop || b.Mode || b.Meter {
				t.Fatalf("running buttons %+v", b)
			}
			r.h.pump(t, "scan and merge", 120*time.Second, func() bool { return !tr.Running() && !tr.Merging() })
			if !strings.HasPrefix(tr.ResText, "Saved ") {
				t.Fatalf("result: %s\n%s", tr.ResText, tr.LogText())
			}
			macCalls := r.cam.calls()
			macLight := r.l.sim.Received()[len(lightBefore):]
			macOut := lastTriOut
			if c.Cfg.Counter != 1 || c.Busy || r.l.busy {
				t.Fatalf("counter %d busy %v/%v", c.Cfg.Counter, c.Busy, r.l.busy)
			}

			// the Windows sequence on the same rig
			r.cam.mu.Lock()
			r.cam.log = nil
			r.cam.mu.Unlock()
			lightBefore = append([]byte(nil), r.l.sim.Received()...)
			shot, res, _ := windowsTriScan(t, r, tc.evs)
			winCalls := r.cam.calls()
			winLight := r.l.sim.Received()[len(lightBefore):]

			if !reflect.DeepEqual(macCalls, winCalls) {
				t.Fatalf("camera calls differ:\nmac %q\nwin %q", macCalls, winCalls)
			}
			if n := 3 * len(tc.evs); strings.Count(strings.Join(macCalls, "\n"), "capture ") != n {
				t.Fatalf("want %d captures: %q", n, macCalls)
			}
			all := strings.Join(macCalls, "\n")
			for _, only := range []string{"on=[true false false]", "on=[false true false]", "on=[false false true]"} {
				if strings.Count(all, only) != len(tc.evs) {
					t.Fatalf("want %d captures with %s: %q", len(tc.evs), only, macCalls)
				}
			}
			if !bytes.Equal(macLight, winLight) || len(macLight) == 0 {
				t.Fatalf("light commands differ:\nmac %q\nwin %q", cmdNames(macLight), cmdNames(winLight))
			}
			// file names (shot base masked: time and number)
			macRaws, _ := filepath.Glob(filepath.Join(filepath.Dir(macOut), "golden_*_0001_*.tif"))
			var winRaws []string
			for ch := 0; ch < 3; ch++ {
				winRaws = append(winRaws, shot.Raws[ch]...)
			}
			var mn, wn []string
			for _, p := range macRaws {
				mn = append(mn, maskShot(filepath.Base(p)))
			}
			for _, p := range winRaws {
				wn = append(wn, maskShot(filepath.Base(p)))
			}
			sortStrings(mn)
			sortStrings(wn)
			if !reflect.DeepEqual(mn, wn) || len(mn) != 3*len(tc.evs) {
				t.Fatalf("RAW names: mac %q win %q", mn, wn)
			}
			if maskShot(filepath.Base(macOut)) != maskShot(filepath.Base(res.Out)) {
				t.Fatalf("output %s vs %s", macOut, res.Out)
			}
			a, err := os.ReadFile(macOut)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(res.Out)
			if err != nil {
				t.Fatal(err)
			}
			a, b = maskCreated(t, a), maskCreated(t, b)
			if !bytes.Equal(a, b) {
				t.Fatalf("merged TIFFs differ (%d / %d bytes)", len(a), len(b))
			}
			t.Logf("%d camera calls, light commands %q, %d RAWs, TIFF %d bytes identical", len(macCalls), cmdNames(macLight), len(mn), len(a))
		})
	}
}

func cmdNames(b []byte) []string {
	var out []string
	for _, c := range b {
		out = append(out, rgblight.CmdName(c))
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// TestTriStopRestoresLight stops a scan between channels: partial RAFs
// are deleted and the light is back to its pre-scan state.
func TestTriStopRestoresLight(t *testing.T) {
	r := newTriRig(t)
	c, tr := r.c, r.c.Tri
	c.Cfg.ScanMode, c.Cfg.TriBracket = 1, 0
	r.l.SettleText = "400"
	r.lock(t, camera.Exposure{ISO: 160, Shutter: 3906})
	tr.Buttons()
	before := *r.l.cl.CachedState()
	tr.Capture()
	r.h.pump(t, "first capture", 10*time.Second, func() bool { return len(r.cam.calls()) >= 2 })
	tr.Stop()
	r.h.pump(t, "stopped", 20*time.Second, func() bool { return !tr.Running() })
	if !strings.HasPrefix(tr.ResText, "Trichrome scan 1 stopped") || !strings.Contains(tr.ResText, "Light back to its pre-scan state") {
		t.Fatalf("result %q", tr.ResText)
	}
	if left, _ := filepath.Glob(filepath.Join(r.out, "*", "golden_*")); len(left) != 0 {
		t.Fatalf("partial files kept: %q", left)
	}
	if cs := r.l.cl.CachedState(); cs.On != before.On || cs.Value != before.Value {
		t.Fatalf("light %v, before %v", cs, before)
	}
}

// TestTriMetadataLeveling: the Film base metadata check box writes the
// accepted leveling result when the scan values match it.
func TestTriMetadataLeveling(t *testing.T) {
	_, c, _ := newLightCtl(t)
	v := [3]int{40, 55, 70}
	if c.metadataLeveling() != nil {
		t.Fatal("no leveling yet")
	}
	c.Light.cfg.Leveling = &scanmeta.Leveling{Values: v, Base: [3]float64{0.8, 0.79, 0.81}, At: "2026-11-26T02:00:00Z"}
	c.SetUseLastLeveling(true)
	if got := c.lastLevelingText(v); got != "Last leveling: R 0.80000 G 0.79000 B 0.81000 (after profile, 2026-11-26 10:00:00 CST (UTC+8))" {
		t.Fatal(got)
	}
	l := c.metadataLeveling()
	if !l.Matches(v) || l.Matches([3]int{40, 55, 71}) {
		t.Fatal("matches")
	}
	md := scanmeta.Metadata{}.WithLeveling(l, v)
	if md.FilmBase == nil || md.BaseSource != scanmeta.LevelingSource {
		t.Fatalf("%+v", md)
	}
	c.SetUseLastLeveling(false)
	if c.metadataLeveling() != nil {
		t.Fatal("off still writes")
	}
}
