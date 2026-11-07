package main

// Page 4: straighten + crop positive scans. Detection runs on a proxy in the
// background, the preview is the rotated proxy, export applies the same
// angle / rectangle to the full-resolution file.

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"basetool/cropper"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

// ---------- settings ----------

type CropConfig struct {
	Aspect   int // index into cropAspects
	Inset    int // index into cropInsets
	Grid     int
	OnImport int // 0 auto, 1 manual
	Interp   int
	OutMode  int // 0 "cropped" subfolder, 1 custom folder
	OutDir   string
	LZW      bool
	JPEGQ    int // index into cropJPEGQ
	Edges    bool
}

var (
	cropAspectNames = []string{"Auto 3:2 / 2:3", "3:2", "2:3", "4:3", "3:4", "4:5", "5:4", "1:1", "16:9", "6:7", "Original", "Free"}
	cropAspects     = []float64{cropper.AspectAuto, 1.5, 2.0 / 3, 4.0 / 3, 3.0 / 4, 0.8, 1.25, 1, 16.0 / 9, 6.0 / 7, cropper.AspectOriginal, cropper.AspectFree}
	cropInsetNames  = []string{"0", "0.5%", "1%", "2%", "3%"}
	cropInsets      = []float64{0, 0.005, 0.01, 0.02, 0.03}
	cropGridNames   = []string{"Grid off", "Thirds", "Fine grid"}
	cropJPEGQNames  = []string{"100", "98", "95", "92", "90", "85"}
	cropJPEGQ       = []int{100, 98, 95, 92, 90, 85}
)

func cropConfigFile() string { return filepath.Join(configDir(), "crop_config.txt") }

func loadCropConfig() CropConfig {
	c := CropConfig{Inset: 2, Grid: 1, LZW: true, JPEGQ: 2, Edges: true}
	b, err := os.ReadFile(cropConfigFile())
	if err != nil {
		return c
	}
	idx := func(v string, n int, def int) int {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 && i < n {
			return i
		}
		return def
	}
	for _, ln := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
		k, v, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		switch k {
		case "ASPECT":
			c.Aspect = idx(v, len(cropAspects), c.Aspect)
		case "INSET":
			c.Inset = idx(v, len(cropInsets), c.Inset)
		case "GRID":
			c.Grid = idx(v, len(cropGridNames), c.Grid)
		case "ON_IMPORT":
			c.OnImport = idx(v, 2, 0)
		case "INTERP":
			c.Interp = idx(v, 2, 0)
		case "OUT_MODE":
			c.OutMode = idx(v, 2, 0)
		case "OUT_DIR":
			c.OutDir = v
		case "LZW":
			c.LZW = v == "1"
		case "JPEG_Q":
			c.JPEGQ = idx(v, len(cropJPEGQ), c.JPEGQ)
		case "EDGES":
			c.Edges = v == "1"
		}
	}
	return c
}

func saveCropConfig(c CropConfig) {
	os.MkdirAll(configDir(), 0755)
	b := func(x bool) int {
		if x {
			return 1
		}
		return 0
	}
	s := fmt.Sprintf("ASPECT=%d\nINSET=%d\nGRID=%d\nON_IMPORT=%d\nINTERP=%d\nOUT_MODE=%d\nOUT_DIR=%s\nLZW=%d\nJPEG_Q=%d\nEDGES=%d\n",
		c.Aspect, c.Inset, c.Grid, c.OnImport, c.Interp, c.OutMode, c.OutDir, b(c.LZW), c.JPEGQ, b(c.Edges))
	os.WriteFile(cropConfigFile(), []byte(s), 0644)
}

// ---------- items ----------

const (
	ciPending = iota
	ciAnalyzing
	ciReady
	ciExporting
	ciDone
	ciError
)

type cropItem struct {
	Path, Name string
	State      int
	Status     string
	W, H, Bits int
	Scale      float64 // full-res px per proxy px
	Det        cropper.Detection
	Angle      float64
	Rect       cropper.Rect
	Manual     bool // angle or rectangle changed by hand, or detection failed
	RectEdited bool
	Out        string
}

func (it *cropItem) analysed() bool { return it.W > 0 }

type CropModel struct {
	walk.TableModelBase
	u *CropUI
}

func (m *CropModel) RowCount() int {
	m.u.mu.Lock()
	defer m.u.mu.Unlock()
	return len(m.u.items)
}

func (m *CropModel) Value(row, col int) interface{} {
	m.u.mu.Lock()
	defer m.u.mu.Unlock()
	if row >= len(m.u.items) {
		return ""
	}
	it := m.u.items[row]
	switch col {
	case 0:
		return it.Name
	case 1:
		if !it.analysed() {
			return ""
		}
		return fmt.Sprintf("%+.2f°", it.Angle)
	case 2:
		if !it.analysed() {
			return ""
		}
		_, _, w, h := cropper.IntRect(it.Rect, it.W, it.H)
		return fmt.Sprintf("%d×%d", w, h)
	case 3:
		if !it.analysed() {
			return ""
		}
		if it.Manual {
			return "Manual"
		}
		return fmt.Sprintf("Auto %.0f%%", 100*math.Min(it.Det.Confidence, 1))
	case 4:
		return it.Status
	}
	return ""
}

func (m *CropModel) StyleCell(style *walk.CellStyle) {
	m.u.mu.Lock()
	defer m.u.mu.Unlock()
	if style.Row() >= len(m.u.items) {
		return
	}
	it := m.u.items[style.Row()]
	switch {
	case it.State == ciError:
		style.TextColor = walk.RGB(240, 100, 86)
	case it.State == ciPending || it.State == ciAnalyzing:
		style.TextColor = walk.RGB(118, 118, 126)
	case it.State == ciExporting:
		style.TextColor = walk.RGB(240, 150, 70)
	case it.State == ciDone && style.Col() == 4:
		style.TextColor = walk.RGB(96, 204, 116)
	case style.Col() == 3 && it.analysed() && !it.Det.OK:
		style.TextColor = walk.RGB(240, 150, 70)
	}
}

// ---------- page state ----------

type cropDrag struct {
	mode   int // 0 none, 1 move, 2 corner, 3 edge
	handle int // corner 0..3 (tl,tr,br,bl) or edge 0..3 (l,t,r,b)
	start  cropper.Rect
	mx, my float64 // mouse at start, canvas px
}

type CropUI struct {
	mw  **walk.MainWindow
	cfg CropConfig
	mu  sync.Mutex

	items   []*cropItem
	proxies map[string]*cropper.Proxy
	lru     []string
	cur     int // selected item, -1 none
	queue   chan *cropItem
	loading map[string]bool

	// preview cache (UI thread only)
	rotKey  string
	rotRGBA []byte
	rotW    int
	rotH    int
	bmp     *walk.Bitmap
	bmpKey  string
	drag    cropDrag
	font    *walk.Font
	fontB   *walk.Font
	fontDPI int

	model                              *CropModel
	tv                                 *walk.TableView
	view                               *walk.CustomWidget
	slider                             *walk.CustomWidget
	aspectCB, insetCB, gridCB, onImpCB *walk.ComboBox
	interpCB, outCB, compCB, jpegCB    *walk.ComboBox
	edgesChk                           *walk.CheckBox
	angleEd, outEd                     *walk.LineEdit
	statusLbl, detLbl, countLbl        *walk.Label
	angleLbl                           *walk.Label
	expSelBtn, expAllBtn, chooseBtn    *walk.PushButton
	pb                                 *walk.ProgressBar
	logTE                              *walk.TextEdit
	exporting                          bool
	sliderDrag                         bool
}

