package hdrctl

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"basetool/camera"
	"basetool/hdrscan"
	"basetool/hikcam"
	"basetool/rgblight"
)

// gateHik is recHik that delivers n frames and then blocks in Grab until
// the gate opens, so the Focus loop stops after exactly n frames.
type gateHik struct {
	*recHik
	n       int32
	grabs   atomic.Int32
	waiting atomic.Bool
	gate    chan struct{}
}

func newGateHik(n int) *gateHik {
	return &gateHik{recHik: newRecHik(rgblight.NewSimDevice()), n: int32(n), gate: make(chan struct{})}
}

func (g *gateHik) Grab(d time.Duration) (*hikcam.Frame, error) {
	if g.grabs.Add(1) > g.n {
		g.waiting.Store(true)
		<-g.gate
		return nil, hikcam.ErrNotGrab
	}
	return g.recHik.Grab(d)
}

// focusRef is what the Windows code path leaves behind.
type focusRef struct {
	ae       hdrscan.LiveAE
	peak     hdrscan.PeakHold
	score    hdrscan.FocusScore
	samples  []float64
	visW     int
	visH     int
	scores   []float64
	meterBox [4]int
}

// windowsVisibleRectAt is FocusUI.visibleRectAt with the stored view size
// and pan centre as arguments.
func windowsVisibleRectAt(imgW, imgH, widgetW, widgetH, z, viewW, viewH int, fcx, fcy float64) (x0, y0, x1, y1 int) {
	if imgW <= 0 || imgH <= 0 {
		return 0, 0, 0, 0
	}
	if z == 0 {
		return 0, 0, imgW, imgH
	}
	if vwStored := viewW; vwStored > 8 {
		if vhStored := viewH; vhStored > 8 {
			widgetW, widgetH = vwStored, vhStored
		}
	}
	vw := widgetW / z
	vh := widgetH / z
	if vw < 16 {
		vw = 16
	}
	if vh < 16 {
		vh = 16
	}
	if vw > imgW {
		vw = imgW
	}
	if vh > imgH {
		vh = imgH
	}
	cx := int(fcx * float64(imgW))
	cy := int(fcy * float64(imgH))
	x0 = cx - vw/2
	y0 = cy - vh/2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x0+vw > imgW {
		x0 = imgW - vw
	}
	if y0+vh > imgH {
		y0 = imgH - vh
	}
	return x0, y0, x0 + vw, y0 + vh
}

