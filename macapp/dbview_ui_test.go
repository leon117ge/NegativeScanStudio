//go:build darwin || (linux && gio)

package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"basetool/camera"
	"basetool/filmdb"
	"basetool/macapp/appstate"
)

// rafCam is the film mock whose shots are copies of a real RAW.
type rafCam struct {
	*filmCam
	raf string
	n   atomic.Int32
	exp atomic.Value
}

func (m *rafCam) Exposure() (camera.Exposure, error) {
	if e, ok := m.exp.Load().(camera.Exposure); ok {
		return e, nil
	}
	return m.filmCam.Exposure()
}
func (m *rafCam) SetExposure(e camera.Exposure) (camera.Exposure, error) {
	m.exp.Store(e)
	return e, nil
}
func (m *rafCam) Capture(dir, prefix string) (string, error) {
	b, err := os.ReadFile(m.raf)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, fmt.Sprintf("%s%04d.RAF", prefix, m.n.Add(1)))
	return p, os.WriteFile(p, b, 0o644)
}

// TestFilmDBPanels drives the Camera card's film-base DB buttons and the
// two panels (saveToDB / manageDB on Windows) on a camera whose shots are
// a real RAW (NSS_TEST_RAF); NSS_SHOTS=<dir> keeps the screenshots.
func TestFilmDBPanels(t *testing.T) {
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
	c.Cfg.OutDir, c.Cfg.Roll, c.Cfg.Frame0 = t.TempDir(), "roll 12", 1
	u.setNav()
	startHDR(u)
	s.pump("start", func() bool { return c.LogSeq > 0 })
	s.save("01-camera-idle.png")
	if b := c.Buttons(); b.Rec || b.SaveDB || b.SaveF0 {
		t.Fatalf("idle %+v", b)
	}
	if got := c.StockItems(); len(got) != 2 || got[1] != "Session 1" {
		t.Fatalf("stocks %q", got)
	}

	u.pick = func(_ string, _ pickKind, _ string, _ []string, done func(string, bool)) { done("mock.raf", true) }
	c.OpenSim = func(string, *camera.Logger) (camera.Device, error) {
		return &rafCam{filmCam: newFilmCam(), raf: raf}, nil
	}
	c.Connect()
	s.pump("connected", func() bool { return c.Connected() && c.CamF != "" })
	s.save("02-camera-connected.png")
	if b := c.Buttons(); !b.Rec || b.SaveDB || b.SaveF0 {
		t.Fatalf("connected %+v", b)
	}

	// Record base: the save panel (new stock, the roll name as the cue)
	c.RecordBase()
	s.pumpFor("record base", 60*time.Second, func() bool { return !c.Busy && c.DBSave.Shown })
	s.save("03-record-base-save.png")
	if u.db.nameEd.Text() != "roll_12" || !strings.HasPrefix(c.DBSave.Info, "Base recorded at ISO 250, 1/60 (f/4 on the lens)") {
		t.Fatalf("panel name %q info %q", u.db.nameEd.Text(), c.DBSave.Info)
	}
	u.db.nameEd.SetText("")
	s.frame()
	c.DBSaveSave()
	s.save("04-record-base-name-message.png")
	if len(u.dialogs) != 1 || u.dialogs[0].text != "Enter a name (1-40 characters, no quotes)." || !c.DBSave.Shown {
		t.Fatalf("dialogs %+v", u.dialogs)
	}
	u.dialogs = nil
	u.db.nameEd.SetText("Portra 400")
	u.db.lightEd.SetText("CS-LITE 5000K")
	s.frame()
	c.DBSaveSave()
	s.save("05-camera-stock-saved.png")
	if c.DBSave.Shown || c.Cfg.Stock != "Portra 400" {
		t.Fatalf("not saved: %+v", u.dialogs)
	}
	if e := mustDB(t).Find("Portra 400"); e == nil || e.Light != "CS-LITE 5000K" || e.Source != "record" {
		t.Fatalf("entry %+v", e)
	}

	// lock frame 0, Save base to DB: update mode on the selected stock
	c.Meter()
	s.pumpFor("lock", 60*time.Second, func() bool { return !c.Busy && c.Locked() })
	c.SaveBaseToDB()
	s.pumpFor("probe", 60*time.Second, func() bool { return !c.Busy && c.DBSave.Shown })
	s.save("06-save-base-update.png")
	if c.DBSave.Act != 1 || u.db.lightEd.Text() != "CS-LITE 5000K" || !strings.HasPrefix(c.DBSave.Cur, "Stored: ") {
		t.Fatalf("update panel %+v", *c.DBSave)
	}
	c.DBSaveCancel()

	// Manage: rename, delete (Yes / No), close
	c.ManageDB()
	s.save("07-manage.png")
	if u.db.mNameEd.Text() != "Portra 400" || !strings.Contains(c.DBMan.Info, "\nLight: CS-LITE 5000K") {
		t.Fatalf("manage %+v", *c.DBMan)
	}
	u.db.mNameEd.SetText("Portra 400 NC")
	s.frame()
	c.ManageRename()
	c.ManageDelete()
	s.save("08-manage-delete-confirm.png")
	if len(u.dialogs) != 1 || u.dialogs[0].text != "Delete \"Portra 400 NC\" from the film-base DB?" {
		t.Fatalf("dialogs %+v", u.dialogs)
	}
	u.dialogs[0].done(false)
	u.dialogs = nil
	c.ManageClose()
	s.save("09-camera-after-manage.png")
	if c.Cfg.Stock != "Portra 400 NC" || c.DBOpen() || appstate.LoadHDRConfig().Stock != "Portra 400 NC" {
		t.Fatalf("stock %q", c.Cfg.Stock)
	}
	c.Disconnect()
}

func mustDB(t *testing.T) *filmdb.DB {
	db, err := filmdb.Load(filmdb.Path(appstate.ConfigDir()))
	if err != nil {
		t.Fatal(err)
	}
	return db
}