func (u *CropUI) win() *walk.MainWindow { return *u.mw }

func (u *CropUI) ui(f func()) {
	if mw := u.win(); mw != nil {
		mw.Synchronize(f)
	}
}

func (u *CropUI) log(s string) {
	if u.logTE == nil {
		return
	}
	line := time.Now().Format("15:04:05") + "  " + s + "\r\n"
	t := u.logTE.Text() + line
	if len(t) > 20000 {
		t = t[len(t)-16000:]
	}
	u.logTE.SetText(t)
	u.logTE.SetTextSelection(len(t), len(t))
	u.logTE.ScrollToCaret()
}

func (u *CropUI) refresh() {
	u.model.PublishRowsReset()
	u.mu.Lock()
	n := len(u.items)
	u.mu.Unlock()
	if n > 0 {
		u.model.PublishRowsChanged(0, n-1)
	}
	if u.tv != nil {
		u.tv.Invalidate()
	}
	if u.countLbl != nil {
		u.countLbl.SetText(fmt.Sprintf("%d file(s)", n))
	}
}

func (u *CropUI) aspect() float64 { return cropAspects[u.cfg.Aspect] }
func (u *CropUI) inset() float64  { return cropInsets[u.cfg.Inset] }

// current returns the selected item (caller holds no lock; items are only
// replaced on the UI thread).
func (u *CropUI) current() *cropItem {
	if u.cur < 0 || u.cur >= len(u.items) {
		return nil
	}
	return u.items[u.cur]
}

// ---------- adding files / analysis ----------

func (u *CropUI) AddFiles(paths []string) int {
	var all []string
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			ents, _ := os.ReadDir(p)
			var sub []string
			for _, e := range ents {
				if !e.IsDir() {
					sub = append(sub, filepath.Join(p, e.Name()))
				}
			}
			sort.Strings(sub)
			all = append(all, sub...)
		} else {
			all = append(all, p)
		}
	}
	u.mu.Lock()
	have := map[string]bool{}
	for _, it := range u.items {
		have[strings.ToLower(it.Path)] = true
	}
	var added []*cropItem
	for _, p := range all {
		if !cropper.Supported(p) || have[strings.ToLower(p)] {
			continue
		}
		// do not re-import our own output
		if strings.Contains(strings.ToLower(filepath.Base(p)), "_crop") {
			continue
		}
		have[strings.ToLower(p)] = true
		it := &cropItem{Path: p, Name: filepath.Base(p), State: ciPending, Status: "Waiting"}
		u.items = append(u.items, it)
		added = append(added, it)
	}
	first := len(u.items) - len(added)
	u.mu.Unlock()
	for _, it := range added {
		u.queue <- it
	}
	u.refresh()
	if len(added) > 0 {
		if u.cur < 0 {
			u.selectRow(first)
		}
		u.statusLbl.SetText(fmt.Sprintf("%d image(s) added. Detecting edges...", len(added)))
	} else if len(paths) > 0 {
		u.statusLbl.SetText("No new images (TIFF 8/16-bit, JPEG, PNG). Files with _crop in the name are skipped.")
	}
	return len(added)
}

func (u *CropUI) selectRow(i int) {
	u.cur = i
	if u.tv != nil && i >= 0 && u.tv.CurrentIndex() != i {
		u.tv.SetCurrentIndex(i)
	}
	u.showCurrent()
}

func (u *CropUI) worker() {
	for it := range u.queue {
		u.analyse(it, false)
	}
}

// analyse loads the image, builds the proxy and (unless keep) detects edges.
func (u *CropUI) analyse(it *cropItem, keep bool) {
	u.mu.Lock()
	if it.State == ciAnalyzing {
		u.mu.Unlock()
		return
	}
	if !keep {
		it.State, it.Status = ciAnalyzing, "Detecting..."
	}
	u.loading[it.Path] = true
	u.mu.Unlock()
	u.ui(u.refresh)
	im, err := cropper.Load(it.Path)
	var p *cropper.Proxy
	var det cropper.Detection
	if err == nil {
		p = cropper.MakeProxy(im, cropper.ProxySide)
		if !keep {
			det = cropper.Detect(p)
		}
	}
	u.mu.Lock()
	delete(u.loading, it.Path)
	if err != nil {
		it.State, it.Status = ciError, "ERROR: "+err.Error()
		u.mu.Unlock()
		u.ui(func() { u.refresh(); u.showCurrent() })
		return
	}
	u.putProxy(it.Path, p)
	if !keep || !it.analysed() {
		it.W, it.H, it.Bits, it.Scale = im.W, im.H, im.Bits, p.Scale
		it.Det = det
		u.resetLocked(it, u.cfg.OnImport == 1)
		it.State = ciReady
		if det.OK && u.cfg.OnImport == 0 {
			it.Status = "Ready"
		} else if u.cfg.OnImport == 1 {
			it.Status = "Ready (manual)"
		} else {
			it.Status = "Ready - check by hand"
		}
	}
	u.mu.Unlock()
	im = nil
	u.ui(func() {
		u.refresh()
		if u.current() == it {
			u.showCurrent()
		}
	})
}

func (u *CropUI) putProxy(path string, p *cropper.Proxy) {
	u.proxies[path] = p
	for i, k := range u.lru {
		if k == path {
			u.lru = append(u.lru[:i], u.lru[i+1:]...)
			break
		}
	}
	u.lru = append(u.lru, path)
	for len(u.lru) > 10 {
		delete(u.proxies, u.lru[0])
		u.lru = u.lru[1:]
	}
}

// resetLocked puts it back to the detected (or manual full-frame) state.
func (u *CropUI) resetLocked(it *cropItem, manual bool) {
	it.RectEdited = false
	if manual || !it.Det.OK {
		it.Manual = true
		it.Angle = 0
		a := u.aspect()
		if a == cropper.AspectAuto {
			a = cropper.AspectOriginal
		}
		it.Rect = u.fullRect(it, a)
		return
	}
	it.Manual = false
	it.Angle = it.Det.Angle
	it.Rect = u.autoRect(it)
}

func (u *CropUI) fullRect(it *cropItem, a float64) cropper.Rect {
	if it.Angle == 0 && (a == cropper.AspectOriginal || a == cropper.AspectFree) {
		return cropper.Rect{X0: 0, Y0: 0, X1: float64(it.W), Y1: float64(it.H)}
	}
	hp := cropper.Constraints(it.W, it.H, it.Angle, nil, 0)
	r, _ := cropper.FitRect(hp, it.W, it.H, a, 0)
	return r
}

func (u *CropUI) autoRect(it *cropItem) cropper.Rect {
	if !it.Det.OK {
		a := u.aspect()
		if a == cropper.AspectAuto {
			a = cropper.AspectOriginal
		}
		return u.fullRect(it, a)
	}
	r, ok := cropper.AutoRect(it.W, it.H, it.Angle, &it.Det, it.Scale, u.aspect(), u.inset())
	if !ok {
		return u.fullRect(it, cropper.AspectOriginal)
	}
	return r
}

