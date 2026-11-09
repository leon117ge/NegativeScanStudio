package main

// Page 4 "Convert" (v0.96): digital camera RAW / linear TIFF -> simulated
// colour-negative TIFF. Replaces the Develop module. English UI only.

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"basetool/convert"
	"basetool/cropper"
	"basetool/rawconv"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type ConvertUI struct {
	mw **walk.MainWindow

	mu     sync.Mutex
	files  []string
	cur    int
	busy   bool
	stock  convert.Stock
	ev     float64
	useCam bool
	border float64
	softBorder bool
	grain     convert.GrainISO
	gStrength float64

	status, info *walk.Label
	fileCB       *walk.ComboBox
	stockCB      *walk.ComboBox
	evNE         *walk.NumberEdit
	camChk       *walk.CheckBox
	borderNE     *walk.NumberEdit
	softChk      *walk.CheckBox
	grainCB      *walk.ComboBox
	gStrNE       *walk.NumberEdit
	cachePath    string
	cachePix     []uint16
	cacheW, cacheH int
	cacheNote    string
	pb           *walk.ProgressBar
	logTE        *walk.TextEdit
	view         *walk.CustomWidget
	bmp          *walk.Bitmap
	pw, ph       int
	pix          []uint16
}

func (u *ConvertUI) win() *walk.MainWindow {
	if u.mw == nil {
		return nil
	}
	return *u.mw
}

func (u *ConvertUI) ui(f func()) {
	if w := u.win(); w != nil {
		w.Synchronize(f)
	}
}

func (u *ConvertUI) log(s string) {
	if u.logTE == nil {
		return
	}
	t := u.logTE.Text() + time.Now().Format("15:04:05") + "  " + s + "\r\n"
	if len(t) > 16000 {
		t = t[len(t)-12000:]
	}
	u.logTE.SetText(t)
	u.logTE.SetTextSelection(len(t), len(t))
	u.logTE.ScrollToCaret()
}

func (u *ConvertUI) init() {
	ss := convert.Stocks()
	if len(ss) > 0 {
		u.stock = ss[0]
	}
	u.ev = 0
	u.useCam = true
	u.border = 0.04
	u.softBorder = true
	u.grain = convert.GrainOff
	u.gStrength = 1
}

func (u *ConvertUI) options() convert.Options {
	o := convert.Options{
		Stock: u.stock, EV: u.ev, ApplyCamProfile: u.useCam,
		BorderPct: u.border, SoftBorder: u.softBorder, Grain: u.grain, GrainStrength: u.gStrength,
		Software: appVersion, LZW: true,
	}
	if p, k := activeCam(); p != nil && u.useCam {
		o.CamMatrix, o.CamStrength = p.M, k
		o.ApplyCamProfile = true
	} else {
		o.ApplyCamProfile = false
	}
	return o
}

func (u *ConvertUI) Open(paths []string) {
	var files []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.IsDir() {
			filepath.Walk(p, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if isConvertInput(path) {
					files = append(files, path)
				}
				return nil
			})
			continue
		}
		if isConvertInput(p) {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		walk.MsgBox(u.win(), "Convert", "No RAW or TIFF files found.", walk.MsgBoxIconInformation)
		return
	}
	u.files = files
	u.cur = 0
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	if u.fileCB != nil {
		u.fileCB.SetModel(names)
		u.fileCB.SetCurrentIndex(0)
	}
	u.loadPreview(files[0])
}

func isConvertInput(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".tif" || ext == ".tiff" || rawconv.IsRaw(path)
}

func (u *ConvertUI) loadPreview(path string) {
	if u.busy {
		return
	}
	u.busy = true
	if u.status != nil {
		u.status.SetText("Loading " + filepath.Base(path) + "...")
	}
	go func() {
		defer func() { u.ui(func() { u.busy = false }) }()
		pix, w, h, note, err := loadLinearRGB16(path, 1600)
		u.ui(func() {
			if err != nil {
				u.log("load failed: " + err.Error())
				u.status.SetText("Load failed")
				u.cachePath, u.cachePix = "", nil
				return
			}
			u.cachePath, u.cachePix, u.cacheW, u.cacheH, u.cacheNote = path, pix, w, h, note
			u.renderPreviewFromCache()
		})
	}()
}

