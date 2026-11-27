//go:build darwin || (linux && gio)

package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"basetool/camera"
	"basetool/macapp/appstate"
)

// filmCam is a mock camera whose live view looks like a strip of colour
// negative on a light table (orange base, two frames, sprocket holes).
type filmCam struct{ jpg []byte }

func newFilmCam() *filmCam {
	img := image.NewRGBA(image.Rect(0, 0, 600, 400))
	base := color.RGBA{214, 150, 96, 255}
	for y := 0; y < 400; y++ {
		for x := 0; x < 600; x++ {
			c := base
			if y > 60 && y < 340 && ((x > 30 && x < 290) || (x > 310 && x < 570)) {
				v := uint8(60 + (x*y/700)%120)
				c = color.RGBA{v + 40, v / 2, v / 3, 255}
			}
			if (y > 18 && y < 40 || y > 360 && y < 382) && x%40 < 18 {
				c = color.RGBA{250, 250, 245, 255}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	return &filmCam{jpg: b.Bytes()}
}

func (m *filmCam) Info() camera.Info {
	return camera.Info{Manufacturer: "Fujifilm", Model: "X-T3 (mock)"}
}
func (m *filmCam) Exposure() (camera.Exposure, error) {
	return camera.Exposure{ISO: 160, Shutter: 3906}, nil
}
func (m *filmCam) SetExposure(e camera.Exposure) (camera.Exposure, error) { return e, nil }
func (m *filmCam) LiveView() ([]byte, error)                              { return m.jpg, nil }
func (m *filmCam) StopLiveView() error                                    { return nil }
func (m *filmCam) Capture(string, string) (string, error)                 { return "", errors.New("mock") }
func (m *filmCam) Diagnostics() error                                     { return nil }
func (m *filmCam) Close() error                                           { return nil }
func (m *filmCam) Status() (camera.Exposure, float64, error) {
	return camera.Exposure{ISO: 160, Shutter: 3906}, 4, nil
}

// startHDR starts the HDR controller as the app does, without the real
// camera scan: on macOS that is the ImageCaptureCore scan, which needs the
// main run loop that a test binary doesn't run, so it can block the camera
// worker for its 5 s timeout (see newLightCtl in hdrctl/light_test.go).
func startHDR(u *UI) {
	u.hdr.ListDevices = nil
	u.hdr.Start()
}

func isolate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("NSS_LOG_DIR", filepath.Join(home, "logs"))
}

type shooter struct {
	t   *testing.T
	w   *headless.Window
	u   *UI
	sz  image.Point
	dir string
	ops op.Ops
}

func (s *shooter) frame() {
	gtx := layout.Context{Ops: &s.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(s.sz), Now: time.Now()}
	s.ops.Reset()
	s.u.Layout(gtx)
	if err := s.w.Frame(&s.ops); err != nil {
		s.t.Fatal(err)
	}
}

func (s *shooter) save(name string) {
	s.frame()
	s.frame()
	if s.dir == "" {
		return
	}
	img := image.NewRGBA(image.Rectangle{Max: s.sz})
	if err := s.w.Screenshot(img); err != nil {
		s.t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(s.dir, name))
	if err != nil {
		s.t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, img)
}

func (s *shooter) pump(what string, ok func() bool) { s.pumpFor(what, 10*time.Second, ok) }

func (s *shooter) pumpFor(what string, d time.Duration, ok func() bool) {
	end := time.Now().Add(d)
	for !ok() {
		select {
		case f := <-s.u.posts:
			f()
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(end) {
			s.t.Fatalf("timeout: %s", what)
		}
	}
}

// TestShellAndHDRPages lays out every page headlessly (GPU: EGL on Linux,
// Metal on macOS) and, with NSS_SHOTS=<dir>, saves screenshots.
func TestShellAndHDRPages(t *testing.T) {
	isolate(t)
	w, err := headless.NewWindow(1280, 800)
	if err != nil {
		t.Skip("no headless GPU:", err)
	}
	defer w.Release()
	u := newUI(newTheme(), nil)
	s := &shooter{t: t, w: w, u: u, sz: image.Pt(1280, 800), dir: os.Getenv("NSS_SHOTS")}
	if s.dir != "" {
		os.MkdirAll(s.dir, 0o755)
	}
	u.setNav()
	startHDR(u)
	s.pump("start", func() bool { return u.hdr.LogSeq > 0 })
	s.save("01-camera-idle.png")

	// every page lays out
	for m, mod := range appstate.Modules {
		u.nav.SetModule(m)
		for i := range max(1, len(mod.Subs)) {
			u.nav.SetSub(i)
			u.setNav()
			s.frame()
		}
	}

	// connect the mock camera: live view through a GPU image op
	u.nav = appstate.Nav{}
	u.setNav()
	u.pick = func(_ string, _ pickKind, _ string, _ []string, done func(string, bool)) { done("mock.raf", true) }
	u.hdr.OpenSim = func(string, *camera.Logger) (camera.Device, error) { return newFilmCam(), nil }
	u.hdr.Connect()
	s.pump("connected", func() bool { return u.hdr.Connected() && u.hdr.FrameSeq > 2 && u.hdr.CamF != "" })
	u.hdr.BaseNorm.X0, u.hdr.BaseNorm.Y0, u.hdr.BaseNorm.X1, u.hdr.BaseNorm.Y1 = 0.05, 0.05, 0.95, 0.1
	u.hdr.F0ISO, u.hdr.F0Sh, u.hdr.BaseLv, u.hdr.BaseR, u.hdr.BaseG, u.hdr.BaseB = "160", "1/250", "0.80 FS", "0.802", "0.561", "0.377"
	u.hdr.ExpLbl = "LOCKED  (set over USB, ISO stays 160)"
	s.save("02-camera-live.png")
	if u.hp.views[0].img.Size() != image.Pt(600, 400) {
		t.Fatalf("live view image op %v", u.hp.views[0].img.Size())
	}

	u.nav.SetModule(1)
	u.setNav()
	s.pump("white live", func() bool { return u.hdr.FrameSeq > 6 })
	s.save("03-white-light-scan.png")

	u.hp.brCB.open = true
	s.save("04-white-bracket-open.png")
	u.hp.brCB.open = false

	u.Message("About", aboutText())
	s.save("06-about.png")
	u.dialogs = nil
	u.hdr.Disconnect()
}

func TestNavClicksSwitchLiveView(t *testing.T) {
	isolate(t)
	u := newUI(newTheme(), nil)
	u.setNav()
	if u.nav.Page() != appstate.PageCamera || u.nav.LiveView() != 0 {
		t.Fatal(u.nav)
	}
	u.nav.SetModule(2)
	u.setNav()
	if u.nav.LiveView() != -1 {
		t.Fatal("crop & merge has no live view")
	}
	// no fourth module (Windows' Convert is not part of the macOS app)
	if u.nav.SetModule(3) || len(u.navBtns) != len(appstate.Modules) {
		t.Fatal("fourth module accepted")
	}
}

// TestLightPageLayout: the Light page at the default 1280x800 and at the
// minimum window size, before and after a simulated leveling. The preview
// column keeps the whole live view and the log inside the window (no
// clipping); with NSS_SHOTS=<dir> the frames are saved.
func TestLightPageLayout(t *testing.T) {
	isolate(t)
	u := newUI(newTheme(), nil)
	u.nav = appstate.Nav{}
	u.nav.SetModule(0)
	u.nav.SetSub(1)
	u.setNav()
	startHDR(u)
	l := u.hdr.Light
	dir := os.Getenv("NSS_SHOTS")
	if dir != "" {
		os.MkdirAll(dir, 0o755)
	}
	shoot := func(sz image.Point, name string) *shooter {
		w, err := headless.NewWindow(sz.X, sz.Y)
		if err != nil {
			t.Skip("no headless GPU:", err)
		}
		t.Cleanup(w.Release)
		s := &shooter{t: t, w: w, u: u, sz: sz, dir: dir}
		s.save(name)
		v := u.lp.view.ir
		if v.Empty() && l.Comp != nil {
			t.Fatalf("%s: live view not laid out", name)
		}
		return s
	}
	s := shoot(image.Pt(1280, 800), "07-light-idle-1280x800.png")
	s.pump("light ready", func() bool { return strings.Contains(l.LogText(), "Light page ready") })

	// camera live view in the Light page's preview
	u.pick = func(_ string, _ pickKind, _ string, _ []string, done func(string, bool)) { done("mock.raf", true) }
	u.hdr.OpenSim = func(string, *camera.Logger) (camera.Device, error) { return newFilmCam(), nil }
	u.hdr.Connect()
	s.pump("connected", func() bool { return u.hdr.Connected() && u.hdr.FrameSeq > 2 })
	s.frame()
	ir := u.lp.view.ir
	if ir.Empty() || ir.Max.X > s.sz.X || ir.Max.Y > s.sz.Y {
		t.Fatalf("live view %v outside the %v window", ir, s.sz)
	}
	if ir.Dx() < 360 && ir.Dy() < 240 {
		t.Fatalf("live view too small at 1280x800: %v", ir)
	}

	// simulated light + leveling
	l.PortIdx = len(l.PortItems) - 2
	l.ToggleConnect()
	s.pump("light connected", func() bool { return l.Connected() })
	u.hdr.Disconnect() // the simulated light is measured by the simulated camera
	s.pump("camera off", func() bool { return !u.hdr.Connected() })
	l.StartLevel()
	s.pumpFor("leveling", 120*time.Second, func() bool { return !l.Leveling() && l.Comp != nil })
	if !strings.HasPrefix(l.ResText, "LEVELLED: ") {
		t.Fatalf("leveling: %s", l.ResText)
	}
	u.lp.cards[3].collapsed = false
	s.save("08-light-levelled-1280x800.png")
	if c := u.lp.comp; c.Empty() || c.Max.X > s.sz.X {
		t.Fatalf("captures %v", c)
	}
	shoot(image.Pt(1024, 640), "09-light-levelled-1024x640.png")
	shoot(image.Pt(1440, 900), "10-light-levelled-1440x900.png")

	// simulated Hikrobot leveling camera: its preview goes into the Light
	// page's live view (main.go: lightui.startHikPreview)
	seq := u.hdr.FrameSeq
	l.Settings().CamSrc = 2
	l.CamSourceChanged()
	s.pump("hik preview", func() bool { return u.hdr.FrameSeq > seq+1 })
	s.save("11-light-hik-sim-1280x800.png")
	u.nav.SetSub(0)
	u.setNav() // Camera page: the Hikrobot preview stops
	// Wait for the loop itself to end, then apply the frames it posted before
	// it stopped: after that no frame may arrive (no sleep-and-hope window).
	s.pump("hik preview stopped", func() bool { return !u.hdr.HikPreviewRunning() })
	u.drain()
	seq = u.hdr.FrameSeq
	s.frame()
	u.drain()
	if u.hdr.FrameSeq != seq || u.hdr.HikPreviewRunning() {
		t.Fatal("Hikrobot preview still running off the Light page")
	}
	u.hdr.Shutdown()
}
