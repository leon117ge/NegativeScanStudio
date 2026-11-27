package hdrctl

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"basetool/camera"
	"basetool/filmdb"
	"basetool/hdrscan"

	"basetool/macapp/appstate"
)

// The DB is film_base_db.json in the settings folder (filmdb.Path(configDir())
// as on Windows) and a first start imports the Film base presets like
// loadDB on Windows.
func TestFilmDBLoadImportsPresets(t *testing.T) {
	isolate(t)
	h := newHost()
	c := New(h)
	want := filepath.Join(appstate.ConfigDir(), "film_base_db.json")
	if c.dbPath() != want {
		t.Fatalf("db path %q, want %q", c.dbPath(), want)
	}
	if got := c.StockItems(); !reflect.DeepEqual(got, []string{"None", "Session 1"}) {
		t.Fatalf("stocks %q", got)
	}
	db, err := filmdb.Load(want)
	if err != nil || len(db.Entries) != 1 || db.Entries[0].Source != "preset" || db.Entries[0].R != 0.1933 {
		t.Fatalf("file %+v %v", db, err)
	}
	h.pump(t, "import log", 2*time.Second, func() bool { return strings.Contains(c.LogText(), "imported 1 Film base preset(s) into "+want) })

	// config.txt presets replace the default; deleted presets stay hidden
	isolate(t)
	os.MkdirAll(appstate.ConfigDir(), 0o755)
	os.WriteFile(filepath.Join(appstate.ConfigDir(), "config.txt"),
		[]byte("// FBM_PRESET|Portra 400|0.2|0.18|0.07\r\n// FBM_PRESET|bad\"name|1|1|1\n// FBM_PRESET|Gold|0.3|0.2|0.1\n"), 0o644)
	c = New(newHost())
	if got := c.StockItems(); !reflect.DeepEqual(got, []string{"None", "Gold", "Portra 400"}) {
		t.Fatalf("config.txt stocks %q", got)
	}
	c.DB.Delete("Gold")
	c.DB.Save()
	if got := New(newHost()).StockItems(); !reflect.DeepEqual(got, []string{"None", "Portra 400"}) {
		t.Fatalf("deleted preset imported again: %q", got)
	}
}

func dbNew(t *testing.T) (*testHost, *Ctl) {
	isolate(t)
	h := newHost()
	c := New(h)
	c.Cfg.Roll = "roll 7"
	return h, c
}

func logHas(t *testing.T, h *testHost, c *Ctl, s string) {
	t.Helper()
	h.pump(t, "log "+s, 2*time.Second, func() bool { return strings.Contains(c.LogText(), s) })
}