// windowsFocusHik replays FocusUI.enter / loopBody / leave for a Hikrobot
// camera, statement by statement, on cam (no light connected): n frames
// at zoom z, view viewW x viewH, pan centre (cx, cy).
func windowsFocusHik(t *testing.T, cam hikcam.Device, savedHik hikcam.Settings, n, z, viewW, viewH int, cx, cy float64) focusRef {
	t.Helper()
	var r focusRef
	// enter
	r.peak.Reset()
	r.ae.Reset(hdrscan.ExposureState{}, hdrscan.LiveAutoExp())
	r.ae.Reset(hdrscan.ExposureState{ExposureUS: savedHik.ExposureUS, GainDB: savedHik.GainDB}, hdrscan.LiveAutoExp())
	// runSetup
	ae := hdrscan.ExposureState{ExposureUS: savedHik.ExposureUS, GainDB: savedHik.GainDB}
	ae, _ = hdrscan.AdjustExposure(nil, ae, hdrscan.LiveAutoExp())
	if _, err := applyLiveSettings(cam, savedHik, true); err != nil {
		t.Fatal(err)
	}
	_ = cam.SetExposureGain(ae.ExposureUS, ae.GainDB)
	cur := cam.Settings()
	r.ae.Reset(hdrscan.ExposureState{ExposureUS: cur.ExposureUS, GainDB: cur.GainDB}, hdrscan.LiveAutoExp())
	// startLoop + loopBody
	if err := cam.StartGrab(); err != nil {
		t.Fatal(err)
	}
	cfg := hdrscan.LiveAutoExp()
	for i := 0; i < n; i++ {
		fr, err := cam.Grab(hikcam.GrabTimeout(cam.Settings().ExposureUS))
		if err != nil {
			t.Fatal(err)
		}
		pix, w, h := fr.Pix, fr.W, fr.H
		mz := z
		if mz < 1 {
			mz = 1
		}
		x0, y0, x1, y1 := windowsVisibleRectAt(w, h, 640, 400, mz, viewW, viewH, cx, cy)
		r.meterBox = [4]int{x0, y0, x1, y1}
		samples := hdrscan.RegionSamples(pix, w, h, x0, y0, x1, y1, 2)
		crop, cw, ch := hdrscan.CropGray16ToFloat64(pix, w, h, x0, y0, x1, y1)
		score := hdrscan.MeasureFocus(crop, cw, ch)
		next, why, epoch, needApply := r.ae.Prepare(samples, cfg)
		if needApply {
			// applyAE, Hikrobot branch
			var applied hdrscan.ExposureState
			var changed bool
			cur := cam.Settings()
			if math.Abs(cur.ExposureUS-next.ExposureUS)/math.Max(cur.ExposureUS, 1) < 0.01 && math.Abs(cur.GainDB-next.GainDB) < 0.05 {
				applied = hdrscan.ExposureState{ExposureUS: cur.ExposureUS, GainDB: cur.GainDB}
			} else {
				if err := cam.SetExposureGain(next.ExposureUS, next.GainDB); err != nil {
					t.Fatal(err)
				}
				after := cam.Settings()
				changed = math.Abs(after.ExposureUS-cur.ExposureUS) > 0.5 || math.Abs(after.GainDB-cur.GainDB) > 0.005
				applied = hdrscan.ExposureState{ExposureUS: after.ExposureUS, GainDB: after.GainDB}
			}
			r.ae.Record(samples, cfg, why, epoch, applied, changed)
		}
		r.samples = samples
		r.visW, r.visH = cw, ch
		r.score = score
		r.peak.Update(score.Combined)
		r.scores = append(r.scores, score.Combined)
	}
	_ = cam.StopGrab()
	// leave: restoreExposure
	if err := cam.Apply(savedHik); err != nil {
		t.Fatal(err)
	}
	return r
}

type focusCase struct {
	n, zoomIdx int
	cx, cy     float64
}

// runMacFocusHik runs the Mac page on a gated simulator: enter, n frames,
// leave. It returns the controller state after frame n and the camera log.
func runMacFocusHik(t *testing.T, fc focusCase, viewW, viewH int) (focusRef, []string, hikcam.Settings, *Focus) {
	t.Helper()
	h, c, _ := newLightCtl(t)
	m, f := c.Mono, c.Focus
	h.pump(t, "focus ready", 5*time.Second, func() bool { return strings.Contains(f.LogText(), "Focus check ready") })
	g := newGateHik(fc.n)
	m.hik.setCam(g)
	saved := m.readSettings()
	f.ZoomIdx = fc.zoomIdx
	f.SetViewSize(viewW, viewH)
	f.Enter()
	if fc.cx != 0.5 || fc.cy != 0.5 {
		// a pan before the first frame (as a click at the zoom level would)
		f.mu.Lock()
		f.cx, f.cy = fc.cx, fc.cy
		f.mu.Unlock()
	}
	h.pump(t, "n frames", 20*time.Second, func() bool { return g.waiting.Load() })
	f.mu.Lock()
	got := focusRef{ae: f.ae, peak: f.peak, score: f.score, samples: f.samples, visW: f.visW, visH: f.visH}
	f.mu.Unlock()
	f.Leave()
	close(g.gate)
	h.pump(t, "left", 10*time.Second, func() bool { return strings.Contains(f.LogText(), "Left Focus check") })
	stopMonoWorker(m)
	return got, g.calls(), saved, f
}

