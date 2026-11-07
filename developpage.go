package main

// Page 5 "Develop" (v0.70, v0.71: 2383 + Rec.709 on by default, film base estimate, Auto film WB): negative -> positive inside the program with the
// three DCTLs (FilmScan_Density_In -> FilmScan_MaskCrosstalk ->
// FilmScan_Positive_Out) as fixed serial nodes 1-3, followed by Contrast,
// Exposure (+HDR zones), White Balance, Curves, Cineon -> 2383 LUT and
// Color space out. Nodes can be bypassed and edited, not removed or moved.

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"basetool/cropper"
	"basetool/develop"
	"basetool/filmdb"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

const devPresetName = "develop_preset.json"
const devPresetApp = "NegativeScan Studio develop preset"
const devProxyMax = 960

type devPreset struct {
	App     string        `json:"app"`
	Version string        `json:"version"`
	Saved   string        `json:"saved"`
	Chain   develop.Chain `json:"chain"`
	// v0.71: per-frame orientation (file name -> develop.Orient, 0 omitted).
	Orient map[string]int `json:"orient,omitempty"`
}

type paramCtl struct {
	p    develop.Param
	sl   *walk.Slider
	ne   *walk.NumberEdit
	chk  *walk.CheckBox
	cb   *walk.ComboBox
	le   *walk.LineEdit
	tick int // slider steps
}

type DevUI struct {
	mw **walk.MainWindow

	mu     sync.Mutex
	chain  develop.Chain
	cx     *develop.Context
	sel    int
	files  []string
	cur    int
	folder string
	db     []filmdb.Entry

	// preview proxy of the current file (raw scan values 0..1)
	pPath   string
	pw, ph  int
	pix     []float32
	bmp     *walk.Bitmap
	gen     int64
	busy    bool
	pick    int // 0 none, 1 film base, 2 neutral
	syncing bool
	dirty   bool
	// v0.71: a roll opened without preset: estimate the film base from the
	// image (no DB match) / set Auto film WB, once the file is loaded.
	estPending, wbPending bool
	baseNote              string // shown in the status line while the base is an estimate
	vwDim                 [2]int // frame size the view centre refers to
	// v0.71: per-frame orientation, full-resolution frame and view (zoom/pan).
	orient             map[string]develop.Orient
	pix0               []float32 // proxy as scanned (pw0 x ph0); pix/pw/ph are oriented
	pw0, ph0           int
	pk                 int      // proxy block size (full-res px per proxy px)
	full0              []uint16 // full-resolution frame as scanned
	fW, fH             int
	vw                 develop.View
	content            int64 // bumps on every render (chain/file/orientation)
	hgen               int64
	hiBmp              *walk.Bitmap
	hiRect             [4]int // oriented full-res x0, y0, w, h
	hiFor              int64  // content of hiBmp
	hiTimer            *time.Timer
	pan                bool
	panIX, panIY       float64
	lastMid            time.Time
	lastMidX, lastMidY int
	zoomLbl, orientLbl *walk.Label

	strip, view                            *walk.CustomWidget
	nodeTitle, nodeNote, status, presetLbl *walk.Label
	enableChk, autoBaseChk                 *walk.CheckBox
	panels                                 map[string]*walk.Composite
	ctls                                   map[string][]*paramCtl
	fileCB, viewCB, cmpCB, dispCB          *walk.ComboBox
	filterCB, compCB                       *walk.ComboBox
	pb                                     *walk.ProgressBar
	logTE                                  *walk.TextEdit
	expOneBtn, expAllBtn                   *walk.PushButton
	pickBaseBtn, pickWBBtn                 *walk.PushButton
	font, fontB                            *walk.Font
	pv                                     []*walk.Composite
}

func (u *DevUI) win() *walk.MainWindow {
	if u.mw == nil {
		return nil
	}
	return *u.mw
}

func (u *DevUI) ui(f func()) {
	if w := u.win(); w != nil {
		w.Synchronize(func() {
			defer u.guard("develop UI")
			f()
		})
	}
}

// guard logs a panic to crash_log.txt and shows it instead of exiting.
func (u *DevUI) guard(where string) {
	r := recover()
	if r == nil {
		return
	}
	msg := fmt.Sprintf("PANIC in Develop %s: %v", where, r)
	appendCrash(msg + "\n" + string(debug.Stack()))
	if w := u.win(); w != nil {
		w.Synchronize(func() {
			defer func() { recover() }()
			u.busy = false
			u.setExporting(false)
			walk.MsgBox(u.win(), "Develop - error", msg+"\r\n\r\nDetails were written to "+crashFile(), walk.MsgBoxIconError)
		})
	}
}

func (u *DevUI) log(s string) {
	if u.logTE == nil {
		return
	}
	t := u.logTE.Text() + time.Now().Format("15:04:05") + "  " + s + "\r\n"
	if len(t) > 20000 {
		t = t[len(t)-16000:]
	}
	u.logTE.SetText(t)
	u.logTE.SetTextSelection(len(t), len(t))
	u.logTE.ScrollToCaret()
}

