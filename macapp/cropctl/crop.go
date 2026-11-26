package cropctl

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"basetool/cropper"
)

// CropModel is the file table (Value and RowCount are in copied.go).
type CropModel struct{ u *CropUI }

// CropUI is the Crop page. The fields up to drag are the Windows ones the
// copied methods use; the exported fields are what the walk widgets showed.
type CropUI struct {
	h   Host
	cfg CropConfig
	mu  sync.Mutex

	items   []*cropItem
	proxies map[string]*cropper.Proxy
	lru     []string
	cur     int // selected item, -1 none
	queue   chan *cropItem
	loading map[string]bool
	drag    cropDrag

	model     *CropModel
	exporting bool
	sel       map[int]bool // table selection (MultiSelection)

	// Labels and fields of the page (UI goroutine).
	StatusLbl, DetLbl, CountLbl, AngleLbl string
	AngleText                             string // the Angle LineEdit
	AngleFocused                          bool   // the view reports focus, like angleEd.Focused()
	AngleSeq                              int    // bumps when AngleText is set by the page
	LogText                               string
	LogSeq                                int
	PBMax, PBVal                          int
	Seq                                   int // bumps on every table / preview change
	SliderDrag                            bool
}

// NewCrop is Page(): settings, the queue and two detection workers.
func NewCrop(h Host) *CropUI {
	u := &CropUI{h: h, cfg: loadCropConfig(), proxies: map[string]*cropper.Proxy{}, loading: map[string]bool{},
		cur: -1, queue: make(chan *cropItem, 4096), sel: map[int]bool{},
		StatusLbl: "Drop positive scans here. Drag inside the orange frame to move it, drag its corners or sides to resize.",
		DetLbl:    "No image selected.", CountLbl: "0 file(s)", AngleLbl: "—", LogText: "Output: <name>_crop.<same format>", LogSeq: 1}
	u.model = &CropModel{u: u}
	for i := 0; i < 2; i++ {
		go u.worker()
	}
	return u
}

// Cfg is the current settings (read on the UI goroutine).
func (u *CropUI) Cfg() CropConfig { return u.cfg }

// set changes the settings under mu (the detection workers read cfg
// under mu in analyse) and saves them.
func (u *CropUI) set(f func(c *CropConfig)) {
	u.mu.Lock()
	f(&u.cfg)
	c := u.cfg
	u.mu.Unlock()
	saveCropConfig(c)
}

func (u *CropUI) ui(f func()) { u.h.Post(f) }

func (u *CropUI) log(s string) {
	line := time.Now().Format("15:04:05") + "  " + s + "\n"
	t := u.LogText + "\n" + line
	if u.LogText == "" {
		t = line
	}
	if len(t) > 20000 {
		t = t[len(t)-16000:]
	}
	u.LogText = strings.TrimRight(t, "\n")
	u.LogSeq++
}

func (u *CropUI) refresh() {
	u.mu.Lock()
	n := len(u.items)
	u.mu.Unlock()
	u.CountLbl = fmt.Sprintf("%d file(s)", n)
	u.Seq++
}

// ---------- adding files / analysis ----------

// AddFiles adds images and folders (one level) and queues edge detection.
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
		u.StatusLbl = fmt.Sprintf("%d image(s) added. Detecting edges...", len(added))
	} else if len(paths) > 0 {
		u.StatusLbl = "No new images (TIFF 8/16-bit, JPEG, PNG). Files with _crop in the name are skipped."
	}
	return len(added)
}

func (u *CropUI) selectRow(i int) {
	u.cur = i
	u.sel = map[int]bool{}
	if i >= 0 {
		u.sel[i] = true
	}
	u.showCurrent()
}

// AddFilesDialog is "Add files...".
func (u *CropUI) AddFilesDialog() {
	u.h.PickFiles("Select positive scans", "", []string{"tif", "tiff", "jpg", "jpeg", "png"}, func(ps []string, ok bool) {
		if ok {
			u.AddFiles(ps)
		}
	})
}

// AddFolderDialog is "Add folder...".
func (u *CropUI) AddFolderDialog() {
	u.h.PickFolder("Select a folder with positive scans", "", func(p string, ok bool) {
		if ok {
			u.AddFiles([]string{p})
		}
	})
}

