package main

// v0.73: DCTL > Mask & Crosstalk sub-page (see maskdefaults.go).

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"basetool/filmdb"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

const (
	maskOnTip = "Orange-mask compensation as the DCTL default (Off by default). On: \"Orange mask compensation\" is ticked and\n" +
		"Base density R/G/B = how far each channel's film base sits above code 95 after FilmScan_Density_In (see Base density from).\n" +
		"Keep it Off when Density_In already divides by this film base: the base is then at code 95 in every channel and the mask is gone."
	maskBaseTip = "Film base for Mask comp. Film base result: the R/G/B result of DCTL > Film base, taken with \"Use current result\"\n" +
		"(stored with these settings). Film stock: the selected stock's base from the film-base DB."
	xtTip2   = "Dye crosstalk correction as the DCTL default: Crosstalk strength 0.50 or 1.00 of the matrix (blended with no correction). Off: 0."
	xtMatTip = "Crosstalk matrix preset written as the default. Stock entries are mask-aware spectral matrices from the Kodak datasheet dye-density curves\n" +
		"(Ektar 100 E-4046, Portra 400 E-4050, UltraMax 400 E-7023, Gold 200 E-7022: estimated from the neutral curve; Vision3 5219 H-1-5219;\n" +
		"Vision3 500T: H-1-5219 dyes + its D-min curve, with the grey axis measured on Vision3 500T / X-T3 scans)\n" +
		"and the Sensor model / Light source / Matrix form below - model-based starting points, not a calibration of your camera and light.\n" +
		"Auto: the selected film stock's chart matrix if it has one, else the built-in set matching its name\n" +
		"(Portra 160/800 use Portra 400; 500T / 5219 / 7219 use Vision3 500T; other Vision3/ECN use 5219), else Vision3 5219.\n" +
		"Calibrated (chart): the ColorChecker matrix of the selected stock (a preset of the DCTL; falls back to Auto if none). Identity: no mixing."
	maskRefTip = "What FilmScan_Density_In divides by (its Base R/G/B). Brightest base channel: a neutral reference, the other channels keep the orange mask\n" +
		"above code 95 (Base density = log10(brightest / base)). 1.0 (full scale): Density_In base 1.0, Base density = -log10(base)."
	xtSensorTip = "Spectral sensitivity behind the built-in stock matrices (v0.75).\n" +
		"X-T3 measured (default): the Fujifilm X-T3 curves measured by Weta Digital (rawtoaces-data, Apache-2.0), weighted by the stock's\n" +
		"film base (D-min) and the Light source below. Adds four presets to the DCTL (White LED / Narrowband x Mask-aware / Full inverse).\n" +
		"Gaussian (legacy v0.74): the old modelled X-Trans curves with flat light - exactly the v0.74 presets (Light source and Matrix form are not used).\n" +
		"Not used for Calibrated (chart) and Identity."
	xtLightTip = "Scan light for the X-T3 measured matrix. White LED: phosphor white LED model.\n" +
		"Narrowband RGB 450/540/660: three LEDs (assumed FWHM 20/30/20 nm), powers balanced so R, G and B are equal on the film base.\n" +
		"Narrowband: in this model the R and B channels see about half the dye mixing of White LED, but the G channel still sees the yellow\n" +
		"dye about as much, so with Mask-aware its G row (G<-yellow) is stronger than with White LED. Model values, not a calibration."
	xtFormTip = "Mask-aware (default): the G<-cyan, B<-cyan and B<-magenta terms are set to zero before inverting, because the coloured couplers\n" +
		"(the orange mask) of colour negative film are designed to cancel exactly these unwanted absorptions. Assumes complete masking.\n" +
		"Full inverse (no masking): inverts the dye mixing as if there were no masking couplers - an upper bound; stronger, noisier and it\n" +
		"double-corrects what the mask already removed. Both forms are presets of the DCTL."
	xtNeutTip = "Keep neutral axis (default, v0.76): the matrix rows are rescaled so the film's grey axis passes unchanged and only the\n" +
		"colour separation changes. A negative's greys do not have equal R, G, B densities above the base (R < G < B on a typical scan);\n" +
		"the v0.75 rows kept only R = G = B and so pushed greys towards blue/cyan. The axis is a property of the film + light + camera:\n" +
		"the default was measured on an Ektar 100 frame scanned with an X-T3 (Vision3 500T: its own axis measured on 500T / X-T3 scans);\n" +
		"use \"Measure from scan...\" for your own setup (stored for the stock it was measured with).\n" +
		"Off: the v0.75 matrix rows. Only used with the X-T3 measured presets."
	xtMeasureTip = "Measure the grey axis from a negative scan (16-bit TIFF of the negative, as fed to FilmScan_Density_In) with the Mask comp\n" +
		"base (Base source: Film base result or film stock): the median density ratio R/G and B/G of the pixels near the main axis.\n" +
		"Use a frame with some grey / low-saturation content and a correct film base."
	maskStockTip = "Film stock of the film-base DB: base for \"Base: film stock\" and the stock for Auto / Calibrated (chart)."
)

