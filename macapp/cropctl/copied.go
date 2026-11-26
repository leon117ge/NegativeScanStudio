package cropctl

// Declarations copied verbatim from the Windows Crop page (croppage.go) and
// Batch RAW to TIFF page (rawpage.go). copied_test.go fails when one of
// them drifts from its original, so the settings files, detection,
// rectangle rules, output names and conversion options stay the same as on
// Windows.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"basetool/cropper"
	"basetool/rawconv"
)

// ---------- croppage.go ----------

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

type cropDrag struct {
	mode   int // 0 none, 1 move, 2 corner, 3 edge
	handle int // corner 0..3 (tl,tr,br,bl) or edge 0..3 (l,t,r,b)
	start  cropper.Rect
	mx, my float64 // mouse at start, canvas px
}

func (u *CropUI) aspect() float64 { return cropAspects[u.cfg.Aspect] }

func (u *CropUI) inset() float64 { return cropInsets[u.cfg.Inset] }

// current returns the selected item (caller holds no lock; items are only
// replaced on the UI thread).
func (u *CropUI) current() *cropItem {
	if u.cur < 0 || u.cur >= len(u.items) {
		return nil
	}
	return u.items[u.cur]
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

func (u *CropUI) toCanvas(g pvGeom, x, y int) (float64, float64) {
	return (float64(x) - g.ox) / g.k, (float64(y) - g.oy) / g.k
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

const sliderMax = 5.0

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

// ---------- rawpage.go ----------

func (m *RawModel) RowCount() int { return len(m.rows) }

func (m *RawModel) Value(row, col int) interface{} {
	r := m.rows[row]
	switch col {
	case 0:
		return r.Name
	case 1:
		return filepath.Dir(r.Path)
	case 2:
		return r.Status
	case 3:
		if r.Secs > 0 {
			return fmt.Sprintf("%.1f s", r.Secs)
		}
	}
	return ""
}

type RawConfig struct {
	Subfolder bool
	LZW       bool
	ICC       string
	UseDNG    bool
	Workers   int
	WatchDir  string
}

func rawConfigFile() string { return filepath.Join(configDir(), "raw_config.txt") }

func loadRawConfig() RawConfig {
	c := RawConfig{Workers: rawconv.DefaultWorkers()}
	b, err := os.ReadFile(rawConfigFile())
	if err != nil {
		return c
	}
	for _, ln := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
		k, v, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		switch k {
		case "SUBFOLDER":
			c.Subfolder = v == "1"
		case "LZW":
			c.LZW = v == "1"
		case "ICC":
			c.ICC = v
		case "USE_DNG":
			c.UseDNG = v == "1"
		case "WORKERS":
			if n, err := strconv.Atoi(v); err == nil && n >= 1 {
				c.Workers = n
			}
		case "WATCH_DIR":
			c.WatchDir = v
		}
	}
	return c
}

func saveRawConfig(c RawConfig) {
	os.MkdirAll(configDir(), 0755)
	b := func(x bool) string {
		if x {
			return "1"
		}
		return "0"
	}
	s := fmt.Sprintf("SUBFOLDER=%s\nLZW=%s\nICC=%s\nUSE_DNG=%s\nWORKERS=%d\nWATCH_DIR=%s\n",
		b(c.Subfolder), b(c.LZW), c.ICC, b(c.UseDNG), c.Workers, c.WatchDir)
	os.WriteFile(rawConfigFile(), []byte(s), 0644)
}

func maxRawWorkers() int {
	n := runtime.NumCPU() / 2
	if n < 1 {
		n = 1
	}
	return n
}

const (
	rsPending = iota
	rsWorking
	rsDone
	rsSkipped
	rsError
)

type RawRow struct {
	Path, Name string
	State      int
	Status     string
	Secs       float64
}

func (u *RAWUI) addFolder(dir string) {
	ents, _ := os.ReadDir(dir)
	var all []string
	for _, e := range ents {
		all = append(all, filepath.Join(dir, e.Name()))
	}
	sort.Strings(all)
	u.AddFiles(all)
}

func (u *RAWUI) options() rawconv.Options {
	return rawconv.Options{
		Subfolder: u.cfg.Subfolder,
		LZW:       u.cfg.LZW,
		ICC:       u.cfg.ICC,
		UseDNG:    u.cfg.UseDNG && rawconv.DNGConverterPath() != "",
		Software:  appVersion,
	}
}

func (u *RAWUI) next() (*RawRow, rawconv.Options) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.stopReq {
		return nil, rawconv.Options{}
	}
	for _, r := range u.model.rows {
		if r.State == rsPending {
			r.State = rsWorking
			r.Status = "Converting..."
			return r, u.options()
		}
	}
	return nil, rawconv.Options{}
}

type fileSeen struct {
	size   int64
	mod    time.Time
	stable int
	done   bool
}

// ---------- croppage.go: table rows ----------

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