// The save panel replays saveToDB: the same entries as filmdb.Put with
// the Windows values, the same messages, the same stock selection.
func TestSaveToDBPanel(t *testing.T) {
	h, c := dbNew(t)
	e := camera.Exposure{ISO: 250, Shutter: 16667}
	rgb := [3]float64{0.21, 0.19, 0.08}

	closed := 0
	c.saveToDB("T", "info", [3]float64{0.2, 0, 0.1}, 5, e, 4, "frame0", func() { closed++ })
	if c.DBSave.Shown || closed != 1 || h.messages[0] != "T: No usable base values to save." {
		t.Fatalf("zero base: shown %v closed %d %q", c.DBSave.Shown, closed, h.messages)
	}

	c.saveToDB("Save base to DB", "info", rgb, 0, e, 0, "frame0", func() { closed++ })
	d := c.DBSave
	if !d.Shown || d.Act != 0 || d.Name != "roll 7" || !d.NameOn || d.UpdOn || d.AvgOn || d.Cur != "" ||
		!reflect.DeepEqual(d.Names, []string{"Session 1"}) || d.ActItems[1] != "Update an existing film stock" ||
		d.AvgItems[1] != "Average with the stored values (weighted)" {
		t.Fatalf("new panel %+v", *d)
	}
	h.messages = nil
	d.Name = `bad"name`
	c.DBSaveSave()
	d.Name = "session 1"
	c.DBSaveSave()
	if !d.Shown || !reflect.DeepEqual(h.messages, []string{
		"Save base to DB: Enter a name (1-40 characters, no quotes).",
		"Save base to DB: \"session 1\" already exists. Choose \"Update an existing film stock\" or another name."}) {
		t.Fatalf("validation %q", h.messages)
	}
	d.Name, d.Light = " Portra 400 ", " CS-LITE 5000K "
	c.DBSaveSave()
	if d.Shown || closed != 2 || c.Cfg.Stock != "Portra 400" || c.StockIndex() != 1 {
		t.Fatalf("saved: shown %v closed %d stock %q", d.Shown, closed, c.Cfg.Stock)
	}
	// golden: the same Put on a fresh DB (the date is the only free field)
	ref := &filmdb.DB{}
	want, _ := ref.Put(filmdb.Entry{Name: "Portra 400", R: rgb[0], G: rgb[1], B: rgb[2], ISO: 250, Shutter: 16667,
		Aperture: filmdb.ApertureUnknown, Light: "CS-LITE 5000K", Samples: 1, Source: "frame0"}, false)
	disk, err := filmdb.Load(c.dbPath())
	if err != nil {
		t.Fatal(err)
	}
	got := disk.Find("Portra 400")
	if got == nil {
		t.Fatal("not on disk")
	}
	w := *want
	w.Date = got.Date
	if !reflect.DeepEqual(*got, w) {
		t.Fatalf("entry\n got %+v\nwant %+v", *got, w)
	}
	logHas(t, h, c, "Film-base DB: saved "+entryText(got))
	if appstate.LoadHDRConfig().Stock != "Portra 400" {
		t.Fatal("stock not saved to hdr_config.txt")
	}

	// update: the selected stock is preselected, Light comes from it,
	// weighted average as filmdb.Put
	rgb2 := [3]float64{0.25, 0.2, 0.1}
	c.saveToDB("Record base", "info", rgb2, 3, e, 4, "record", func() { closed++ })
	if d.Act != 1 || d.Names[d.Sel] != "Portra 400" || d.NameOn || !d.UpdOn || !d.AvgOn || d.Light != "CS-LITE 5000K" ||
		!strings.HasPrefix(d.Cur, "Stored: R 0.2100  G 0.1900  B 0.0800\n") || !strings.HasSuffix(d.Cur, ", 1 sample(s), "+got.Date) {
		t.Fatalf("update panel %+v", *d)
	}
	d.Avg = 1
	c.DBSaveSave()
	ref.Put(filmdb.Entry{Name: "Portra 400", R: rgb2[0], G: rgb2[1], B: rgb2[2], ISO: 250, Shutter: 16667, Aperture: 4,
		Light: "CS-LITE 5000K", Samples: 3, Source: "record"}, true)
	disk, _ = filmdb.Load(c.dbPath())
	got, w = disk.Find("Portra 400"), *ref.Find("Portra 400")
	w.Date = got.Date
	if !reflect.DeepEqual(*got, w) || got.Samples != 4 {
		t.Fatalf("average\n got %+v\nwant %+v", *got, w)
	}

	// cancel
	c.saveToDB("Save last frame 0 to DB", "info", rgb, 1, e, 4, "frame0", func() { closed++ })
	c.DBSaveCancel()
	if d.Shown || closed != 4 {
		t.Fatal("cancel")
	}
	logHas(t, h, c, "Save last frame 0 to DB: not saved")

	// update with nothing to update
	os.Remove(c.dbPath())
	c.DB = mustLoad(t, c.dbPath())
	c.Cfg.Stock = ""
	c.saveToDB("T", "i", rgb, 1, e, 4, "frame0", nil)
	if !reflect.DeepEqual(d.Names, []string{"(no stocks yet)"}) {
		t.Fatalf("names %q", d.Names)
	}
	h.messages = nil
	d.Act = 1
	c.DBSaveChanged()
	c.DBSaveSave()
	if !d.Shown || d.UpdOn || h.messages[0] != "T: There is no stock to update yet: save a new one." {
		t.Fatalf("no stock %q", h.messages)
	}
	c.DBSaveCancel()
}