// renderPreviewFromCache runs Convert on the cached linear preview (no RAW decode).
func (u *ConvertUI) renderPreviewFromCache() {
	if len(u.cachePix) == 0 || u.cacheW == 0 {
		return
	}
	o := u.options()
	src := append([]uint16(nil), u.cachePix...)
	w, h, note, path := u.cacheW, u.cacheH, u.cacheNote, u.cachePath
	u.busy = true
	go func() {
		defer func() { u.ui(func() { u.busy = false }) }()
		out := convert.Process16Fast(src, w, h, o)
		u.ui(func() {
			u.pw, u.ph, u.pix = w, h, out
			u.makeBitmap()
			u.status.SetText(fmt.Sprintf("%s  preview %dx%d  %s  / %s", filepath.Base(path), w, h, note, o.Stock.Name))
			u.log("preview render: " + o.Stock.Name + " grain=" + o.Grain.Label())
			if u.view != nil {
				u.view.Invalidate()
			}
		})
	}()
}

func (u *ConvertUI) reprocess() {
	if u.cur < 0 || u.cur >= len(u.files) {
		return
	}
	path := u.files[u.cur]
	if u.cachePath == path && len(u.cachePix) > 0 {
		if u.busy {
			return
		}
		u.renderPreviewFromCache()
		return
	}
	u.loadPreview(path)
}

func (u *ConvertUI) makeBitmap() {
	if u.pw <= 0 || u.ph <= 0 || len(u.pix) < u.pw*u.ph*3 {
		return
	}
	rgba := make([]byte, u.pw*u.ph*4)
	for i, o := 0, 0; i < u.pw*u.ph; i++ {
		rgba[o] = byte(u.pix[i*3] >> 8)
		rgba[o+1] = byte(u.pix[i*3+1] >> 8)
		rgba[o+2] = byte(u.pix[i*3+2] >> 8)
		rgba[o+3] = 255
		o += 4
	}
	img := &image.RGBA{Pix: rgba, Stride: 4 * u.pw, Rect: image.Rect(0, 0, u.pw, u.ph)}
	bm, err := walk.NewBitmapFromImageForDPI(img, 96)
	if err != nil {
		return
	}
	if u.bmp != nil {
		u.bmp.Dispose()
	}
	u.bmp = bm
}

func (u *ConvertUI) paintView(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	if u.bmp == nil || u.view == nil {
		return nil
	}
	sz := u.view.ClientBounds().Size()
	bw, bh := u.bmp.Size().Width, u.bmp.Size().Height
	scale := math.Min(float64(sz.Width)/float64(bw), float64(sz.Height)/float64(bh))
	if scale <= 0 {
		return nil
	}
	dw, dh := int(float64(bw)*scale), int(float64(bh)*scale)
	x, y := (sz.Width-dw)/2, (sz.Height-dh)/2
	return canvas.DrawImageStretched(u.bmp, walk.Rectangle{X: x, Y: y, Width: dw, Height: dh})
}

func (u *ConvertUI) exportOne() {
	if u.cur < 0 || u.cur >= len(u.files) || u.busy {
		return
	}
	src := u.files[u.cur]
	u.busy = true
	if u.pb != nil {
		u.pb.SetValue(0)
	}
	o := u.options()
	go func() {
		defer u.ui(func() {
			u.busy = false
			if u.pb != nil {
				u.pb.SetValue(0)
			}
		})
		dst, err := convertFile(src, o, func(p float64) {
			u.ui(func() {
				if u.pb != nil {
					u.pb.SetValue(int(p * 100))
				}
			})
		})
		u.ui(func() {
			if err != nil {
				u.log("export failed: " + err.Error())
				walk.MsgBox(u.win(), "Convert", err.Error(), walk.MsgBoxIconError)
				return
			}
			u.log("wrote " + dst)
			u.status.SetText("Wrote " + filepath.Base(dst))
		})
	}()
}

func (u *ConvertUI) exportAll() {
	if len(u.files) == 0 || u.busy {
		return
	}
	u.busy = true
	files := append([]string(nil), u.files...)
	o := u.options()
	go func() {
		defer u.ui(func() {
			u.busy = false
			if u.pb != nil {
				u.pb.SetValue(0)
			}
		})
		var done, failed int
		for i, src := range files {
			dst, err := convertFile(src, o, nil)
			u.ui(func() {
				if u.pb != nil {
					u.pb.SetValue((i + 1) * 100 / len(files))
				}
				if err != nil {
					failed++
					u.log("fail " + filepath.Base(src) + ": " + err.Error())
				} else {
					done++
					u.log("wrote " + filepath.Base(dst))
				}
			})
		}
		u.ui(func() {
			u.status.SetText(fmt.Sprintf("Export finished: %d written, %d failed", done, failed))
		})
	}()
}

