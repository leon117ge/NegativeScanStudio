package main

// "Chart calibration" (v0.84): camera + light input profile from a 24-patch
// transmission chart (IT8.7/1 reference). Replaces the v0.66 "Calibrate
// crosstalk from chart" dialog; opened from DCTL > Mask & Crosstalk.

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"basetool/colorsci"
	"basetool/hdrscan"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

const calTip = "Camera + light input profile: scan a 24-patch transmission chart (e.g. SoftLoaf / IT8.7/1) with the trichrome light, load its CGATS reference, fit a grey-preserving 3x3 (mean ΔE2000), save it as a camera profile (applied to the automatic trichrome film-base measurement) and export CameraCal_Trichrome.dctl."

type chartCal struct {
	u       *MaskUI
	dlg     *walk.Dialog
	view    *walk.CustomWidget
	fileLbl *walk.Label
	refLbl  *walk.Label
	resTE   *walk.TextEdit
	nameLE  *walk.LineEdit
	saveBtn *walk.PushButton
	dctlBtn *walk.PushButton
	profCB  *walk.ComboBox
	enCB    *walk.CheckBox
	strNE   *walk.NumberEdit

	path    string
	pix     []uint16
	w, h    int
	bmp     *walk.Bitmap
	pw, ph  int
	corners colorsci.ChartCorners
	drag    int
	ref     *colorsci.CamRef
	refPath string
	ps      *[24]colorsci.CamPatch
	fit     *colorsci.CamFit
	loading bool
}

func matText(m colorsci.Mat3) string {
	return fmt.Sprintf("[%.3f %.3f %.3f | %.3f %.3f %.3f | %.3f %.3f %.3f]", m[0][0], m[0][1], m[0][2], m[1][0], m[1][1], m[1][2], m[2][0], m[2][1], m[2][2])
}

func (u *MaskUI) calibrateChart() {
	k := &chartCal{u: u, drag: -1}
	grey := walk.RGB(128, 128, 136)
	var closePB *walk.PushButton
	mono := Font{Family: "Consolas", PointSize: 9}
	err := Dialog{
		AssignTo: &k.dlg, Title: "Chart calibration - camera + light input profile", CancelButton: &closePB,
		MinSize: Size{Width: 960, Height: 760},
		Layout:  VBox{Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}, Spacing: 6},
		Children: []Widget{
			Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				PushButton{Text: "Open chart TIFF...", OnClicked: k.open, ToolTipText: "16-bit linear RGB TIFF of the chart (a trichrome scan from this program)."},
				PushButton{Text: "Use last scan", OnClicked: k.useLast, ToolTipText: "The last trichrome TIFF written on the Trichrome page in this session."},
				Label{AssignTo: &k.fileLbl, Text: "No chart loaded.", TextColor: grey},
				HSpacer{},
			}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				PushButton{Text: "Load reference (CGATS)...", OnClicked: k.openRef, ToolTipText: "IT8.7/1 reference data: SAMPLE_ID and XYZ_X/Y/Z (or LAB_L/A/B), D50."},
				Label{AssignTo: &k.refLbl, Text: "No reference loaded.", TextColor: grey},
				HSpacer{},
			}},
			Label{Text: "The 4x6 grid is found automatically, row A = neutral ramp, A1 darkest. Drag the corner markers A1 / A6 / D6 / D1 to correct it. Green = clean, red = clipped (left out).", TextColor: grey},
			CustomWidget{AssignTo: &k.view, Paint: k.paint, PaintMode: PaintBuffered, InvalidatesOnResize: true, MinSize: Size{Width: 900, Height: 360}, StretchFactor: 1,
				OnMouseDown: k.mouseDown, OnMouseMove: k.mouseMove, OnMouseUp: k.mouseUp},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				PushButton{Text: "Auto-detect", OnClicked: func() { k.detect(); k.changed() }},
				PushButton{Text: "Rotate 180°", OnClicked: func() {
					c := k.corners
					k.corners = colorsci.ChartCorners{c[2], c[3], c[0], c[1]}
					k.changed()
				}, ToolTipText: "Swap A1 and D6 ends (chart upside down)."},
				PushButton{Text: "Reset grid", OnClicked: func() { k.resetCorners(); k.changed() }},
				HSpacer{},
				PushButton{Text: "Fit", MinSize: Size{Width: 100}, OnClicked: k.solve},
			}},
			TextEdit{AssignTo: &k.resTE, ReadOnly: true, VScroll: true, Font: mono, MinSize: Size{Height: 170}, Text: "Open a chart TIFF and load its reference, then press Fit."},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				Label{Text: "Profile name"},
				LineEdit{AssignTo: &k.nameLE, Text: "XT3 trichrome " + time.Now().Format("2006-01-02"), MinSize: Size{Width: 200}},
				PushButton{AssignTo: &k.saveBtn, Text: "Save as camera profile", Enabled: false, OnClicked: k.save},
				PushButton{AssignTo: &k.dctlBtn, Text: "Export DCTL...", Enabled: false, OnClicked: k.exportDCTL, ToolTipText: "Standalone CameraCal_Trichrome.dctl: Enable, Strength, Neutralise grey. Place it before FilmScan_Density_In."},
				HSpacer{},
			}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				Label{Text: "Active camera profile"},
				ComboBox{AssignTo: &k.profCB, MinSize: Size{Width: 220}, OnCurrentIndexChanged: k.settingsChanged},
				CheckBox{AssignTo: &k.enCB, Text: "Enable (apply to trichrome film-base measurement)", OnCheckedChanged: k.settingsChanged},
				Label{Text: "Strength"},
				NumberEdit{AssignTo: &k.strNE, Decimals: 2, MinValue: 0, MaxValue: 1, Increment: 0.05, Value: 1, MinSize: Size{Width: 70}, OnValueChanged: k.settingsChanged},
				PushButton{Text: "Delete profile", OnClicked: k.deleteProfile},
				HSpacer{},
				PushButton{AssignTo: &closePB, Text: "Close", MinSize: Size{Width: 80}, OnClicked: func() { k.dlg.Cancel() }},
			}},
		},
	}.Create(u.win())
	if err != nil {
		walk.MsgBox(u.win(), "Chart calibration", err.Error(), walk.MsgBoxIconError)
		return
	}
	darkDialog(k.dlg)
	k.loadSettingsUI()
	if s := camSettings(); s.LastRef != "" {
		k.loadRef(s.LastRef, true)
	}
	k.dlg.Run()
	u.refresh()
	if k.bmp != nil {
		k.bmp.Dispose()
	}
}

