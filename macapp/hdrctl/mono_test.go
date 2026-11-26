package hdrctl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"basetool/hdrscan"
	"basetool/hikcam"
	"basetool/monocap"
	"basetool/rgblight"
)

// recHik is the Hikrobot simulator (hikcam.OpenSim) with every call
// recorded together with the simulated RGB light's state at that moment.
// reset swaps in a fresh simulator so two runs see the same frames.
type recHik struct {
	mu    sync.Mutex
	s     *hikcam.Sim
	light *rgblight.SimDevice
	log   []string
}

func newRecHik(light *rgblight.SimDevice) *recHik {
	return &recHik{s: hikcam.OpenSim(), light: light}
}

func (r *recHik) reset() {
	r.mu.Lock()
	r.s, r.log = hikcam.OpenSim(), nil
	r.mu.Unlock()
}

func (r *recHik) sim() *hikcam.Sim {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.s
}

func (r *recHik) rec(f string, a ...any) {
	on, v := r.light.Lights()
	r.mu.Lock()
	r.log = append(r.log, fmt.Sprintf(f, a...)+fmt.Sprintf(" | light on=%v value=%v", on, v))
	r.mu.Unlock()
}

func (r *recHik) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.log...)
}

func (r *recHik) Info() hikcam.Info         { return r.sim().Info() }
func (r *recHik) Settings() hikcam.Settings { return r.sim().Settings() }
func (r *recHik) Apply(st hikcam.Settings) error {
	r.rec("apply %+v", st)
	return r.sim().Apply(st)
}
func (r *recHik) StartGrab() error { r.rec("start grab"); return r.sim().StartGrab() }
func (r *recHik) StopGrab() error  { r.rec("stop grab"); return r.sim().StopGrab() }
func (r *recHik) Grab(d time.Duration) (*hikcam.Frame, error) {
	f, err := r.sim().Grab(d)
	if err == nil {
		r.rec("grab %.0f us %.2f dB %dx%d", f.ExposureUS, f.GainDB, f.W, f.H)
	}
	return f, err
}
func (r *recHik) SetExposureGain(us, db float64) error {
	r.rec("exposure %.0f us %.2f dB", us, db)
	return r.sim().SetExposureGain(us, db)
}
func (r *recHik) Close() error { return r.sim().Close() }

// stopMonoWorker waits until the Mono page's preview loop has stopped.
func stopMonoWorker(m *Mono) {
	m.StopPreview()
	done := make(chan struct{})
	m.worker().Do(func() { m.worker().StopSync(); close(done) })
	<-done
}

