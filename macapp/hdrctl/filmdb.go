package hdrctl

// Film-base DB on the Camera card (hdrdb.go on Windows): Record base, Save
// base to DB, Save last frame 0 to DB, Manage. The two Windows modal
// dialogs (saveToDB, manageDB) are in-window panels (dbview.go); their
// state and every handler are here, with the Windows texts. The DB file is
// film_base_db.json through filmdb.Path(configDir()), the same file name
// and format as on Windows.

import (
	"fmt"
	"image"
	"strings"
	"time"

	"basetool/camera"
	"basetool/filmdb"
	"basetool/hdrscan"

	"basetool/macapp/appstate"
)

// leakPct is u.leak() on Windows (the light-leak rejection of the base).
const leakPct = 20

// DBSave is the "save to film-base DB" dialog (saveToDB).
type DBSave struct {
	Shown       bool
	Title, Info string
	ActItems    []string
	Act         int // 0 = new stock, 1 = update an existing one
	Names       []string
	Sel         int
	AvgItems    []string
	Avg         int
	Name, Light string // the two LineEdits (the view writes them back)
	Seq         int    // bumps when Name / Light are set here
	Cur         string // curLbl
	NameOn      bool
	UpdOn       bool
	AvgOn       bool

	rgb    [3]float64
	n      int
	e      camera.Exposure
	fnum   float64
	source string
	done   func()
}

// DBManage is the "Film-base DB" dialog (manageDB).
type DBManage struct {
	Shown bool
	Path  string
	Names []string
	Sel   int
	Info  string
	Name  string // the Name LineEdit (the view writes it back)
	Seq   int    // bumps when Name is set here
}

func (c *Ctl) dbPath() string { return filmdb.Path(configDir()) }

// loadDB reads film_base_db.json and imports the Film base page's presets that are
// not in it yet.
func (c *Ctl) loadDB() {
	db, err := filmdb.Load(c.dbPath())
	if err != nil {
		c.say("Film-base DB: " + err.Error())
	}
	var ps []filmdb.Preset
	for _, p := range loadConfig().Presets {
		ps = append(ps, filmdb.Preset{Name: p.Name, R: p.R, G: p.G, B: p.B})
	}
	if n := db.Import(ps); n > 0 {
		if err := db.Save(); err != nil {
			c.say("Film-base DB: " + err.Error())
		} else {
			c.say(fmt.Sprintf("Film-base DB: imported %d Film base preset(s) into %s", n, c.dbPath()))
		}
	}
	c.DB = db
}

// refreshStocks reloads the dropdown and selects name ("" keeps none).
func (c *Ctl) refreshStocks(name string) {
	c.SetStock(c.stockIndexOf(name))
}

func (c *Ctl) stockIndexOf(name string) int {
	for i, n := range c.StockItems() {
		if i > 0 && strings.EqualFold(n, name) {
			return i
		}
	}
	return 0
}

// stock is the selected entry (nil = None).
func (c *Ctl) stock() *filmdb.Entry {
	if c.DB == nil || c.Cfg.Stock == "" {
		return nil
	}
	return c.DB.Find(c.Cfg.Stock)
}

func (c *Ctl) tol() float64 { return appstate.Tol(c.Cfg) }

// DBOpen reports whether a film-base DB panel covers the window.
func (c *Ctl) DBOpen() bool { return c.DBSave.Shown || c.DBMan.Shown }

// ---------- save to DB ----------

// saveToDB asks "new stock or update an existing one" and stores rgb. done
// runs when the panel closes (saved or not), or at once when it does not
// open.
func (c *Ctl) saveToDB(title, info string, rgb [3]float64, n int, e camera.Exposure, fnum float64, source string, done func()) {
	if done == nil {
		done = func() {}
	}
	if c.DB == nil {
		c.loadDB()
	}
	if !(rgb[0] > 0 && rgb[1] > 0 && rgb[2] > 0) {
		c.Host.Message(title, "No usable base values to save.")
		done()
		return
	}
	if fnum <= 0 {
		fnum = filmdb.ApertureUnknown // the lens reports no aperture (manual/adapted lens)
	}
	names := c.DB.Names()
	act, sel := 0, 0
	if s := c.Cfg.Stock; s != "" {
		for i, nm := range names {
			if strings.EqualFold(nm, s) {
				act, sel = 1, i
			}
		}
	}
	if len(names) == 0 {
		names = []string{"(no stocks yet)"}
	}
	d := c.DBSave
	*d = DBSave{Shown: true, Title: title, Info: info,
		ActItems: []string{"Save as a new film stock", "Update an existing film stock"}, Act: act,
		Names: names, Sel: sel, AvgItems: []string{"Replace the stored values", "Average with the stored values (weighted)"},
		Name: c.Cfg.Roll, Seq: d.Seq + 1,
		rgb: rgb, n: n, e: e, fnum: fnum, source: source, done: done}
	c.DBSaveChanged()
}