// lockAspect returns the aspect a hand-edited rectangle keeps (0 = free).
func (u *CropUI) lockAspect(it *cropItem, r cropper.Rect) float64 {
	a := u.aspect()
	switch a {
	case cropper.AspectFree:
		return 0
	case cropper.AspectAuto:
		if r.W() >= r.H() {
			return 1.5
		}
		return 2.0 / 3
	case cropper.AspectOriginal:
		return float64(it.W) / float64(it.H)
	}
	return a
}

// ---------- changes from controls ----------

func (u *CropUI) setAngle(v float64, edited bool) {
	it := u.current()
	if it == nil || !it.analysed() {
		return
	}
	v = math.Round(v/0.05) * 0.05
	if v > 5 {
		v = 5
	}
	if v < -5 {
		v = -5
	}
	u.mu.Lock()
	it.Angle = v
	if edited {
		it.Manual = true
	}
	if !it.RectEdited {
		it.Rect = u.autoRect(it)
	} else {
		hp := cropper.Constraints(it.W, it.H, it.Angle, nil, 0)
		it.Rect = cropper.FitInside(hp, it.W, it.H, it.Rect)
	}
	if it.State == ciDone {
		it.State, it.Status = ciReady, "Changed since export"
	}
	u.mu.Unlock()
	u.refresh()
	u.showCurrent()
}

// settingsChanged re-fits every rectangle that was not drawn by hand, and
// re-shapes the selected one if it was.
func (u *CropUI) settingsChanged(reshapeCurrent bool) {
	u.mu.Lock()
	cur := u.current()
	for _, it := range u.items {
		if !it.analysed() {
			continue
		}
		if !it.RectEdited {
			it.Rect = u.autoRect(it)
		} else if it == cur && reshapeCurrent {
			a := u.lockAspect(it, it.Rect)
			r := it.Rect
			if a > 0 {
				area := r.W() * r.H()
				w := math.Sqrt(area * a)
				h := w / a
				r = cropper.Rect{X0: r.Cx() - w/2, Y0: r.Cy() - h/2, X1: r.Cx() + w/2, Y1: r.Cy() + h/2}
			}
			hp := cropper.Constraints(it.W, it.H, it.Angle, nil, 0)
			it.Rect = cropper.FitInside(hp, it.W, it.H, r)
		}
	}
	u.mu.Unlock()
	u.refresh()
	u.showCurrent()
}

// ---------- preview ----------

func (u *CropUI) showCurrent() {
	it := u.current()
	if it == nil {
		u.angleLbl.SetText("—")
		u.detLbl.SetText("No image selected.")
		u.angleEd.SetText("")
		u.view.Invalidate()
		u.slider.Invalidate()
		return
	}
	u.mu.Lock()
	ang, det, manual, ok, st, edited := it.Angle, it.Det, it.Manual, it.analysed(), it.State, it.RectEdited
	_, _, cw, ch := cropper.IntRect(it.Rect, it.W, it.H)
	needProxy := ok && u.proxies[it.Path] == nil && !u.loading[it.Path]
	u.mu.Unlock()
	if needProxy {
		go u.analyse(it, true)
	}
	if !ok {
		u.angleLbl.SetText("—")
		u.angleEd.SetText("")
		if st == ciError {
			u.detLbl.SetText(it.Status)
		} else {
			u.detLbl.SetText("Detecting edges...")
		}
	} else {
		u.angleLbl.SetText(fmt.Sprintf("%+.2f°", ang))
		if !u.angleEd.Focused() {
			u.angleEd.SetText(fmt.Sprintf("%.2f", ang))
		}
		var s string
		if det.OK {
			s = fmt.Sprintf("Detected %+.2f°, confidence %.0f%%.\r\n%s.", det.Angle, 100*math.Min(det.Confidence, 1), det.Message)
		} else {
			s = det.Message + ". Showing the full image."
		}
		mode := "Auto"
		if manual {
			mode = "Manual"
		}
		if edited {
			mode += ", crop drawn by hand"
		}
		u.detLbl.SetText(s)
		u.statusLbl.SetText(fmt.Sprintf("%s  |  %s  |  %d×%d %d-bit  →  crop %d×%d  |  %s", it.Name, mode, it.W, it.H, it.Bits, cw, ch, ratioText(cw, ch)))
	}
	u.view.Invalidate()
	u.slider.Invalidate()
}

func ratioText(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	r := float64(w) / float64(h)
	for _, c := range []struct {
		a float64
		s string
	}{{1.5, "3:2"}, {2.0 / 3, "2:3"}, {4.0 / 3, "4:3"}, {0.75, "3:4"}, {0.8, "4:5"}, {1.25, "5:4"}, {1, "1:1"}, {16.0 / 9, "16:9"}, {6.0 / 7, "6:7"}, {7.0 / 6, "7:6"}} {
		if math.Abs(r/c.a-1) < 0.004 {
			return c.s
		}
	}
	return fmt.Sprintf("%.3f:1", r)
}

// layout of the preview: proxy canvas fitted into the widget.
type pvGeom struct {
	ok     bool
	ox, oy float64 // widget px of canvas (0,0)
	k      float64 // widget px per full-res px
	W, H   int
}

func (u *CropUI) geom(it *cropItem) pvGeom {
	if it == nil || !it.analysed() || u.view == nil {
		return pvGeom{}
	}
	cb := u.view.ClientBoundsPixels()
	m := float64(px(14, dpiOf(u.view)))
	aw, ah := float64(cb.Width)-2*m, float64(cb.Height)-2*m
	if aw < 10 || ah < 10 {
		return pvGeom{}
	}
	k := math.Min(aw/float64(it.W), ah/float64(it.H))
	return pvGeom{ok: true, k: k, W: it.W, H: it.H,
		ox: float64(cb.X) + (float64(cb.Width)-k*float64(it.W))/2,
		oy: float64(cb.Y) + (float64(cb.Height)-k*float64(it.H))/2}
}

func (u *CropUI) bitmapFor(it *cropItem, p *cropper.Proxy) *walk.Bitmap {
	key := fmt.Sprintf("%s|%.4f|%p", it.Path, it.Angle, p)
	if u.bmp != nil && u.bmpKey == key {
		return u.bmp
	}
	rgba := cropper.RenderProxy(p, it.Angle)
	img := &image.RGBA{Pix: rgba, Stride: 4 * p.W, Rect: image.Rect(0, 0, p.W, p.H)}
	bm, err := walk.NewBitmapFromImageForDPI(img, 96)
	if err != nil {
		return nil
	}
	if u.bmp != nil {
		u.bmp.Dispose()
	}
	u.bmp, u.bmpKey = bm, key
	return bm
}

func (u *CropUI) fonts() (*walk.Font, *walk.Font) {
	d := dpiOf(u.view)
	if u.font == nil || u.fontDPI != d {
		u.font, _ = walk.NewFont("Segoe UI", 9, 0)
		u.fontB, _ = walk.NewFont("Segoe UI", 9, walk.FontBold)
		u.fontDPI = d
	}
	return u.font, u.fontB
}