func TestMonoStartFieldsAndOptions(t *testing.T) {
	h, c, _ := newLightCtl(t)
	m := c.Mono
	h.pump(t, "device list", 10*time.Second, func() bool {
		return strings.Contains(m.LogText(), "Mono trichrome page ready") && m.hik.getSDKMsg() != ""
	})
	if m.PresetIdx != 1 || m.FmtIdx != hikcam.PixelFormatIndex(hikcam.PixelMono12) || m.TrigIdx != 1 || !m.Full || m.Gamma {
		t.Fatalf("best scan quality not filled: %+v", m)
	}
	if m.GainText != "0.00" || m.BlackText != "32" || m.WText != "6016" || m.HText != "4096" {
		t.Fatalf("fields %q %q %q %q", m.GainText, m.BlackText, m.WText, m.HText)
	}
	if m.DevItems[0] != hikcam.OpenSim().Info().Label() {
		t.Fatal(m.DevItems)
	}
	b := m.Buttons()
	if b.ConnText != "Connect" || !b.Conn || !b.DevList || b.Apply || b.Capture || b.Stop {
		t.Fatalf("idle buttons %+v", b)
	}
	if m.WhyText != "Capture disabled: camera not connected" || !strings.HasPrefix(m.SDKText, "SDK: ") {
		t.Fatalf("%q %q", m.WhyText, m.SDKText)
	}
	// NumberEdit ranges and decimals
	m.GainText, m.BlackText, m.WText = "30", "-5", "abc"
	m.FieldEdited()
	if m.GainText != "24.00" || m.BlackText != "0" || m.WText != "6016" {
		t.Fatalf("clamped %q %q %q", m.GainText, m.BlackText, m.WText)
	}
	// the Exposure field is the shared Hikrobot exposure
	m.ExpText = "25000 us"
	m.ExposureEdited()
	if m.ExpText != "25000" || c.sharedHikExposure() != 25000 || c.Cfg.HikExposureUS != 25000 {
		t.Fatalf("shared exposure %q %v", m.ExpText, c.sharedHikExposure())
	}
	if got := c.Light.monoScanExposureUS(); got != 25000 {
		t.Fatalf("Light leveling exposure %v, want the Mono field", got)
	}
	m.ExpText = "10" // clamped to the CS240 minimum, as readSettings does on Windows
	if got := c.Light.monoScanExposureUS(); got != hikcam.CS240ExpMinUS {
		t.Fatalf("clamped %v", got)
	}
	m.ExpText = "25000"
	c.Mono = nil // no Mono page: Best scan quality, as before
	if got := c.Light.monoScanExposureUS(); got != hikcam.BestScanQuality().ExposureUS {
		t.Fatalf("fallback %v", got)
	}
	c.Mono = m
	// mode / bracket / frames per exposure are saved
	m.CapMode, m.BrIdx, m.FPEText = 0, 0, "99"
	m.OptionsChanged()
	if m.CapText != "Capture mono trichrome" || m.FPEText != strconv.Itoa(framesPerExpMax) {
		t.Fatalf("%q %q", m.CapText, m.FPEText)
	}
	m.MeterEach = true
	m.MeterEachChanged()
	c2 := New(newHost())
	if c2.Cfg.HikHDR || c2.Cfg.HikFramesPerExp != framesPerExpMax || !c2.Cfg.HikMeterEach || c2.Cfg.HikExposureUS != 25000 {
		t.Fatalf("not saved: %+v", c2.Cfg)
	}
	// Custom preset keeps the fields; Best scan quality refills them
	m.PresetIdx, m.GainText = 0, "3.00"
	m.PresetChanged()
	if m.GainText != "3.00" {
		t.Fatal("custom preset changed the fields")
	}
	m.PresetIdx = 1
	m.PresetChanged()
	if m.GainText != "0.00" || m.ExpText != "25000" {
		t.Fatalf("best scan %q %q", m.GainText, m.ExpText)
	}
}

// TestMonoSDKMissing: without MvCameraControl.framework the page lists the
// simulator only, says why, and connecting the simulator still works.
func TestMonoSDKMissing(t *testing.T) {
	h, c, _ := newLightCtl(t)
	m := c.Mono
	const msg = "Hikrobot MVS SDK not found (MvCameraControl.framework is not installed)"
	opened := 0
	// injected on the Hikrobot worker, behind Start's first refresh
	injected := make(chan struct{})
	m.worker().Do(func() {
		m.hik.enumerate = func() hikcam.ListResult { return hikcam.ListResult{SDK: msg, Err: hikcam.ErrNoSDK} }
		m.hik.openReal = func(hikcam.Info) (hikcam.Device, error) { opened++; return nil, hikcam.ErrNoSDK }
		close(injected)
	})
	<-injected
	m.RefreshDevices()
	h.pump(t, "refresh", 5*time.Second, func() bool { return strings.Contains(m.LogText(), msg) })
	m.Buttons()
	want := hikcam.ListResult{SDK: msg, Err: hikcam.ErrNoSDK}
	if len(m.DevItems) != 1 || m.SDKText != "SDK: "+msg || want.Message() != msg {
		t.Fatalf("items %q sdk %q", m.DevItems, m.SDKText)
	}
	// a stale index (the camera vanished) is reset to the simulator
	m.DevIdx = 3
	m.RefreshDevices()
	h.pump(t, "refresh 2", 5*time.Second, func() bool { return m.DevIdx == 0 })
	// Light leveling with the monochrome camera cannot open a real one
	if _, err := m.hik.ensureHik(false); !errors.Is(err, hikcam.ErrNoSDK) {
		t.Fatalf("ensureHik: %v", err)
	}
	if opened != 0 {
		t.Fatal("tried to open a camera without the SDK")
	}
	// the simulator connects, previews and disconnects
	m.ToggleConnect()
	h.pump(t, "sim connect", 30*time.Second, func() bool { return m.Connected() && !m.ConnBusy() })
	h.pump(t, "preview", 30*time.Second, func() bool { return m.FrameSeq > 1 })
	if !strings.HasPrefix(m.ViewWhat, "Live  ") || m.Frame == nil {
		t.Fatal(m.ViewWhat)
	}
	b := m.Buttons()
	if b.ConnText != "Disconnect" || !b.Apply || b.DevList || !b.Capture || m.WhyText == "" {
		t.Fatalf("sim buttons %+v %q", b, m.WhyText)
	}
	// the Light page's preview / leveling of the same camera stops the
	// Mono preview first (stopOther runs on the leveling goroutine)
	done := make(chan struct{})
	go func() { m.hik.stopOther(); close(done) }()
	<-done
	if m.PreviewRunning() {
		t.Fatal("Mono preview still running after stopOther")
	}
	m.ToggleConnect()
	h.pump(t, "disconnect", 30*time.Second, func() bool { return !m.Connected() && !m.ConnBusy() && !m.PreviewRunning() })
	if len(h.messages) != 0 {
		t.Fatal(h.messages)
	}
}