// TestFocusGoldenHikMatchesWindows: the focus metric, the auto-exposure
// steps and every camera call of the Mac page equal the Windows code path
// on the same simulator frames, exactly.
func TestFocusGoldenHikMatchesWindows(t *testing.T) {
	parallelParent(t)
	const vw, vh = 800, 560
	for _, fc := range []focusCase{
		{n: 1, zoomIdx: 0, cx: 0.5, cy: 0.5},
		{n: 6, zoomIdx: 0, cx: 0.5, cy: 0.5},
		{n: 12, zoomIdx: 2, cx: 0.5, cy: 0.5},
		{n: 8, zoomIdx: 3, cx: 0.5, cy: 0.5},
		{n: 8, zoomIdx: 1, cx: 0.5, cy: 0.5},
		{n: 6, zoomIdx: 2, cx: 0.2, cy: 0.8},
		{n: 6, zoomIdx: 3, cx: 0.97, cy: 0.03},
	} {
		t.Run(fmt.Sprintf("n%d_zoom%d_pan%.2f_%.2f", fc.n, fc.zoomIdx, fc.cx, fc.cy), func(t *testing.T) {
			if forked(t) {
				return
			}
			got, macCalls, saved, f := runMacFocusHik(t, fc, vw, vh)
			ref := newRecHik(rgblight.NewSimDevice())
			want := windowsFocusHik(t, ref, saved, fc.n, zoomVals[fc.zoomIdx], vw, vh, fc.cx, fc.cy)
			if got.score != want.score {
				t.Fatalf("score\n mac %+v\n win %+v", got.score, want.score)
			}
			if got.peak != want.peak {
				t.Fatalf("peak mac %+v win %+v (scores %v)", got.peak, want.peak, want.scores)
			}
			if !reflect.DeepEqual(got.ae, want.ae) {
				t.Fatalf("auto exposure\n mac %+v\n win %+v", got.ae, want.ae)
			}
			if !reflect.DeepEqual(got.samples, want.samples) || got.visW != want.visW || got.visH != want.visH {
				t.Fatalf("meter region mac %dx%d win %dx%d", got.visW, got.visH, want.visW, want.visH)
			}
			if wc := ref.calls(); !reflect.DeepEqual(macCalls, wc) {
				t.Fatalf("camera calls differ\n mac %q\n win %q", macCalls, wc)
			}
			if want.score.Combined <= 0 || len(want.samples) == 0 {
				t.Fatalf("degenerate frame: %+v", want.score)
			}
			if !strings.Contains(f.LogText(), "Restored Hikrobot exposure: ") {
				t.Fatal(f.LogText())
			}
			t.Logf("n=%d zoom=%s: %d calls, meter %dx%d, combined %.6f, peak %.6f, AE %.0f us %.2f dB locked=%v",
				fc.n, zoomItems[fc.zoomIdx], len(macCalls), got.visW, got.visH, got.score.Combined, got.peak.Peak,
				got.ae.State.ExposureUS, got.ae.State.GainDB, got.ae.Locked)
		})
	}
}

// grainJPEG is a textured X-T3 live-view frame so the metric is non-zero.
func grainJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(12345)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*1664525 + 1013904223
			v := uint8(60 + (seed>>24)%40)
			if (x/16+y/16)%2 == 0 {
				v += 90
			}
			img.Set(x, y, color.RGBA{v, v + 5, v - 10, 255})
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	return b.Bytes()
}

// focusFuji is an X-T3 with a recorded exposure and a gate after n frames.
type focusFuji struct {
	mockCam
	mu      sync.Mutex
	log     []string
	n       int32
	grabs   atomic.Int32
	waiting atomic.Bool
	gate    chan struct{}
}

func newFocusFuji(n int) *focusFuji {
	f := &focusFuji{n: int32(n), gate: make(chan struct{})}
	f.exp = camera.Exposure{ISO: 160, Shutter: 3906}
	f.jpg = grainJPEG(320, 214)
	return f
}

