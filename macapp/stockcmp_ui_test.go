//go:build darwin || (linux && gio)

package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"basetool/camera"
	"basetool/filmdb"
	"basetool/macapp/appstate"
)

// TestStockCompareShots: the Fixed frame 0 film-stock line on the Camera
// card (no stock, pass, fail with the Frame 0 notice, a stock change after
// the lock) and a capture result, on a camera whose shots are a real RAW
// (NSS_TEST_RAF); NSS_SHOTS=<dir> keeps the screenshots.
func TestStockCompareShots(t *testing.T) {
	var raf string
	for _, p := range filepath.SplitList(os.Getenv("NSS_TEST_RAF")) {
		if _, err := os.Stat(p); err == nil {
			raf = p
			break
		}
	}
	if raf == "" {
		t.Skip("set NSS_TEST_RAF to a RAW file to run this test")
	}
	isolate(t)
	sz := image.Pt(1280, 1800)
	w, err := headless.NewWindow(sz.X, sz.Y)
	if err != nil {
		t.Skip("no headless GPU:", err)
	}
	defer w.Release()
	u := newUI(newTheme(), nil)
	s := &shooter{t: t, w: w, u: u, sz: sz, dir: os.Getenv("NSS_SHOTS")}
	if s.dir != "" {
		os.MkdirAll(s.dir, 0o755)
	}
	c := u.hdr
	c.Cfg.OutDir, c.Cfg.Roll, c.Cfg.Frame0, c.Cfg.ISO = t.TempDir(), "roll 12", 1, 2 // Fixed, ISO 250 (as Record base)
	u.setNav()
	startHDR(u)
	s.pump("start", func() bool { return c.LogSeq > 0 })
	u.pick = func(_ string, _ pickKind, _ string, _ []string, done func(string, bool)) { done("mock.raf", true) }
	c.OpenSim = func(string, *camera.Logger) (camera.Device, error) {
		return &rafCam{filmCam: newFilmCam(), raf: raf}, nil
	}
	c.Connect()
	s.pump("connected", func() bool { return c.Connected() && c.CamF != "" })
	lock := func() {
		u.dialogs = nil
		c.Meter()
		s.pumpFor("lock", 60*time.Second, func() bool { return !c.Busy && c.Locked() })
	}

	// no stock
	lock()
	s.save("01-meter-no-stock.png")
	if !strings.HasSuffix(c.BaseLbl, "\nFilm stock: None (no DB check)") {
		t.Fatalf("no stock %q", c.BaseLbl)
	}
	base := strings.TrimSuffix(c.BaseLbl, "\nFilm stock: None (no DB check)")

	// a stock recorded from this base (Record base), and one that differs
	c.RecordBase()
	s.pumpFor("record base", 60*time.Second, func() bool { return !c.Busy && c.DBSave.Shown })
	s.frame() // the panel loads its fields
	u.db.nameEd.SetText("Portra 400")
	s.frame()
	c.DBSaveSave()
	p := filmdb.Path(appstate.ConfigDir())
	db, _ := filmdb.Load(p)
	if db.Find("Portra 400") == nil {
		t.Fatalf("not saved: %+v %q %+v", u.dialogs, c.DB.Names(), *c.DBSave)
	}
	m := *db.Find("Portra 400")
	m.Name, m.R = "Gold 200", m.R*1.5
	c.DB.Put(m, false)
	c.DB.Save()

	// pass at Meter base
	c.SetStock(c.StockIndex()) // Portra 400 (selected by the save)
	lock()
	s.save("02-meter-pass.png")
	if !strings.Contains(c.BaseLbl, "\nPortra 400: Film base OK vs DB: ") {
		t.Fatalf("pass %q %+v", c.BaseLbl, u.dialogs)
	}

	// stock changed after the lock: rechecked from the probe
	for i, n := range c.StockItems() {
		if n == "Gold 200" {
			c.SetStock(i)
		}
	}
	s.pumpFor("recheck", 30*time.Second, func() bool { return strings.Contains(c.BaseLbl, "\nGold 200: Film base WARNING vs DB (") })
	s.save("03-stock-changed-recheck.png")
	if strings.Contains(c.BaseLbl, "Portra 400:") || !strings.HasPrefix(c.BaseLbl, base) {
		t.Fatalf("recheck %q", c.BaseLbl)
	}

	// fail at Meter base: the in-window Frame 0 notice
	lock()
	s.save("04-meter-fail-notice.png")
	if len(u.dialogs) != 1 || u.dialogs[0].title != "Frame 0" || !strings.Contains(u.dialogs[0].text, "Gold 200: Film base WARNING vs DB (R -0.58") {
		t.Fatalf("notice %d %q", len(u.dialogs), u.dialogs[0].text)
	}
	u.dialogs = nil

	// capture: the result on the White light scan page
	c.Capture()
	s.pumpFor("capture", 300*time.Second, func() bool { return !c.Busy && !c.Working && !c.Scanning() })
	u.nav.SetModule(1)
	u.setNav()
	s.save("05-capture-result.png")
	if !strings.HasPrefix(c.Res, "Saved ") {
		t.Fatalf("result %q", c.Res)
	}
	t.Logf("capture result last line: %q", c.Res[strings.LastIndex(c.Res, "\n")+1:])
	c.Disconnect()
}