// windowsMonoScan is MonoUI.capture (../monopage.go) transcribed onto the
// rig's camera and light, run synchronously: the reference.
func windowsMonoScan(t *testing.T, c *Ctl, cam hikcam.Device) string {
	t.Helper()
	m, lu := c.Mono, c.Light
	dir, err := c.rollDir()
	if err != nil {
		t.Fatal(err)
	}
	vals := lu.cl.CachedState().Value
	settle := 300 * time.Millisecond
	if ms, perr := strconv.Atoi(strings.TrimSpace(lu.SettleText)); perr == nil && ms >= 0 && ms <= 5000 {
		settle = time.Duration(ms) * time.Millisecond
	}
	correct := m.CA
	meterEach := c.hikMeterEach()
	baseUS := c.sharedHikExposure()
	if !meterEach {
		m.setExpField(baseUS)
		m.setGainField(0)
	}
	evs := m.captureEVs()
	st := m.readSettings()
	sw := &rgblight.ChannelSwitch{C: lu.cl, Values: vals}
	opt := monocap.Options{FramesPerExposure: m.framesPerExposure(), Noise: hdrscan.DefaultMonoNoise()}
	box := c.ROI
	roll, last := c.Cfg.Roll, c.Cfg.Counter
	scanMeta := c.scanMetadata()
	levelMeta := c.metadataLeveling()
	if err := cam.Apply(st); err != nil {
		t.Fatal(err)
	}
	if _, e := sw.Snapshot(); e != nil {
		t.Fatal(e)
	}
	if err := cam.StartGrab(); err != nil {
		t.Fatal(err)
	}
	opt.Cam = cam
	opt.Light = sw
	opt.Settle = settle
	opt.SettleTimeout = max(monocap.DefaultSettleTimeout, 4*settle)
	opt.BaseUS = baseUS
	opt.MeterEach = meterEach
	opt.MeterBox = box
	opt.EVs = evs
	opt.Ctx = context.Background()
	cr, err := monocap.Capture(opt)
	if err != nil {
		t.Fatal(err)
	}
	planes := cr.Planes
	base, num := hdrscan.NextShot(dir, roll, time.Now(), last)
	c.Cfg.Counter = num
	out := filepath.Join(dir, base+"_RGB.tiff")
	meta := scanMeta
	meta.Mode = "mono-trichrome"
	meta.Encoding = "linear"
	meta.LightModel = "CuteNewDesign RGB Light Air"
	meta.LightRGB = &vals
	meta = meta.WithLeveling(levelMeta, vals)
	meta.CameraProfile = cam.Info().Model
	meta.CameraMatrix = nil
	meta = c.withHikExposureMeta(meta, baseUS, meterEach)
	mc := cr.Mono
	meta.Mono = &mc
	info := cam.Info()
	r, err := hdrscan.ProcessMonoTrichrome(planes[0], planes[1], planes[2], hdrscan.MonoTriOptions{
		OutPath: out, LZW: true, CorrectCA: correct, CheckMovement: true,
		Software: appVersion + monoHDRTag(evs), Make: info.Vendor, Model: info.Model, Metadata: meta,
	})
	// deferred in Windows: StopGrab runs before Restore
	_ = cam.StopGrab()
	_ = sw.Restore()
	if err != nil {
		t.Fatal(err)
	}
	return r.Out
}