func convertFile(src string, o convert.Options, progress func(float64)) (string, error) {
	if progress != nil {
		progress(0.05)
	}
	pix, w, h, note, err := loadLinearRGB16(src, 0)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(note, "LibRaw") {
		parts := strings.SplitN(strings.TrimPrefix(note, "LibRaw "), " ", 2)
		if len(parts) >= 1 {
			o.Make = parts[0]
		}
		if len(parts) == 2 {
			o.Model = parts[1]
		}
	}
	if progress != nil {
		progress(0.4)
	}
	out := convert.Process16Fast(pix, w, h, o)
	if progress != nil {
		progress(0.8)
	}
	dst := convert.OutputPath(src, o.Stock)
	sw := o.Software
	if sw == "" {
		sw = appVersion
	}
	sw += " convert / " + o.Stock.Name
	desc, err := convert.Metadata(o).JSON(sw)
	if err != nil {
		return "", err
	}
	if err := rawconv.WriteTIFF16Description(dst, w, h, out, o.LZW, o.Make, o.Model, sw, desc); err != nil {
		return "", err
	}
	if progress != nil {
		progress(1)
	}
	return dst, nil
}

func loadLinearRGB16(path string, maxEdge int) (pix []uint16, w, h int, note string, err error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".tif" || ext == ".tiff" {
		im, e := cropper.Load(path)
		if e != nil {
			return nil, 0, 0, "", e
		}
		pix, w, h = im.Pix, im.W, im.H
		note = "TIFF"
	} else {
		img, e := rawconv.Decode(path)
		if e != nil {
			return nil, 0, 0, "", e
		}
		defer img.Free()
		n := img.Width * img.Height * 3
		pix = make([]uint16, n)
		copy(pix, img.Pix[:n])
		w, h = img.Width, img.Height
		note = "LibRaw " + strings.TrimSpace(img.Make+" "+img.Model)
	}
	if maxEdge > 0 && (w > maxEdge || h > maxEdge) {
		pix, w, h = downscaleNN(pix, w, h, maxEdge)
		note += " (preview)"
	}
	return pix, w, h, note, nil
}

