//go:build darwin || (linux && gio)

package main

import (
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"basetool/camera"
	"basetool/macapp/appstate"
)

// TestTrichromeAndMonoPages lays out HDR Scan > Trichrome and Mono
// trichrome at 1280x800: idle, with the simulated RGB light and cameras
// connected, and a real Mono trichrome capture on the Hikrobot simulator;
// with NSS_SHOTS=<dir> the frames are saved.
func TestTrichromeAndMonoPages(t *testing.T) {
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
	u.hdr.Start()
	l, tr, m := u.hdr.Light, u.hdr.Tri, u.hdr.Mono
	s.pump("pages ready", func() bool {
		return strings.Contains(l.LogText(), "Light page ready") && strings.Contains(m.LogText(), "Mono trichrome page ready") && len(l.PortItems) >= 2
	})
	open := func(sub int) {
		u.nav.SetModule(1)
		u.nav.SetSub(sub)
		u.setNav()
	}

	// Trichrome
	open(1)
	if u.nav.Page() != appstate.PageTri || u.nav.LiveView() != 2 {
		t.Fatal(u.nav.Page())
	}
	s.save("01-trichrome-idle.png")
	if !strings.HasPrefix(tr.WhyText, "Capture disabled: camera not connected") {
		t.Fatal(tr.WhyText)
	}
	l.PortIdx = len(l.PortItems) - 2 // Simulated light
	l.ToggleConnect()
	s.pump("light", func() bool { return l.Connected() })
	u.pick = func(_ string, _ pickKind, _ string, _ []string, done func(string, bool)) { done("mock.raf", true) }
	u.hdr.OpenSim = func(string, *camera.Logger) (camera.Device, error) { return newFilmCam(), nil }
	u.hdr.Connect()
	s.pump("camera", func() bool { return u.hdr.Connected() && u.hdr.FrameSeq > 2 })
	s.save("02-trichrome-connected.png")
	if tr.WhyText != "Capture disabled: frame 0 not locked" || !strings.HasPrefix(tr.LightText, "Light now: ") || !strings.HasPrefix(tr.ValText, "Scan values: R ") {
		t.Fatalf("%q %q %q", tr.WhyText, tr.LightText, tr.ValText)
	}
	u.tp.modeCB.open = true
	s.save("03-trichrome-mode-open.png")
	u.tp.modeCB.open = false
	u.hdr.Disconnect()
	s.pump("camera off", func() bool { return !u.hdr.Connected() })

	// Mono trichrome
	open(2)
	if u.nav.Page() != appstate.PageMono {
		t.Fatal(u.nav.Page())
	}
	s.save("04-mono-idle.png")
	if !strings.HasPrefix(m.SDKText, "SDK: ") || m.WhyText != "Capture disabled: camera not connected" {
		t.Fatalf("%q %q", m.SDKText, m.WhyText)
	}
	m.DevIdx = 0 // the simulator
	m.ToggleConnect()
	s.pump("mono preview", func() bool { return m.Connected() && m.FrameSeq > 2 })
	s.save("05-mono-live.png")
	if u.mo.img.Size().X == 0 {
		t.Fatal("live view not painted")
	}
	m.Mark = true
	m.MarkChanged()
	s.save("06-mono-center-mark.png")
	m.CapMode, m.FPEText = 0, "1"
	m.OptionsChanged()
	l.SettleText = "20"
	m.Capture()
	s.save("07-mono-capturing.png")
	s.pumpFor("mono capture", 180*time.Second, func() bool { return !m.Running() })
	if !strings.Contains(m.RepText, "Saved ") {
		t.Fatalf("result: %s", m.RepText)
	}
	s.pump("preview again", func() bool { return m.PreviewRunning() })
	s.save("08-mono-captured.png")
	u.mo.list.Position.First, u.mo.list.Position.Offset = 2, 0 // the capture card
	s.save("09-mono-capture-card.png")

	// leaving the page stops the Hikrobot preview
	open(0)
	s.pump("preview stops", func() bool { return !m.PreviewRunning() })
	m.ToggleConnect()
	s.pump("mono off", func() bool { return !m.Connected() && !m.ConnBusy() })
	l.ToggleConnect()
	s.pump("light off", func() bool { return !l.Connected() })
	u.hdr.Shutdown()
}
