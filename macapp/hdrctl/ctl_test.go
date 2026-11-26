package hdrctl

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"basetool/camera"
)

// testHost runs posted functions when the test pumps them, like the Gio
// window goroutine does between frames.
type testHost struct {
	q        chan func()
	scene    string
	messages []string
	confirm  bool
}

func newHost() *testHost { return &testHost{q: make(chan func(), 1024), confirm: true} }

func (h *testHost) Post(f func()) { h.q <- f }
func (h *testHost) PickSimScene(start string, done func(string, bool)) {
	done(h.scene, h.scene != "")
}
func (h *testHost) Message(title, text string) { h.messages = append(h.messages, title+": "+text) }
func (h *testHost) Confirm(title, text string, done func(bool)) {
	done(h.confirm)
}

func (h *testHost) pump(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for !ok() {
		select {
		case f := <-h.q:
			f()
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(end) {
			t.Fatalf("timeout waiting for %s", what)
		}
	}
}

// mockCam is a camera.Device with a synthetic live view.
type mockCam struct {
	lv, stops, closed atomic.Int32
	exp               camera.Exposure
	jpg               []byte
}

func newMock() *mockCam {
	img := image.NewRGBA(image.Rect(0, 0, 64, 40))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(3, 3, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return &mockCam{exp: camera.Exposure{ISO: 160, Shutter: 3906}, jpg: b.Bytes()}
}

func (m *mockCam) Info() camera.Info                  { return camera.Info{Manufacturer: "Mock", Model: "Cam"} }
func (m *mockCam) Exposure() (camera.Exposure, error) { return m.exp, nil }
func (m *mockCam) SetExposure(e camera.Exposure) (camera.Exposure, error) {
	m.exp = e
	return e, nil
}
func (m *mockCam) LiveView() ([]byte, error) { m.lv.Add(1); return m.jpg, nil }
func (m *mockCam) StopLiveView() error       { m.stops.Add(1); return nil }
func (m *mockCam) Capture(dir, prefix string) (string, error) {
	return "", errors.New("mock camera has no sensor")
}
func (m *mockCam) Diagnostics() error { return nil }
func (m *mockCam) Close() error       { m.closed.Add(1); return nil }
func (m *mockCam) Status() (camera.Exposure, float64, error) {
	return m.exp, 4, nil
}

func isolate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

func TestMockConnectLiveViewDisconnect(t *testing.T) {
	isolate(t)
	h := newHost()
	h.scene = "mock.raf"
	c := New(h)
	m := newMock()
	c.OpenSim = func(scene string, _ *camera.Logger) (camera.Device, error) {
		if scene != "mock.raf" {
			t.Errorf("scene %q", scene)
		}
		return m, nil
	}
	c.Start()
	b := c.Buttons()
	if b.ConnText != "Connect" || b.Test || b.Meter || b.Capture || !b.DevList {
		t.Fatalf("idle buttons %+v", b)
	}
	if items := c.DeviceItems(); items[len(items)-1] != "Simulated camera (test with a RAW file)" {
		t.Fatal(items)
	}
	c.SetLiveVisible(true)
	c.Connect()
	h.pump(t, "connect", 5*time.Second, c.Connected)
	if c.CamLbl != "Connected: Mock Cam" || !c.LvWanted {
		t.Fatalf("label %q wanted %v", c.CamLbl, c.LvWanted)
	}
	h.pump(t, "live frames", 5*time.Second, func() bool { return c.FrameSeq >= 3 })
	if c.Frame.Bounds().Dx() != 64 || !strings.HasPrefix(c.ViewWhat, "Live view") {
		t.Fatalf("frame %v %q", c.Frame.Bounds(), c.ViewWhat)
	}
	h.pump(t, "status poll", 5*time.Second, func() bool { return c.CamF == "f/4.0" })
	if c.CamISO == "" || c.CamSh == "" {
		t.Fatal("status readout empty")
	}
	b = c.Buttons()
	if b.ConnText != "Disconnect" || !b.Test || !b.Meter || b.Capture || b.DevList || b.LiveText != "Stop live view" || b.MeterText != "Meter base" {
		t.Fatalf("connected buttons %+v", b)
	}
	// leaving the page stops polling without forgetting the user's choice
	c.SetLiveVisible(false)
	h.pump(t, "live loop exit", 5*time.Second, func() bool { return !c.LiveRunning() })
	if !c.LvWanted {
		t.Fatal("LvWanted cleared by a page switch")
	}
	n := m.lv.Load()
	time.Sleep(200 * time.Millisecond)
	if m.lv.Load() != n {
		t.Fatal("live view still polled while hidden")
	}
	c.SetLiveVisible(true)
	h.pump(t, "live resumes", 5*time.Second, func() bool { return m.lv.Load() > n+2 })
	c.Disconnect()
	h.pump(t, "close", 5*time.Second, func() bool { return m.closed.Load() == 1 && !c.LiveRunning() })
	if c.Connected() || c.CamLbl != "Not connected" || c.Buttons().ConnText != "Connect" {
		t.Fatal("not disconnected")
	}
}

func TestMeterFailureIsReported(t *testing.T) {
	isolate(t)
	h := newHost()
	h.scene = "mock.raf"
	c := New(h)
	m := newMock()
	c.OpenSim = func(string, *camera.Logger) (camera.Device, error) { return m, nil }
	c.Connect()
	h.pump(t, "connect", 5*time.Second, c.Connected)
	c.Meter()
	if !c.Busy || c.Buttons().Meter {
		t.Fatal("not busy while metering")
	}
	h.pump(t, "meter result", 10*time.Second, func() bool { return !c.Busy })
	if c.ExpLbl != "NOT LOCKED" || !strings.Contains(c.BaseLbl, "Metering failed") || c.Locked() {
		t.Fatalf("exp %q base %q", c.ExpLbl, c.BaseLbl)
	}
	c.Disconnect()
}

func TestRectangleModeNeedsARectangle(t *testing.T) {
	isolate(t)
	h := newHost()
	h.scene = "x"
	c := New(h)
	c.OpenSim = func(string, *camera.Logger) (camera.Device, error) { return newMock(), nil }
	c.Connect()
	h.pump(t, "connect", 5*time.Second, c.Connected)
	c.Cfg.AreaMode = 1
	c.Meter()
	if c.Busy || len(h.messages) != 1 || !strings.HasPrefix(h.messages[0], "Meter base:") {
		t.Fatalf("busy %v messages %q", c.Busy, h.messages)
	}
	c.Disconnect()
}

func TestCancelledSceneDoesNotConnect(t *testing.T) {
	isolate(t)
	h := newHost()
	c := New(h)
	c.Connect()
	if c.Busy || c.Connected() {
		t.Fatal("connected without a scene")
	}
}

// TestSimMeterCaptureMerge runs the real simulator (LibRaw) on an
// uncompressed RAF given by NSS_SIM_RAF: meter, capture 0/+2, merge.
func TestSimMeterCaptureMerge(t *testing.T) {
	raf := os.Getenv("NSS_SIM_RAF")
	if raf == "" {
		t.Skip("NSS_SIM_RAF not set")
	}
	isolate(t)
	h := newHost()
	h.scene = raf
	c := New(h)
	c.Cfg.OutDir = t.TempDir()
	c.Cfg.Roll = "simroll"
	c.Connect()
	h.pump(t, "connect", 30*time.Second, func() bool { return c.Connected() || len(h.messages) > 0 })
	if !c.Connected() {
		if len(h.messages) > 0 && strings.Contains(h.messages[0], "layout not recognised") {
			t.Skip("NSS_SIM_RAF is a compressed RAF; the simulator needs an uncompressed one")
		}
		t.Fatalf("sim connect: %q", h.messages)
	}
	c.Meter()
	h.pump(t, "meter", 120*time.Second, func() bool { return !c.Busy })
	if !c.Locked() {
		t.Fatalf("not locked: %q / %q", c.ExpLbl, c.BaseLbl)
	}
	t.Logf("locked: %s; F0 %s %s; brighter %s; base %s %s/%s/%s", c.ExpLbl, c.F0ISO, c.F0Sh, c.P2Sh, c.BaseLv, c.BaseR, c.BaseG, c.BaseB)
	if !c.Buttons().Capture {
		t.Fatal("Capture not enabled after lock")
	}
	c.Capture()
	h.pump(t, "capture and merge", 300*time.Second, func() bool { return !c.Busy && !c.Working && !c.Scanning() })
	if c.LastCap == nil || !strings.HasPrefix(c.Res, "Saved ") {
		t.Fatalf("result: %s", c.Res)
	}
	t.Logf("result: %s", strings.SplitN(c.Res, "\n", 2)[0])
	if _, err := os.Stat(c.LastCap.HDR); err != nil {
		t.Fatal(err)
	}
	c.Disconnect()
}