func (u *CropUI) paint(c *walk.Canvas, _ walk.Rectangle) error {
	cb := u.view.ClientBoundsPixels()
	d := dpiOf(u.view)
	fill(c, walk.RGB(18, 18, 20), cb)
	font, fontB := u.fonts()
	it := u.current()
	u.mu.Lock()
	var p *cropper.Proxy
	var r cropper.Rect
	var det cropper.Detection
	var ang float64
	ready := false
	if it != nil && it.analysed() {
		p = u.proxies[it.Path]
		r, det, ang = it.Rect, it.Det, it.Angle
		ready = true
	}
	u.mu.Unlock()
	msg := ""
	switch {
	case it == nil:
		msg = "Drop positive scans or a folder here, or use Add files.\r\nTIFF (8 / 16-bit), JPEG and PNG.\r\n\r\nEdges are found automatically, the picture is levelled\r\nand cropped to 3:2 / 2:3 inside the film rebate."
	case !ready && it.State == ciError:
		msg = it.Status
	case !ready || p == nil:
		msg = "Loading " + it.Name + " ..."
	}
	if msg != "" {
		if font != nil {
			fmtx := walk.TextCenter | walk.TextWordbreak
			tr := cb
			if m, _, err := c.MeasureTextPixels(msg, font, cb, fmtx); err == nil && m.Height < cb.Height {
				tr.Y = cb.Y + (cb.Height-m.Height)/2
				tr.Height = m.Height + 2
			}
			c.DrawTextPixels(msg, font, walk.RGB(128, 128, 136), tr, fmtx)
		}
		return nil
	}
	g := u.geom(it)
	if !g.ok {
		return nil
	}
	bm := u.bitmapFor(it, p)
	ir := walk.Rectangle{X: int(math.Round(g.ox)), Y: int(math.Round(g.oy)), Width: int(math.Round(g.k * float64(g.W))), Height: int(math.Round(g.k * float64(g.H)))}
	if bm != nil {
		c.DrawImageStretchedPixels(bm, ir)
	}
	tp := func(x, y float64) walk.Point {
		return walk.Point{X: int(math.Round(g.ox + x*g.k)), Y: int(math.Round(g.oy + y*g.k))}
	}
	a, b := tp(r.X0, r.Y0), tp(r.X1, r.Y1)
	cr := walk.Rectangle{X: a.X, Y: a.Y, Width: b.X - a.X, Height: b.Y - a.Y}
	// dim outside the crop
	if bm != nil {
		shade := func(x0, y0, x1, y1 int) {
			if x1 <= x0 || y1 <= y0 {
				return
			}
			fillAlpha(c, walk.Rectangle{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, 150)
		}
		shade(ir.X, ir.Y, ir.X+ir.Width, cr.Y)
		shade(ir.X, cr.Y+cr.Height, ir.X+ir.Width, ir.Y+ir.Height)
		shade(ir.X, cr.Y, cr.X, cr.Y+cr.Height)
		shade(cr.X+cr.Width, cr.Y, ir.X+ir.Width, cr.Y+cr.Height)
	}
	pen := func(col walk.Color, w int) walk.Pen {
		br, _ := walk.NewSolidColorBrush(col)
		if br == nil {
			return nil
		}
		pn, _ := walk.NewGeometricPen(walk.PenSolid|walk.PenCapFlat, w, br)
		br.Dispose()
		return pn
	}
	// detected edges
	if u.cfg.Edges && det.OK {
		if pg := pen(walk.RGB(80, 220, 120), px(2, d)); pg != nil {
			for _, e := range det.Edges {
				x0, y0 := cropper.RotFwd(ang, float64(g.W), float64(g.H), e.P0[0], e.P0[1])
				x1, y1 := cropper.RotFwd(ang, float64(g.W), float64(g.H), e.P1[0], e.P1[1])
				c.DrawLinePixels(pg, tp(x0, y0), tp(x1, y1))
			}
			pg.Dispose()
		}
	}
	// grid
	if u.cfg.Grid > 0 {
		n := 3
		if u.cfg.Grid == 2 {
			n = 8
		}
		if pg, _ := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(150, 150, 156)); pg != nil {
			for i := 1; i < n; i++ {
				x := cr.X + cr.Width*i/n
				y := cr.Y + cr.Height*i/n
				c.DrawLinePixels(pg, walk.Point{X: x, Y: cr.Y}, walk.Point{X: x, Y: cr.Y + cr.Height})
				c.DrawLinePixels(pg, walk.Point{X: cr.X, Y: y}, walk.Point{X: cr.X + cr.Width, Y: y})
			}
			pg.Dispose()
		}
	}
	// crop rectangle and handles
	if po := pen(cAccent, px(2, d)); po != nil {
		c.DrawRectanglePixels(po, cr)
		po.Dispose()
	}
	hs, ht := px(18, d), px(4, d)
	for _, cn := range []struct{ x, y, sx, sy int }{{cr.X, cr.Y, 1, 1}, {cr.X + cr.Width, cr.Y, -1, 1}, {cr.X + cr.Width, cr.Y + cr.Height, -1, -1}, {cr.X, cr.Y + cr.Height, 1, -1}} {
		x0 := cn.x - ht/2
		if cn.sx < 0 {
			x0 = cn.x - hs + ht/2
		}
		y0 := cn.y - ht/2
		if cn.sy < 0 {
			y0 = cn.y - ht/2
		}
		fill(c, cAccent, walk.Rectangle{X: x0, Y: y0, Width: hs, Height: ht})
		y0 = cn.y - ht/2
		if cn.sy < 0 {
			y0 = cn.y - hs + ht/2
		}
		x0 = cn.x - ht/2
		fill(c, cAccent, walk.Rectangle{X: x0, Y: y0, Width: ht, Height: hs})
	}
	for _, m := range []walk.Point{{X: cr.X + cr.Width/2, Y: cr.Y}, {X: cr.X + cr.Width/2, Y: cr.Y + cr.Height}, {X: cr.X, Y: cr.Y + cr.Height/2}, {X: cr.X + cr.Width, Y: cr.Y + cr.Height/2}} {
		s := px(8, d)
		fill(c, cAccent, walk.Rectangle{X: m.X - s/2, Y: m.Y - s/2, Width: s, Height: s})
	}
	// readout
	if fontB != nil {
		mode := "AUTO"
		col := walk.RGB(96, 204, 116)
		if it.Manual {
			mode, col = "MANUAL", cAccent
		}
		_, _, w, h := cropper.IntRect(r, g.W, g.H)
		txt := fmt.Sprintf("%s   %+.2f°   %d × %d   %s", mode, ang, w, h, ratioText(w, h))
		if det.OK {
			txt += fmt.Sprintf("   conf %.0f%%", 100*math.Min(det.Confidence, 1))
		}
		tr := walk.Rectangle{X: cb.X + px(8, d), Y: cb.Y + px(4, d), Width: cb.Width - px(16, d), Height: px(18, d)}
		fillAlpha(c, walk.Rectangle{X: tr.X - px(4, d), Y: tr.Y, Width: px(420, d), Height: tr.Height}, 120)
		c.DrawTextPixels(txt, fontB, col, tr, walk.TextLeft|walk.TextVCenter|walk.TextSingleLine)
	}
	return nil
}