func downscaleNN(pix []uint16, w, h, maxEdge int) ([]uint16, int, int) {
	scale := float64(maxEdge) / float64(w)
	if h > w {
		scale = float64(maxEdge) / float64(h)
	}
	nw := int(float64(w)*scale + 0.5)
	nh := int(float64(h)*scale + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	out := make([]uint16, nw*nh*3)
	for y := 0; y < nh; y++ {
		sy := y * h / nh
		for x := 0; x < nw; x++ {
			sx := x * w / nw
			si := (sy*w + sx) * 3
			di := (y*nw + x) * 3
			out[di], out[di+1], out[di+2] = pix[si], pix[si+1], pix[si+2]
		}
	}
	return out, nw, nh
}

func (u *ConvertUI) Page() TabPage {
	names := make([]string, len(convert.Stocks()))
	for i, s := range convert.Stocks() {
		names[i] = s.Name
	}
	info0 := ""
	if len(convert.Stocks()) > 0 {
		info0 = convert.Stocks()[0].Source
	}
	return TabPage{
		Title:  "  4. Convert  ",
		Layout: HBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 8},
		Children: []Widget{
			Composite{
				StretchFactor: 3,
				Layout:        VBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					CustomWidget{AssignTo: &u.view, PaintPixels: u.paintView, PaintMode: PaintBuffered, InvalidatesOnResize: true,
						MinSize: Size{Width: 420, Height: 280}, StretchFactor: 4,
						ToolTipText: "Preview of the simulated colour negative (transmittance)."},
					Label{AssignTo: &u.status, EllipsisMode: EllipsisEnd,
						Text: "Open a Fujifilm RAF (or linear TIFF). Leveled-scan look: base 0.5, Kodak Status M D-logH (still stocks), rebate border, grain option, nss.scan/1 for OFX.",
						TextColor: walk.RGB(128, 128, 136)},
					Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
						PushButton{Text: "Open files...", OnClicked: func() {
							dlg := new(walk.FileDialog)
							dlg.Title = "Open RAW or linear TIFF"
							dlg.Filter = "RAW / TIFF (*.raf;*.tif;*.tiff)|*.raf;*.tif;*.tiff|All files (*.*)|*.*"
							if ok, _ := dlg.ShowOpenMultiple(u.win()); ok {
								u.Open(dlg.FilePaths)
							}
						}},
						PushButton{Text: "Open folder...", OnClicked: func() {
							dlg := new(walk.FileDialog)
							dlg.Title = "Folder with RAF / TIFF"
							if ok, _ := dlg.ShowBrowseFolder(u.win()); ok {
								u.Open([]string{dlg.FilePath})
							}
						}},
						ComboBox{AssignTo: &u.fileCB, StretchFactor: 1, OnCurrentIndexChanged: func() {
							if i := u.fileCB.CurrentIndex(); i >= 0 && i < len(u.files) {
								u.cur = i
								u.loadPreview(u.files[i])
							}
						}},
					}},
				},
			},
			Composite{
				StretchFactor: 1,
				MinSize:       Size{Width: 360},
				MaxSize:       Size{Width: 380},
				Layout:        VBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					GroupBox{Title: "Film model", Layout: VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 4, Right: 8, Bottom: 6}}, Children: []Widget{
						Label{Text: "Stock"},
						ComboBox{AssignTo: &u.stockCB, Model: names, CurrentIndex: 0, OnCurrentIndexChanged: func() {
							if i := u.stockCB.CurrentIndex(); i >= 0 && i < len(convert.Stocks()) {
								u.stock = convert.Stocks()[i]
								if u.info != nil {
									u.info.SetText(u.stock.Source)
								}
								u.reprocess()
							}
						}},
						Label{AssignTo: &u.info, Text: info0, TextColor: walk.RGB(128, 128, 136), MinSize: Size{Height: 56}},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Exposure (EV)"},
							NumberEdit{AssignTo: &u.evNE, Value: 0, MinValue: -5, MaxValue: 5, Decimals: 2, Increment: 0.1,
								OnValueChanged: func() { u.ev = u.evNE.Value(); u.reprocess() }},
						}},
						CheckBox{AssignTo: &u.camChk, Text: "Apply Chart camera profile (X-T3 CamCal)", Checked: true, OnCheckedChanged: func() {
							u.useCam = u.camChk.Checked()
							u.reprocess()
						}, ToolTipText: "Uses activeCam() matrix from camera_profiles.json via colorsci.ApplyCam - same as Light leveling."},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Film-base border"},
							NumberEdit{AssignTo: &u.borderNE, Value: 0.04, MinValue: 0, MaxValue: 0.15, Decimals: 3, Increment: 0.005,
								OnValueChanged: func() { u.border = u.borderNE.Value(); u.reprocess() },
								ToolTipText: "Rebate width on each side as a fraction of the short edge (like a scanned 35mm frame edge)."},
						}},
						CheckBox{AssignTo: &u.softChk, Text: "Soft border edge", Checked: true, OnCheckedChanged: func() {
							u.softBorder = u.softChk.Checked()
							u.reprocess()
						}},
						Label{Text: "Film grain"},
						ComboBox{AssignTo: &u.grainCB, Model: []string{"Off", "ISO 100", "ISO 400"}, CurrentIndex: 0, OnCurrentIndexChanged: func() {
							switch u.grainCB.CurrentIndex() {
							case 1:
								u.grain = convert.GrainISO100
							case 2:
								u.grain = convert.GrainISO400
							default:
								u.grain = convert.GrainOff
	u.gStrength = 1
							}
							u.reprocess()
						}, ToolTipText: "Poisson dye-cloud grain; local transmittance equalised so mean tone stays put. ISO 100 finer; ISO 400 coarser."},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Grain strength"},
							NumberEdit{AssignTo: &u.gStrNE, Value: 1, MinValue: 0, MaxValue: 3, Decimals: 2, Increment: 0.1,
								OnValueChanged: func() { u.gStrength = u.gStrNE.Value(); u.reprocess() }},
						}},
					}},
					GroupBox{Title: "Export (16-bit TIFF, RAFBasename_Stock.tiff + nss.scan/1 metadata)", Layout: VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 4, Right: 8, Bottom: 6}}, Children: []Widget{
						Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
							PushButton{Text: "Export current", StretchFactor: 1, OnClicked: u.exportOne},
							PushButton{Text: "Export all in list", StretchFactor: 1, OnClicked: u.exportAll},
						}},
						ProgressBar{AssignTo: &u.pb, MaxSize: Size{Height: 14}},
						TextEdit{AssignTo: &u.logTE, ReadOnly: true, VScroll: true, MinSize: Size{Height: 120}},
					}},
					VSpacer{},
				},
			},
		},
	}
}