func (d *DBSave) updText() string {
	if d.Sel < 0 || d.Sel >= len(d.Names) {
		return ""
	}
	return d.Names[d.Sel]
}

// DBSaveChanged is upd(): the Action / Existing stock choice changed.
func (c *Ctl) DBSaveChanged() {
	d := c.DBSave
	isNew := d.Act == 0
	d.NameOn = isNew
	d.UpdOn = !isNew && c.DB.Find(d.updText()) != nil
	d.AvgOn = !isNew
	d.Cur = ""
	if !isNew {
		if x := c.DB.Find(d.updText()); x != nil {
			d.Cur = "Stored: " + rgbText([3]float64{x.R, x.G, x.B}) + "\n" + filmdb.Cond(*x) + fmt.Sprintf(", %d sample(s), %s", x.Samples, x.Date)
			if d.Light == "" {
				d.Light = x.Light
				d.Seq++
			}
		}
	}
}

// DBSaveSave is the Save button (validation runs before the panel closes,
// as in the Windows Save handler).
func (c *Ctl) DBSaveSave() {
	d := c.DBSave
	if !d.Shown {
		return
	}
	title, rgb, e := d.Title, d.rgb, d.e
	ent := filmdb.Entry{R: rgb[0], G: rgb[1], B: rgb[2], ISO: e.ISO, Shutter: e.Shutter, Aperture: d.fnum,
		Light: strings.TrimSpace(d.Light), Samples: max(1, d.n), Source: d.source}
	average := false
	if d.Act == 0 {
		nm, ok := cleanName(d.Name)
		if !ok {
			c.Host.Message(title, "Enter a name (1-40 characters, no quotes).")
			return
		}
		if c.DB.Find(nm) != nil {
			c.Host.Message(title, "\""+nm+"\" already exists. Choose \"Update an existing film stock\" or another name.")
			return
		}
		ent.Name = nm
	} else {
		x := c.DB.Find(d.updText())
		if x == nil {
			c.Host.Message(title, "There is no stock to update yet: save a new one.")
			return
		}
		ent.Name = x.Name
		average = d.Avg == 1
	}
	got, err := c.DB.Put(ent, average)
	if err != nil {
		c.Host.Message(title, err.Error())
		return
	}
	if err := c.DB.Save(); err != nil {
		c.Host.Message(title, "Could not write "+c.dbPath()+":\n"+err.Error())
		return
	}
	done := d.done
	d.Shown, d.done = false, nil
	c.say("Film-base DB: saved " + entryText(got))
	c.refreshStocks(got.Name)
	done()
}

// DBSaveCancel is Cancel (or Escape).
func (c *Ctl) DBSaveCancel() {
	d := c.DBSave
	if !d.Shown {
		return
	}
	done := d.done
	d.Shown, d.done = false, nil
	c.say(d.Title + ": not saved")
	done()
}

// ---------- Manage ----------

func (c *Ctl) manNames() []string {
	n := c.DB.Names()
	if len(n) == 0 {
		return []string{"(no stocks)"}
	}
	return n
}

// ManageDB is the Manage button (manageDB): rename or delete stocks.
func (c *Ctl) ManageDB() {
	if c.DB == nil {
		c.loadDB()
	}
	m := c.DBMan
	m.Path, m.Names, m.Sel, m.Shown = c.dbPath(), c.manNames(), 0, true
	for k, n := range m.Names {
		if strings.EqualFold(n, c.Cfg.Stock) {
			m.Sel = k
		}
	}
	c.manShow()
}

func (m *DBManage) cbText() string {
	if m.Sel < 0 || m.Sel >= len(m.Names) {
		return ""
	}
	return m.Names[m.Sel]
}

func (c *Ctl) manShow() {
	m := c.DBMan
	if x := c.DB.Find(m.cbText()); x != nil {
		m.Info = rgbText([3]float64{x.R, x.G, x.B}) + "\n" + filmdb.Cond(*x) + fmt.Sprintf(", %d sample(s), %s", x.Samples, x.Date) + map[bool]string{true: "\nLight: " + x.Light, false: ""}[x.Light != ""] + map[bool]string{true: "\nCrosstalk: chart matrix (" + x.CrosstalkInfo + ")", false: ""}[x.Crosstalk != nil]
		m.Name = x.Name
	} else {
		m.Info = "The DB is empty. Use Record base or Save base to DB."
		m.Name = ""
	}
	m.Seq++
}

func (c *Ctl) manReload(sel string) {
	m := c.DBMan
	m.Names, m.Sel = c.manNames(), 0
	for k, n := range m.Names {
		if strings.EqualFold(n, sel) {
			m.Sel = k
		}
	}
	c.manShow()
}