// fillAlpha darkens r with black at the given opacity (0..255).
func fillAlpha(c *walk.Canvas, r walk.Rectangle, a byte) {
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	hdc := c.HDC()
	mem := win.CreateCompatibleDC(hdc)
	if mem == 0 {
		return
	}
	defer win.DeleteDC(mem)
	bmp := win.CreateCompatibleBitmap(hdc, 1, 1)
	if bmp == 0 {
		return
	}
	defer win.DeleteObject(win.HGDIOBJ(bmp))
	old := win.SelectObject(mem, win.HGDIOBJ(bmp))
	win.SetPixel(mem, 0, 0, 0)
	win.AlphaBlend(hdc, int32(r.X), int32(r.Y), int32(r.Width), int32(r.Height), mem, 0, 0, 1, 1, win.BLENDFUNCTION{BlendOp: 0, SourceConstantAlpha: a})
	win.SelectObject(mem, old)
}

// ---------- mouse on the preview ----------

func (u *CropUI) toCanvas(g pvGeom, x, y int) (float64, float64) {
	return (float64(x) - g.ox) / g.k, (float64(y) - g.oy) / g.k
}

// hit returns drag mode and handle for widget px (x, y).
func (u *CropUI) hit(it *cropItem, g pvGeom, x, y int) (int, int) {
	r := it.Rect
	tol := float64(px(9, dpiOf(u.view))) / g.k
	cx, cy := u.toCanvas(g, x, y)
	corners := [4][2]float64{{r.X0, r.Y0}, {r.X1, r.Y0}, {r.X1, r.Y1}, {r.X0, r.Y1}}
	for i, c := range corners {
		if math.Abs(cx-c[0]) <= tol && math.Abs(cy-c[1]) <= tol {
			return 2, i
		}
	}
	inY := cy >= r.Y0-tol && cy <= r.Y1+tol
	inX := cx >= r.X0-tol && cx <= r.X1+tol
	switch {
	case inY && math.Abs(cx-r.X0) <= tol:
		return 3, 0
	case inX && math.Abs(cy-r.Y0) <= tol:
		return 3, 1
	case inY && math.Abs(cx-r.X1) <= tol:
		return 3, 2
	case inX && math.Abs(cy-r.Y1) <= tol:
		return 3, 3
	}
	if cx > r.X0 && cx < r.X1 && cy > r.Y0 && cy < r.Y1 {
		return 1, 0
	}
	return 0, 0
}

func (u *CropUI) cursorFor(mode, h int) walk.Cursor {
	switch mode {
	case 1:
		return walk.CursorSizeAll()
	case 2:
		if h == 0 || h == 2 {
			return walk.CursorSizeNWSE()
		}
		return walk.CursorSizeNESW()
	case 3:
		if h == 0 || h == 2 {
			return walk.CursorSizeWE()
		}
		return walk.CursorSizeNS()
	}
	return walk.CursorArrow()
}

func (u *CropUI) mouseDown(x, y int, b walk.MouseButton) {
	it := u.current()
	if b != walk.LeftButton || it == nil || !it.analysed() || u.exporting {
		return
	}
	g := u.geom(it)
	if !g.ok {
		return
	}
	mode, h := u.hit(it, g, x, y)
	if mode == 0 {
		return
	}
	mx, my := u.toCanvas(g, x, y)
	u.drag = cropDrag{mode: mode, handle: h, start: it.Rect, mx: mx, my: my}
	win.SetCapture(u.view.Handle())
}

func (u *CropUI) mouseMove(x, y int, b walk.MouseButton) {
	it := u.current()
	if it == nil || !it.analysed() {
		return
	}
	g := u.geom(it)
	if !g.ok {
		return
	}
	if u.drag.mode == 0 {
		mode, h := u.hit(it, g, x, y)
		u.view.SetCursor(u.cursorFor(mode, h))
		return
	}
	mx, my := u.toCanvas(g, x, y)
	hp := cropper.Constraints(it.W, it.H, it.Angle, nil, 0)
	s := u.drag.start
	var r cropper.Rect
	switch u.drag.mode {
	case 1:
		dx, dy := mx-u.drag.mx, my-u.drag.my
		r = cropper.Rect{X0: s.X0 + dx, Y0: s.Y0 + dy, X1: s.X1 + dx, Y1: s.Y1 + dy}
		r = cropper.FitInside(hp, it.W, it.H, r)
	case 2, 3:
		r = u.resize(it, hp, s, mx, my)
	}
	u.mu.Lock()
	it.Rect = r
	u.mu.Unlock()
	u.view.Invalidate()
}

// resize drags a corner or an edge of s to (mx, my), keeping the aspect lock
// and the rectangle inside the rotated image.
func (u *CropUI) resize(it *cropItem, hp []cropper.HalfPlane, s cropper.Rect, mx, my float64) cropper.Rect {
	a := u.lockAspect(it, s)
	minS := 16 * it.Scale
	h := u.drag.handle
	var make func(f float64) cropper.Rect
	if u.drag.mode == 2 {
		// anchor = opposite corner
		ax, ay := s.X0, s.Y0
		sx, sy := 1.0, 1.0
		switch h {
		case 0:
			ax, ay, sx, sy = s.X1, s.Y1, -1, -1
		case 1:
			ax, ay, sx, sy = s.X0, s.Y1, 1, -1
		case 2:
			ax, ay, sx, sy = s.X0, s.Y0, 1, 1
		case 3:
			ax, ay, sx, sy = s.X1, s.Y0, -1, 1
		}
		w := math.Max(minS, (mx-ax)*sx)
		hh := math.Max(minS, (my-ay)*sy)
		if a > 0 {
			if w/hh > a {
				hh = w / a
			} else {
				w = hh * a
			}
		}
		make = func(f float64) cropper.Rect {
			ww, h2 := w*f, hh*f
			x0, x1 := ax, ax+sx*ww
			y0, y1 := ay, ay+sy*h2
			return cropper.Rect{X0: math.Min(x0, x1), Y0: math.Min(y0, y1), X1: math.Max(x0, x1), Y1: math.Max(y0, y1)}
		}
	} else {
		make = func(f float64) cropper.Rect {
			r := s
			switch h {
			case 0:
				r.X0 = math.Min(mx, s.X1-minS)
			case 2:
				r.X1 = math.Max(mx, s.X0+minS)
			case 1:
				r.Y0 = math.Min(my, s.Y1-minS)
			case 3:
				r.Y1 = math.Max(my, s.Y0+minS)
			}
			// blend towards s by f
			r = cropper.Rect{X0: s.X0 + (r.X0-s.X0)*f, Y0: s.Y0 + (r.Y0-s.Y0)*f, X1: s.X1 + (r.X1-s.X1)*f, Y1: s.Y1 + (r.Y1-s.Y1)*f}
			if a > 0 {
				if h == 0 || h == 2 {
					nh := r.W() / a
					r.Y0, r.Y1 = s.Cy()-nh/2, s.Cy()+nh/2
				} else {
					nw := r.H() * a
					r.X0, r.X1 = s.Cx()-nw/2, s.Cx()+nw/2
				}
			}
			return r
		}
	}
	if r := make(1); cropper.Fits(hp, r) {
		return r
	}
	if u.drag.mode == 2 {
		// shrink about the anchor until it fits
		lo, hi := 0.0, 1.0
		for i := 0; i < 30; i++ {
			m := (lo + hi) / 2
			if cropper.Fits(hp, make(m)) {
				lo = m
			} else {
				hi = m
			}
		}
		if r := make(lo); lo > 0 && r.W() >= minS {
			return r
		}
		return s
	}
	lo, hi := 0.0, 1.0
	for i := 0; i < 30; i++ {
		m := (lo + hi) / 2
		if cropper.Fits(hp, make(m)) {
			lo = m
		} else {
			hi = m
		}
	}
	return make(lo)
}

