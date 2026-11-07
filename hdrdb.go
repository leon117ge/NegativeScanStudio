package main

// Film-base database UI for the HDR scan page (v0.63): film stock dropdown, Fixed
// frame-0 comparison, Record base, Save base / last frame 0 to DB, Manage.

import (
	"fmt"
	"strings"
	"time"

	"basetool/camera"
	"basetool/filmdb"
	"basetool/hdrscan"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

const stockNone = "None"

// frame0Base is the clear base of the last captured frame 0.
type frame0Base struct {
	RGB  [3]float64
	N    int
	Exp  camera.Exposure
	Pure bool   // frame 0 was a usable pure-base exposure (no clipping, enough base)
	Why  string // why not
	Src  string
	Fn   float64 // f-number the camera reported at capture (0 = unknown)
}

func (u *HDRUI) dbPath() string { return filmdb.Path(configDir()) }

// loadDB reads film_base_db.json and imports the Film base page's presets that are
// not in it yet.
func (u *HDRUI) loadDB() {
	db, err := filmdb.Load(u.dbPath())
	if err != nil {
		u.say("Film-base DB: " + err.Error())
	}
	var ps []filmdb.Preset
	for _, p := range loadConfig().Presets {
		ps = append(ps, filmdb.Preset{Name: p.Name, R: p.R, G: p.G, B: p.B})
	}
	if n := db.Import(ps); n > 0 {
		if err := db.Save(); err != nil {
			u.say("Film-base DB: " + err.Error())
		} else {
			u.say(fmt.Sprintf("Film-base DB: imported %d Film base preset(s) into %s", n, u.dbPath()))
		}
	}
	u.db = db
}

func (u *HDRUI) stockItems() []string {
	items := []string{stockNone}
	if u.db != nil {
		items = append(items, u.db.Names()...)
	}
	return items
}

func (u *HDRUI) stockIndex(name string) int {
	for i, n := range u.stockItems() {
		if i > 0 && strings.EqualFold(n, name) {
			return i
		}
	}
	return 0
}

// refreshStocks reloads the dropdown and selects name ("" keeps none).
func (u *HDRUI) refreshStocks(name string) {
	if u.stockCB == nil {
		return
	}
	u.stockCB.SetModel(u.stockItems())
	u.stockCB.SetCurrentIndex(u.stockIndex(name))
	u.save()
}

// stock is the selected entry (nil = None).
func (u *HDRUI) stock() *filmdb.Entry {
	if u.db == nil || u.cfg.Stock == "" {
		return nil
	}
	return u.db.Find(u.cfg.Stock)
}

func (u *HDRUI) tol() float64 {
	i := u.cfg.StockTol
	if i < 0 || i >= len(stockTolVals) {
		i = stockTolDefault
	}
	return stockTolVals[i]
}

func (u *HDRUI) fixed() bool { return u.cfg.Frame0 == 1 }

func (u *HDRUI) fixedISO() int {
	i := u.cfg.ISO
	if i < 0 || i >= len(isoVals) {
		i = 0
	}
	return isoVals[i]
}

// rollDir is <Save to>\<roll> (created); the date of an empty roll name is
// the session's first capture.
func (u *HDRUI) rollDir() (string, error) {
	if u.sessFirst.IsZero() {
		u.sessFirst = time.Now()
	}
	return hdrscan.RollDir(u.outDir(), u.cfg.Roll, u.sessFirst)
}

func rgbText(v [3]float64) string { return fmt.Sprintf("R %.4f  G %.4f  B %.4f", v[0], v[1], v[2]) }

func entryText(e *filmdb.Entry) string {
	return fmt.Sprintf("%s: %s, %s, %d sample(s), %s", e.Name, rgbText([3]float64{e.R, e.G, e.B}), filmdb.Cond(*e), e.Samples, e.Date)
}

// darkDialog themes a dialog like the main window.
func darkDialog(d *walk.Dialog) {
	d.SetBackground(bgBrush)
	bgOfHwnd[d.Handle()] = cBg
	darkTitleBar(d.Handle())
	applyDark(d)
}

// saveToDB asks "new stock or update an existing one" and stores rgb.
func (u *HDRUI) saveToDB(title, info string, rgb [3]float64, n int, e camera.Exposure, fnum float64, source string) {
	if u.db == nil {
		u.loadDB()
	}
	if !(rgb[0] > 0 && rgb[1] > 0 && rgb[2] > 0) {
		walk.MsgBox(u.win(), title, "No usable base values to save.", walk.MsgBoxIconWarning)
		return
	}
	if fnum <= 0 {
		fnum = filmdb.ApertureUnknown // the lens reports no aperture (manual/adapted lens)
	}
	names := u.db.Names()
	var dlg *walk.Dialog
	var actCB, updCB, avgCB *walk.ComboBox
	var nameEd, lightEd *walk.LineEdit
	var okPB, cancelPB *walk.PushButton
	var curLbl *walk.Label
	actItems := []string{"Save as a new film stock", "Update an existing film stock"}
	act := 0
	sel := 0
	if s := u.cfg.Stock; s != "" {
		for i, nm := range names {
			if strings.EqualFold(nm, s) {
				act, sel = 1, i
			}
		}
	}
	if len(names) == 0 {
		names = []string{"(no stocks yet)"}
	}
	upd := func() {
		isNew := actCB.CurrentIndex() == 0
		nameEd.SetEnabled(isNew)
		updCB.SetEnabled(!isNew && u.db.Find(updCB.Text()) != nil)
		avgCB.SetEnabled(!isNew)
		curLbl.SetText("")
		if !isNew {
			if x := u.db.Find(updCB.Text()); x != nil {
				curLbl.SetText("Stored: " + rgbText([3]float64{x.R, x.G, x.B}) + "\n" + filmdb.Cond(*x) + fmt.Sprintf(", %d sample(s), %s", x.Samples, x.Date))
				if lightEd.Text() == "" {
					lightEd.SetText(x.Light)
				}
			}
		}
	}
	// Validation runs inside the Save handler: a walk Dialog cannot be Run a
	// second time once it has closed (v0.63 crashed with "send on closed
	// channel" after the "Enter a name" message). Every value is read while
	// the dialog is still alive.
	var saved *filmdb.Entry
	onSave := func() {
		defer u.guard("Save to film-base DB")
		owner := walk.Form(dlg)
		ent := filmdb.Entry{R: rgb[0], G: rgb[1], B: rgb[2], ISO: e.ISO, Shutter: e.Shutter, Aperture: fnum,
			Light: strings.TrimSpace(lightEd.Text()), Samples: max(1, n), Source: source}
		average := false
		if actCB.CurrentIndex() == 0 {
			nm, ok := cleanName(nameEd.Text())
			if !ok {
				walk.MsgBox(owner, title, "Enter a name (1-40 characters, no quotes).", walk.MsgBoxIconInformation)
				nameEd.SetFocus()
				return
			}
			if u.db.Find(nm) != nil {
				walk.MsgBox(owner, title, "\""+nm+"\" already exists. Choose \"Update an existing film stock\" or another name.", walk.MsgBoxIconInformation)
				return
			}
			ent.Name = nm
		} else {
			x := u.db.Find(updCB.Text())
			if x == nil {
				walk.MsgBox(owner, title, "There is no stock to update yet: save a new one.", walk.MsgBoxIconInformation)
				return
			}
			ent.Name = x.Name
			average = avgCB.CurrentIndex() == 1
		}
		got, err := u.db.Put(ent, average)
		if err != nil {
			walk.MsgBox(owner, title, err.Error(), walk.MsgBoxIconWarning)
			return
		}
		if err := u.db.Save(); err != nil {
			walk.MsgBox(owner, title, "Could not write "+u.dbPath()+":\n"+err.Error(), walk.MsgBoxIconError)
			return
		}
		saved = got
		dlg.Accept()
	}
	err := Dialog{
		AssignTo: &dlg, Title: title, DefaultButton: &okPB, CancelButton: &cancelPB,
		MinSize: Size{Width: 460}, Layout: Grid{Columns: 2, Spacing: 6, Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
		Children: []Widget{
			Label{ColumnSpan: 2, Text: info},
			Label{Text: "Action"},
			ComboBox{AssignTo: &actCB, Model: actItems, CurrentIndex: act, OnCurrentIndexChanged: func() { upd() }},
			Label{Text: "New stock name"},
			LineEdit{AssignTo: &nameEd, CueBanner: "e.g. Portra 400", Text: u.cfg.Roll},
			Label{Text: "Existing stock"},
			ComboBox{AssignTo: &updCB, Model: names, CurrentIndex: sel, OnCurrentIndexChanged: func() { upd() }},
			Label{Text: "Update mode"},
			ComboBox{AssignTo: &avgCB, Model: []string{"Replace the stored values", "Average with the stored values (weighted)"}, CurrentIndex: 0},
			Label{Text: ""},
			Label{AssignTo: &curLbl, Text: "\n", MinSize: Size{Height: 30}},
			Label{Text: "Light source"},
			LineEdit{AssignTo: &lightEd, CueBanner: "optional, e.g. CS-LITE 5000K"},
			Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true}, Children: []Widget{
				HSpacer{},
				PushButton{AssignTo: &okPB, Text: "Save", MinSize: Size{Width: 90}, OnClicked: func() { onSave() }},
				PushButton{AssignTo: &cancelPB, Text: "Cancel", MinSize: Size{Width: 90}, OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Create(u.win())
	if err != nil {
		walk.MsgBox(u.win(), title, err.Error(), walk.MsgBoxIconError)
		return
	}
	darkDialog(dlg)
	upd()
	dlg.Run()
	if saved == nil {
		u.say(title + ": not saved")
		return
	}
	u.say("Film-base DB: saved " + entryText(saved))
	u.refreshStocks(saved.Name)
}

// manageDB renames or deletes stocks.
func (u *HDRUI) manageDB() {
	if u.db == nil {
		u.loadDB()
	}
	var dlg *walk.Dialog
	var cb *walk.ComboBox
	var nameEd *walk.LineEdit
	var info *walk.Label
	var closePB *walk.PushButton
	names := func() []string {
		n := u.db.Names()
		if len(n) == 0 {
			return []string{"(no stocks)"}
		}
		return n
	}
	show := func() {
		if x := u.db.Find(cb.Text()); x != nil {
			info.SetText(rgbText([3]float64{x.R, x.G, x.B}) + "\n" + filmdb.Cond(*x) + fmt.Sprintf(", %d sample(s), %s", x.Samples, x.Date) + map[bool]string{true: "\nLight: " + x.Light, false: ""}[x.Light != ""] + map[bool]string{true: "\nCrosstalk: chart matrix (" + x.CrosstalkInfo + ")", false: ""}[x.Crosstalk != nil])
			nameEd.SetText(x.Name)
		} else {
			info.SetText("The DB is empty. Use Record base or Save base to DB.")
			nameEd.SetText("")
		}
	}
	reload := func(sel string) {
		cb.SetModel(names())
		i := 0
		for k, n := range names() {
			if strings.EqualFold(n, sel) {
				i = k
			}
		}
		cb.SetCurrentIndex(i)
		show()
	}
	start := 0
	for k, n := range names() {
		if strings.EqualFold(n, u.cfg.Stock) {
			start = k
		}
	}
	err := Dialog{
		AssignTo: &dlg, Title: "Film-base DB", CancelButton: &closePB,
		MinSize: Size{Width: 460}, Layout: Grid{Columns: 2, Spacing: 6, Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}},
		Children: []Widget{
			Label{ColumnSpan: 2, Text: u.dbPath()},
			Label{Text: "Film stock"},
			ComboBox{AssignTo: &cb, Model: names(), CurrentIndex: start, OnCurrentIndexChanged: func() { show() }},
			Label{Text: ""},
			Label{AssignTo: &info, Text: "\n\n", MinSize: Size{Height: 44}},
			Label{Text: "Name"},
			LineEdit{AssignTo: &nameEd},
			Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true}, Children: []Widget{
				PushButton{Text: "Rename", MinSize: Size{Width: 90}, OnClicked: func() {
					from := cb.Text()
					if u.db.Find(from) == nil {
						return
					}
					to, ok := cleanName(nameEd.Text())
					if !ok {
						walk.MsgBox(dlg, "Rename", "Enter a name (1-40 characters, no quotes).", walk.MsgBoxIconInformation)
						return
					}
					if err := u.db.Rename(from, to); err != nil {
						walk.MsgBox(dlg, "Rename", err.Error(), walk.MsgBoxIconWarning)
						return
					}
					if err := u.db.Save(); err != nil {
						walk.MsgBox(dlg, "Rename", err.Error(), walk.MsgBoxIconError)
					}
					u.say("Film-base DB: renamed " + from + " to " + to)
					if strings.EqualFold(u.cfg.Stock, from) {
						u.cfg.Stock = to
					}
					reload(to)
				}},
				PushButton{Text: "Delete", MinSize: Size{Width: 90}, OnClicked: func() {
					nm := cb.Text()
					if u.db.Find(nm) == nil {
						return
					}
					if walk.MsgBox(dlg, "Delete", "Delete \""+nm+"\" from the film-base DB?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
						return
					}
					u.db.Delete(nm)
					if err := u.db.Save(); err != nil {
						walk.MsgBox(dlg, "Delete", err.Error(), walk.MsgBoxIconError)
					}
					u.say("Film-base DB: deleted " + nm)
					reload("")
				}},
				HSpacer{},
				PushButton{AssignTo: &closePB, Text: "Close", MinSize: Size{Width: 90}, OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Create(u.win())
	if err != nil {
		walk.MsgBox(u.win(), "Film-base DB", err.Error(), walk.MsgBoxIconError)
		return
	}
	darkDialog(dlg)
	show()
	dlg.Run()
	u.refreshStocks(u.cfg.Stock)
}

// compareText checks a frame-0 base against the selected stock.
func compareText(ent *filmdb.Entry, rgb [3]float64, e camera.Exposure, tol float64) (string, bool) {
	if ent == nil {
		return "", true
	}
	c := filmdb.Compare(*ent, rgb, e.ISO, e.Shutter, tol)
	return ent.Name + ": " + c.String(), c.OK
}

// recordBase shoots a standalone Fixed frame 0 at ISO 250, 1/60 s (f/4 on
// the lens), keeps the RAW as <roll>_..._BASE.RAF and offers to save it.
func (u *HDRUI) recordBase() {
	if u.dev == nil || u.busy || u.sess == nil {
		return
	}
	dir, err := u.rollDir()
	if err != nil {
		walk.MsgBox(u.win(), "Record base", err.Error(), walk.MsgBoxIconError)
		return
	}
	u.save()
	u.pauseLiveView()
	u.busy = true
	u.updateButtons()
	s := u.sess
	s.Target = u.target()
	s.Slowest = slowestVals[u.cfg.Slowest]
	if u.cfg.AreaMode == 1 {
		s.ROI = u.roi
	} else {
		s.ROI = hdrscan.Rect{}
	}
	roll, last, leak := u.cfg.Roll, u.cfg.Counter, u.leak()
	ent, tol := u.stock(), u.tol()
	s.Fixed = u.fixedShutter()
	shl := camera.ShutterLabel(s.Fixed)
	setStatus(u.expLbl, fmt.Sprintf("Recording base (ISO %d, %s) ...", hdrscan.RefISO, shl))
	u.worker.do(func() {
		defer u.guard("Record base (camera)")
		u.log.Printf("==== Record base: ISO %d, %s ====", hdrscan.RefISO, shl)
		rec, n, err := s.RecordBase(dir, roll, hdrscan.RefISO, last, leak)
		var fn float64
		if sr, ok := s.Dev.(camera.StatusReader); ok {
			if _, f, e := sr.Status(); e == nil {
				fn = f
			}
		}
		u.ui(func() {
			defer u.guard("Record base (result)")
			u.busy = false
			if n > 0 {
				u.cfg.Counter = n
				u.save()
			}
			if s.Locked == nil {
				u.clearLocked()
				setStatus(u.expLbl, "NOT LOCKED (Record base changed the exposure - meter again)")
			} else {
				setStatus(u.expLbl, "")
			}
			if s.LastProbe != nil {
				u.setImage(hdrscan.Preview(s.LastProbe), "Record base RAW - live view resumes when the save dialog closes")
			}
			u.updateButtons()
			if err != nil {
				time.AfterFunc(4*time.Second, func() { u.ui(u.resumeLiveView) })
				u.say("Record base failed: " + err.Error())
				walk.MsgBox(u.win(), "Record base", "Record base failed:\n\n"+err.Error(), walk.MsgBoxIconWarning)
				return
			}
			u.baseNorm = rec.Reading.Norm
			u.showBase(rec.Reading)
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
			u.say(strings.ReplaceAll(info, "\n", " | "))
			// live view stays paused (the base RAW stays on screen) until
			// the save dialog is closed
			defer u.resumeLiveView()
			u.saveToDB("Record base", info, rec.Base1, rec.Base1N, rec.Exposure, fn, "record")
		})
	})
}

// fnText formats the f-number the camera reported; 0 means the lens reports
// none (manual or adapted lens).
func fnText(f float64) string {
	if f > 0 {
		return fmt.Sprintf("f/%g", f)
	}
	return "aperture unknown (lens reports none)"
}

// saveBaseToDB stores the locked frame 0's base (the metering probe, the
// same exposure as frame 0).
func (u *HDRUI) saveBaseToDB() {
	if u.sess == nil || u.sess.Locked == nil || u.busy {
		walk.MsgBox(u.win(), "Save base to DB", "Meter (lock) frame 0 first, or use Record base.", walk.MsgBoxIconInformation)
		return
	}
	s := u.sess
	rgb, n, err := s.ProbeBase(u.leak())
	if err != nil {
		walk.MsgBox(u.win(), "Save base to DB", "No usable base on the locked probe: "+err.Error(), walk.MsgBoxIconWarning)
		return
	}
	e := *s.Locked
	info := fmt.Sprintf("Locked frame 0 (%s), metering probe\nFilm base scale: %s  (%d samples)", e, rgbText(rgb), n)
	for _, w := range hdrscan.FixedWarnings(s.Reading) {
		info += "\nWARNING: " + w
	}
	fn := u.lockFn
	if fn <= 0 {
		fn = u.camFn
	}
	info += "\nAperture: " + fnText(fn)
	u.saveToDB("Save base to DB", info, rgb, n, e, fn, "frame0")
}

// saveLastFrame0 stores the last capture's frame-0 base without reshooting.
func (u *HDRUI) saveLastFrame0() {
	f := u.lastF0
	if f == nil {
		walk.MsgBox(u.win(), "Save last frame 0 to DB", "No capture yet in this session.", walk.MsgBoxIconInformation)
		return
	}
	if !f.Pure {
		walk.MsgBox(u.win(), "Save last frame 0 to DB", "The last frame 0 cannot be used as a base record:\n"+f.Why, walk.MsgBoxIconWarning)
		return
	}
	info := fmt.Sprintf("Last frame 0 (%s, %s)\n%s\nFilm base scale: %s  (%d samples)", f.Exp, fnText(f.Fn), f.Src, rgbText(f.RGB), f.N)
	u.saveToDB("Save last frame 0 to DB", info, f.RGB, f.N, f.Exp, f.Fn, "frame0")
}

// setLastF0 remembers the frame-0 base of a finished capture.
func (u *HDRUI) setLastF0(res *hdrscan.Result) {
	f := &frame0Base{RGB: res.Base1F0, N: res.Base1N, Exp: u.capExp, Src: res.HDR, Fn: u.camFn}
	switch {
	case res.Base1Err != "":
		f.Why = res.Base1Err
	case res.Base1N == 0:
		f.Why = "no film base found"
	case !hdrscan.PureBase(u.capReading):
		f.Why = "frame 0's base was clipped or too small: " + strings.Join(hdrscan.FixedWarnings(u.capReading), "; ")
	default:
		f.Pure = true
	}
	u.lastF0 = f
}

// dustCLI writes <stem>_dust.tiff for every file; the summary goes to
// stdout (when run from a console) and to a message box.
func dustCLI(level string, files []string) int {
	l, ok := hdrscan.ParseDust(level)
	if !ok || l == hdrscan.DustOff {
		walk.MsgBox(nil, appVersion, "Usage: NegativeScanStudio.exe --dust light|strong <file.tiff> [...]", walk.MsgBoxIconInformation)
		return 2
	}
	var lines []string
	code := 0
	for _, f := range files {
		out, ds, err := hdrscan.DespeckleFile(f, l, appVersion)
		if err != nil {
			lines = append(lines, f+": "+err.Error())
			code = 1
			continue
		}
		lines = append(lines, fmt.Sprintf("%s -> %s: %d spots (%d hairs), %d fibres, %d px, %.1f s", f, out, ds.Spots, ds.Hairs, ds.Fibres, ds.Pixels, ds.Elapsed.Seconds()))
	}
	msg := strings.Join(lines, "\n")
	fmt.Println(msg)
	walk.MsgBox(nil, appVersion+" - dust removal "+hdrscan.DustName(l), msg, walk.MsgBoxIconInformation)
	return code
}