func (f *focusFuji) Exposure() (camera.Exposure, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exp, nil
}
func (f *focusFuji) SetExposure(e camera.Exposure) (camera.Exposure, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, "set "+e.String())
	f.exp = e
	return e, nil
}
func (f *focusFuji) LiveView() ([]byte, error) {
	if f.grabs.Add(1) > f.n {
		f.waiting.Store(true)
		<-f.gate
		return nil, errors.New("closed")
	}
	f.mu.Lock()
	f.log = append(f.log, "live view")
	f.mu.Unlock()
	return f.jpg, nil
}

// TestFocusGoldenFujiMatchesWindows: the X-T3 live-view path (JPEG to
// 16-bit gray, metric, shutter-only auto-exposure, restore) equals the
// Windows one on the same frames.
func TestFocusGoldenFujiMatchesWindows(t *testing.T) {
	const n = 10
	h, c, _ := newLightCtl(t)
	f := c.Focus
	cam := newFocusFuji(n)
	c.dev = cam
	f.ZoomIdx = 2
	f.SetViewSize(640, 400)
	f.Enter()
	h.pump(t, "frames", 20*time.Second, func() bool { return cam.waiting.Load() })
	f.mu.Lock()
	got := focusRef{ae: f.ae, peak: f.peak, score: f.score, samples: f.samples, visW: f.visW, visH: f.visH}
	f.mu.Unlock()
	if f.SrcText != "Camera: Mock Cam" {
		t.Fatal(f.SrcText)
	}
	f.Leave()
	close(cam.gate)
	h.pump(t, "left", 10*time.Second, func() bool { return strings.Contains(f.LogText(), "Left Focus check") })
	cam.mu.Lock()
	macLog := append([]string(nil), cam.log...)
	cam.mu.Unlock()

	// Windows: enter (no Hikrobot), runSetup Fuji branch, loopBody, restore.
	ref := newFocusFuji(n + 1)
	var r focusRef
	r.peak.Reset()
	r.ae.Reset(hdrscan.ExposureState{}, hdrscan.LiveAutoExp())
	savedFuji, _ := ref.Exposure()
	r.ae.Reset(hdrscan.ExposureState{ExposureUS: float64(savedFuji.Shutter), GainDB: 0}, hdrscan.LiveAutoExp())
	cfg := hdrscan.LiveAutoExp()
	for i := 0; i < n; i++ {
		jpg, _ := ref.LiveView()
		im, err := jpeg.Decode(bytes.NewReader(jpg))
		if err != nil {
			t.Fatal(err)
		}
		g := imageToGray16(im)
		b := g.Bounds()
		pix, w, hh := gray16Pix(g), b.Dx(), b.Dy()
		x0, y0, x1, y1 := windowsVisibleRectAt(w, hh, 640, 400, 2, 640, 400, 0.5, 0.5)
		samples := hdrscan.RegionSamples(pix, w, hh, x0, y0, x1, y1, 2)
		crop, cw, ch := hdrscan.CropGray16ToFloat64(pix, w, hh, x0, y0, x1, y1)
		score := hdrscan.MeasureFocus(crop, cw, ch)
		next, why, epoch, needApply := r.ae.Prepare(samples, cfg)
		if needApply {
			us := uint32(math.Max(1, next.ExposureUS))
			i := camera.ShutterIndex(us)
			nx := camera.Exposure{ISO: 160, Shutter: camera.Shutters[i].Value}
			nx.ISO = savedFuji.ISO
			cur, _ := ref.Exposure()
			applied, changed := cur, false
			if !(cur.Shutter == nx.Shutter && cur.ISO == nx.ISO) {
				got, _ := ref.SetExposure(nx)
				applied = got
				changed = got.Shutter != cur.Shutter || got.ISO != cur.ISO
			}
			r.ae.Record(samples, cfg, why, epoch, hdrscan.ExposureState{ExposureUS: float64(applied.Shutter), GainDB: 0}, changed)
		}
		r.samples, r.visW, r.visH, r.score = samples, cw, ch, score
		r.peak.Update(score.Combined)
	}
	_, _ = ref.SetExposure(savedFuji)

	if got.score != r.score || got.peak != r.peak || !reflect.DeepEqual(got.ae, r.ae) || !reflect.DeepEqual(got.samples, r.samples) {
		t.Fatalf("X-T3 path differs\n mac %+v %+v %+v\n win %+v %+v %+v", got.score, got.peak, got.ae, r.score, r.peak, r.ae)
	}
	if !reflect.DeepEqual(macLog, ref.log) {
		t.Fatalf("camera calls\n mac %q\n win %q", macLog, ref.log)
	}
	if r.score.Combined <= 0 {
		t.Fatal("degenerate")
	}
	if !strings.Contains(f.LogText(), "Restored camera exposure: "+savedFuji.String()) {
		t.Fatal(f.LogText())
	}
	t.Logf("X-T3: %d calls, meter %dx%d, combined %.6f, AE shutter %.0f us", len(macLog), got.visW, got.visH, got.score.Combined, got.ae.State.ExposureUS)
}