func (u *CropUI) mouseUp(x, y int, b walk.MouseButton) {
	if u.drag.mode == 0 {
		return
	}
	win.ReleaseCapture()
	it := u.current()
	changed := it != nil && it.Rect != u.drag.start
	u.drag = cropDrag{}
	if changed {
		u.mu.Lock()
		it.RectEdited, it.Manual = true, true
		if it.State == ciDone {
			it.State, it.Status = ciReady, "Changed since export"
		}
		u.mu.Unlock()
		u.refresh()
	}
	u.showCurrent()
}

// ---------- angle slider (custom dark) ----------

const sliderMax = 5.0

func (u *CropUI) sliderGeom() (x0, x1, cy int) {
	cb := u.slider.ClientBoundsPixels()
	m := px(10, dpiOf(u.slider))
	return cb.X + m, cb.X + cb.Width - m, cb.Y + cb.Height/2
}

func (u *CropUI) paintSlider(c *walk.Canvas, _ walk.Rectangle) error {
	cb := u.slider.ClientBoundsPixels()
	d := dpiOf(u.slider)
	fill(c, bgAt(win.GetParent(u.slider.Handle())), cb)
	x0, x1, cy := u.sliderGeom()
	th := px(4, d)
	roundBox(c, cField, cFieldLine, walk.Rectangle{X: x0, Y: cy - th/2, Width: x1 - x0, Height: th}, th/2)
	pos := func(v float64) int { return x0 + int(math.Round((v+sliderMax)/(2*sliderMax)*float64(x1-x0))) }
	for v := -5; v <= 5; v++ {
		hgt := px(4, d)
		col := cFieldLine
		if v == 0 {
			hgt = px(8, d)
			col = cDim
		}
		fill(c, col, walk.Rectangle{X: pos(float64(v)), Y: cy + th/2 + px(3, d), Width: 1, Height: hgt})
	}
	it := u.current()
	if it == nil || !it.analysed() {
		return nil
	}
	v := it.Angle
	zx, vx := pos(0), pos(v)
	if vx != zx {
		a, b := zx, vx
		if a > b {
			a, b = b, a
		}
		fill(c, cAccent, walk.Rectangle{X: a, Y: cy - th/2, Width: b - a, Height: th})
	}
	if it.Det.OK {
		dx := pos(it.Det.Angle)
		fill(c, walk.RGB(80, 220, 120), walk.Rectangle{X: dx - px(1, d), Y: cy - px(10, d), Width: px(2, d), Height: px(6, d)})
	}
	r := px(7, d)
	knob := walk.Rectangle{X: vx - r, Y: cy - r, Width: 2 * r, Height: 2 * r}
	col := cAccent
	if u.sliderDrag {
		col = cAccentHot
	}
	roundBox(c, col, walk.RGB(255, 196, 140), knob, r)
	return nil
}

func (u *CropUI) sliderValue(x int) float64 {
	x0, x1, _ := u.sliderGeom()
	if x1 <= x0 {
		return 0
	}
	return (float64(x-x0)/float64(x1-x0))*2*sliderMax - sliderMax
}

// ---------- export ----------