// RemoveSelected is "Remove selected".
func (u *CropUI) RemoveSelected() {
	if u.exporting {
		return
	}
	del := u.sel
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
	u.sel = map[int]bool{}
	u.refresh()
	if len(keep) > 0 {
		u.selectRow(0)
	} else {
		u.showCurrent()
	}
}

// Clear is "Clear".
func (u *CropUI) Clear() {
	if u.exporting {
		return
	}
	u.mu.Lock()
	u.items = nil
	u.mu.Unlock()
	u.cur = -1
	u.sel = map[int]bool{}
	u.refresh()
	u.showCurrent()
}

// ---------- table ----------

// Rows is the table model's row count.
func (u *CropUI) Rows() int { return u.model.RowCount() }

// Cell is the table text of row, col (CropModel.Value).
func (u *CropUI) Cell(row, col int) string {
	s, _ := u.model.Value(row, col).(string)
	return s
}

// Cell colours of CropModel.StyleCell.
const (
	CellNormal = iota
	CellError
	CellDim
	CellBusy
	CellDone
)

// CellStyle is CropModel.StyleCell.
func (u *CropUI) CellStyle(row, col int) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	if row >= len(u.items) {
		return CellNormal
	}
	it := u.items[row]
	switch {
	case it.State == ciError:
		return CellError
	case it.State == ciPending || it.State == ciAnalyzing:
		return CellDim
	case it.State == ciExporting:
		return CellBusy
	case it.State == ciDone && col == 4:
		return CellDone
	case col == 3 && it.analysed() && !it.Det.OK:
		return CellBusy
	}
	return CellNormal
}

// Selected reports whether row is selected.
func (u *CropUI) Selected(row int) bool { return u.sel[row] }

// Current is the current row (-1 none).
func (u *CropUI) Current() int { return u.cur }

// ClickRow is a click on a table row: plain selects it, extend toggles it
// (Cmd-click), span selects from the current row (Shift-click).
func (u *CropUI) ClickRow(i int, extend, span bool) {
	if i < 0 || i >= u.Rows() {
		return
	}
	switch {
	case span && u.cur >= 0:
		a, b := min(u.cur, i), max(u.cur, i)
		u.sel = map[int]bool{}
		for k := a; k <= b; k++ {
			u.sel[k] = true
		}
	case extend:
		if u.sel[i] && len(u.sel) > 1 {
			delete(u.sel, i)
		} else {
			u.sel[i] = true
		}
	default:
		u.sel = map[int]bool{i: true}
	}
	if i != u.cur {
		u.cur = i
		u.showCurrent()
	}
	u.Seq++
}

// ---------- settings ----------

// SetAspect, SetInset, SetGrid, SetEdges and SetOnImport are the combos
// and the check box of the Crop card.
func (u *CropUI) SetAspect(i int) {
	if i >= 0 && i < len(cropAspects) {
		u.set(func(c *CropConfig) { c.Aspect = i })
		u.settingsChanged(true)
	}
}

func (u *CropUI) SetInset(i int) {
	if i >= 0 && i < len(cropInsets) {
		u.set(func(c *CropConfig) { c.Inset = i })
		u.settingsChanged(false)
	}
}

func (u *CropUI) SetGrid(i int) {
	if i >= 0 && i < len(cropGridNames) {
		u.set(func(c *CropConfig) { c.Grid = i })
		u.Seq++
	}
}

func (u *CropUI) SetEdges(on bool) {
	u.set(func(c *CropConfig) { c.Edges = on })
	u.Seq++
}

func (u *CropUI) SetOnImport(i int) {
	if i >= 0 && i < 2 {
		u.set(func(c *CropConfig) { c.OnImport = i })
	}
}

// ResetToAuto is "Reset to auto".
func (u *CropUI) ResetToAuto() { u.reset(false) }

// FullImage is "Full image".
func (u *CropUI) FullImage() { u.reset(true) }