// ManageSelect is the Film stock combo of the Manage panel.
func (c *Ctl) ManageSelect() { c.manShow() }

// ManageRename is Rename.
func (c *Ctl) ManageRename() {
	m := c.DBMan
	from := m.cbText()
	if c.DB.Find(from) == nil {
		return
	}
	to, ok := cleanName(m.Name)
	if !ok {
		c.Host.Message("Rename", "Enter a name (1-40 characters, no quotes).")
		return
	}
	if err := c.DB.Rename(from, to); err != nil {
		c.Host.Message("Rename", err.Error())
		return
	}
	if err := c.DB.Save(); err != nil {
		c.Host.Message("Rename", err.Error())
	}
	c.say("Film-base DB: renamed " + from + " to " + to)
	if strings.EqualFold(c.Cfg.Stock, from) {
		c.Cfg.Stock = to
	}
	c.manReload(to)
}

// ManageDelete is Delete (after a Yes / No question).
func (c *Ctl) ManageDelete() {
	nm := c.DBMan.cbText()
	if c.DB.Find(nm) == nil {
		return
	}
	c.Host.Confirm("Delete", "Delete \""+nm+"\" from the film-base DB?", func(ok bool) {
		if !ok || c.DB.Find(nm) == nil {
			return
		}
		c.DB.Delete(nm)
		if err := c.DB.Save(); err != nil {
			c.Host.Message("Delete", err.Error())
		}
		c.say("Film-base DB: deleted " + nm)
		c.manReload("")
	})
}

// ManageClose is Close (or Escape).
func (c *Ctl) ManageClose() {
	if !c.DBMan.Shown {
		return
	}
	c.DBMan.Shown = false
	c.refreshStocks(c.Cfg.Stock)
}

// ---------- Record base / Save base / Save last frame 0 ----------

// RecordBase shoots a standalone Fixed frame 0 at ISO 250, 1/60 s (f/4 on
// the lens), keeps the RAW as <roll>_..._BASE.RAF and offers to save it.
func (c *Ctl) RecordBase() {
	if c.dev == nil || c.Busy || c.sess == nil {
		return
	}
	dir, err := c.rollDir()
	if err != nil {
		c.Host.Message("Record base", err.Error())
		return
	}
	c.Save()
	c.pauseLiveView()
	c.Busy = true
	s := c.sess
	s.Target = appstate.Target(c.Cfg)
	s.Slowest = appstate.Slowest(c.Cfg)
	if c.Cfg.AreaMode == 1 {
		s.ROI = c.ROI
	} else {
		s.ROI = hdrscan.Rect{}
	}
	roll, last := c.Cfg.Roll, c.Cfg.Counter
	ent, tol := c.stock(), c.tol()
	s.Fixed = c.fixedShutter()
	shl := camera.ShutterLabel(s.Fixed)
	c.ExpLbl = fmt.Sprintf("Recording base (ISO %d, %s) ...", hdrscan.RefISO, shl)
	c.worker.do(func() {
		c.Log.Printf("==== Record base: ISO %d, %s ====", hdrscan.RefISO, shl)
		rec, n, err := s.RecordBase(dir, roll, hdrscan.RefISO, last, leakPct)
		var fn float64
		if sr, ok := s.Dev.(camera.StatusReader); ok {
			if _, f, e := sr.Status(); e == nil {
				fn = f
			}
		}
		lock := lockedCopy(s)
		var prev image.Image
		if s.LastProbe != nil {
			prev = hdrscan.Preview(s.LastProbe)
		}
		c.Host.Post(func() {
			c.Busy = false
			if n > 0 {
				c.Cfg.Counter = n
				c.Save()
			}
			if c.sess == s {
				c.lock = lock
			}
			if lock == nil {
				c.clearLocked()
				c.ExpLbl = "NOT LOCKED (Record base changed the exposure - meter again)"
			} else {
				c.ExpLbl = ""
			}
			if prev != nil {
				c.setImage(prev, "Record base RAW - live view resumes when the save dialog closes")
			}
			if err != nil {
				time.AfterFunc(4*time.Second, func() { c.Host.Post(c.resumeLiveView) })
				c.say("Record base failed: " + err.Error())
				c.Host.Message("Record base", "Record base failed:\n\n"+err.Error())
				return
			}
			c.BaseNorm = rec.Reading.Norm
			c.showBase(rec.Reading)
			info := fmt.Sprintf("Base recorded at %s (%s on the lens)\nRAW kept: %s\nFilm base scale: %s  (%d samples)\nBrightest channel %.3f FS, peak %.3f",
				rec.Exposure, fnText(fn), rec.Raw, rgbText(rec.Base1), rec.Base1N, rec.Reading.Max, rec.Reading.Peak)
			if rec.EXIF != "" {
				info += "\n" + rec.EXIF
			}
			for _, w := range rec.Warnings {
				info += "\nWARNING: " + w
			}
			if t, _ := compareText(ent, rec.Base1, rec.Exposure, tol); t != "" {
				info += "\n" + t
			}
			c.say(strings.ReplaceAll(info, "\n", " | "))
			// live view stays paused (the base RAW stays on screen) until
			// the save panel is closed
			c.saveToDB("Record base", info, rec.Base1, rec.Base1N, rec.Exposure, fn, "record", c.resumeLiveView)
		})
	})
}