func mustLoad(t *testing.T, p string) *filmdb.DB {
	db, err := filmdb.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestManageDBPanel(t *testing.T) {
	h, c := dbNew(t)
	c.DB.Put(filmdb.Entry{Name: "Portra 400", R: 0.2, G: 0.18, B: 0.07, ISO: 250, Shutter: 16667, Aperture: 4, Light: "CS-LITE",
		Crosstalk: &[9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}, CrosstalkInfo: "chart 3"}, false)
	c.DB.Save()
	c.SetStock(1)
	c.ManageDB()
	m := c.DBMan
	if !m.Shown || m.Path != c.dbPath() || !reflect.DeepEqual(m.Names, []string{"Portra 400", "Session 1"}) || m.Sel != 0 || m.Name != "Portra 400" {
		t.Fatalf("manage %+v", *m)
	}
	x := c.DB.Find("Portra 400")
	if m.Info != "R 0.2000  G 0.1800  B 0.0700\n"+filmdb.Cond(*x)+", 1 sample(s), "+x.Date+"\nLight: CS-LITE\nCrosstalk: chart matrix (chart 3)" {
		t.Fatalf("info %q", m.Info)
	}
	m.Name = "a/b"
	c.ManageRename()
	m.Name = "Session 1"
	c.ManageRename()
	if !reflect.DeepEqual(h.messages, []string{"Rename: Enter a name (1-40 characters, no quotes).", "Rename: \"Session 1\" already exists"}) {
		t.Fatalf("rename messages %q", h.messages)
	}
	m.Name = "Portra 400 NC"
	c.ManageRename()
	if c.Cfg.Stock != "Portra 400 NC" || m.Names[m.Sel] != "Portra 400 NC" || mustLoad(t, c.dbPath()).Find("Portra 400 NC") == nil {
		t.Fatalf("rename: stock %q names %q", c.Cfg.Stock, m.Names)
	}
	logHas(t, h, c, "Film-base DB: renamed Portra 400 to Portra 400 NC")
	h.confirm = false
	c.ManageDelete()
	if c.DB.Find("Portra 400 NC") == nil {
		t.Fatal("deleted without a yes")
	}
	h.confirm = true
	c.ManageDelete()
	m.Sel = 0
	c.ManageSelect()
	c.ManageDelete()
	if !reflect.DeepEqual(m.Names, []string{"(no stocks)"}) || m.Info != "The DB is empty. Use Record base or Save base to DB." || m.Name != "" {
		t.Fatalf("empty %+v", *m)
	}
	logHas(t, h, c, "Film-base DB: deleted Session 1")
	if d := mustLoad(t, c.dbPath()); len(d.Entries) != 0 || !reflect.DeepEqual(d.Hidden, []string{"Session 1"}) {
		t.Fatalf("disk %+v", d)
	}
	c.ManageClose()
	if m.Shown || c.Cfg.Stock != "" || c.StockIndex() != 0 {
		t.Fatalf("close: stock %q", c.Cfg.Stock)
	}
}

func TestSaveLastFrame0(t *testing.T) {
	h, c := dbNew(t)
	c.SaveLastFrame0()
	if h.messages[0] != "Save last frame 0 to DB: No capture yet in this session." {
		t.Fatal(h.messages)
	}
	c.capExp = camera.Exposure{ISO: 250, Shutter: 16667}
	c.camFn = 4
	good := hdrscan.Reading{Max: 0.5, Peak: 0.6, Cells: 40, RGB: [3]float64{0.4, 0.5, 0.2}}
	for _, tc := range []struct {
		res  hdrscan.Result
		r    hdrscan.Reading
		pure bool
		why  string
	}{
		{hdrscan.Result{Base1Err: "boom"}, good, false, "boom"},
		{hdrscan.Result{}, good, false, "no film base found"},
		{hdrscan.Result{Base1N: 9, Base1F0: [3]float64{0.2, 0.18, 0.07}}, hdrscan.Reading{Max: 1, Peak: 1}, false,
			"frame 0's base was clipped or too small: " + strings.Join(hdrscan.FixedWarnings(hdrscan.Reading{Max: 1, Peak: 1}), "; ")},
		{hdrscan.Result{Base1N: 9, Base1F0: [3]float64{0.2, 0.18, 0.07}, HDR: "/x/a_HDR.tiff"}, good, hdrscan.PureBase(good), ""},
	} {
		c.capReading = tc.r
		res := tc.res
		c.setLastF0(&res)
		if f := c.lastF0; f.Pure != tc.pure || (tc.why != "" && f.Why != tc.why) || f.Fn != 4 || f.Exp != c.capExp {
			t.Fatalf("%+v: %+v", tc.res, *f)
		}
	}
	if !c.lastF0.Pure {
		t.Skip("synthetic reading is not a pure base here")
	}
	if !c.Buttons().SaveF0 {
		t.Fatal("Save last frame 0 disabled")
	}
	h.messages = nil
	c.SaveLastFrame0()
	want := fmt.Sprintf("Last frame 0 (%s, f/4)\n/x/a_HDR.tiff\nFilm base scale: R 0.2000  G 0.1800  B 0.0700  (9 samples)", c.capExp)
	if !c.DBSave.Shown || c.DBSave.Info != want || c.DBSave.Title != "Save last frame 0 to DB" {
		t.Fatalf("panel %q %q", c.DBSave.Info, h.messages)
	}
}

// fileCam is the mock camera with a sensor: every capture is a copy of a
// real RAW (NSS_TEST_RAF).
type fileCam struct {
	*mockCam
	raf string
	n   atomic.Int32
}

func (f *fileCam) Capture(dir, prefix string) (string, error) {
	b, err := os.ReadFile(f.raf)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, fmt.Sprintf("%s%04d.RAF", prefix, f.n.Add(1)))
	return p, os.WriteFile(p, b, 0o644)
}

