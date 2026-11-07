package hdrscan

import (
	"context"
	"errors"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"basetool/basestat"
	"basetool/camera"
	"basetool/rgblight"
)

const triScene = "./testdata/negsamples2/_DSF9229.RAF"

type triRig struct {
	sim   *camera.Sim
	light *rgblight.SimDevice
	cl    *rgblight.Client
	sess  *Session
	mu    sync.Mutex
	lit   []string // lit channels at each capture
	onCap func(n int)
}

func newTriRig(t *testing.T, ack rgblight.AckStyle) *triRig {
	if _, err := os.Stat(triScene); err != nil {
		t.Skip("scene RAF not available")
	}
	r := &triRig{}
	lg := camera.NewLogger("")
	lg.OnLine = func(s string) { t.Log(s) }
	sim, err := camera.OpenSim(triScene, lg)
	if err != nil {
		t.Fatal(err)
	}
	sim.Native = camera.Exposure{ISO: 160, Shutter: 31250}
	sim.SetDials(camera.Exposure{ISO: 160, Shutter: 4000})
	d := rgblight.NewSimDevice()
	d.Protocol, d.Firmware, d.OddAck = "1.4", "V1.0.5", ack
	d.TelEvery, d.StepTime = 50*time.Millisecond, 5*time.Millisecond
	d.Start()
	c := rgblight.NewClient(d)
	c.Timeout = 300 * time.Millisecond
	c.OnLog = func(s string) { t.Log("light: " + s) }
	c.Start()
	t.Cleanup(func() { c.Close() })
	sim.ChannelGain = func() [3]float64 {
		on, v := d.Lights()
		var g [3]float64
		lit := ""
		for k := 0; k < 3; k++ {
			if on[k] {
				g[k] = float64(v[k]) / 50
				lit += rgblight.ChannelNames[k][:1]
			}
		}
		r.mu.Lock()
		r.lit = append(r.lit, lit)
		n := len(r.lit)
		f := r.onCap
		r.mu.Unlock()
		if f != nil {
			f(n)
		}
		return g
	}
	r.sim, r.light, r.cl = sim, d, c
	r.sess = &Session{Dev: sim, Log: lg, Target: 0.8, ProbeDir: t.TempDir(), Fixed: camera.Shutter1_125}
	r.sess.Dials = func(e camera.Exposure, why string) bool { sim.SetDials(e); return true }
	return r
}

func (r *triRig) lock(t *testing.T) {
	// frame 0 is locked under white light (all on), as the user does
	d := r.light
	d.SetLights([3]bool{true, true, true}, [3]int{50, 50, 50})
	e, _, _, err := r.sess.LockFixed(250)
	if err != nil || e != (camera.Exposure{ISO: 250, Shutter: 7812}) {
		t.Fatal(e, err)
	}
	r.mu.Lock()
	r.lit = nil
	r.mu.Unlock()
}