// SaveBaseToDB stores the locked frame 0's base (the metering probe, the
// same exposure as frame 0). Windows reads the probe on the UI thread; here
// it is read off the UI goroutine with the controls busy meanwhile.
func (c *Ctl) SaveBaseToDB() {
	if c.sess == nil || !c.Locked() || c.Busy {
		c.Host.Message("Save base to DB", "Meter (lock) frame 0 first, or use Record base.")
		return
	}
	s, e := c.sess, *c.lock
	warns := hdrscan.FixedWarnings(s.Reading)
	fn := c.lockFn
	if fn <= 0 {
		fn = c.camFn
	}
	c.Busy = true
	go func() {
		rgb, n, err := s.ProbeBase(leakPct)
		c.Host.Post(func() {
			c.Busy = false
			if err != nil {
				c.Host.Message("Save base to DB", "No usable base on the locked probe: "+err.Error())
				return
			}
			info := fmt.Sprintf("Locked frame 0 (%s), metering probe\nFilm base scale: %s  (%d samples)", e, rgbText(rgb), n)
			for _, w := range warns {
				info += "\nWARNING: " + w
			}
			info += "\nAperture: " + fnText(fn)
			c.saveToDB("Save base to DB", info, rgb, n, e, fn, "frame0", nil)
		})
	}()
}

// SaveLastFrame0 stores the last capture's frame-0 base without reshooting.
func (c *Ctl) SaveLastFrame0() {
	f := c.lastF0
	if f == nil {
		c.Host.Message("Save last frame 0 to DB", "No capture yet in this session.")
		return
	}
	if !f.Pure {
		c.Host.Message("Save last frame 0 to DB", "The last frame 0 cannot be used as a base record:\n"+f.Why)
		return
	}
	info := fmt.Sprintf("Last frame 0 (%s, %s)\n%s\nFilm base scale: %s  (%d samples)", f.Exp, fnText(f.Fn), f.Src, rgbText(f.RGB), f.N)
	c.saveToDB("Save last frame 0 to DB", info, f.RGB, f.N, f.Exp, f.Fn, "frame0", nil)
}

// setLastF0 remembers the frame-0 base of a finished capture.
func (c *Ctl) setLastF0(res *hdrscan.Result) {
	f := &frame0Base{RGB: res.Base1F0, N: res.Base1N, Exp: c.capExp, Src: res.HDR, Fn: c.camFn}
	switch {
	case res.Base1Err != "":
		f.Why = res.Base1Err
	case res.Base1N == 0:
		f.Why = "no film base found"
	case !hdrscan.PureBase(c.capReading):
		f.Why = "frame 0's base was clipped or too small: " + strings.Join(hdrscan.FixedWarnings(c.capReading), "; ")
	default:
		f.Pure = true
	}
	c.lastF0 = f
}

// ---------- Fixed frame 0 film-stock comparison ----------

// stockRecheck redoes the film stock line of a fixed lock when the stock is
// changed after metering (QA L5), from the locked probe; nothing is reshot.
func (c *Ctl) stockRecheck() {
	if c.sess == nil || !c.Locked() || c.lockBase == "" {
		return
	}
	s, e, base := c.sess, *c.lock, c.lockBase
	ent, tol := c.stock(), c.tol()
	if ent == nil {
		c.BaseLbl = base + "\nFilm stock: None (no DB check)"
		return
	}
	c.worker.do(func() {
		var t string
		if b1, _, err := s.ProbeBase(leakPct); err != nil {
			t = ent.Name + ": no base to compare (" + err.Error() + ")"
		} else {
			t, _ = compareText(ent, b1, e, tol)
		}
		c.Host.Post(func() {
			if c.sess == s && c.lockBase == base {
				c.BaseLbl = base + "\n" + t
			}
		})
	})
}

// captureCompare is the film-stock line of a capture result (process on
// Windows): a clean Fixed frame 0 is compared with the selected stock.
func (c *Ctl) captureCompare() string {
	if f := c.lastF0; f != nil && f.Pure {
		if t, _ := compareText(c.stock(), f.RGB, f.Exp, c.tol()); t != "" && appstate.Fixed(c.Cfg) {
			return "\n" + t
		}
	}
	return ""
}