// ---------- profile settings ----------

func (k *chartCal) loadSettingsUI() {
	k.loading = true
	defer func() { k.loading = false }()
	s := camSettings()
	names := []string{"(none)"}
	cur := 0
	for i, p := range s.Profiles {
		names = append(names, p.Name)
		if p.Name == s.Active {
			cur = i + 1
		}
	}
	k.profCB.SetModel(names)
	k.profCB.SetCurrentIndex(cur)
	k.enCB.SetChecked(s.Enabled)
	k.strNE.SetValue(s.Strength)
}

func (k *chartCal) settingsChanged() {
	if k.loading {
		return
	}
	s := camSettings()
	s.Active = ""
	if i := k.profCB.CurrentIndex(); i > 0 && i <= len(s.Profiles) {
		s.Active = s.Profiles[i-1].Name
	}
	s.Enabled = k.enCB.Checked()
	s.Strength = math.Min(math.Max(k.strNE.Value(), 0), 1)
	saveCamSettings(s)
	if p, st := activeCam(); p != nil {
		k.u.say(fmt.Sprintf("Camera profile \"%s\" enabled at strength %.2f: applied to the trichrome film-base measurement.", p.Name, st))
	} else {
		k.u.say("Camera profile: off.")
	}
}

func (k *chartCal) deleteProfile() {
	s := camSettings()
	i := k.profCB.CurrentIndex()
	if i <= 0 || i > len(s.Profiles) {
		return
	}
	name := s.Profiles[i-1].Name
	if walk.MsgBox(k.dlg, "Delete profile", "Delete camera profile \""+name+"\"?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	s.Profiles = append(s.Profiles[:i-1], s.Profiles[i:]...)
	if s.Active == name {
		s.Active, s.Enabled = "", false
	}
	saveCamSettings(s)
	k.loadSettingsUI()
}

// ---------- inputs ----------

func (k *chartCal) open() {
	dlg := new(walk.FileDialog)
	dlg.Title = "16-bit TIFF of the 24-patch transmission chart"
	dlg.Filter = "16-bit TIFF (*.tif;*.tiff)|*.tif;*.tiff|All files (*.*)|*.*"
	if ok, _ := dlg.ShowOpen(k.dlg); !ok {
		return
	}
	k.load(dlg.FilePath)
}

func (k *chartCal) useLast() {
	if lastTriOut == "" {
		walk.MsgBox(k.dlg, "Use last scan", "No trichrome scan has been written in this session yet.", walk.MsgBoxIconInformation)
		return
	}
	k.load(lastTriOut)
}

func (k *chartCal) openRef() {
	dlg := new(walk.FileDialog)
	dlg.Title = "Chart reference data (IT8.7/1 CGATS, XYZ D50)"
	dlg.Filter = "CGATS / IT8 (*.txt;*.it8;*.cie;*.cgats)|*.txt;*.it8;*.cie;*.cgats|All files (*.*)|*.*"
	if s := camSettings(); s.LastRef != "" {
		dlg.InitialDirPath = filepath.Dir(s.LastRef)
	}
	if ok, _ := dlg.ShowOpen(k.dlg); !ok {
		return
	}
	k.loadRef(dlg.FilePath, false)
}

func (k *chartCal) loadRef(path string, quiet bool) {
	f, err := os.Open(path)
	if err == nil {
		var r colorsci.CamRef
		r, err = colorsci.ParseCGATS(f)
		f.Close()
		if err == nil {
			k.ref, k.refPath = &r, path
			txt := filepath.Base(path) + "  (24 patches"
			if r.Illum != "" {
				txt += ", " + r.Illum
				if !strings.EqualFold(r.Illum, "D50") {
					txt += " - WARNING: treated as D50"
				}
			}
			k.refLbl.SetText(txt + ")")
			s := camSettings()
			s.LastRef = path
			saveCamSettings(s)
			k.changed()
			return
		}
	}
	if quiet {
		k.refLbl.SetText("Last reference " + filepath.Base(path) + " not loaded: " + err.Error())
		return
	}
	walk.MsgBox(k.dlg, "Reference", err.Error(), walk.MsgBoxIconWarning)
}

func (k *chartCal) load(path string) {
	pl, err := hdrscan.Load(path)
	if err != nil {
		walk.MsgBox(k.dlg, "Open chart", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	k.path, k.pix, k.w, k.h = path, append([]uint16(nil), pl.Pix...), pl.W, pl.H
	pl.Free()
	k.makePreview()
	k.fileLbl.SetText(fmt.Sprintf("%s  (%d x %d)", filepath.Base(path), k.w, k.h))
	k.detect()
	k.changed()
}

func (k *chartCal) detect() {
	if k.pix == nil {
		return
	}
	c, orient, err := colorsci.DetectCamChart(k.pix, k.w, k.h)
	if err != nil {
		k.resetCorners()
		k.resTE.SetText("Auto-detect failed (" + err.Error() + "). Drag the four corner markers onto A1, A6, D6, D1.")
		return
	}
	k.corners = c
	k.resTE.SetText("Grid found automatically (chart " + orient + "). Check the labels, then press Fit.")
}

// makePreview: linear scan shown with a display gamma, levelled to the 99.5th percentile.
func (k *chartCal) makePreview() {
	s := max(1, int(math.Ceil(math.Max(float64(k.w)/1100, float64(k.h)/700))))
	pw, ph := k.w/s, k.h/s
	var hist []float64
	for y := 0; y < ph; y += 3 {
		for x := 0; x < pw; x += 3 {
			i := ((y*s+s/2)*k.w + x*s + s/2) * 3
			hist = append(hist, math.Max(float64(k.pix[i]), math.Max(float64(k.pix[i+1]), float64(k.pix[i+2]))))
		}
	}
	sort.Float64s(hist)
	hi := math.Max(hist[len(hist)*995/1000], 1)
	img := image.NewNRGBA(image.Rect(0, 0, pw, ph))
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			i := ((y*s+s/2)*k.w + x*s + s/2) * 3
			var px [3]uint8
			for c := 0; c < 3; c++ {
				t := math.Min(float64(k.pix[i+c])/hi, 1)
				px[c] = uint8(255*math.Pow(t, 1/2.2) + 0.5)
			}
			img.SetNRGBA(x, y, color.NRGBA{px[0], px[1], px[2], 255})
		}
	}
	if k.bmp != nil {
		k.bmp.Dispose()
		k.bmp = nil
	}
	k.bmp, _ = walk.NewBitmapFromImageForDPI(img, 96)
	k.pw, k.ph = pw, ph
}

func (k *chartCal) resetCorners() {
	if k.w == 0 {
		return
	}
	w, h := float64(k.w), float64(k.h)
	cw := math.Min(w*0.8, h*0.8*1.5)
	ch := cw / 1.5
	x0, y0 := (w-cw)/2+cw/12, (h-ch)/2+ch/8
	x1, y1 := (w+cw)/2-cw/12, (h+ch)/2-ch/8
	k.corners = colorsci.ChartCorners{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
}

// changed re-samples the patches (clip status for the overlay) and drops the fit.
func (k *chartCal) changed() {
	k.fit = nil
	k.saveBtn.SetEnabled(false)
	k.dctlBtn.SetEnabled(false)
	if k.pix != nil {
		ps := colorsci.SampleCamChart(k.pix, k.w, k.h, k.corners, 0.25)
		k.ps = &ps
	}
	k.view.Invalidate()
}

func (k *chartCal) imageRect() walk.Rectangle {
	cb := k.view.ClientBoundsPixels()
	if k.pw == 0 {
		return cb
	}
	sc := math.Min(float64(cb.Width)/float64(k.w), float64(cb.Height)/float64(k.h))
	iw, ih := int(float64(k.w)*sc), int(float64(k.h)*sc)
	return walk.Rectangle{X: cb.X + (cb.Width-iw)/2, Y: cb.Y + (cb.Height-ih)/2, Width: iw, Height: ih}
}

func (k *chartCal) toView(x, y float64) walk.Point {
	ir := k.imageRect()
	return walk.Point{X: ir.X + int(x/float64(k.w)*float64(ir.Width)), Y: ir.Y + int(y/float64(k.h)*float64(ir.Height))}
}

func (k *chartCal) toImage(px, py int) (float64, float64) {
	ir := k.imageRect()
	x := float64(px-ir.X) / float64(max(1, ir.Width)) * float64(k.w)
	y := float64(py-ir.Y) / float64(max(1, ir.Height)) * float64(k.h)
	return math.Min(math.Max(x, 0), float64(k.w-1)), math.Min(math.Max(y, 0), float64(k.h-1))
}

func (k *chartCal) paint(c *walk.Canvas, _ walk.Rectangle) error {
	cb := k.view.ClientBoundsPixels()
	bg, _ := walk.NewSolidColorBrush(walk.RGB(22, 22, 25))
	if bg != nil {
		c.FillRectanglePixels(bg, cb)
		bg.Dispose()
	}
	f, _ := walk.NewFont("Segoe UI", 8, walk.FontBold)
	if f != nil {
		defer f.Dispose()
	}
	if k.bmp == nil {
		if f != nil {
			c.DrawTextPixels("Open a 16-bit TIFF of the 24-patch transmission chart (or Use last scan).\r\nScan it with the same light levels as your film; no patch may clip.", f, walk.RGB(128, 128, 136), cb, walk.TextCenter|walk.TextVCenter|walk.TextWordbreak)
		}
		return nil
	}
	c.DrawImageStretchedPixels(k.bmp, k.imageRect())
	pen := func(col walk.Color, wd int) walk.Pen {
		br, _ := walk.NewSolidColorBrush(col)
		if br == nil {
			return nil
		}
		p, _ := walk.NewGeometricPen(walk.PenSolid, wd, br)
		br.Dispose()
		return p
	}
	ok, bad, hand := pen(walk.RGB(60, 210, 90), 2), pen(walk.RGB(235, 60, 60), 2), pen(walk.RGB(255, 255, 255), 2)
	defer func() {
		for _, p := range []walk.Pen{ok, bad, hand} {
			if p != nil {
				p.Dispose()
			}
		}
	}()
	ir := k.imageRect()
	hv := int(k.corners.PatchHalf(0.25) / float64(k.w) * float64(ir.Width))
	for i := 0; i < 24; i++ {
		x, y := k.corners.PatchCentre(i)
		p := k.toView(x, y)
		pn, col, lbl := ok, walk.RGB(60, 210, 90), colorsci.CamPatchNames[i]
		if k.ps != nil && k.ps[i].Clipped {
			pn, col, lbl = bad, walk.RGB(235, 60, 60), lbl+" CLIP"
		}
		if pn != nil {
			c.DrawRectanglePixels(pn, walk.Rectangle{X: p.X - hv, Y: p.Y - hv, Width: 2*hv + 1, Height: 2*hv + 1})
		}
		if f != nil {
			c.DrawTextPixels(lbl, f, col, walk.Rectangle{X: p.X - hv + 3, Y: p.Y - hv + 2, Width: 2*hv + 40, Height: 14}, walk.TextLeft|walk.TextSingleLine)
		}
	}
	labels := [4]string{"A1", "A6", "D6", "D1"}
	for i, cn := range k.corners {
		p := k.toView(cn[0], cn[1])
		if hand != nil {
			c.DrawEllipsePixels(hand, walk.Rectangle{X: p.X - 7, Y: p.Y - 7, Width: 15, Height: 15})
		}
		if f != nil {
			c.DrawTextPixels(labels[i], f, walk.RGB(255, 255, 255), walk.Rectangle{X: p.X + 9, Y: p.Y + 4, Width: 30, Height: 14}, walk.TextLeft|walk.TextSingleLine)
		}
	}
	return nil
}

func (k *chartCal) mouseDown(x, y int, b walk.MouseButton) {
	if b != walk.LeftButton || k.bmp == nil {
		return
	}
	best, bd := -1, 14.0*14
	for i, cn := range k.corners {
		p := k.toView(cn[0], cn[1])
		if d := float64((p.X-x)*(p.X-x) + (p.Y-y)*(p.Y-y)); d < bd {
			best, bd = i, d
		}
	}
	k.drag = best
}

func (k *chartCal) mouseMove(x, y int, b walk.MouseButton) {
	if k.drag < 0 {
		return
	}
	ix, iy := k.toImage(x, y)
	k.corners[k.drag] = [2]float64{ix, iy}
	k.fit = nil
	k.view.Invalidate()
}

func (k *chartCal) mouseUp(x, y int, b walk.MouseButton) {
	if k.drag >= 0 {
		k.drag = -1
		k.changed()
	}
}

// ---------- fit and outputs ----------

func (k *chartCal) solve() {
	if k.pix == nil || k.ref == nil {
		walk.MsgBox(k.dlg, "Fit", "Open a chart TIFF and load its reference first.", walk.MsgBoxIconInformation)
		return
	}
	k.changed()
	f, err := colorsci.FitCamChart(*k.ps, *k.ref)
	var sb strings.Builder
	if ev := colorsci.ClipStops(*k.ps, [3]float64{1, 1, 1}); len(f.Clipped) > 0 || ev > 0 {
		cl := f.Clipped
		if err != nil {
			cl = nil
			for i, p := range k.ps {
				if p.Clipped {
					cl = append(cl, colorsci.CamPatchNames[i])
				}
			}
		}
		stops := f.ClipEV
		if err != nil || stops == 0 {
			stops = math.Max(ev, 1.0/3)
		}
		fmt.Fprintf(&sb, "WARNING: %d patch(es) clipped (>%.0f%% of pixels above %.0f%% FS), left out: %s. Reduce exposure by about %.1f stop(s) and rescan to use them.\r\n",
			len(cl), colorsci.CamClipFrac*100, colorsci.CamClipLevel*100, strings.Join(cl, " "), stops)
	}
	if err != nil {
		sb.WriteString("Could not fit: " + err.Error())
		k.resTE.SetText(sb.String())
		return
	}
	k.fit = &f
	fmt.Fprintf(&sb, "Neutral-preserving 3x3 on %d clean patches (white balance factored out, camera grey unchanged).\r\n", f.Used)
	fmt.Fprintf(&sb, "ΔE2000  before (per-channel gain on %s): mean %.2f  max %.2f    after: mean %.2f  max %.2f\r\n", f.GainPatch, f.MeanB, f.MaxB, f.MeanA, f.MaxA)
	fmt.Fprintf(&sb, "Matrix  R' = %8.5f R %+8.5f G %+8.5f B\r\n", f.Mx[0][0], f.Mx[0][1], f.Mx[0][2])
	fmt.Fprintf(&sb, "        G' = %8.5f R %+8.5f G %+8.5f B\r\n", f.Mx[1][0], f.Mx[1][1], f.Mx[1][2])
	fmt.Fprintf(&sb, "        B' = %8.5f R %+8.5f G %+8.5f B\r\n", f.Mx[2][0], f.Mx[2][1], f.Mx[2][2])
	fmt.Fprintf(&sb, "Film-base multipliers (row sums; a measured base R/G/B is scaled by these when the profile is on): R %.4f  G %.4f  B %.4f\r\n", f.BaseMul[0], f.BaseMul[1], f.BaseMul[2])
	fmt.Fprintf(&sb, "Camera grey R:G:B = %.4f : %.4f : %.4f\r\n\r\n", f.Grey[0], f.Grey[1], f.Grey[2])
	sb.WriteString("Patch  R        G        B        ΔE before  ΔE after\r\n")
	for i, p := range k.ps {
		st := ""
		if !f.Use[i] {
			st = "  (clipped, not fitted)"
		}
		fmt.Fprintf(&sb, "%-5s  %.5f  %.5f  %.5f  %8.2f  %8.2f%s\r\n", colorsci.CamPatchNames[i], p.Med[0], p.Med[1], p.Med[2], f.DEBefore[i], f.DEAfter[i], st)
	}
	k.resTE.SetText(sb.String())
	k.saveBtn.SetEnabled(true)
	k.dctlBtn.SetEnabled(true)
	k.view.Invalidate()
}

func (k *chartCal) info() string {
	f := k.fit
	return fmt.Sprintf("chart %s, ref %s, %s, %d patches, ΔE2000 mean %.2f max %.2f (before %.2f / %.2f)",
		filepath.Base(k.path), filepath.Base(k.refPath), time.Now().Format("2006-01-02 15:04"), f.Used, f.MeanA, f.MaxA, f.MeanB, f.MaxB)
}

func (k *chartCal) save() {
	if k.fit == nil {
		return
	}
	name, ok := cleanName(k.nameLE.Text())
	if !ok {
		walk.MsgBox(k.dlg, "Save", "Enter a profile name.", walk.MsgBoxIconInformation)
		return
	}
	s := camSettings()
	p := CamProfile{Name: name, M: k.fit.Mx, Neutral: k.fit.Neutral, Info: k.info()}
	if e := s.find(name); e != nil {
		if walk.MsgBox(k.dlg, "Save", "Replace camera profile \""+name+"\"?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
			return
		}
		*e = p
	} else {
		s.Profiles = append(s.Profiles, p)
	}
	s.Active = name
	if err := saveCamSettings(s); err != nil {
		walk.MsgBox(k.dlg, "Save", err.Error(), walk.MsgBoxIconError)
		return
	}
	k.loadSettingsUI()
	k.u.say("Camera profile \"" + name + "\" saved (" + p.Info + "). Tick Enable to apply it to the trichrome film-base measurement.")
	k.resTE.SetText("Saved camera profile \"" + name + "\" and made it the active profile (Enable is " + map[bool]string{true: "on", false: "off"}[s.Enabled] + ").\r\n" + k.resTE.Text())
}

func (k *chartCal) exportDCTL() {
	if k.fit == nil {
		return
	}
	dlg := new(walk.FileDialog)
	dlg.Title = "Export camera calibration DCTL"
	dlg.Filter = "DCTL (*.dctl)|*.dctl"
	dlg.FilePath = "CameraCal_Trichrome.dctl"
	if s := camSettings(); s.LastDir != "" {
		dlg.InitialDirPath = s.LastDir
	}
	if ok, _ := dlg.ShowSave(k.dlg); !ok {
		return
	}
	path := dlg.FilePath
	if !strings.EqualFold(filepath.Ext(path), ".dctl") {
		path += ".dctl"
	}
	name, _ := cleanName(k.nameLE.Text())
	txt := colorsci.CamDCTL(name, k.info(), k.fit.Mx, k.fit.Neutral)
	if err := os.WriteFile(path, []byte(txt), 0644); err != nil {
		walk.MsgBox(k.dlg, "Export DCTL", err.Error(), walk.MsgBoxIconError)
		return
	}
	s := camSettings()
	s.LastDir = filepath.Dir(path)
	saveCamSettings(s)
	k.u.say("Camera calibration DCTL written: " + path)
}