func TestTrichromeSingleAndHDR(t *testing.T) {
	for _, tc := range []struct {
		name string
		evs  []int
		ack  rgblight.AckStyle
	}{{"single", []int{0}, rgblight.AckNormal}, {"hdr-noack", []int{0, 2}, rgblight.AckNone}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTriRig(t, tc.ack)
			r.lock(t)
			dir := t.TempDir()
			vals := [3]int{40, 55, 70}
			sw := &rgblight.ChannelSwitch{C: r.cl, Values: vals}
			if _, err := sw.Snapshot(); err != nil {
				t.Fatal(err)
			}
			sh, err := r.sess.CaptureTrichrome(context.Background(), dir, tc.evs, 10*time.Millisecond, sw, func(time.Time) string { return "Roll_20261102_120000_0001" })
			if err != nil {
				t.Fatal(err)
			}
			n := len(tc.evs)
			var want []string
			for _, c := range []string{"R", "G", "B"} {
				for k := 0; k < n; k++ {
					want = append(want, c)
				}
			}
			if strings.Join(r.lit, ",") != strings.Join(want, ",") {
				t.Fatalf("lit at each capture %v, want %v", r.lit, want)
			}
			for ch := 0; ch < 3; ch++ {
				if len(sh.Raws[ch]) != n || !strings.HasPrefix(filepath.Base(sh.Raws[ch][0]), "Roll_20261102_120000_0001_"+TriTags[ch]+"_0EV") {
					t.Fatalf("raws %v", sh.Raws)
				}
			}
			// v0.82: the light is back exactly as before the scan
			on, v := r.light.Lights()
			if on != [3]bool{true, true, true} || v != [3]int{50, 50, 50} {
				t.Fatalf("light after scan %v %v, want the pre-scan state", on, v)
			}
			if e, _ := r.sim.Exposure(); e != *r.sess.Locked {
				t.Fatalf("camera left at %v", e)
			}
			mo := DefaultOptions()
			po := ProcessOptions{Merge: mo, Software: "test", OutDir: dir, BaseArea: r.sess.Reading.Area}
			if !r.sess.Reading.Region.Rect.Empty() {
				reg := r.sess.Reading.Region
				po.BaseRegion = &reg
			}
			res, err := ProcessTrichrome(sh.Raws, sh.Exposures, po)
			if err != nil {
				t.Fatal(err)
			}
			wantName := "Roll_20261102_120000_0001_RGB.tiff"
			if n > 1 {
				wantName = "Roll_20261102_120000_0001_RGB_HDR.tiff"
			}
			if filepath.Base(res.Out) != wantName {
				t.Fatal(res.Out)
			}
			for ch := 0; ch < 3; ch++ {
				for c := 0; c < 3; c++ {
					if c != ch && res.Leak[ch][c]*5 > res.Level[ch] {
						t.Fatalf("%s-lit frame: own %.4f vs %d %.4f", TriTags[ch], res.Level[ch], c, res.Leak[ch][c])
					}
				}
				if (n > 1) != (res.Channel[ch] != nil) {
					t.Fatal("per-channel HDR")
				}
			}
			p, err := Load(res.Out)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Free()
			if m, _ := filepath.Glob(filepath.Join(dir, "*_HDR.tiff")); n > 1 && len(m) != 1 {
				t.Fatalf("per-channel HDR TIFFs left: %v", m)
			}
			// Normal scan capture must not measure scene content as film base.
			if res.BaseN != 0 {
				t.Fatal("automatic post-merge base measurement survived")
			}
			// Only the explicit Light-page calibration check measures the TIFF.
			measureTriBase(res, p.Pix, p, ProcessOptions{})
			if res.BaseErr != "" || res.BaseN < 100 {
				t.Fatalf("base not measured: %q n=%d", res.BaseErr, res.BaseN)
			}
			if res.BaseRegion.Rect.Empty() || !res.BaseRegion.Rect.In(image.Rect(0, 0, p.W, p.H)) {
				t.Fatalf("base region %v", res.BaseRegion.Rect)
			}
			again, _, _, err := basestat.Measure(basestat.PlaneGetter(p.Pix, p.W), p.W, p.H, res.BaseRegion, 20)
			if err != nil {
				t.Fatal(err)
			}
			for c := 0; c < 3; c++ {
				if res.Base[c] <= 0.02 || res.Base[c] >= 1 || math.Abs(again[c]-res.Base[c]) > 1.0/65535 {
					t.Fatalf("base %v, re-read from the TIFF %v", res.Base, again)
				}
			}
			t.Logf("Density_In film base R %.4f G %.4f B %.4f (%d samples, %v)", res.Base[0], res.Base[1], res.Base[2], res.BaseN, res.BaseRegion.Rect)
			t.Logf("%s: %dx%d levels R %.4f G %.4f B %.4f", filepath.Base(res.Out), p.W, p.H, res.Level[0], res.Level[1], res.Level[2])
		})
	}
}