// TestFocusLabelsAndNoCamera: the start texts, "none connected" and the
// No camera log line, the meter clamp and Reset peak.
func TestFocusLabelsAndNoCamera(t *testing.T) {
	h, c, _ := newLightCtl(t)
	f := c.Focus
	h.pump(t, "ready", 5*time.Second, func() bool { return strings.Contains(f.LogText(), "Focus check ready") })
	if !strings.Contains(f.LogText(), " Focus check ready. Green is the focus baseline for trichrome; white is for white-light scans.") {
		t.Fatal(f.LogText())
	}
	if f.SrcText != "Camera: none connected" || f.SrcTip != "Open the Hikrobot camera on Mono trichrome, or the Fuji body on Camera." {
		t.Fatalf("%q %q", f.SrcText, f.SrcTip)
	}
	if f.LightText != "Light: not connected (wanted: green only)" || f.LightOK {
		t.Fatal(f.LightText)
	}
	if f.ExpText != "Auto exposure: 0 us, 0.00 dB\n" || f.ExpTip != "Auto exposure: 0 us, 0.00 dB" {
		t.Fatalf("%q", f.ExpText)
	}
	if f.SharpText != "Sharpness: 0.0000\nLap 0.0000, grain 0.0000" || f.PeakText != "Peak hold: -" || f.Meter != 0 {
		t.Fatalf("%q %q %d", f.SharpText, f.PeakText, f.Meter)
	}
	f.Enter()
	h.pump(t, "no camera", 30*time.Second, func() bool { return strings.Contains(f.LogText(), "No camera to preview.") })
	f.Leave()
	h.pump(t, "left", 30*time.Second, func() bool {
		return strings.Contains(f.LogText(), "Left Focus check: previous capture exposure restored.")
	})

	// meter: Combined*20000 clamped to 0..1000; peak hold text
	for _, x := range []struct {
		v    float64
		want int
	}{{-1, 0}, {0.0123, 246}, {0.05, 1000}, {3, 1000}} {
		f.mu.Lock()
		f.score = hdrscan.FocusScore{Combined: x.v, Laplacian: 1, Grain: 2}
		f.peak.Update(x.v)
		f.ae.Why = "settling"
		f.mu.Unlock()
		f.updateLabels()
		if f.Meter != x.want {
			t.Fatalf("meter(%v) = %d, want %d", x.v, f.Meter, x.want)
		}
	}
	if f.PeakText != "Peak hold: 3.0000" || !strings.HasSuffix(f.ExpText, "\n(settling)") || f.ExpTip != "Auto exposure: 32 us, 0.00 dB (settling)" {
		t.Fatalf("%q %q", f.PeakText, f.ExpText)
	}
	if f.SharpTip != "Sharpness: 3.0000 (Lap 1.0000, grain 2.0000)" {
		t.Fatal(f.SharpTip)
	}
	f.ResetPeak()
	if f.PeakText != "Peak hold: -" {
		t.Fatal(f.PeakText)
	}
}