// MaskUI is the DCTL > Mask & Crosstalk page.
type MaskUI struct {
	mw      **walk.MainWindow
	getBase func() (string, string, string) // Film base result
	db      func() *filmdb.DB               // the film-base DB shared with HDR scan
	cfg     maskSettings
	loading bool

	stockCB, maskCB, baseCB, refCB, xtCB, matCB *walk.ComboBox
	sensorCB, lightCB, formCB, neutCB           *walk.ComboBox
	measureBtn                                  *walk.PushButton
	baseLbl, pathLbl, infoLbl, neutLbl          *walk.Label
	prevTE                                      *walk.TextEdit
}

func (u *MaskUI) win() *walk.MainWindow { return *u.mw }

func (u *MaskUI) say(s string) {
	if u.infoLbl != nil {
		u.infoLbl.SetText(s)
	}
}

func (u *MaskUI) stockNames() []string {
	names := []string{stockNone}
	if db := u.db(); db != nil {
		names = append(names, db.Names()...)
	}
	return names
}

func (u *MaskUI) Page() TabPage {
	u.cfg = loadMaskSettings()
	grey := walk.RGB(128, 128, 136)
	onChange := func() { u.changed() }
	row := func(cb **walk.ComboBox, items []string, tip string, w int, extra ...Widget) Widget {
		ch := []Widget{ComboBox{AssignTo: cb, Model: items, ToolTipText: tip, MinSize: Size{Width: w}, OnCurrentIndexChanged: onChange}}
		ch = append(ch, extra...)
		ch = append(ch, HSpacer{})
		return Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: ch}
	}
	return TabPage{
		Title:  "  Mask & Crosstalk  ",
		Layout: VBox{Margins: Margins{Left: 10, Top: 10, Right: 10, Bottom: 10}, Spacing: 8},
		Children: []Widget{
			Label{Text: "Defaults written into " + maskDCTLName + " (orange mask compensation and dye crosstalk). Apply the DCTL in Resolve after FilmScan_Density_In;\n" +
				"the image files are not changed. All Off writes the DCTL exactly as before (mask off, identity, strength 0).", TextColor: grey},
			Card{
				Title:  "DCTL parameter defaults",
				Layout: Grid{Columns: 2, Spacing: 8, Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}},
				Children: []Widget{
					Label{Text: "Film stock"},
					row(&u.stockCB, u.stockNames(), maskStockTip, 220, PushButton{Text: "Refresh", OnClicked: u.refresh, ToolTipText: "Reload the film stocks of the film-base DB."}),
					Label{Text: "Mask comp"},
					row(&u.maskCB, maskItems, maskOnTip, 90),
					Label{Text: "Base source"},
					row(&u.baseCB, maskBaseItems, maskBaseTip, 200,
						PushButton{Text: "Use current result", OnClicked: u.takeResult, ToolTipText: "Store the current R/G/B result of DCTL > Film base as the base."},
						Label{AssignTo: &u.baseLbl, Text: "", TextColor: grey}),
					Label{Text: "Base density from"},
					row(&u.refCB, maskRefItems, maskRefTip, 260),
					Label{Text: "Crosstalk"},
					row(&u.xtCB, xtItems, xtTip2, 90),
					Label{Text: "Crosstalk matrix"},
					row(&u.matCB, xtMatItems, xtMatTip, 200),
					Label{Text: "Sensor model"},
					row(&u.sensorCB, xtSensorItems, xtSensorTip, 200),
					Label{Text: "Light source"},
					row(&u.lightCB, xtLightItems, xtLightTip, 200),
					Label{Text: "Matrix form"},
					row(&u.formCB, xtFormItems, xtFormTip, 200),
					Label{Text: "Neutral axis"},
					row(&u.neutCB, xtNeutItems, xtNeutTip, 200,
						PushButton{AssignTo: &u.measureBtn, Text: "Measure from scan...", OnClicked: u.measureNeutral, ToolTipText: xtMeasureTip},
						Label{AssignTo: &u.neutLbl, Text: "", TextColor: grey}),
					Label{Text: ""},
					Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
						PushButton{Text: "Chart calibration (camera profile)...", OnClicked: u.calibrateChart, ToolTipText: calTip},
						HSpacer{},
					}},
				},
			},
			Card{
				Title:   "Result",
				MaxSize: Size{Height: 160},
				Layout:  VBox{Spacing: 6, Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}},
				Children: []Widget{
					TextEdit{AssignTo: &u.prevTE, ReadOnly: true, VScroll: true, MinSize: Size{Height: 84}, MaxSize: Size{Height: 84}, ToolTipText: "What the next " + maskDCTLName + " will hold as its parameter defaults."},
					Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
						PushButton{Text: "Write " + maskDCTLName, OnClicked: u.write, ToolTipText: "Write the DCTL now next to the DCTL exported on DCTL > Film base (only a file written by this program is replaced)."},
						Label{AssignTo: &u.pathLbl, Text: "", TextColor: grey},
						HSpacer{},
					}},
				},
			},
			Label{AssignTo: &u.infoLbl, Text: ""},
			VSpacer{},
		},
	}
}