// developContext keeps existing saved base and WB presets available for preview.
// Rounding preserves the conversion behavior of earlier app versions.
func developContext() (*develop.Context, []filmdb.Entry) {
	cx := &develop.Context{}
	for _, p := range loadConfig().Presets {
		cx.BasePresets = append(cx.BasePresets, develop.NamedRGB{Name: p.Name, V: [3]float64{develop.Round(p.R, 5), develop.Round(p.G, 5), develop.Round(p.B, 5)}})
	}
	cx.Matrices = develop.BuiltinMatrices()
	ents := calibratedStocks()
	for _, e := range ents {
		if e.Crosstalk == nil {
			continue
		}
		var m [9]float64
		for i, v := range *e.Crosstalk {
			m[i] = develop.Round(v, 4)
		}
		cx.Matrices = append(cx.Matrices, develop.NamedMat{Name: e.Name + " - chart", M: m})
	}
	for _, g := range loadWBConfig().Presets {
		cx.WBGrays = append(cx.WBGrays, develop.NamedRGB{Name: g.Name, V: [3]float64{develop.Round(g.R, 2), develop.Round(g.G, 2), develop.Round(g.B, 2)}})
	}
	return cx, ents
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func normName(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

// matchStock finds the DB stock whose name starts the file name (longest wins).
func matchStock(path string, ents []filmdb.Entry) *filmdb.Entry {
	f := normName(filepath.Base(path))
	var best *filmdb.Entry
	for i := range ents {
		n := normName(ents[i].Name)
		if n != "" && strings.HasPrefix(f, n) && (best == nil || len(n) > len(normName(best.Name))) {
			best = &ents[i]
		}
	}
	return best
}

func (u *DevUI) nodeIndex(t string) int {
	for i, n := range u.chain.Nodes {
		if n.Type == t {
			return i
		}
	}
	return -1
}

// ---------- files ----------

func (u *DevUI) isInput(name string) bool {
	l := strings.ToLower(name)
	if !(strings.HasSuffix(l, ".tif") || strings.HasSuffix(l, ".tiff")) || strings.Contains(l, "_pos") {
		return false
	}
	if u.filterCB != nil && u.filterCB.CurrentIndex() == 0 {
		return strings.HasSuffix(l, "_hdr.tif") || strings.HasSuffix(l, "_hdr.tiff")
	}
	return true
}

// Open opens TIFF files or a roll folder.
func (u *DevUI) Open(paths []string) {
	defer u.guard("open")
	u.baseNote = ""
	var files []string
	folder := ""
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			folder = p
			ents, _ := os.ReadDir(p)
			for _, e := range ents {
				if !e.IsDir() && u.isInput(e.Name()) {
					files = append(files, filepath.Join(p, e.Name()))
				}
			}
		} else if strings.HasSuffix(strings.ToLower(p), ".tif") || strings.HasSuffix(strings.ToLower(p), ".tiff") {
			files = append(files, p)
			if folder == "" {
				folder = filepath.Dir(p)
			}
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		u.status.SetText("No input TIFF found (filter: " + u.filterCB.Text() + "). Develop outputs (_POS) are skipped.")
		return
	}
	u.files, u.cur = files, 0
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	u.syncing = true
	u.fileCB.SetModel(names)
	u.fileCB.SetCurrentIndex(0)
	u.syncing = false
	if folder != u.folder {
		u.folder = folder
		if !u.loadPresetFile(filepath.Join(folder, devPresetName), true) {
			u.presetLbl.SetText("Roll preset: none in this folder (defaults)")
			u.orient = map[string]develop.Orient{}
			u.estPending = !u.autoBase(files[0])
			u.wbPending = true
		}
	}
	u.log(fmt.Sprintf("Opened %d file(s) in %s", len(files), folder))
	u.loadProxy(files[0])
}

// autoBase sets the film base from the film base DB; false = no DB stock
// matched the file (or DB lookup is off): the base is then estimated from
// the image (loadProxy).
func (u *DevUI) autoBase(path string) bool {
	if u.autoBaseChk == nil || !u.autoBaseChk.Checked() {
		return false
	}
	e := matchStock(path, u.db)
	if e == nil {
		return false
	}
	i := u.nodeIndex(develop.TDensity)
	n := &u.chain.Nodes[i]
	ty := develop.TypeByID(develop.TDensity)
	items := u.cx.ComboItems(ty.Params[0])
	for k := 1; k < len(items); k++ {
		if normName(items[k]) == normName(e.Name) {
			n.SetCombo(u.cx, ty.Params[0], k)
			u.syncPanel(develop.TDensity)
			u.changed()
			u.log("Film base: DB stock " + e.Name + " -> preset \"" + items[k] + "\"")
			return true
		}
	}
	n.SetCombo(u.cx, ty.Params[0], 0)
	n.V["base_r"], n.V["base_g"], n.V["base_b"] = develop.Round(e.R, 5), develop.Round(e.G, 5), develop.Round(e.B, 5)
	u.syncPanel(develop.TDensity)
	u.changed()
	u.log(fmt.Sprintf("Film base: DB stock %s -> %.5f %.5f %.5f", e.Name, e.R, e.G, e.B))
	return true
}

// stockName is the film stock of path for Auto film WB: the film base DB
// stock matching the file name, else a known stock named in the file name,
// the Density In preset or the crosstalk preset ("" = unknown).
func (u *DevUI) stockName(path string) string {
	if e := matchStock(path, u.db); e != nil {
		if s := develop.FindStock(e.Name); s != "" {
			return s
		}
		return e.Name
	}
	if s := develop.FindStock(filepath.Base(path)); s != "" {
		return s
	}
	for _, tk := range [][2]string{{develop.TDensity, "preset"}, {develop.TMask, "mat_preset"}} {
		i := u.nodeIndex(tk[0])
		if i < 0 {
			continue
		}
		n := u.chain.Nodes[i]
		for _, p := range develop.TypeByID(tk[0]).Params {
			if p.Key != tk[1] {
				continue
			}
			items := u.cx.ComboItems(p)
			if k := int(n.Get(p.Key)); k > 0 && k < len(items) {
				if s := develop.FindStock(items[k]); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// autoResult carries what loadProxy estimated off the UI thread.
type autoResult struct {
	base           [3]float64
	baseOK, wbDone bool
	pos            develop.Node
	notes, logs    []string
}

// runAuto estimates the film base (est) and applies Auto film WB (wb) on
// a copy of the chain with the full-resolution frame pix.
func runAuto(chain develop.Chain, cx *develop.Context, pix []uint16, stock string, est, wb bool) autoResult {
	var r autoResult
	if est {
		b, used, ok := develop.EstimateBaseU16(pix)
		if ok {
			r.base, r.baseOK = b, true
			n := &chain.Nodes[0]
			n.SetCombo(cx, develop.TypeByID(develop.TDensity).Params[0], 0)
			n.V["base_r"], n.V["base_g"], n.V["base_b"] = b[0], b[1], b[2]
			r.logs = append(r.logs, fmt.Sprintf("Film base: no roll preset and no film base DB match - estimated from the image: %.5f %.5f %.5f (p%.1f of %d px below %.1f)", b[0], b[1], b[2], develop.EstPct, used, develop.EstClip))
			r.notes = append(r.notes, "Film base ESTIMATED from the image - use Pick film base (node 1) on clear film, or the film base DB, for an exact base")
		} else {
			r.logs = append(r.logs, fmt.Sprintf("Film base: estimate failed (%d usable px) - built-in default base kept; use Pick film base", used))
			r.notes = append(r.notes, "Film base: built-in default (estimate failed) - use Pick film base or the film base DB")
		}
	}
	if wb {
		if msg := develop.ApplyAutoFilmWB(&chain, cx, stock, pix); msg != "" {
			r.wbDone = true
			r.pos = chain.Nodes[2].Clone()
			r.logs = append(r.logs, msg)
		} else if chain.Nodes[2].Get("film_wb_auto") != 0 {
			r.logs = append(r.logs, "Film WB Auto: film stock unknown (no DB match, no stock in the file name) - film WB left at \"Kodak Vision3 500T\" (built-in default)")
		}
	}
	return r
}

// applyAuto copies an autoResult into the UI chain (UI thread).
func (u *DevUI) applyAuto(r autoResult) {
	if r.baseOK {
		n := &u.chain.Nodes[u.nodeIndex(develop.TDensity)]
		n.SetCombo(u.cx, develop.TypeByID(develop.TDensity).Params[0], 0)
		n.V["base_r"], n.V["base_g"], n.V["base_b"] = r.base[0], r.base[1], r.base[2]
		u.syncPanel(develop.TDensity)
		u.dirty = true
	}
	if r.wbDone {
		n := &u.chain.Nodes[u.nodeIndex(develop.TPositive)]
		for _, k := range []string{"film_wb", "gray_on", "gray_r", "gray_g", "gray_b"} {
			n.V[k] = r.pos.V[k]
		}
		if v, ok := r.pos.Items["film_wb"]; ok {
			if n.Items == nil {
				n.Items = map[string]string{}
			}
			n.Items["film_wb"] = v
		}
		u.syncPanel(develop.TPositive)
		u.dirty = true
	}
	for _, l := range r.logs {
		u.log(l)
	}
	if u.strip != nil {
		u.strip.Invalidate()
	}
}

// autoWBNow re-applies Auto film WB (the checkbox was turned on): reloads
// the current file at full resolution like at roll open.
func (u *DevUI) autoWBNow() {
	path := u.pPath
	if path == "" {
		return
	}
	chain, cx, stock := u.chain.Clone(), u.cx, u.stockName(path)
	u.status.SetText("Film WB Auto: reading " + filepath.Base(path) + " ...")
	go func() {
		defer u.guard("auto film WB")
		im, err := cropper.Load(path)
		if err != nil {
			u.ui(func() { u.status.SetText("ERROR " + filepath.Base(path) + ": " + err.Error()) })
			return
		}
		r := runAuto(chain, cx, im.Pix, stock, false, true)
		u.ui(func() {
			u.applyAuto(r)
			u.status.SetText(filepath.Base(path) + "  " + strings.Join(r.logs, "; "))
			u.render()
		})
	}()
}

func (u *DevUI) loadProxy(path string) {
	u.status.SetText("Loading " + filepath.Base(path) + " ...")
	est, wb := u.estPending, u.wbPending
	u.estPending, u.wbPending = false, false
	chain, cx, stock := u.chain.Clone(), u.cx, ""
	if wb {
		stock = u.stockName(path)
	}
	go func() {
		defer u.guard("load preview")
		im, err := cropper.Load(path)
		if err != nil {
			u.ui(func() { u.status.SetText("ERROR " + filepath.Base(path) + ": " + err.Error()) })
			return
		}
		k := int(math.Ceil(float64(max(im.W, im.H)) / devProxyMax))
		if k < 1 {
			k = 1
		}
		pw, ph := im.W/k, im.H/k
		pix := make([]float32, pw*ph*3)
		inv := float32(1.0 / (65535 * float64(k*k)))
		for y := 0; y < ph; y++ {
			for x := 0; x < pw; x++ {
				var s [3]uint32
				for yy := 0; yy < k; yy++ {
					row := ((y*k+yy)*im.W + x*k) * 3
					for xx := 0; xx < k*3; xx += 3 {
						s[0] += uint32(im.Pix[row+xx])
						s[1] += uint32(im.Pix[row+xx+1])
						s[2] += uint32(im.Pix[row+xx+2])
					}
				}
				o := (y*pw + x) * 3
				pix[o], pix[o+1], pix[o+2] = float32(s[0])*inv, float32(s[1])*inv, float32(s[2])*inv
			}
		}
		w, h := im.W, im.H
		full := im.Pix
		var ar autoResult
		if est || wb {
			ar = runAuto(chain, cx, im.Pix, stock, est, wb)
		}
		u.ui(func() {
			u.pPath, u.pw0, u.ph0, u.pix0, u.pk = path, pw, ph, pix, k
			u.full0, u.fW, u.fH = full, w, h
			u.applyOrient()
			st := fmt.Sprintf("%s  %d x %d  (preview %d x %d)", filepath.Base(path), w, h, pw, ph)
			defer func() {
				if u.baseNote != "" {
					st += "   |   " + u.baseNote
				}
				u.status.SetText(st)
			}()
			if est || wb {
				u.applyAuto(ar)
				if len(ar.notes) > 0 {
					u.baseNote = strings.Join(ar.notes, "; ")
				}
			}
			u.render()
		})
	}()
}

// ---------- preview ----------

func (u *DevUI) viewUpto() int {
	if u.viewCB != nil && u.viewCB.CurrentIndex() == 1 {
		return u.sel
	}
	return -1
}

// render recomputes the preview bitmap off the UI thread (latest request wins).
func (u *DevUI) render() {
	if u.pix == nil {
		u.view.Invalidate()
		return
	}
	gen := atomic.AddInt64(&u.gen, 1)
	atomic.AddInt64(&u.content, 1)
	u.scheduleHi(0)
	chain := u.chain.Clone()
	cx := u.cx
	upto := u.viewUpto()
	cmp := u.cmpCB.CurrentIndex()
	disp := u.dispCB.CurrentIndex()
	src, pw, ph := u.pix, u.pw, u.ph
	go func() {
		defer u.guard("preview")
		prog := chain.Compile(cx, upto)
		out := make([]float32, len(src))
		copy(out, src)
		if cmp != 1 {
			prog.Run(out)
			if disp == 0 {
				if vf := develop.ViewFunc(prog.Domain); vf != nil {
					runPix(out, vf)
				}
			}
		}
		rgba := make([]byte, pw*ph*4)
		b8 := func(v float32) byte {
			if !(v > 0) {
				return 0
			}
			if v >= 1 {
				return 255
			}
			return byte(v*255 + 0.5)
		}
		for y := 0; y < ph; y++ {
			for x := 0; x < pw; x++ {
				i := (y*pw + x) * 3
				r, g, b := out[i], out[i+1], out[i+2]
				if cmp == 1 || (cmp == 2 && x < pw/2) {
					r, g, b = develop.Encode(develop.DomRec709, src[i]), develop.Encode(develop.DomRec709, src[i+1]), develop.Encode(develop.DomRec709, src[i+2])
				}
				o := (y*pw + x) * 4
				rgba[o], rgba[o+1], rgba[o+2], rgba[o+3] = b8(r), b8(g), b8(b), 255
			}
		}
		notes := strings.Join(prog.Notes, "; ")
		dom := develop.DomainNames[prog.Domain]
		u.ui(func() {
			if atomic.LoadInt64(&u.gen) != gen {
				return
			}
			img := &image.RGBA{Pix: rgba, Stride: 4 * pw, Rect: image.Rect(0, 0, pw, ph)}
			if bm, err := walk.NewBitmapFromImageForDPI(img, 96); err == nil {
				if u.bmp != nil {
					u.bmp.Dispose()
				}
				u.bmp = bm
			}
			s := fmt.Sprintf("%s  |  output: %s", filepath.Base(u.pPath), dom)
			if notes != "" {
				s += "  |  " + notes
			}
			if u.baseNote != "" {
				s += "  |  " + u.baseNote
			}
			u.status.SetText(s)
			u.view.Invalidate()
		})
	}()
}

func runPix(px []float32, f develop.PixFunc) {
	var wg sync.WaitGroup
	n := len(px) / 3
	parts := 8
	for w := 0; w < parts; w++ {
		a, b := w*n/parts, (w+1)*n/parts
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := a; i < b; i++ {
				px[i*3], px[i*3+1], px[i*3+2] = f(px[i*3], px[i*3+1], px[i*3+2])
			}
		}()
	}
	wg.Wait()
}

func (u *DevUI) fonts() {
	if u.font == nil {
		u.font, _ = walk.NewFont("Segoe UI", 9, 0)
		u.fontB, _ = walk.NewFont("Segoe UI", 9, walk.FontBold)
	}
}

func (u *DevUI) viewClick(x, y int, btn walk.MouseButton) {
	defer u.guard("pick")
	if u.navDown(x, y, btn) {
		return
	}
	if u.pick == 0 || btn != walk.LeftButton {
		return
	}
	r, g, b, ok := u.sample(x, y)
	if !ok {
		return
	}
	mode := u.pick
	u.setPick(0)
	switch mode {
	case 1:
		i := u.nodeIndex(develop.TDensity)
		n := &u.chain.Nodes[i]
		n.SetCombo(u.cx, develop.TypeByID(develop.TDensity).Params[0], 0)
		n.V["base_r"], n.V["base_g"], n.V["base_b"] = develop.Round(float64(r), 5), develop.Round(float64(g), 5), develop.Round(float64(b), 5)
		u.log(fmt.Sprintf("Film base picked: %.5f %.5f %.5f (preset set to Custom)", r, g, b))
		u.baseNote = ""
		u.selectNode(i)
	case 2:
		i := u.nodeIndex(develop.TWB)
		prog := u.chain.Compile(u.cx, i-1)
		r, g, b = prog.Pixel(r, g, b)
		d := prog.Domain
		lr, lg, lb := develop.Decode(d, r), develop.Decode(d, g), develop.Decode(d, b)
		t, ti := develop.WBFromNeutral(lr, lg, lb)
		n := &u.chain.Nodes[i]
		n.Bypass = false
		n.V["temp"], n.V["tint"] = math.Round(float64(t)*1000)/1000, math.Round(float64(ti)*1000)/1000
		u.log(fmt.Sprintf("Neutral picked: temperature %+.3f, tint %+.3f", t, ti))
		u.selectNode(i)
	}
	u.changed()
}

func (u *DevUI) setPick(m int) {
	u.pick = m
	if u.view != nil {
		u.view.SetCursor(map[bool]walk.Cursor{true: walk.CursorCross(), false: walk.CursorArrow()}[m != 0])
		u.view.Invalidate()
	}
}

// ---------- node strip ----------

func (u *DevUI) stripBoxes() []walk.Rectangle {
	cb := u.strip.ClientBoundsPixels()
	n := len(u.chain.Nodes)
	d := dpiOf(u.strip)
	gap := px(14, d)
	w := (cb.Width - gap*(n-1) - 2*px(4, d)) / n
	out := make([]walk.Rectangle, n)
	for i := range out {
		out[i] = walk.Rectangle{X: cb.X + px(4, d) + i*(w+gap), Y: cb.Y + px(6, d), Width: w, Height: cb.Height - px(12, d)}
	}
	return out
}

func (u *DevUI) paintStrip(c *walk.Canvas, _ walk.Rectangle) error {
	cb := u.strip.ClientBoundsPixels()
	fill(c, cBar, cb)
	u.fonts()
	boxes := u.stripBoxes()
	for i, r := range boxes {
		n := u.chain.Nodes[i]
		ty := develop.TypeByID(n.Type)
		if i > 0 {
			p := boxes[i-1]
			fill(c, cBtnLine, walk.Rectangle{X: p.X + p.Width, Y: r.Y + r.Height/2, Width: r.X - p.X - p.Width, Height: max(1, px(2, dpiOf(u.strip)))})
		}
		bg, line := cPanel, cBorder
		if i < develop.Fixed {
			bg = walk.RGB(48, 44, 40)
		}
		if i == u.sel {
			line = cAccent
		}
		roundBox(c, bg, line, r, px(6, dpiOf(u.strip)))
		if i == u.sel {
			roundBox(c, bg, line, walk.Rectangle{X: r.X + 1, Y: r.Y + 1, Width: r.Width - 2, Height: r.Height - 2}, px(6, dpiOf(u.strip)))
		}
		tc, sc := cText, cDim
		if n.Bypass {
			tc, sc = cTextOff, cTextOff
		}
		if u.fontB != nil {
			d := dpiOf(u.strip)
			c.DrawTextPixels(fmt.Sprintf("%d  %s", i+1, ty.Name), u.fontB, tc, walk.Rectangle{X: r.X + 6, Y: r.Y + 4, Width: r.Width - 12 - px(10, d), Height: px(30, d)}, walk.TextLeft|walk.TextWordbreak|walk.TextEndEllipsis)
			s := n.Summary(u.cx)
			if n.Bypass {
				s = "Bypassed"
			}
			c.DrawTextPixels(s, u.font, sc, walk.Rectangle{X: r.X + 6, Y: r.Y + px(36, d), Width: r.Width - 12, Height: r.Height - px(38, d)}, walk.TextLeft|walk.TextWordbreak|walk.TextEndEllipsis)
		}
		// on/off dot
		dc := cGreen
		if n.Bypass {
			dc = cTextOff
		}
		ds := px(8, dpiOf(u.strip))
		roundBox(c, dc, dc, walk.Rectangle{X: r.X + r.Width - ds - 6, Y: r.Y + 7, Width: ds, Height: ds}, ds)
	}
	return nil
}

func (u *DevUI) stripClick(x, y int, btn walk.MouseButton) {
	defer u.guard("node strip")
	for i, r := range u.stripBoxes() {
		if x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height {
			if btn == walk.RightButton || x >= r.X+r.Width-px(22, dpiOf(u.strip)) && y < r.Y+px(22, dpiOf(u.strip)) {
				u.chain.Nodes[i].Bypass = !u.chain.Nodes[i].Bypass
				u.selectNode(i)
				u.changed()
				return
			}
			u.selectNode(i)
			if u.viewCB.CurrentIndex() == 1 {
				u.render()
			}
			return
		}
	}
}

func (u *DevUI) selectNode(i int) {
	u.sel = i
	n := u.chain.Nodes[i]
	ty := develop.TypeByID(n.Type)
	fixed := ""
	if i < develop.Fixed {
		fixed = "   (conversion - fixed)"
	}
	u.nodeTitle.SetText(fmt.Sprintf("Node %d: %s%s", i+1, ty.Name, fixed))
	u.nodeNote.SetText(wrapText(ty.Note, 64))
	u.syncing = true
	u.enableChk.SetChecked(!n.Bypass)
	u.syncing = false
	for t, p := range u.panels {
		p.SetVisible(t == n.Type)
	}
	u.syncPanel(n.Type)
	u.strip.Invalidate()
}

// ---------- parameter panels ----------

func decimals(step float64) int {
	d := int(math.Ceil(-math.Log10(step) - 1e-9))
	if d < 0 {
		d = 0
	}
	if d > 5 {
		d = 5
	}
	return d
}

func (u *DevUI) panelWidgets(t string) []Widget {
	ty := develop.TypeByID(t)
	// Fixed-width first row: walk's Grid ignores column-spanning widgets when
	// sizing columns, so a panel of only combos would get zero-width columns.
	ws := []Widget{HSpacer{Size: 120}, HSpacer{Size: 150}, HSpacer{Size: 76}}
	for _, p := range ty.Params {
		pc := &paramCtl{p: p}
		u.ctls[t] = append(u.ctls[t], pc)
		switch p.Kind {
		case develop.Slider:
			pc.tick = int(math.Round((p.Max - p.Min) / p.Step))
			if pc.tick > 2000 {
				pc.tick = 2000
			}
			ws = append(ws,
				Label{Text: p.Label, ToolTipText: p.Key},
				Slider{AssignTo: &pc.sl, MinValue: 0, MaxValue: pc.tick, Tracking: true, MinSize: Size{Width: 90}, OnValueChanged: func() {
					if u.syncing {
						return
					}
					v := p.Min + float64(pc.sl.Value())*(p.Max-p.Min)/float64(pc.tick)
					v = develop.Round(v, decimals(p.Step))
					u.setParam(t, p.Key, v, pc, true)
				}},
				NumberEdit{AssignTo: &pc.ne, Decimals: decimals(p.Step), MinValue: p.Min, MaxValue: p.Max, Increment: p.Step, Value: p.Def, MinSize: Size{Width: 76}, MaxSize: Size{Width: 76}, OnValueChanged: func() {
					if u.syncing {
						return
					}
					u.setParam(t, p.Key, pc.ne.Value(), pc, false)
				}},
			)
		case develop.Check:
			ws = append(ws, CheckBox{AssignTo: &pc.chk, Text: p.Label, ColumnSpan: 3, OnCheckedChanged: func() {
				if u.syncing {
					return
				}
				v := 0.0
				if pc.chk.Checked() {
					v = 1
				}
				u.setParam(t, p.Key, v, pc, false)
			}})
		case develop.Combo:
			ws = append(ws, Label{Text: p.Label}, ComboBox{AssignTo: &pc.cb, ColumnSpan: 2, MinSize: Size{Width: 220}, Model: u.cx.ComboItems(p), OnCurrentIndexChanged: func() {
				if u.syncing || pc.cb.CurrentIndex() < 0 {
					return
				}
				n := &u.chain.Nodes[u.nodeIndex(t)]
				n.SetCombo(u.cx, p, pc.cb.CurrentIndex())
				if t == develop.TDensity {
					u.baseNote = ""
				}
				if t == develop.TPositive && p.Key == "film_wb" {
					u.manualFilmWB()
				}
				u.changed()
			}})
		case develop.Path:
			ws = append(ws, Label{Text: p.Label}, Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
				LineEdit{AssignTo: &pc.le, ReadOnly: true, MinSize: Size{Width: 150}},
				PushButton{Text: "Browse...", MaxSize: Size{Width: 70}, OnClicked: func() {
					dlg := new(walk.FileDialog)
					dlg.Title = "Choose a .cube LUT (Cineon log in, Rec.709 gamma 2.4 out)"
					dlg.Filter = "Cube LUT (*.cube)|*.cube|All files (*.*)|*.*"
					if ok, _ := dlg.ShowOpen(u.win()); ok {
						n := &u.chain.Nodes[u.nodeIndex(t)]
						n.SetStr(p.Key, dlg.FilePath)
						n.V["source"] = 2
						u.syncPanel(t)
						u.changed()
					}
				}},
			}})
		}
	}
	switch t {
	case develop.TDensity:
		ws = append(ws, PushButton{AssignTo: &u.pickBaseBtn, ColumnSpan: 3, Text: "Pick film base in the preview", OnClicked: func() {
			u.viewCB.SetCurrentIndex(0)
			u.cmpCB.SetCurrentIndex(1) // the negative
			u.setPick(1)
			u.render()
		}})
	case develop.TWB:
		ws = append(ws, PushButton{AssignTo: &u.pickWBBtn, ColumnSpan: 3, Text: "Pick neutral point in the preview", OnClicked: func() {
			u.cmpCB.SetCurrentIndex(0)
			u.setPick(2)
			u.render()
		}})
	case develop.TPrint:
		ws = append(ws, Label{ColumnSpan: 3, TextColor: cDim, MaxSize: Size{Width: 370}, Text: "Embedded: Resolve Film Look 2383 LUTs built into this program.\r\n" +
			"Auto-detect looks in\r\n" + `C:\ProgramData\Blackmagic Design\DaVinci Resolve\` + "\r\n" + `   Support\LUT\Film Looks` + "\r\nand falls back to the embedded LUT."})
	}
	ws = append(ws, PushButton{ColumnSpan: 3, Text: "Reset this node", OnClicked: func() {
		i := u.nodeIndex(t)
		byp := u.chain.Nodes[i].Bypass
		u.chain.Nodes[i] = develop.NewNode(t)
		u.chain.Nodes[i].Bypass = byp
		u.syncPanel(t)
		u.changed()
	}})
	return ws
}

func (u *DevUI) setParam(t, key string, v float64, src *paramCtl, fromSlider bool) {
	i := u.nodeIndex(t)
	u.chain.Nodes[i].V[key] = v
	if t == develop.TDensity {
		u.baseNote = "" // the base is set by hand now
	}
	if t == develop.TPositive {
		switch key {
		case "film_wb_auto":
			if v != 0 {
				defer u.autoWBNow()
			}
		case "gray_on", "gray_r", "gray_g", "gray_b":
			u.manualFilmWB()
		}
	}
	u.syncing = true
	if src.ne != nil && fromSlider {
		src.ne.SetValue(v)
	}
	if src.sl != nil && !fromSlider {
		src.sl.SetValue(int(math.Round((v - src.p.Min) / (src.p.Max - src.p.Min) * float64(src.tick))))
	}
	u.syncing = false
	u.changed()
}

// manualFilmWB: a film WB setting was changed by hand - Auto turns off so
// the choice is kept (and saved with the roll preset).
func (u *DevUI) manualFilmWB() {
	n := &u.chain.Nodes[u.nodeIndex(develop.TPositive)]
	if n.Get("film_wb_auto") == 0 {
		return
	}
	n.V["film_wb_auto"] = 0
	for _, pc := range u.ctls[develop.TPositive] {
		if pc.p.Key == "film_wb_auto" && pc.chk != nil {
			u.syncing = true
			pc.chk.SetChecked(false)
			u.syncing = false
		}
	}
	u.log("Film WB Auto off (film WB set by hand)")
}

// syncPanel writes the node values into its controls.
func (u *DevUI) syncPanel(t string) {
	i := u.nodeIndex(t)
	if i < 0 {
		return
	}
	n := u.chain.Nodes[i]
	u.syncing = true
	defer func() { u.syncing = false }()
	for _, pc := range u.ctls[t] {
		v := n.Get(pc.p.Key)
		switch {
		case pc.sl != nil:
			pc.sl.SetValue(int(math.Round((v - pc.p.Min) / (pc.p.Max - pc.p.Min) * float64(pc.tick))))
			pc.ne.SetValue(v)
		case pc.chk != nil:
			pc.chk.SetChecked(v != 0)
		case pc.cb != nil:
			items := u.cx.ComboItems(pc.p)
			pc.cb.SetModel(items)
			if k := int(v); k >= 0 && k < len(items) {
				pc.cb.SetCurrentIndex(k)
			}
		case pc.le != nil:
			pc.le.SetText(n.Str(pc.p.Key))
		}
	}
	if u.sel == i && u.enableChk != nil {
		u.enableChk.SetChecked(!n.Bypass)
	}
}

func (u *DevUI) changed() {
	u.dirty = true
	if u.strip != nil {
		u.strip.Invalidate()
	}
	u.render()
}

// ---------- presets ----------

func (u *DevUI) savePreset() {
	if u.folder == "" {
		walk.MsgBox(u.win(), "Save roll preset", "Open a roll folder or a TIFF first: the preset is saved in its folder as "+devPresetName+".", walk.MsgBoxIconInformation)
		return
	}
	p := filepath.Join(u.folder, devPresetName)
	if old, err := os.ReadFile(p); err == nil {
		var dp devPreset
		if json.Unmarshal(old, &dp) != nil || dp.App != devPresetApp {
			walk.MsgBox(u.win(), "Save roll preset", p+" exists and was not written by this program - left unchanged.", walk.MsgBoxIconWarning)
			return
		}
		if walk.MsgBox(u.win(), "Save roll preset", "Replace the develop preset of this roll?\r\n"+p, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != win.IDYES {
			return
		}
	}
	b, _ := json.MarshalIndent(devPreset{App: devPresetApp, Version: appVersion, Saved: time.Now().Format("2006-01-02 15:04:05"), Chain: u.chain, Orient: u.orientMap()}, "", "  ")
	if err := os.WriteFile(p+".part", b, 0o644); err == nil {
		err = os.Rename(p+".part", p)
		if err != nil {
			walk.MsgBox(u.win(), "Save roll preset", err.Error(), walk.MsgBoxIconError)
			return
		}
	}
	u.dirty = false
	u.presetLbl.SetText("Roll preset: saved " + p)
	u.log("Roll preset saved: " + p)
}

func (u *DevUI) loadPresetFile(p string, quiet bool) bool {
	b, err := os.ReadFile(p)
	if err != nil {
		if !quiet {
			walk.MsgBox(u.win(), "Load preset", err.Error(), walk.MsgBoxIconError)
		}
		return false
	}
	var dp devPreset
	if err := json.Unmarshal(b, &dp); err != nil || dp.App != devPresetApp {
		if !quiet {
			walk.MsgBox(u.win(), "Load preset", p+" is not a develop preset of this program.", walk.MsgBoxIconError)
		}
		return false
	}
	c := dp.Chain
	// Presets saved before v0.71 have no film_wb_auto: their film WB was
	// chosen by hand (or the built-in default) and is kept as saved.
	for i := range c.Nodes {
		if c.Nodes[i].Type == develop.TPositive && c.Nodes[i].V != nil {
			if _, ok := c.Nodes[i].V["film_wb_auto"]; !ok {
				c.Nodes[i].V["film_wb_auto"] = 0
			}
		}
	}
	c.Normalize()
	c.Resolve(u.cx)
	u.chain = c
	u.orient = map[string]develop.Orient{}
	for k, v := range dp.Orient {
		if o := develop.Orient(v); o.Valid() && o != 0 {
			u.orient[k] = o
		}
	}
	if u.pix0 != nil {
		u.applyOrient()
	}
	for _, t := range develop.Order {
		u.syncPanel(t)
	}
	u.selectNode(u.sel)
	u.dirty = false
	u.presetLbl.SetText("Roll preset: loaded " + p)
	u.log("Preset loaded: " + p)
	u.baseNote = ""
	u.render()
	return true
}

// ---------- export ----------

func devOutPath(src string) (string, error) {
	dir := filepath.Dir(src)
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	for n := 1; n < 1000; n++ {
		name := base + "_POS.tiff"
		if n > 1 {
			name = fmt.Sprintf("%s_POS_%d.tiff", base, n)
		}
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if _, err := os.Stat(p + ".part"); os.IsNotExist(err) {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("too many existing _POS files")
}

// DevelopFile renders one file at full resolution and writes a new 16-bit TIFF.
// v0.71: o rotates / flips the result (lossless remap after the colour
// chain; o == 0 writes exactly what v0.70 wrote).
func DevelopFile(src string, prog develop.Program, o develop.Orient, lzw bool, progress func(float64)) (string, error) {
	im, err := cropper.Load(src)
	if err != nil {
		return "", err
	}
	out := &cropper.Image{W: im.W, H: im.H, Bits: 16, Format: "tiff", Pix: make([]uint16, len(im.Pix))}
	prog.RunU16(im.Pix, out.Pix, progress)
	im = nil
	out.Pix, out.W, out.H = develop.OrientU16(out.Pix, out.W, out.H, o)
	p, err := devOutPath(src)
	if err != nil {
		return "", err
	}
	return p, cropper.Save(p, out, cropper.SaveOptions{LZW: lzw, Software: appVersion})
}

func (u *DevUI) setExporting(on bool) {
	u.busy = on
	if u.expOneBtn != nil {
		u.expOneBtn.SetEnabled(!on)
		u.expAllBtn.SetEnabled(!on)
	}
}

func (u *DevUI) export(all bool) {
	if u.busy || len(u.files) == 0 {
		if len(u.files) == 0 {
			u.status.SetText("Nothing to export: open a TIFF or a roll folder first.")
		}
		return
	}
	todo := []string{u.files[u.cur]}
	if all {
		todo = append([]string(nil), u.files...)
	}
	prog := u.chain.Clone().Compile(u.cx, -1)
	ors := make([]develop.Orient, len(todo))
	for i, f := range todo {
		ors[i] = u.orient[filepath.Base(f)]
	}
	lzw := u.compCB.CurrentIndex() == 1
	u.setExporting(true)
	u.pb.SetRange(0, 1000)
	u.pb.SetValue(0)
	u.log(fmt.Sprintf("Export %d file(s), 16-bit TIFF %s, output %s%s.", len(todo), map[bool]string{true: "LZW", false: "uncompressed"}[lzw], develop.DomainNames[prog.Domain], map[bool]string{true: " (" + strings.Join(prog.Notes, "; ") + ")", false: ""}[len(prog.Notes) > 0]))
	go func() {
		defer u.guard("export")
		done, failed := 0, 0
		for i, f := range todo {
			t0 := time.Now()
			last := time.Time{}
			out, err := DevelopFile(f, prog, ors[i], lzw, func(fr float64) {
				if time.Since(last) > 150*time.Millisecond || fr >= 1 {
					last = time.Now()
					v := int((float64(i) + fr*0.9) / float64(len(todo)) * 1000)
					u.ui(func() { u.pb.SetValue(v) })
				}
			})
			msg := ""
			if err != nil {
				failed++
				msg = filepath.Base(f) + ": ERROR " + err.Error()
			} else {
				done++
				msg = fmt.Sprintf("%s -> %s  (%.1f s)", filepath.Base(f), filepath.Base(out), time.Since(t0).Seconds())
				if ors[i] != 0 {
					msg += "  [" + ors[i].String() + "]"
				}
			}
			v := (i + 1) * 1000 / len(todo)
			u.ui(func() { u.pb.SetValue(v); u.log(msg) })
		}
		u.ui(func() {
			u.setExporting(false)
			s := fmt.Sprintf("Develop export finished: %d written, %d failed. Existing files are never overwritten (new _POS names).", done, failed)
			u.log(s)
			u.status.SetText(s)
		})
	}()
}

// ---------- page ----------

func (u *DevUI) Page() TabPage {
	// Controls fire their change events while they are created (initial
	// value, combo model): ignore them until init has synced the panels -
	// they would otherwise turn Auto film WB off before a roll is opened.
	u.syncing = true
	u.cx, u.db = developContext()
	u.chain = develop.DefaultChain()
	u.panels = map[string]*walk.Composite{}
	u.ctls = map[string][]*paramCtl{}
	grey := walk.RGB(110, 110, 110)
	var panelWs []Widget
	u.pv = make([]*walk.Composite, len(develop.Order))
	pv := u.pv
	for i, t := range develop.Order {
		panelWs = append(panelWs, Composite{AssignTo: &pv[i], Visible: i == 0, Layout: Grid{Columns: 3, Spacing: 4, MarginsZero: true}, Children: u.panelWidgets(t)})
	}
	return TabPage{
		Title:  "  4. Develop  ",
		Layout: HBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 8},
		Children: []Widget{
			Composite{
				StretchFactor: 3,
				Layout:        VBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					CustomWidget{AssignTo: &u.strip, PaintPixels: u.paintStrip, PaintMode: PaintBuffered, InvalidatesOnResize: true,
						MinSize: Size{Height: 96}, MaxSize: Size{Height: 96}, OnMouseDown: u.stripClick,
						ToolTipText: "Serial nodes, left to right. Click: edit node. Right-click or the dot: bypass / enable."},
					CustomWidget{AssignTo: &u.view, PaintPixels: u.paintView, PaintMode: PaintBuffered, InvalidatesOnResize: true,
						MinSize: Size{Width: 420, Height: 260}, StretchFactor: 4, OnMouseDown: u.viewClick, OnMouseMove: u.navMove, OnMouseUp: u.navUp,
						ToolTipText: "Wheel: zoom at the cursor. Middle-drag or Space+left-drag: pan. Double-click middle: fit."},
					Composite{Layout: HBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
						PushButton{Text: "Rotate left 90", OnClicked: func() { u.orientOp(develop.OpRotLeft) }, ToolTipText: "Rotate this frame 90 degrees counter-clockwise (preview and export, saved per frame in the roll preset)"},
						PushButton{Text: "Rotate right 90", OnClicked: func() { u.orientOp(develop.OpRotRight) }, ToolTipText: "Rotate this frame 90 degrees clockwise"},
						PushButton{Text: "180", OnClicked: func() { u.orientOp(develop.OpRot180) }, ToolTipText: "Rotate this frame 180 degrees"},
						PushButton{Text: "Flip H", OnClicked: func() { u.orientOp(develop.OpFlipH) }, ToolTipText: "Flip horizontally (mirror left-right)"},
						PushButton{Text: "Flip V", OnClicked: func() { u.orientOp(develop.OpFlipV) }, ToolTipText: "Flip vertically (mirror top-bottom)"},
						PushButton{Text: "Reset", OnClicked: func() { u.orientOp(-1) }, ToolTipText: "Back to the orientation as scanned"},
						Label{AssignTo: &u.orientLbl, Text: "as scanned", TextColor: grey},
						HSpacer{},
						PushButton{Text: "Fit", OnClicked: u.zoomFit, ToolTipText: "Fit the image to the preview (double-click middle button)"},
						PushButton{Text: "100%", OnClicked: func() { u.zoom100() }, ToolTipText: "1:1 - one image pixel per screen pixel, rendered from the full-resolution frame"},
						Label{AssignTo: &u.zoomLbl, Text: "Fit", MinSize: Size{Width: 70}},
					}},
					Label{AssignTo: &u.status, EllipsisMode: EllipsisEnd, Text: "Open an HDR TIFF (HDR Scan) or a roll folder.", TextColor: grey},
					Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
						PushButton{Text: "Open TIFF...", OnClicked: func() {
							dlg := new(walk.FileDialog)
							dlg.Title = "Open an HDR TIFF (negative)"
							dlg.Filter = "TIFF (*.tif;*.tiff)|*.tif;*.tiff"
							if ok, _ := dlg.ShowOpen(u.win()); ok {
								u.Open([]string{dlg.FilePath})
							}
						}},
						PushButton{Text: "Open roll folder...", OnClicked: func() {
							dlg := new(walk.FileDialog)
							dlg.Title = "Select a roll folder with HDR TIFFs"
							if ok, _ := dlg.ShowBrowseFolder(u.win()); ok {
								u.Open([]string{dlg.FilePath})
							}
						}},
						ComboBox{AssignTo: &u.fileCB, StretchFactor: 1, OnCurrentIndexChanged: func() {
							if u.syncing {
								return
							}
							if i := u.fileCB.CurrentIndex(); i >= 0 && i < len(u.files) {
								u.cur = i
								u.loadProxy(u.files[i])
							}
						}},
					}},
					Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
						Label{Text: "Show"},
						ComboBox{AssignTo: &u.viewCB, Model: []string{"Final output", "Output of selected node"}, CurrentIndex: 0, OnCurrentIndexChanged: func() { u.render() }},
						ComboBox{AssignTo: &u.cmpCB, Model: []string{"After", "Before (negative)", "Split before / after"}, CurrentIndex: 0, OnCurrentIndexChanged: func() { u.render() }},
						Label{Text: "Viewer"},
						ComboBox{AssignTo: &u.dispCB, Model: []string{"Rec.709 view of log signals", "Signal as is"}, CurrentIndex: 0, OnCurrentIndexChanged: func() { u.render() },
							ToolTipText: "Preview only: a Cineon / DI / ACEScct / linear output is shown through a Rec.709 transform. Exports always write the signal as is."},
						HSpacer{},
					}},
				},
			},
			Composite{
				StretchFactor: 1,
				MinSize:       Size{Width: 400},
				MaxSize:       Size{Width: 400},
				Layout:        VBox{MarginsZero: true, Spacing: 4},
				Children: []Widget{
					GroupBox{Title: "Node", StretchFactor: 1, Layout: VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 6}}, Children: []Widget{
						Label{AssignTo: &u.nodeTitle, Text: "Node 1", Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}, TextColor: cAccent},
						Label{AssignTo: &u.nodeNote, Text: "", TextColor: grey, MinSize: Size{Height: 44}, MaxSize: Size{Width: 380}},
						CheckBox{AssignTo: &u.enableChk, Text: "Node enabled (off = bypass)", Checked: true, OnCheckedChanged: func() {
							if u.syncing {
								return
							}
							u.chain.Nodes[u.sel].Bypass = !u.enableChk.Checked()
							u.changed()
						}},
						ScrollView{StretchFactor: 1, HorizontalFixed: true, Layout: VBox{MarginsZero: true, Spacing: 0}, Children: append(panelWs, VSpacer{})},
					}},
					GroupBox{Title: "Roll preset", Layout: VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 6}}, Children: []Widget{
						Label{AssignTo: &u.presetLbl, Text: "Roll preset: none", TextColor: grey},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
							PushButton{Text: "Save to roll folder", StretchFactor: 1, OnClicked: u.savePreset},
							PushButton{Text: "Load...", StretchFactor: 1, OnClicked: func() {
								dlg := new(walk.FileDialog)
								dlg.Filter = "Develop preset (" + devPresetName + ")|*.json"
								if ok, _ := dlg.ShowOpen(u.win()); ok {
									u.loadPresetFile(dlg.FilePath, false)
								}
							}},
							PushButton{Text: "Defaults", StretchFactor: 1, OnClicked: func() {
								u.chain = develop.DefaultChain()
								for _, t := range develop.Order {
									u.syncPanel(t)
								}
								u.selectNode(u.sel)
								u.changed()
							}},
						}},
						CheckBox{AssignTo: &u.autoBaseChk, Text: "Film base from the film base DB when a roll has no preset", Checked: true},
					}},
					GroupBox{Title: "Export (16-bit TIFF, new _POS file next to the source)", Layout: Grid{Columns: 2, Spacing: 4, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 6}}, Children: []Widget{
						Label{Text: "Folder files"},
						ComboBox{AssignTo: &u.filterCB, Model: []string{"HDR TIFFs only (*_HDR.tiff)", "All TIFFs"}, CurrentIndex: 0},
						Label{Text: "Compression"},
						ComboBox{AssignTo: &u.compCB, Model: []string{"None", "LZW"}, CurrentIndex: 0},
						Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
							PushButton{AssignTo: &u.expOneBtn, Text: "Export current", StretchFactor: 1, OnClicked: func() { u.export(false) }},
							PushButton{AssignTo: &u.expAllBtn, Text: "Export all in list", StretchFactor: 1, OnClicked: func() { u.export(true) }},
						}},
						ProgressBar{AssignTo: &u.pb, ColumnSpan: 2, MaxSize: Size{Height: 14}},
						TextEdit{AssignTo: &u.logTE, ColumnSpan: 2, ReadOnly: true, VScroll: true, MinSize: Size{Height: 90}, MaxSize: Size{Height: 120}},
					}},
				},
			},
		},
	}
}

// init runs once the window exists: maps the per-node panels and syncs them.
func (u *DevUI) init() {
	u.syncing = false
	if u.view == nil {
		return
	}
	for i, t := range develop.Order {
		u.panels[t] = u.pv[i]
		u.syncPanel(t)
	}
	u.selectNode(0)
	u.navInit()
}

// wrapText breaks a note into lines of at most n characters at spaces, so the
// fixed-width node panel never grows sideways (a walk Label does not wrap).
func wrapText(s string, n int) string {
	var out []string
	line := ""
	for _, w := range strings.Fields(s) {
		if line != "" && len(line)+1+len(w) > n {
			out = append(out, line)
			line = w
			continue
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	if line != "" {
		out = append(out, line)
	}
	return strings.Join(out, "\r\n")
}