// TestFocusZoomClickPan: Fit click zooms to 200 % around the clicked
// point; a zoomed click maps through the visible rectangle; drag pans and
// clamps; zoom changes and pans unlock auto-exposure.
func TestFocusZoomClickPan(t *testing.T) {
	h, c, _ := newLightCtl(t)
	f := c.Focus
	_ = h
	if got := ZoomItems(); !reflect.DeepEqual(got, []string{"Fit", "100 %", "200 %", "400 %"}) {
		t.Fatal(got)
	}
	f.SrcW, f.SrcH = 960, 654
	vw, vh := 800, 545 // letterbox fills exactly: 960x654 -> 800x545
	ir := image.Rect(0, 0, 800, 545)
	f.mu.Lock()
	f.ae.Locked = true
	f.mu.Unlock()
	f.Click(200, 409, vw, vh, ir) // (0.25, 0.75) of the frame
	if f.ZoomIdx != 2 || f.ZoomFactor() != 2 {
		t.Fatalf("fit click must zoom to 200 %%: %d", f.ZoomIdx)
	}
	f.mu.Lock()
	cx, cy, locked := f.cx, f.cy, f.ae.Locked
	f.mu.Unlock()
	if cx != 0.25 || math.Abs(cy-409.0/545) > 1e-12 || locked {
		t.Fatalf("centre %v %v locked=%v", cx, cy, locked)
	}
	x0, y0, x1, y1 := f.visibleRect(960, 654, 640, 400)
	wx0, wy0, wx1, wy1 := windowsVisibleRectAt(960, 654, 640, 400, 2, vw, vh, cx, cy)
	if [4]int{x0, y0, x1, y1} != [4]int{wx0, wy0, wx1, wy1} || x1-x0 != 400 || y1-y0 != 272 {
		t.Fatalf("visible %v %v %v %v", x0, y0, x1, y1)
	}
	f.Up()
	// a zoomed click: view (0,0) is the visible rectangle's corner
	f.Click(0, 0, vw, vh, ir)
	f.mu.Lock()
	cx, cy = f.cx, f.cy
	f.mu.Unlock()
	if cx != float64(x0)/960 || cy != float64(y0)/654 {
		t.Fatalf("zoomed click %v %v, want %v %v", cx, cy, float64(x0)/960, float64(y0)/654)
	}
	// drag 100 px left at 200 %: the image follows, the centre moves 50
	// source px right
	f.Move(-100, 0)
	f.mu.Lock()
	cx2 := f.cx
	f.mu.Unlock()
	if math.Abs(cx2-(cx+50.0/960)) > 1e-12 {
		t.Fatalf("pan %v -> %v", cx, cx2)
	}
	f.Move(100000, -100000) // clamps at 0 / 1
	f.mu.Lock()
	cx2, cy2 := f.cx, f.cy
	f.mu.Unlock()
	if cx2 != 0 || cy2 != 1 {
		t.Fatalf("clamp %v %v", cx2, cy2)
	}
	f.Up()
	f.Move(10, 10) // no drag after mouse-up
	f.mu.Lock()
	if f.cx != 0 || f.cy != 1 {
		t.Fatal("moved without a drag")
	}
	f.ae.Locked = true
	f.mu.Unlock()
	f.ZoomIdx = 0
	f.ZoomChanged()
	f.mu.Lock()
	locked = f.ae.Locked
	f.mu.Unlock()
	if locked || f.ZoomFactor() != 0 {
		t.Fatal("zoom change must unlock AE")
	}
	if x0, y0, x1, y1 := f.visibleRect(960, 654, 640, 400); [4]int{x0, y0, x1, y1} != [4]int{0, 0, 960, 654} {
		t.Fatal("Fit shows the full frame")
	}
	// the meter uses at least 100 %
	if x0, _, x1, _ := f.meterRect(960, 654); x1-x0 != 800 {
		t.Fatalf("meter width %d", x1-x0)
	}
}