func (u *CropUI) outPath(src string) (string, error) {
	dir := filepath.Join(filepath.Dir(src), "cropped")
	if u.cfg.OutMode == 1 {
		if u.cfg.OutDir == "" {
			return "", fmt.Errorf("choose the output folder first")
		}
		dir = u.cfg.OutDir
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	ext := filepath.Ext(src)
	for n := 1; n < 1000; n++ {
		name := base + "_crop" + ext
		if n > 1 {
			name = fmt.Sprintf("%s_crop%d%s", base, n, ext)
		}
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if _, err := os.Stat(p + ".part"); os.IsNotExist(err) {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("too many existing _crop files")
}

func (u *CropUI) export(all bool) {
	if u.exporting {
		return
	}
	var list []*cropItem
	u.mu.Lock()
	if all {
		list = append(list, u.items...)
	} else if u.tv != nil {
		for _, i := range u.tv.SelectedIndexes() {
			if i >= 0 && i < len(u.items) {
				list = append(list, u.items[i])
			}
		}
		if len(list) == 0 && u.current() != nil {
			list = append(list, u.current())
		}
	}
	var todo []*cropItem
	for _, it := range list {
		if it.State != ciError {
			todo = append(todo, it)
		}
	}
	u.mu.Unlock()
	if len(todo) == 0 {
		u.statusLbl.SetText("Nothing to export. Add images first.")
		return
	}
	if u.cfg.OutMode == 1 && u.cfg.OutDir == "" {
		walk.MsgBox(u.win(), "Export", "Choose the output folder first, or use the \"cropped\" subfolder.", walk.MsgBoxIconInformation)
		return
	}
	u.exporting = true
	u.expSelBtn.SetEnabled(false)
	u.expAllBtn.SetEnabled(false)
	u.pb.SetRange(0, len(todo))
	u.pb.SetValue(0)
	interp := u.cfg.Interp
	opt := cropper.SaveOptions{LZW: u.cfg.LZW, JPEGQuality: cropJPEGQ[u.cfg.JPEGQ], Software: appVersion}
	u.log(fmt.Sprintf("Export %d image(s): %s, %s.", len(todo), map[bool]string{true: "TIFF LZW", false: "TIFF uncompressed"}[opt.LZW], []string{"bicubic", "Lanczos-3"}[interp]))
	go func() {
		done, failed := 0, 0
		for i, it := range todo {
			// wait for detection if it is still queued
			for k := 0; k < 600; k++ {
				u.mu.Lock()
				ready := it.analysed() || it.State == ciError
				u.mu.Unlock()
				if ready {
					break
				}
				if k == 0 {
					go u.analyse(it, false)
				}
				time.Sleep(100 * time.Millisecond)
			}
			u.mu.Lock()
			if !it.analysed() {
				u.mu.Unlock()
				failed++
				continue
			}
			it.State, it.Status = ciExporting, "Exporting..."
			ang, r := it.Angle, it.Rect
			u.mu.Unlock()
			u.ui(u.refresh)
			t0 := time.Now()
			out, err := u.outPath(it.Path)
			var res *cropper.Image
			if err == nil {
				var im *cropper.Image
				im, err = cropper.Load(it.Path)
				if err == nil {
					res = cropper.Apply(im, ang, r, interp)
					im = nil
					err = cropper.Save(out, res, opt)
				}
			}
			secs := time.Since(t0).Seconds()
			u.mu.Lock()
			var msg string
			if err != nil {
				it.State, it.Status = ciError, "ERROR: "+err.Error()
				msg = it.Name + ": " + it.Status
				failed++
			} else {
				it.State, it.Out = ciDone, out
				it.Status = "Exported " + filepath.Base(out)
				msg = fmt.Sprintf("%s → %s  (%+.2f°, %d×%d %d-bit, %.1f s)", it.Name, out, ang, res.W, res.H, res.Bits, secs)
				done++
			}
			u.mu.Unlock()
			res = nil
			n := i + 1
			u.ui(func() {
				u.refresh()
				u.pb.SetValue(n)
				u.log(msg)
			})
		}
		u.ui(func() {
			u.exporting = false
			u.expSelBtn.SetEnabled(true)
			u.expAllBtn.SetEnabled(true)
			s := fmt.Sprintf("Export finished: %d written, %d failed. Existing files are never overwritten.", done, failed)
			u.log(s)
			u.statusLbl.SetText(s)
		})
	}()
}

// ---------- page ----------

func (u *CropUI) Page() TabPage {
	u.cfg = loadCropConfig()
	u.model = &CropModel{u: u}
	u.proxies = map[string]*cropper.Proxy{}
	u.loading = map[string]bool{}
	u.cur = -1
	u.queue = make(chan *cropItem, 4096)
	for i := 0; i < 2; i++ {
		go u.worker()
	}
	mw := u.win
	grey := walk.RGB(110, 110, 110)
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	save := func() { saveCropConfig(u.cfg) }
	nudge := func(dv float64) func() {
		return func() {
			if it := u.current(); it != nil {
				u.setAngle(it.Angle+dv, true)
			}
		}
	}
	return TabPage{
		Title:  "  4. Crop  ",
		Layout: HBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 8},
		Children: []Widget{
			Composite{
				StretchFactor: 3,
				Layout:        VBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					CustomWidget{
						AssignTo:            &u.view,
						PaintPixels:         u.paint,
						PaintMode:           PaintBuffered,
						InvalidatesOnResize: true,
						MinSize:             Size{Width: 420, Height: 260},
						StretchFactor:       4,
						OnMouseDown:         u.mouseDown,
						OnMouseMove:         u.mouseMove,
						OnMouseUp:           u.mouseUp,
					},
					Label{AssignTo: &u.statusLbl, Text: "Drop positive scans here. Drag inside the orange frame to move it, drag its corners or sides to resize.", TextColor: grey},
					Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
						PushButton{Text: "Add files...", OnClicked: func() {
							dlg := new(walk.FileDialog)
							dlg.Title = "Select positive scans"
							dlg.Filter = "Images (*.tif;*.tiff;*.jpg;*.jpeg;*.png)|*.tif;*.tiff;*.jpg;*.jpeg;*.png|All files (*.*)|*.*"
							if ok, _ := dlg.ShowOpenMultiple(mw()); ok {
								u.AddFiles(dlg.FilePaths)
							}
						}},
						PushButton{Text: "Add folder...", OnClicked: func() {
							dlg := new(walk.FileDialog)
							dlg.Title = "Select a folder with positive scans"
							if ok, _ := dlg.ShowBrowseFolder(mw()); ok {
								u.AddFiles([]string{dlg.FilePath})
							}
						}},
						PushButton{Text: "Remove selected", OnClicked: func() {
							if u.exporting {
								return
							}
							del := map[int]bool{}
							for _, i := range u.tv.SelectedIndexes() {
								del[i] = true
							}
							u.mu.Lock()
							var keep []*cropItem
							for i, it := range u.items {
								if !del[i] {
									keep = append(keep, it)
								}
							}
							u.items = keep
							u.mu.Unlock()
							u.cur = -1
							u.refresh()
							if len(keep) > 0 {
								u.selectRow(0)
							} else {
								u.showCurrent()
							}
						}},
						PushButton{Text: "Clear", OnClicked: func() {
							if u.exporting {
								return
							}
							u.mu.Lock()
							u.items = nil
							u.mu.Unlock()
							u.cur = -1
							u.refresh()
							u.showCurrent()
						}},
						Label{AssignTo: &u.countLbl, Text: "0 file(s)", TextColor: grey},
						HSpacer{},
					}},
					TableView{
						AssignTo:         &u.tv,
						AlternatingRowBG: true,
						ColumnsOrderable: false,
						MultiSelection:   true,
						Model:            u.model,
						StretchFactor:    1,
						MinSize:          Size{Height: 110},
						MaxSize:          Size{Height: 170},
						Columns: []TableViewColumn{
							{Title: "File", Width: 220},
							{Title: "Angle", Width: 70},
							{Title: "Crop", Width: 100},
							{Title: "Mode", Width: 90},
							{Title: "Status", Width: 240},
						},
						OnCurrentIndexChanged: func() {
							if i := u.tv.CurrentIndex(); i >= 0 && i != u.cur {
								u.cur = i
								u.showCurrent()
							}
						},
					},
				},
			},
			Composite{
				StretchFactor: 1,
				MinSize:       Size{Width: 350},
				MaxSize:       Size{Width: 350},
				Layout:        VBox{MarginsZero: true, Spacing: 4},
				Children: []Widget{
					Card{
						Title:  "Crop",
						Icon:   iconCrop,
						Layout: Grid{Columns: 2, Spacing: 4, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 6}},
						Children: []Widget{
							Label{Text: "Aspect"},
							ComboBox{AssignTo: &u.aspectCB, Model: cropAspectNames, CurrentIndex: u.cfg.Aspect, OnCurrentIndexChanged: func() {
								if i := u.aspectCB.CurrentIndex(); i >= 0 {
									u.cfg.Aspect = i
									save()
									u.settingsChanged(true)
								}
							}, ToolTipText: "Auto: 3:2 for landscape frames, 2:3 for portrait frames. Free: any shape, drag the sides."},
							Label{Text: "Inset"},
							ComboBox{AssignTo: &u.insetCB, Model: cropInsetNames, CurrentIndex: u.cfg.Inset, OnCurrentIndexChanged: func() {
								if i := u.insetCB.CurrentIndex(); i >= 0 {
									u.cfg.Inset = i
									save()
									u.settingsChanged(false)
								}
							}, ToolTipText: "Extra margin inside the detected picture edge on every side, so no film base or soft edge is left."},
							Label{Text: "Overlay"},
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								ComboBox{AssignTo: &u.gridCB, Model: cropGridNames, CurrentIndex: u.cfg.Grid, StretchFactor: 1, OnCurrentIndexChanged: func() {
									if i := u.gridCB.CurrentIndex(); i >= 0 {
										u.cfg.Grid = i
										save()
										u.view.Invalidate()
									}
								}},
								CheckBox{AssignTo: &u.edgesChk, Text: "Edges", Checked: u.cfg.Edges, OnCheckedChanged: func() {
									u.cfg.Edges = u.edgesChk.Checked()
									save()
									u.view.Invalidate()
								}, ToolTipText: "Show the detected picture edges in green."},
							}},
							Label{Text: "On import"},
							ComboBox{AssignTo: &u.onImpCB, Model: []string{"Auto straighten + crop", "Manual (full image)"}, CurrentIndex: u.cfg.OnImport, OnCurrentIndexChanged: func() {
								if i := u.onImpCB.CurrentIndex(); i >= 0 {
									u.cfg.OnImport = i
									save()
								}
							}},
							Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
								PushButton{Text: "Reset to auto", StretchFactor: 1, OnClicked: func() {
									it := u.current()
									if it == nil || !it.analysed() {
										return
									}
									u.mu.Lock()
									u.resetLocked(it, false)
									u.mu.Unlock()
									u.refresh()
									u.showCurrent()
								}, ToolTipText: "Back to the detected angle and the automatic crop."},
								PushButton{Text: "Full image", StretchFactor: 1, OnClicked: func() {
									it := u.current()
									if it == nil || !it.analysed() {
										return
									}
									u.mu.Lock()
									u.resetLocked(it, true)
									u.mu.Unlock()
									u.refresh()
									u.showCurrent()
								}, ToolTipText: "Manual mode: angle 0, crop = whole image (in the chosen aspect)."},
								PushButton{Text: "Re-detect", StretchFactor: 1, OnClicked: func() {
									it := u.current()
									if it == nil {
										return
									}
									u.mu.Lock()
									it.W = 0
									it.State = ciPending
									u.mu.Unlock()
									go u.analyse(it, false)
								}},
							}},
							Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
								PushButton{Text: "Apply crop settings to all", StretchFactor: 1, OnClicked: func() {
									u.mu.Lock()
									for _, it := range u.items {
										if it.analysed() && !it.Manual {
											it.RectEdited = false
											it.Rect = u.autoRect(it)
										}
									}
									u.mu.Unlock()
									u.refresh()
									u.showCurrent()
								}, ToolTipText: "Recompute the automatic crop of every image that was not adjusted by hand."},
							}},
						},
					},
					Card{
						Title:    "Rotation",
						OnReset:  func() { u.setAngle(0, true) },
						ResetTip: "Set the rotation back to 0°.",
						Layout:   VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 6}},
						Children: []Widget{
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								Label{AssignTo: &u.angleLbl, Text: "—", Font: Font{Family: "Consolas", PointSize: 14, Bold: true}, TextColor: walk.RGB(232, 124, 44)},
								HSpacer{},
								Label{Text: "Angle"},
								LineEdit{AssignTo: &u.angleEd, MaxSize: Size{Width: 64}, OnEditingFinished: func() {
									v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(u.angleEd.Text(), "°")), 64)
									if err == nil {
										u.setAngle(v, true)
									}
								}, ToolTipText: "Degrees, positive = counter-clockwise. Steps of 0.05°, at most ±5°."},
							}},
							CustomWidget{
								AssignTo:            &u.slider,
								PaintPixels:         u.paintSlider,
								PaintMode:           PaintBuffered,
								InvalidatesOnResize: true,
								MinSize:             Size{Height: 30},
								MaxSize:             Size{Height: 30},
								OnMouseDown: func(x, y int, b walk.MouseButton) {
									if b == walk.LeftButton && u.current() != nil && u.current().analysed() {
										u.sliderDrag = true
										win.SetCapture(u.slider.Handle())
										u.setAngle(u.sliderValue(x), true)
									}
								},
								OnMouseMove: func(x, y int, b walk.MouseButton) {
									if u.sliderDrag {
										u.setAngle(u.sliderValue(x), true)
									}
								},
								OnMouseUp: func(x, y int, b walk.MouseButton) {
									if u.sliderDrag {
										u.sliderDrag = false
										win.ReleaseCapture()
										u.slider.Invalidate()
									}
								},
							},
							Composite{Layout: HBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
								PushButton{Text: "-0.5°", StretchFactor: 1, OnClicked: nudge(-0.5)},
								PushButton{Text: "-0.05°", StretchFactor: 1, OnClicked: nudge(-0.05)},
								PushButton{Text: "0", StretchFactor: 1, OnClicked: func() { u.setAngle(0, true) }},
								PushButton{Text: "+0.05°", StretchFactor: 1, OnClicked: nudge(0.05)},
								PushButton{Text: "+0.5°", StretchFactor: 1, OnClicked: nudge(0.5)},
							}},
							Label{AssignTo: &u.detLbl, Text: "No image selected.", TextColor: grey, MinSize: Size{Height: 30}},
						},
					},
					Card{
						Title:  "Export",
						Icon:   iconDevelop,
						Layout: Grid{Columns: 2, Spacing: 4, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 6}},
						Children: []Widget{
							Label{Text: "Save to"},
							ComboBox{AssignTo: &u.outCB, Model: []string{"\"cropped\" subfolder next to each file", "Custom folder"}, CurrentIndex: u.cfg.OutMode, OnCurrentIndexChanged: func() {
								if i := u.outCB.CurrentIndex(); i >= 0 {
									u.cfg.OutMode = i
									save()
									u.outEd.SetEnabled(i == 1)
									u.chooseBtn.SetEnabled(i == 1)
								}
							}},
							Label{Text: "Folder"},
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								LineEdit{AssignTo: &u.outEd, Text: u.cfg.OutDir, Enabled: u.cfg.OutMode == 1, OnEditingFinished: func() {
									u.cfg.OutDir = strings.TrimSpace(u.outEd.Text())
									save()
								}},
								PushButton{AssignTo: &u.chooseBtn, Text: "Choose...", Enabled: u.cfg.OutMode == 1, MaxSize: Size{Width: 70}, OnClicked: func() {
									dlg := new(walk.FileDialog)
									dlg.Title = "Folder for the cropped images"
									dlg.FilePath = u.cfg.OutDir
									if ok, _ := dlg.ShowBrowseFolder(mw()); ok {
										u.cfg.OutDir = dlg.FilePath
										u.outEd.SetText(dlg.FilePath)
										save()
									}
								}},
							}},
							Label{Text: "TIFF"},
							ComboBox{AssignTo: &u.compCB, Model: []string{"None (fastest)", "LZW + predictor (lossless)"}, CurrentIndex: b2i(u.cfg.LZW), OnCurrentIndexChanged: func() {
								u.cfg.LZW = u.compCB.CurrentIndex() == 1
								save()
							}},
							Label{Text: "JPEG quality"},
							ComboBox{AssignTo: &u.jpegCB, Model: cropJPEGQNames, CurrentIndex: u.cfg.JPEGQ, OnCurrentIndexChanged: func() {
								if i := u.jpegCB.CurrentIndex(); i >= 0 {
									u.cfg.JPEGQ = i
									save()
								}
							}},
							Label{Text: "Resampling"},
							ComboBox{AssignTo: &u.interpCB, Model: []string{"Bicubic (Catmull-Rom)", "Lanczos-3 (sharpest)"}, CurrentIndex: u.cfg.Interp, OnCurrentIndexChanged: func() {
								if i := u.interpCB.CurrentIndex(); i >= 0 {
									u.cfg.Interp = i
									save()
								}
							}, ToolTipText: "Used only when the angle is not 0. At 0° the crop is a lossless pixel copy."},
							Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
								PushButton{AssignTo: &u.expSelBtn, Text: "Export selected", MinSize: Size{Height: 32}, StretchFactor: 1, OnClicked: func() { u.export(false) }},
								PushButton{AssignTo: &u.expAllBtn, Text: "Export all", MinSize: Size{Height: 32}, StretchFactor: 1, Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}, OnClicked: func() { u.export(true) }},
							}},
							ProgressBar{AssignTo: &u.pb, ColumnSpan: 2, MaxSize: Size{Height: 8}},
							TextEdit{AssignTo: &u.logTE, ColumnSpan: 2, ReadOnly: true, VScroll: true, MinSize: Size{Height: 50},
								Text: "Output: <name>_crop.<same format>, same bit depth, ICC profile and metadata kept. Existing files are never overwritten."},
						},
					},
					VSpacer{},
				},
			},
		},
	}
}