func (u *CropUI) reset(manual bool) {
	it := u.current()
	if it == nil || !it.analysed() {
		return
	}
	u.mu.Lock()
	u.resetLocked(it, manual)
	u.mu.Unlock()
	u.refresh()
	u.showCurrent()
}

// Redetect is "Re-detect".
func (u *CropUI) Redetect() {
	it := u.current()
	if it == nil {
		return
	}
	u.mu.Lock()
	it.W = 0
	it.State = ciPending
	u.mu.Unlock()
	go u.analyse(it, false)
}

// ApplyAll is "Apply crop settings to all".
func (u *CropUI) ApplyAll() {
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
}

// SetAngle sets the rotation (slider, buttons, Reset on the card).
func (u *CropUI) SetAngle(v float64) { u.setAngle(v, true) }

// Nudge is one of the -0.5° ... +0.5° buttons.
func (u *CropUI) Nudge(dv float64) {
	if it := u.current(); it != nil {
		u.setAngle(it.Angle+dv, true)
	}
}

// AngleEdited is the Angle field's OnEditingFinished.
func (u *CropUI) AngleEdited(text string) {
	u.AngleText = text
	v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(text, "°")), 64)
	if err == nil {
		u.setAngle(v, true)
	}
}

// SetOutMode, SetOutDir, ChooseOutDir, SetLZW, SetJPEGQ and SetInterp are
// the Export card.
func (u *CropUI) SetOutMode(i int) {
	if i >= 0 && i < 2 {
		u.set(func(c *CropConfig) { c.OutMode = i })
	}
}

func (u *CropUI) SetOutDir(s string) {
	u.set(func(c *CropConfig) { c.OutDir = strings.TrimSpace(s) })
}

func (u *CropUI) ChooseOutDir(done func(string)) {
	u.h.PickFolder("Folder for the cropped images", u.cfg.OutDir, func(p string, ok bool) {
		if ok {
			u.set(func(c *CropConfig) { c.OutDir = p })
			if done != nil {
				done(p)
			}
		}
	})
}

func (u *CropUI) SetLZW(i int) {
	u.set(func(c *CropConfig) { c.LZW = i == 1 })
}

func (u *CropUI) SetJPEGQ(i int) {
	if i >= 0 && i < len(cropJPEGQ) {
		u.set(func(c *CropConfig) { c.JPEGQ = i })
	}
}

func (u *CropUI) SetInterp(i int) {
	if i >= 0 && i < 2 {
		u.set(func(c *CropConfig) { c.Interp = i })
	}
}

// Exporting reports a running export.
func (u *CropUI) Exporting() bool { return u.exporting }

// ---------- preview ----------