func testRAF(t *testing.T) string {
	for _, p := range filepath.SplitList(os.Getenv("NSS_TEST_RAF")) {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("set NSS_TEST_RAF to a RAW file to run this test")
	return ""
}

// Record base, Save base to DB and Save last frame 0 to DB on a camera
// whose shots are a real RAW.
func TestRecordAndSaveBaseOnCamera(t *testing.T) {
	if forked(t) {
		return
	}
	raf := testRAF(t)
	h, c := dbNew(t)
	h.scene = "x"
	c.Cfg.OutDir = t.TempDir()
	c.Cfg.Frame0 = 1
	c.Cfg.Bracket = 0
	cam := &fileCam{mockCam: newMock(), raf: raf}
	c.OpenSim = func(string, *camera.Logger) (camera.Device, error) { return cam, nil }
	c.ListDevices = func() ([]Device, error) { return nil, nil }
	if b := c.Buttons(); b.Rec || b.SaveDB || b.SaveF0 {
		t.Fatalf("idle %+v", b)
	}
	c.Connect()
	h.pump(t, "connect", 5*time.Second, c.Connected)
	if b := c.Buttons(); !b.Rec || b.SaveDB || b.SaveF0 {
		t.Fatalf("connected %+v", b)
	}
	h.messages = nil
	c.SaveBaseToDB()
	if h.messages[0] != "Save base to DB: Meter (lock) frame 0 first, or use Record base." {
		t.Fatal(h.messages)
	}

	// Record base
	c.RecordBase()
	if !c.Busy || c.Buttons().Rec || !strings.HasPrefix(c.ExpLbl, "Recording base (ISO 250, ") {
		t.Fatalf("not busy: %q", c.ExpLbl)
	}
	h.pump(t, "record base", 60*time.Second, func() bool { return !c.Busy })
	if !c.DBSave.Shown {
		t.Fatalf("no save panel: %q %q", h.messages, c.LogText())
	}
	d := c.DBSave
	t.Logf("Record base info:\n%s", d.Info)
	if d.Title != "Record base" || !strings.HasPrefix(d.Info, "Base recorded at ISO 250, ") || !strings.Contains(d.Info, "(f/4 on the lens)\nRAW kept: ") ||
		!strings.Contains(d.Info, "_BASE.RAF\nFilm base scale: R ") || d.source != "record" {
		t.Fatalf("panel %+v", *d)
	}
	if c.ExpLbl != "NOT LOCKED (Record base changed the exposure - meter again)" || c.Locked() || c.ViewWhat != "Record base RAW - live view resumes when the save dialog closes" {
		t.Fatalf("after record: %q %q", c.ExpLbl, c.ViewWhat)
	}
	if c.Cfg.Counter != 1 {
		t.Fatalf("counter %d", c.Cfg.Counter)
	}
	d.Name = "Rec stock"
	c.DBSaveSave()
	if d.Shown || c.Cfg.Stock != "Rec stock" {
		t.Fatal(h.messages)
	}
	rec := mustLoad(t, c.dbPath()).Find("Rec stock")
	if rec == nil || rec.Source != "record" || rec.ISO != 250 || rec.Aperture != 4 || rec.Samples < 1 {
		t.Fatalf("record entry %+v", rec)
	}

	// lock (Fixed frame 0), Save base to DB
	c.Meter()
	h.pump(t, "lock", 60*time.Second, func() bool { return !c.Busy })
	if !c.Locked() || c.lockFn != 4 {
		t.Fatalf("not locked: %q %q", c.ExpLbl, c.BaseLbl)
	}
	if b := c.Buttons(); !b.SaveDB {
		t.Fatalf("locked %+v", b)
	}
	c.SaveBaseToDB()
	h.pump(t, "probe base", 60*time.Second, func() bool { return !c.Busy })
	if !d.Shown || d.Title != "Save base to DB" || d.Act != 1 || d.Names[d.Sel] != "Rec stock" ||
		!strings.HasPrefix(d.Info, "Locked frame 0 (ISO ") || !strings.HasSuffix(d.Info, "\nAperture: f/4") {
		t.Fatalf("save base panel %q %q", d.Info, h.messages)
	}
	t.Logf("Save base info:\n%s", d.Info)
	c.DBSaveCancel()

	// capture: the last frame 0
	c.Capture()
	h.pump(t, "capture and merge", 300*time.Second, func() bool { return !c.Busy && !c.Working && !c.Scanning() })
	if c.LastCap == nil || c.lastF0 == nil {
		t.Fatalf("capture: %s", c.Res)
	}
	t.Logf("last frame 0: pure %v why %q", c.lastF0.Pure, c.lastF0.Why)
	if !c.Buttons().SaveF0 {
		t.Fatal("Save last frame 0 disabled after a capture")
	}
	h.messages = nil
	c.SaveLastFrame0()
	if c.lastF0.Pure != d.Shown {
		t.Fatalf("pure %v shown %v %q", c.lastF0.Pure, d.Shown, h.messages)
	}
	if d.Shown {
		c.DBSaveCancel()
	} else if h.messages[0] != "Save last frame 0 to DB: The last frame 0 cannot be used as a base record:\n"+c.lastF0.Why {
		t.Fatal(h.messages)
	}
	c.Disconnect()
}

// Record base on a camera without a sensor fails and says so.
func TestRecordBaseFailure(t *testing.T) {
	h, c := dbNew(t)
	h.scene = "x"
	c.Cfg.OutDir = t.TempDir()
	c.OpenSim = func(string, *camera.Logger) (camera.Device, error) { return newMock(), nil }
	c.ListDevices = func() ([]Device, error) { return nil, nil }
	c.Connect()
	h.pump(t, "connect", 5*time.Second, c.Connected)
	c.RecordBase()
	h.pump(t, "record base", 10*time.Second, func() bool { return !c.Busy })
	if c.DBSave.Shown || len(h.messages) != 1 || !strings.HasPrefix(h.messages[0], "Record base: Record base failed:\n\n") {
		t.Fatalf("%q", h.messages)
	}
	logHas(t, h, c, "Record base failed: ")
	c.Disconnect()
}