// TestMonoGoldenMatchesWindows captures mono trichrome with the macOS page
// and with the literal Windows sequence on fresh copies of the Hikrobot
// simulator and the simulated RGB light, and compares every camera call
// (with the light state at that moment), the light commands, the output
// name, the pixels (exact) and the TIFF bytes (only the creation time
// masked).
func TestMonoGoldenMatchesWindows(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mode   int
		br     int
		fpe    string
		meterE bool
	}{{"single", 0, 0, "1", false}, {"hdr-default", 1, monoBracketDefault, "1", false}, {"hdr-2frames", 1, 0, "2", false}, {"metered", 1, 0, "1", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h, c, l := newLightCtl(t)
			connectSim(t, h, l)
			m := c.Mono
			h.pump(t, "mono ready", 10*time.Second, func() bool { return m.hik.getSDKMsg() != "" })
			c.Cfg.OutDir, c.Cfg.Roll = t.TempDir(), "golden"
			l.SettleText = "20"
			m.CapMode, m.BrIdx, m.FPEText, m.MeterEach = tc.mode, tc.br, tc.fpe, tc.meterE
			m.OptionsChanged()
			m.MeterEachChanged()
			m.ExpText = "12000"
			m.ExposureEdited()
			rec := newRecHik(l.sim)
			m.hik.setCam(rec)
			if b := m.Buttons(); !b.Capture {
				t.Fatalf("not ready: %s", m.WhyText)
			}
			lb := len(l.sim.Received())
			m.Capture()
			if !m.Running() || !m.Buttons().Stop || m.Buttons().Capture {
				t.Fatal("not running")
			}
			h.pump(t, "capture", 180*time.Second, func() bool { return !m.Running() })
			if !strings.Contains(m.RepText, "Saved ") {
				t.Fatalf("result: %s\n%s", m.RepText, m.LogText())
			}
			macOut := strings.Fields(strings.SplitN(m.RepText[strings.Index(m.RepText, "Saved "):], "  (", 2)[0])[1]
			stopMonoWorker(m)
			macCalls := rec.calls()
			macLight := append([]byte(nil), l.sim.Received()[lb:]...)
			macLight = macLight[:len(macLight)-countTrailingReads(macLight)]
			if c.Cfg.Counter != 1 {
				t.Fatalf("counter %d", c.Cfg.Counter)
			}

			rec.reset()
			lb = len(l.sim.Received())
			winOut := windowsMonoScan(t, c, rec)
			winCalls := rec.calls()
			winLight := l.sim.Received()[lb:]
			winLight = winLight[:len(winLight)-countTrailingReads(winLight)]

			// the page's preview start after the capture is not part of it
			macCalls = trimAfterLastStop(macCalls)
			if !reflect.DeepEqual(macCalls, winCalls) {
				t.Fatalf("camera calls differ (%d / %d):\nmac %q\nwin %q", len(macCalls), len(winCalls), macCalls, winCalls)
			}
			if !bytes.Equal(macLight, winLight) {
				t.Fatalf("light commands differ:\nmac %q\nwin %q", cmdNames(macLight), cmdNames(winLight))
			}
			if maskShot(filepath.Base(macOut)) != maskShot(filepath.Base(winOut)) {
				t.Fatalf("names %s / %s", macOut, winOut)
			}
			pa, err := hdrscan.Load(macOut)
			if err != nil {
				t.Fatal(err)
			}
			pb, err := hdrscan.Load(winOut)
			if err != nil {
				t.Fatal(err)
			}
			if pa.W != pb.W || pa.H != pb.H || !reflect.DeepEqual(pa.Pix, pb.Pix) {
				t.Fatalf("pixels differ: %dx%d / %dx%d", pa.W, pa.H, pb.W, pb.H)
			}
			a, _ := os.ReadFile(macOut)
			b, _ := os.ReadFile(winOut)
			a, b = maskCreated(t, a), maskCreated(t, b)
			if !bytes.Equal(a, b) {
				ia := bytes.Index(a, []byte(`{"schema"`))
				ib := bytes.Index(b, []byte(`{"schema"`))
				t.Fatalf("TIFF bytes differ:\nmac %s\nwin %s", a[max(0, ia):min(len(a), ia+1500)], b[max(0, ib):min(len(b), ib+1500)])
			}
			grabs := 0
			for _, s := range macCalls {
				if strings.HasPrefix(s, "grab ") {
					grabs++
				}
			}
			t.Logf("%d camera calls (%d grabs), light %q, %dx%d pixels and %d TIFF bytes identical", len(macCalls), grabs, cmdNames(macLight), pa.W, pa.H, len(a))
		})
	}
}

// countTrailingReads: state reads the page issues after the scan (status
// refresh) are not part of the capture sequence.
func countTrailingReads(b []byte) int {
	n := 0
	for i := len(b) - 1; i >= 0 && b[i] == rgblight.CmdGetState; i-- {
		n++
	}
	return n
}

func trimAfterLastStop(calls []string) []string {
	for i := len(calls) - 1; i >= 0; i-- {
		if strings.HasPrefix(calls[i], "stop grab") {
			return calls[:i+1]
		}
	}
	return calls
}