// TestFocusDisplayAndRefresh: frames reach the view through Host.Post, a
// zoom rebuilds the last frame as a 1:1 crop off the UI goroutine.
func TestFocusDisplayAndRefresh(t *testing.T) {
	h, c, _ := newLightCtl(t)
	m, f := c.Mono, c.Focus
	g := newGateHik(3)
	m.hik.setCam(g)
	f.SetViewSize(640, 400)
	f.Enter()
	h.pump(t, "frames", 10*time.Second, func() bool { return g.waiting.Load() && f.Frame != nil })
	h.pump(t, "labels", 5*time.Second, func() bool { return strings.HasPrefix(f.SharpText, "Sharpness: 0.0") && f.Meter > 0 })
	if f.SrcText != "Camera: "+hikcam.OpenSim().Info().Label() || !f.BmpFit || f.SrcW != 960 || f.SrcH != 654 {
		t.Fatalf("%q fit=%v %dx%d", f.SrcText, f.BmpFit, f.SrcW, f.SrcH)
	}
	if !strings.Contains(f.LogText(), "Entered Focus check: exposure adjusts automatically for the visible region; capture exposure is restored when you leave.") {
		t.Fatal(f.LogText())
	}
	seq := f.FrameSeq
	f.ZoomIdx = 3
	f.ZoomChanged()
	h.pump(t, "zoomed crop", 5*time.Second, func() bool { return f.FrameSeq > seq && !f.BmpFit })
	b := f.Frame.Bounds()
	if b.Dx() != 160 || b.Dy() != 100 || f.BmpX != 480-80 || f.BmpY != 327-50 {
		t.Fatalf("400 %% crop %v at %d,%d", b, f.BmpX, f.BmpY)
	}
	f.Leave()
	close(g.gate)
	h.pump(t, "left", 10*time.Second, func() bool { return strings.Contains(f.LogText(), "Left Focus check") })
	if !strings.Contains(f.LogText(), "Restored Hikrobot exposure: ") {
		t.Fatal(f.LogText())
	}
	stopMonoWorker(m)
}

// TestFocusLightGreenWhiteRestore: entering switches the light to green
// only at the scan values, White lights all three, leaving restores the
// light as it was.
func TestFocusLightGreenWhiteRestore(t *testing.T) {
	h, c, l := newLightCtl(t)
	connectSim(t, h, l)
	m, f := c.Mono, c.Focus
	if _, err := l.cl.SetAllConfirmed([3]bool{true, true, true}, [3]int{30, 40, 50}); err != nil {
		t.Fatal(err)
	}
	f.Refresh()
	if !f.LightOK || f.LightText != "Light: green only" {
		t.Fatal(f.LightText)
	}
	g := newGateHik(1 << 30)
	m.hik.setCam(g)
	f.Enter()
	h.pump(t, "green", 10*time.Second, func() bool { return strings.Contains(f.LogText(), "Light: green only (trichrome focus baseline).") })
	on, val := l.sim.Lights()
	if on != [3]bool{false, true, false} || val[1] != 40 {
		t.Fatalf("green only: %v %v", on, val)
	}
	f.SetLight(1)
	if f.LightText != "Light: white" {
		t.Fatal(f.LightText)
	}
	h.pump(t, "white", 10*time.Second, func() bool {
		return strings.Contains(f.LogText(), "Light: white (all channels) for white-light scanning.")
	})
	if on, val = l.sim.Lights(); on != [3]bool{true, true, true} || val != [3]int{30, 40, 50} {
		t.Fatalf("white: %v %v", on, val)
	}
	f.SetLight(0)
	h.pump(t, "green again", 10*time.Second, func() bool {
		on, _ := l.sim.Lights()
		return on == [3]bool{false, true, false}
	})
	h.pump(t, "light page shows it", 5*time.Second, func() bool { return strings.HasPrefix(l.StateText, "Light reports: ") })
	f.Leave()
	close(g.gate)
	h.pump(t, "left", 10*time.Second, func() bool { return strings.Contains(f.LogText(), "Left Focus check") })
	if on, val = l.sim.Lights(); on != [3]bool{true, true, true} || val != [3]int{30, 40, 50} {
		t.Fatalf("restored: %v %v", on, val)
	}
	stopMonoWorker(m)
}