// init shows the stored settings (after the window exists).
func (u *MaskUI) init() {
	u.loading = true
	u.selectStock()
	u.maskCB.SetCurrentIndex(b2i(u.cfg.MaskOn))
	u.baseCB.SetCurrentIndex(u.cfg.BaseSrc)
	u.refCB.SetCurrentIndex(u.cfg.Ref)
	u.xtCB.SetCurrentIndex(u.cfg.Xt)
	u.matCB.SetCurrentIndex(u.cfg.XtMat)
	u.sensorCB.SetCurrentIndex(u.cfg.Sensor)
	u.lightCB.SetCurrentIndex(u.cfg.Light)
	u.formCB.SetCurrentIndex(u.cfg.Form)
	u.neutCB.SetCurrentIndex(u.cfg.Neut)
	u.loading = false
	for _, cb := range []*walk.ComboBox{u.stockCB, u.maskCB, u.baseCB, u.refCB, u.xtCB, u.matCB, u.sensorCB, u.lightCB, u.formCB, u.neutCB} {
		setDropWidth(cb, 260)
	}
	u.preview()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (u *MaskUI) selectStock() {
	idx := 0
	for i, n := range u.stockNames() {
		if i > 0 && strings.EqualFold(n, u.cfg.Stock) {
			idx = i
		}
	}
	u.stockCB.SetCurrentIndex(idx)
}

// refresh reloads the stock list (after DB changes on other pages).
func (u *MaskUI) refresh() {
	if u.stockCB == nil {
		return
	}
	u.loading = true
	u.stockCB.SetModel(u.stockNames())
	u.selectStock()
	u.loading = false
	u.preview()
}

func (u *MaskUI) read() {
	u.cfg.MaskOn = u.maskCB.CurrentIndex() == 1
	u.cfg.BaseSrc = max(0, u.baseCB.CurrentIndex())
	u.cfg.Ref = max(0, u.refCB.CurrentIndex())
	u.cfg.Xt = max(0, u.xtCB.CurrentIndex())
	u.cfg.XtMat = max(0, u.matCB.CurrentIndex())
	u.cfg.Sensor = max(0, u.sensorCB.CurrentIndex())
	u.cfg.Light = max(0, u.lightCB.CurrentIndex())
	u.cfg.Form = max(0, u.formCB.CurrentIndex())
	u.cfg.Neut = max(0, u.neutCB.CurrentIndex())
	if i := u.stockCB.CurrentIndex(); i > 0 {
		u.cfg.Stock = u.stockCB.Text()
	} else {
		u.cfg.Stock = ""
	}
}

func (u *MaskUI) currentResult() ([3]float64, bool) {
	var b [3]float64
	if u.getBase == nil {
		return b, false
	}
	r, g, bl := u.getBase()
	for i, t := range []string{r, g, bl} {
		v, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil || v <= 0 {
			return b, false
		}
		b[i] = v
	}
	return b, true
}

// forTrichrome (v0.82): a trichrome base was sent to Film base - orange
// mask compensation Off, crosstalk Off (strength 0) in the written DCTL.
func (u *MaskUI) forTrichrome() string {
	s, changed := trichromeMask(u.cfg)
	if !changed {
		return "Mask & Crosstalk: orange mask compensation Off, crosstalk strength 0 (trichrome) - already so."
	}
	u.cfg = s
	if err := saveMaskSettings(u.cfg); err != nil {
		return "Mask & Crosstalk: " + err.Error()
	}
	if u.maskCB != nil {
		u.init()
	}
	msg := "Mask & Crosstalk set for trichrome: orange mask compensation Off, crosstalk strength 0."
	u.say(msg)
	if note := syncMaskDCTL(); note != "" {
		msg += " " + note
	}
	return msg
}

func (u *MaskUI) takeResult() {
	b, ok := u.currentResult()
	if !ok {
		u.say("No Film base result yet: measure on DCTL > Film base first.")
		return
	}
	u.cfg.BaseRGB = b
	if u.baseCB.CurrentIndex() != 0 {
		u.baseCB.SetCurrentIndex(0) // calls changed
	}
	u.changed()
	u.say(fmt.Sprintf("Base set from the Film base result: R %.4f G %.4f B %.4f.", b[0], b[1], b[2]))
}

func (u *MaskUI) changed() {
	if u.loading || u.maskCB == nil {
		return
	}
	u.read()
	if u.cfg.MaskOn && u.cfg.BaseSrc == 0 && u.cfg.BaseRGB[0] <= 0 {
		if b, ok := u.currentResult(); ok {
			u.cfg.BaseRGB = b
		}
	}
	if err := saveMaskSettings(u.cfg); err != nil {
		u.say("Settings: " + err.Error())
	}
	u.preview()
	if note := syncMaskDCTL(); note != "" {
		u.say(note)
	}
}

func (u *MaskUI) preview() {
	if u.prevTE == nil {
		return
	}
	var ents []filmdb.Entry
	if db := u.db(); db != nil {
		ents = db.Entries
	}
	d := resolveMask(u.cfg, ents)
	t := "All Off: " + maskDCTLName + " keeps its defaults (Orange mask compensation off, Custom identity matrix, strength 0)."
	if len(d.Desc) > 0 {
		t = strings.Join(d.Desc, "\n")
	}
	u.prevTE.SetText(strings.ReplaceAll(t, "\n", "\r\n"))
	// Light source / Matrix form only matter for the X-T3 measured matrices.
	_, spectral := xtDyeSet(u.cfg.XtMat, findEntry(ents, u.cfg.Stock))
	en := spectral && u.cfg.Sensor != xtSensorLegacy
	u.sensorCB.SetEnabled(spectral)
	u.lightCB.SetEnabled(en)
	u.formCB.SetEnabled(en)
	u.neutCB.SetEnabled(en)
	u.measureBtn.SetEnabled(en && u.cfg.Neut == 0)
	ds, _, _ := xtResolvedDyes(u.cfg, findEntry(ents, u.cfg.Stock))
	ax, src := u.cfg.neutAxis(ds)
	u.neutLbl.SetText(fmt.Sprintf("R/G %.4f  B/G %.4f (%s)", ax[0], ax[1], src))
	b := u.cfg.BaseRGB
	if b[0] > 0 {
		u.baseLbl.SetText(fmt.Sprintf("result R %.4f G %.4f B %.4f", b[0], b[1], b[2]))
	} else {
		u.baseLbl.SetText("no result taken")
	}
	if p := loadConfig().DCTLPath; p != "" {
		auto := " (updated automatically)"
		if !loadConfig().AutoSync {
			auto = " (auto-update is off: press Write)"
		}
		u.pathLbl.SetText("Folder: " + filepath.Dir(p) + auto)
	} else {
		u.pathLbl.SetText("Export the DCTL on DCTL > Film base first: " + maskDCTLName + " is written next to it.")
	}
}

func (u *MaskUI) write() {
	p := loadConfig().DCTLPath
	if p == "" {
		walk.MsgBox(u.win(), maskDCTLName, "Export the DCTL on DCTL > Film base first: "+maskDCTLName+" is written into the same folder.", walk.MsgBoxIconInformation)
		return
	}
	if err := writeMaskDCTL(p, calibratedStocks()); err != nil {
		u.say(maskDCTLName + ": " + err.Error())
		return
	}
	out := filepath.Join(filepath.Dir(p), maskDCTLName)
	if _, err := os.Stat(out); err == nil {
		u.say(maskDCTLName + " written: " + out)
	}
}

// measureNeutral sets the grey axis from a negative scan (v0.76).
func (u *MaskUI) measureNeutral() {
	base, src := u.cfg.BaseRGB, "Film base result"
	var ents []filmdb.Entry
	if db := u.db(); db != nil {
		ents = db.Entries
	}
	if u.cfg.BaseSrc == 1 {
		base, src = [3]float64{}, "film stock"
		if st := findEntry(ents, u.cfg.Stock); st != nil {
			base, src = [3]float64{st.R, st.G, st.B}, "film stock "+st.Name
		}
	}
	if !(base[0] > 0 && base[1] > 0 && base[2] > 0) {
		if b, ok := u.currentResult(); ok {
			base, src = b, "Film base result"
		}
	}
	if !(base[0] > 0 && base[1] > 0 && base[2] > 0) {
		u.say("No film base: take the Film base result (\"Use current result\") or choose a film stock first.")
		return
	}
	dlg := new(walk.FileDialog)
	dlg.Title = "Negative scan for the neutral axis (16-bit TIFF)"
	dlg.Filter = "16-bit TIFF (*.tif;*.tiff)|*.tif;*.tiff|All files (*.*)|*.*"
	if ok, _ := dlg.ShowOpen(u.win()); !ok {
		return
	}
	u.say("Measuring the neutral axis...")
	ax, n, err := neutralAxisFromImage(dlg.FilePath, base)
	if err != nil {
		u.say("Neutral axis: " + err.Error())
		return
	}
	u.cfg.NeutAxis = ax
	ds, _, _ := xtResolvedDyes(u.cfg, findEntry(ents, u.cfg.Stock))
	u.cfg.NeutDyes = ds.Name
	u.cfg.NeutSrc = fmt.Sprintf("measured on %s, %d px, base %s", filepath.Base(dlg.FilePath), n, src)
	if err := saveMaskSettings(u.cfg); err != nil {
		u.say("Settings: " + err.Error())
	}
	u.preview()
	note := syncMaskDCTL()
	u.say(fmt.Sprintf("Neutral axis R/G %.4f B/G %.4f from %d near-neutral pixels. %s", ax[0], ax[1], n, note))
}