// Abort (ctx cancel) and a failed light switch both restore the light's
// pre-scan state.
func TestTrichromeAbortLightRestored(t *testing.T) {
	r := newTriRig(t, rgblight.AckNone)
	r.lock(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.onCap = func(n int) {
		if n == 3 { // G 0EV
			cancel()
		}
	}
	sw := &rgblight.ChannelSwitch{C: r.cl, Values: [3]int{40, 55, 70}}
	r.light.SetLights([3]bool{true, false, true}, [3]int{33, 44, 22})
	if _, err := sw.Snapshot(); err != nil {
		t.Fatal(err)
	}
	sh, err := r.sess.CaptureTrichrome(ctx, t.TempDir(), []int{0, 2}, 0, sw, nil)
	if !errors.Is(err, context.Canceled) || sh.Done != 1 || len(r.lit) != 3 {
		t.Fatalf("err %v done %d lit %v", err, sh.Done, r.lit)
	}
	if on, v := r.light.Lights(); on != [3]bool{true, false, true} || v != [3]int{33, 44, 22} {
		t.Fatalf("light after abort %v %v, want the pre-scan state", on, v)
	}
	r.onCap = nil
	bad := &rgblight.ChannelSwitch{C: r.cl, Values: [3]int{40, 0, 70}}
	if _, err := bad.Snapshot(); err != nil {
		t.Fatal(err)
	}
	_, err = r.sess.CaptureTrichrome(context.Background(), t.TempDir(), []int{0}, 0, bad, nil)
	if err == nil || len(r.lit) != 3 {
		t.Fatalf("bad values accepted: %v %v", err, r.lit)
	}
	if on, v := r.light.Lights(); on != [3]bool{true, false, true} || v != [3]int{33, 44, 22} {
		t.Fatalf("light after a failed switch %v %v, want the pre-scan state", on, v)
	}
}

// TestTrichromeThreeConsecutive (v0.82): three trichrome+HDR scans in a row
// on the FW V1.0.5 light (03 confirmed by its state event) without
// re-metering: frame 0 stays locked, each scan applies the stored values,
// and the light ends every scan exactly as it was before.
func TestTrichromeThreeConsecutive(t *testing.T) {
	r := newTriRig(t, rgblight.AckEventOnly)
	r.lock(t)
	locked := *r.sess.Locked
	dir := t.TempDir()
	pre := [][2]interface{}{
		{[3]bool{true, true, true}, [3]int{50, 50, 50}},
		{[3]bool{false, false, false}, [3]int{59, 96, 38}},
		{[3]bool{true, false, false}, [3]int{12, 13, 14}},
	}
	for i := 0; i < 3; i++ {
		on0, v0 := pre[i][0].([3]bool), pre[i][1].([3]int)
		r.light.SetLights(on0, v0)
		r.mu.Lock()
		r.lit = nil
		r.mu.Unlock()
		sw := &rgblight.ChannelSwitch{C: r.cl, Values: [3]int{59, 96, 38}}
		if _, err := sw.Snapshot(); err != nil {
			t.Fatal(err)
		}
		t0 := time.Now()
		name := []string{"Roll_0001", "Roll_0002", "Roll_0003"}[i]
		sh, err := r.sess.CaptureTrichrome(context.Background(), dir, []int{0, 2}, 0, sw, func(time.Time) string { return name })
		if err != nil {
			t.Fatalf("scan %d: %v", i+1, err)
		}
		if sh.Done != 3 || strings.Join(r.lit, ",") != "R,R,G,G,B,B" {
			t.Fatalf("scan %d: done %d lit %v", i+1, sh.Done, r.lit)
		}
		if on, v := r.light.Lights(); on != on0 || v != v0 {
			t.Fatalf("scan %d: light after %v %v, want %v %v", i+1, on, v, on0, v0)
		}
		if r.sess.Locked == nil || *r.sess.Locked != locked {
			t.Fatalf("scan %d: frame 0 lock lost", i+1)
		}
		t.Logf("scan %d: %v", i+1, time.Since(t0))
	}
	if n := countCmd(r.light, rgblight.CmdGetState); n != 3 {
		t.Fatalf("20 sent %d times, want 3 (one pre-scan snapshot per scan)", n)
	}
}

func countCmd(d *rgblight.SimDevice, cmd byte) int {
	n := 0
	for _, b := range d.Received() {
		if b == cmd {
			n++
		}
	}
	return n
}
