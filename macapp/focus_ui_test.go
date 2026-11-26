//go:build darwin || (linux && gio)

package main

import (
	"image"
	"os"
	"strings"
	"testing"

	"gioui.org/gpu/headless"

	"basetool/camera"
	"basetool/macapp/appstate"
)

// TestFocusCheckPage lays out HDR Scan > Focus check at 1280x800: idle,
// live on the Hikrobot simulator with the simulated light (green only),
// the centre marks, a click to 200 %, a pan at 400 %, White, the Zoom
// combo, and the X-T3 live-view path; with NSS_SHOTS=<dir> the frames are
// saved.
func TestFocusCheckPage(t *testing.T) {
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
	u.hdr.Cfg.OutDir = t.TempDir()
	startHDR(u)
	l, m, f := u.hdr.Light, u.hdr.Mono, u.hdr.Focus
	s.pump("pages ready", func() bool {
		return strings.Contains(f.LogText(), "Focus check ready") && strings.Contains(m.LogText(), "Mono trichrome page ready") && len(l.PortItems) >= 2
	})
	open := func(mod, sub int) {
		u.nav.SetModule(mod)
		u.nav.SetSub(sub)
		u.setNav()
	}

	open(1, 3)
	if u.nav.Page() != appstate.PageFocus || u.nav.LiveView() != -1 || !appstate.Ported[appstate.PageFocus] {
		t.Fatal(u.nav.Page())
	}
	s.pump("no camera", func() bool { return strings.Contains(f.LogText(), "No camera to preview.") })
	s.save("01-focus-idle.png")
	if f.SrcText != "Camera: none connected" || f.LightText != "Light: not connected (wanted: green only)" || f.LightOK {
		t.Fatalf("%q %q", f.SrcText, f.LightText)
	}

	// light and Hikrobot simulator (connected on Mono trichrome)
	l.PortIdx = len(l.PortItems) - 2
	l.ToggleConnect()
	s.pump("light", func() bool { return l.Connected() })
	open(1, 2)
	m.DevIdx = 0
	m.ToggleConnect()
	s.pump("mono preview", func() bool { return m.Connected() && m.FrameSeq > 1 })
	open(1, 3)
	s.pump("focus live", func() bool {
		return f.Frame != nil && f.FrameSeq > 3 && f.Meter > 0 && strings.Contains(f.LogText(), "Light: green only (trichrome focus baseline).")
	})
	if m.PreviewRunning() {
		t.Fatal("the Mono trichrome preview must stop on Focus check")
	}
	s.save("02-focus-live-fit.png")
	if u.fp.img.Size().X == 0 || !strings.HasPrefix(f.SrcText, "Camera: ") || f.LightText != "Light: green only" {
		t.Fatalf("%q %q", f.SrcText, f.LightText)
	}
	m.Mark = true
	m.MarkChanged()
	s.save("03-focus-center-mark.png")

	// a click on the frame zooms to 200 % around that point
	vw, vh := f.ViewSize()
	ir := appstate.FitRect(image.Rectangle{Max: image.Pt(vw, vh)}, image.Pt(f.SrcW, f.SrcH))
	f.Click(ir.Min.X+ir.Dx()/3, ir.Min.Y+ir.Dy()/3, vw, vh, ir)
	f.Up()
	s.pump("200 %", func() bool { return f.ZoomIdx == 2 && !f.BmpFit && f.FrameSeq > 0 })
	s.save("04-focus-click-200.png")
	f.ZoomIdx = 3
	f.ZoomChanged()
	f.Click(vw/2, vh/2, vw, vh, ir)
	f.Move(vw/2-120, vh/2-60)
	f.Up()
	s.pump("400 %", func() bool { return !f.BmpFit && f.Frame.Bounds().Dx() <= vw/4+1 })
	s.save("05-focus-pan-400.png")
	f.SetLight(1)
	s.pump("white", func() bool {
		return strings.Contains(f.LogText(), "Light: white (all channels)") && f.LightText == "Light: white"
	})
	s.save("06-focus-white.png")
	u.fp.zoomCB.open = true
	s.save("07-focus-zoom-open.png")
	u.fp.zoomCB.open = false

	// leaving restores the capture exposure and starts the Mono preview again
	open(1, 2)
	s.pump("left", func() bool {
		return strings.Contains(f.LogText(), "Left Focus check: previous capture exposure restored.") && m.PreviewRunning()
	})
	if !strings.Contains(f.LogText(), "Restored Hikrobot exposure: ") {
		t.Fatal(f.LogText())
	}
	m.ToggleConnect()
	s.pump("mono off", func() bool { return !m.Connected() && !m.ConnBusy() })

	// the X-T3 live view
	u.pick = func(_ string, _ pickKind, _ string, _ []string, done func(string, bool)) { done("mock.raf", true) }
	u.hdr.OpenSim = func(string, *camera.Logger) (camera.Device, error) { return newFilmCam(), nil }
	u.hdr.Connect()
	s.pump("camera", func() bool { return u.hdr.Connected() })
	f.ZoomIdx = 0
	f.ZoomChanged()
	open(1, 3)
	s.pump("x-t3 live", func() bool {
		return f.Frame != nil && f.BmpFit && f.Meter > 0 && strings.HasPrefix(f.SrcText, "Camera: ") && !strings.Contains(f.SrcText, "none")
	})
	s.save("08-focus-xt3.png")
	open(0, 0)
	s.pump("left again", func() bool { return strings.Count(f.LogText(), "Left Focus check") == 2 })
	u.hdr.Disconnect()
	s.pump("camera off", func() bool { return !u.hdr.Connected() })
	l.ToggleConnect()
	s.pump("light off", func() bool { return !l.Connected() })
	u.hdr.Shutdown()
}