func (u *CropUI) showCurrent() {
	u.Seq++
	it := u.current()
	if it == nil {
		u.AngleLbl = "—"
		u.DetLbl = "No image selected."
		u.setAngleText("")
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
		u.AngleLbl = "—"
		u.setAngleText("")
		if st == ciError {
			u.DetLbl = it.Status
		} else {
			u.DetLbl = "Detecting edges..."
		}
		return
	}
	u.AngleLbl = fmt.Sprintf("%+.2f°", ang)
	if !u.AngleFocused {
		u.setAngleText(fmt.Sprintf("%.2f", ang))
	}
	var s string
	if det.OK {
		s = fmt.Sprintf("Detected %+.2f°, confidence %.0f%%.\n%s.", det.Angle, 100*math.Min(det.Confidence, 1), det.Message)
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
	u.DetLbl = s
	u.StatusLbl = fmt.Sprintf("%s  |  %s  |  %d×%d %d-bit  →  crop %d×%d  |  %s", it.Name, mode, it.W, it.H, it.Bits, cw, ch, ratioText(cw, ch))
}

func (u *CropUI) setAngleText(s string) {
	if u.AngleText != s {
		u.AngleText = s
		u.AngleSeq++
	}
}

// View is what the preview paints for the current item.
type View struct {
	Item          bool // an item is selected
	Ready         bool // analysed and its proxy is loaded
	Msg           string
	Proxy         *cropper.Proxy
	Key           string // changes when the rendered proxy changes
	Angle         float64
	Rect          cropper.Rect
	Det           cropper.Detection
	W, H          int
	Manual        bool
	Grid          int
	Edges         bool
	Readout       string
	ReadoutManual bool
}

// Preview snapshots the current item the way paint() reads it.
func (u *CropUI) Preview() View {
	it := u.current()
	v := View{Grid: u.cfg.Grid, Edges: u.cfg.Edges}
	u.mu.Lock()
	var p *cropper.Proxy
	ready := false
	if it != nil && it.analysed() {
		p = u.proxies[it.Path]
		v.Rect, v.Det, v.Angle = it.Rect, it.Det, it.Angle
		v.W, v.H, v.Manual = it.W, it.H, it.Manual
		ready = true
	}
	u.mu.Unlock()
	v.Item = it != nil
	switch {
	case it == nil:
		v.Msg = "Drop positive scans or a folder here, or use Add files.\nTIFF (8 / 16-bit), JPEG and PNG.\n\nEdges are found automatically, the picture is levelled\nand cropped to 3:2 / 2:3 inside the film rebate."
	case !ready && it.State == ciError:
		v.Msg = it.Status
	case !ready || p == nil:
		v.Msg = "Loading " + it.Name + " ..."
	}
	if v.Msg != "" {
		return v
	}
	v.Ready, v.Proxy = true, p
	v.Key = fmt.Sprintf("%s|%.4f|%p", it.Path, v.Angle, p)
	mode := "AUTO"
	if v.Manual {
		mode = "MANUAL"
	}
	v.ReadoutManual = v.Manual
	_, _, w, h := cropper.IntRect(v.Rect, v.W, v.H)
	v.Readout = fmt.Sprintf("%s   %+.2f°   %d × %d   %s", mode, v.Angle, w, h, ratioText(w, h))
	if v.Det.OK {
		v.Readout += fmt.Sprintf("   conf %.0f%%", 100*math.Min(v.Det.Confidence, 1))
	}
	return v
}

// Geom is geom(): the proxy canvas fitted into the view rectangle
// (x, y, w, h in px) with margin m px.
func (u *CropUI) Geom(x, y, w, h, m float64) pvGeom {
	it := u.current()
	if it == nil || !it.analysed() {
		return pvGeom{}
	}
	aw, ah := w-2*m, h-2*m
	if aw < 10 || ah < 10 {
		return pvGeom{}
	}
	k := math.Min(aw/float64(it.W), ah/float64(it.H))
	return pvGeom{ok: true, k: k, W: it.W, H: it.H,
		ox: x + (w-k*float64(it.W))/2,
		oy: y + (h-k*float64(it.H))/2}
}

// Geometry accessors for the view.
func (g pvGeom) OK() bool                               { return g.ok }
func (g pvGeom) ToView(x, y float64) (float64, float64) { return g.ox + x*g.k, g.oy + y*g.k }

// hit returns drag mode and handle for view px (x, y); tol is 9 px.
func (u *CropUI) hit(it *cropItem, g pvGeom, x, y int, tolPx float64) (int, int) {
	r := it.Rect
	tol := tolPx / g.k
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

// Cursor kinds of cursorFor.
const (
	CursorArrow = iota
	CursorMove
	CursorNWSE
	CursorNESW
	CursorWE
	CursorNS
)

// HitCursor is cursorFor(hit(...)) for the pointer at view px (x, y).
func (u *CropUI) HitCursor(g pvGeom, x, y int, tolPx float64) int {
	it := u.current()
	if it == nil || !it.analysed() || !g.ok {
		return CursorArrow
	}
	if u.drag.mode != 0 {
		return cursorFor(u.drag.mode, u.drag.handle)
	}
	return cursorFor(u.hit(it, g, x, y, tolPx))
}

func cursorFor(mode, h int) int {
	switch mode {
	case 1:
		return CursorMove
	case 2:
		if h == 0 || h == 2 {
			return CursorNWSE
		}
		return CursorNESW
	case 3:
		if h == 0 || h == 2 {
			return CursorWE
		}
		return CursorNS
	}
	return CursorArrow
}

// MouseDown, MouseMove and MouseUp are the preview's mouse handlers.
func (u *CropUI) MouseDown(g pvGeom, x, y int, tolPx float64) {
	it := u.current()
	if it == nil || !it.analysed() || u.exporting {
		return
	}
	if !g.ok {
		return
	}
	mode, h := u.hit(it, g, x, y, tolPx)
	if mode == 0 {
		return
	}
	mx, my := u.toCanvas(g, x, y)
	u.drag = cropDrag{mode: mode, handle: h, start: it.Rect, mx: mx, my: my}
}

func (u *CropUI) MouseMove(g pvGeom, x, y int) {
	it := u.current()
	if it == nil || !it.analysed() {
		return
	}
	if !g.ok || u.drag.mode == 0 {
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
	u.Seq++
}

func (u *CropUI) MouseUp() {
	if u.drag.mode == 0 {
		return
	}
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

// Dragging reports a crop rectangle drag in progress.
func (u *CropUI) Dragging() bool { return u.drag.mode != 0 }

// ---------- angle slider ----------

// SliderValue is sliderValue(): the angle at x on a track from x0 to x1.
func SliderValue(x, x0, x1 int) float64 {
	if x1 <= x0 {
		return 0
	}
	return (float64(x-x0)/float64(x1-x0))*2*sliderMax - sliderMax
}

// SliderMax is the slider's range (±5°).
const SliderMax = sliderMax

// SliderState is what paintSlider draws.
func (u *CropUI) SliderState() (angle, det float64, ok, detOK bool) {
	it := u.current()
	if it == nil || !it.analysed() {
		return 0, 0, false, false
	}
	return it.Angle, it.Det.Angle, true, it.Det.OK
}

// SliderDown, SliderMove and SliderUp are the slider's mouse handlers.
func (u *CropUI) SliderDown(x, x0, x1 int) {
	if u.current() != nil && u.current().analysed() {
		u.SliderDrag = true
		u.setAngle(SliderValue(x, x0, x1), true)
	}
}

func (u *CropUI) SliderMove(x, x0, x1 int) {
	if u.SliderDrag {
		u.setAngle(SliderValue(x, x0, x1), true)
	}
}

func (u *CropUI) SliderUp() {
	if u.SliderDrag {
		u.SliderDrag = false
		u.Seq++
	}
}

// ---------- export ----------

// Export is "Export selected" (all=false) and "Export all".
func (u *CropUI) Export(all bool) {
	if u.exporting {
		return
	}
	var list []*cropItem
	u.mu.Lock()
	if all {
		list = append(list, u.items...)
	} else {
		var idx []int
		for i := range u.sel {
			idx = append(idx, i)
		}
		sort.Ints(idx)
		for _, i := range idx {
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
		u.StatusLbl = "Nothing to export. Add images first."
		return
	}
	if u.cfg.OutMode == 1 && u.cfg.OutDir == "" {
		u.h.Message("Export", "Choose the output folder first, or use the \"cropped\" subfolder.")
		return
	}
	u.exporting = true
	u.PBMax, u.PBVal = len(todo), 0
	interp := u.cfg.Interp
	opt := cropper.SaveOptions{LZW: u.cfg.LZW, JPEGQuality: cropJPEGQ[u.cfg.JPEGQ], Software: appVersion}
	u.log(fmt.Sprintf("Export %d image(s): %s, %s.", len(todo), map[bool]string{true: "TIFF LZW", false: "TIFF uncompressed"}[opt.LZW], []string{"bicubic", "Lanczos-3"}[interp]))
	cfg := u.cfg // outPath reads OutMode / OutDir on the worker
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
			out, err := outPathFor(cfg, it.Path)
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
				u.PBVal = n
				u.log(msg)
			})
		}
		u.ui(func() {
			u.exporting = false
			s := fmt.Sprintf("Export finished: %d written, %d failed. Existing files are never overwritten.", done, failed)
			u.log(s)
			u.StatusLbl = s
		})
	}()
}

// outPathFor is outPath with a copy of the settings: Windows' export
// goroutine reads u.cfg directly, which is a data race under -race.
func outPathFor(cfg CropConfig, src string) (string, error) {
	u := &CropUI{cfg: cfg}
	return u.outPath(src)
}
