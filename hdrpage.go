package main

import (
	"basetool/rgblight"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"basetool/camera"
	"basetool/filmdb"
	"basetool/hdrscan"
	"basetool/rawconv"
	"basetool/scanmeta"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

const appName = "NegativeScan Studio"
const appVersion = appName + " v0.96.3"

// legacyAppName: the program was called Film Base Meter up to v0.67; its
// settings folder is still read (copied once, never changed) by migrateLegacyConfig.
const legacyAppName = "Film Base Meter"

// ---------- config ----------

type HDRConfig struct {
	Target     int // index into targetItems
	AreaMode   int // 0 auto, 1 rectangle
	OutDir     string
	KeepTIFF   bool
	DeleteRAW  bool
	Align      int // index into alignItems
	Merge      int // index into mergeItems (default mergeDefault = Weighted since v0.74)
	LZW        bool
	SimScene   string
	Slowest    int    // index into slowestVals
	Bracket    int    // index into bracketItems
	Roll       string // roll name (file-name prefix, sanitized)
	Counter    int    // last shot number NNNN used
	Dust       int    // index into dustItems (0 = off)
	Frame0     int    // 0 = auto metering, 1 = fixed shutter at ISO
	Shutter    int    // v0.81: index into fixedShutterVals (fixed frame 0, leveling, trichrome)
	ScanMode   int    // v0.82: Trichrome page mode, index into triModeItems
	TriBracket int    // v0.82: Trichrome page bracket per channel, index into bracketItems
	ISO        int    // index into isoVals (fixed frame 0 only)
	Stock      string // film stock from the base database ("" = none)
	StockTol   int    // index into stockTolVals
}

// Bracket presets (display order). Config: BRACKET_EV=0,2 (v0.62+);
// v0.6/v0.61 wrote BRACKET=0 (0/+2) or 1 (0/+2/+4, then called "High").
// v0.77: only two presets remain, 0/+2 (default) and 0/+2/+3. A saved
// config whose preset was removed (0/+3, 0/+2/+4, 0/+1/+2/+3, legacy
// BRACKET=1) loads as the 0/+2 default. Merge files still takes any 2-3
// frames whatever the presets are.
var bracketEVs = [][]int{{0, 2}, {0, 2, 3}}
var bracketItems = []string{"0 / +2 EV (default)", "0 / +2 / +3 EV"}
var bracketLegacy = []int{0, 0} // v0.61 BRACKET index -> bracketItems index (0/+2/+4 is gone)

const bracketDefault = 0 // 0 / +2

// Dust removal on the merged HDR (v0.63). Config DUST=off|light|strong.
var dustItems = []string{"Off", "Light", "Strong"}
var dustKeys = []string{"off", "light", "strong"}

// v0.73: the colour options (COLOR_ENC, MASK_COMP, CROSSTALK, XT_MATRIX,
// COLOR_BASE of v0.66-v0.72) were removed from the HDR page; those keys are
// ignored on load and no longer written.

// Frame 0 exposure (v0.63). Config FRAME0=auto|fixed, ISO=160.
var frame0Items = []string{"Auto (meter the shutter)", "Fixed (ISO + base shutter)"}
var frame0Keys = []string{"auto", "fixed"}
var isoVals = []int{160, 200, 250, 320}
var isoItems = []string{"160", "200", "250", "320"}

// Base check tolerance against the film stock (EV per channel).
var stockTolVals = []float64{0.05, 0.10, 0.15, 0.20, 0.30}
var stockTolItems = []string{"0.05 EV", "0.10 EV", "0.15 EV", "0.20 EV", "0.30 EV"}

const stockTolDefault = 1

func evKey(evs []int) string {
	var p []string
	for _, e := range evs {
		p = append(p, strconv.Itoa(e))
	}
	return strings.Join(p, ",")
}

// evText is "0 / +2 / +3 EV".
func evText(evs []int) string {
	var p []string
	for _, e := range evs {
		if e == 0 {
			p = append(p, "0")
		} else {
			p = append(p, fmt.Sprintf("+%d", e))
		}
	}
	return strings.Join(p, " / ") + " EV"
}

// Slowest frame-0 shutter allowed while metering (ISO is fixed at 160).
var slowestVals = []uint32{camera.Shutter1_60, 31250, 62500, 125000, 250000}
var slowestItems = []string{"1/60 s (default)", "1/30 s", "1/15 s", "1/8 s", "1/4 s"}

var targetVals = []float64{0.50, 0.60, 0.70, 0.75, 0.78, 0.80, 0.82, 0.85}
var targetItems = []string{"0.50 of full scale (more headroom)", "0.60 of full scale (more headroom)", "0.70 of full scale", "0.75 of full scale", "0.78 of full scale",
	"0.80 of full scale (recommended)", "0.82 of full scale", "0.85 of full scale"}

const targetDefault = 5 // 0.80

// v0.4 saved TARGET as an index into this list.
var targetValsV04 = []float64{0.75, 0.78, 0.80, 0.82, 0.85}

func targetIndex(v float64) int {
	for i, t := range targetVals {
		if math.Abs(t-v) < 0.004 {
			return i
		}
	}
	return -1
}

var alignItems = []string{"Check only (report a shift)", "Check and correct a shift", "Off"}
var alignModes = []int{hdrscan.AlignCheck, hdrscan.AlignCorrect, hdrscan.AlignOff}
var mergeItems = []string{"Classic", "Weighted"}
var mergeModes = []int{hdrscan.MergeClassic, hdrscan.MergeWeighted}
var mergeKeys = []string{"classic", "weighted"}

// v0.74: Weighted is the default. Config MERGE_METHOD=classic|weighted is
// the choice made in v0.74+ and is respected. Configs from v0.70-v0.73 only
// have MERGE=0|1, and those versions wrote MERGE=0 even when nobody touched
// the dropdown, so a legacy MERGE=0 cannot be told apart from the old
// default: it moves to Weighted once. MERGE=<index> is still written so an
// older version reads the same choice.
const mergeDefault = 1 // Weighted

const mergeTip = "Weighted (default): merges the RAF sensor data photosite by photosite with noise-optimal weights,\nthen demosaics once - cleaner dense shadows, about 10 s slower per merge (RAF only).\nThe base and the highlights stay frame 0. Use Alignment \"Check and correct\" if frames can move.\nClassic: the merge used up to v0.73 (hard clip masks, demosaiced frames), output unchanged."

func hdrConfigFile() string { return filepath.Join(configDir(), "hdr_config.txt") }

func defaultOutDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, "Pictures", "Film HDR scans")
}

func loadHDRConfig() HDRConfig {
	c := HDRConfig{Target: targetDefault, OutDir: defaultOutDir(), LZW: true, StockTol: stockTolDefault, Bracket: bracketDefault, Merge: mergeDefault}
	haveFS, haveEV, haveMM := false, false, false
	b, err := os.ReadFile(hdrConfigFile())
	if err != nil {
		return c
	}
	for _, ln := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
		k, v, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		n, _ := strconv.Atoi(v)
		switch k {
		case "TARGET": // v0.4 index
			if n >= 0 && n < len(targetValsV04) && !haveFS {
				c.Target = targetIndex(targetValsV04[n])
			}
		case "TARGETFS": // v0.5+: the value itself
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				if i := targetIndex(f); i >= 0 {
					c.Target, haveFS = i, true
				}
			}
		case "AREA":
			c.AreaMode = n & 1
		case "OUTDIR":
			if v != "" {
				c.OutDir = v
			}
		case "KEEP_TIFF":
			c.KeepTIFF = v == "1"
		case "DELETE_RAW":
			c.DeleteRAW = v == "1"
		case "ALIGN":
			if n >= 0 && n < len(alignItems) {
				c.Align = n
			}
		case "MERGE": // v0.70-v0.73 index; 0 was also the untouched default
			if n >= 0 && n < len(mergeItems) && !haveMM {
				c.Merge = n
				if n == 0 {
					c.Merge = mergeDefault
				}
			}
		case "MERGE_METHOD": // v0.74+
			for i, k := range mergeKeys {
				if k == v {
					c.Merge, haveMM = i, true
				}
			}
		case "LZW":
			c.LZW = v == "1"
		case "SIM_SCENE":
			c.SimScene = v
		case "SLOWEST":
			if n >= 0 && n < len(slowestVals) {
				c.Slowest = n
			}
		case "BRACKET": // v0.6/v0.61 index
			if n >= 0 && n < len(bracketLegacy) && !haveEV {
				c.Bracket = bracketLegacy[n]
			}
		case "BRACKET_EV":
			for i, e := range bracketEVs {
				if evKey(e) == v {
					c.Bracket, haveEV = i, true
				}
			}
		case "ROLL":
			c.Roll = hdrscan.SanitizeRoll(v)
		case "DUST":
			for i, k := range dustKeys {
				if k == v {
					c.Dust = i
				}
			}
		case "FRAME0":
			for i, k := range frame0Keys {
				if k == v {
					c.Frame0 = i
				}
			}
		case "FIXED_SHUTTER":
			c.Shutter = fixedShutterIndex(v)
		case "SCAN_MODE": // v0.81 (white / tri / tri_hdr on the HDR page)
			c.ScanMode = map[bool]int{true: 1, false: 0}[v == "tri_hdr"]
		case "TRI_MODE":
			c.ScanMode = keyIndex(triModeKeys, v, 0)
		case "TRI_BRACKET":
			c.TriBracket = keyIndex([]string{"0_2", "0_2_3"}, v, 0)
		case "ISO":
			for i, x := range isoVals {
				if x == n {
					c.ISO = i
				}
			}
		case "STOCK":
			c.Stock = v
		case "STOCK_TOL":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				for i, t := range stockTolVals {
					if math.Abs(t-f) < 0.004 {
						c.StockTol = i
					}
				}
			}
		case "SHOT_COUNTER":
			if n > 0 {
				c.Counter = n
			}
		}
	}
	return c
}

// keyIndex is the index of v in keys (def if absent).
func keyIndex(keys []string, v string, def int) int {
	for i, k := range keys {
		if k == v {
			return i
		}
	}
	return def
}

func saveHDRConfig(c HDRConfig) {
	b := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	ti := c.Target
	if ti < 0 || ti >= len(targetVals) {
		ti = targetDefault
	}
	bi := c.Bracket
	if bi < 0 || bi >= len(bracketEVs) {
		bi = bracketDefault
	}
	legacy := 0 // what v0.61 understands: 0 = 0/+2 (0/+2/+4 no longer offered)
	clamp := func(i, n int) int {
		if i < 0 || i >= n {
			return 0
		}
		return i
	}
	mi := c.Merge
	if mi < 0 || mi >= len(mergeItems) {
		mi = mergeDefault
	}
	tol := c.StockTol
	if tol < 0 || tol >= len(stockTolVals) {
		tol = stockTolDefault
	}
	s := fmt.Sprintf("TARGETFS=%.2f\r\nAREA=%d\r\nOUTDIR=%s\r\nKEEP_TIFF=%s\r\nDELETE_RAW=%s\r\nALIGN=%d\r\nLZW=%s\r\nSIM_SCENE=%s\r\nSLOWEST=%d\r\nBRACKET=%d\r\nBRACKET_EV=%s\r\nROLL=%s\r\nSHOT_COUNTER=%d\r\nDUST=%s\r\nFRAME0=%s\r\nISO=%d\r\nSTOCK=%s\r\nSTOCK_TOL=%.2f\r\nMERGE=%d\r\nMERGE_METHOD=%s\r\nFIXED_SHUTTER=%s\r\nTRI_MODE=%s\r\nTRI_BRACKET=%s\r\n",
		targetVals[ti], c.AreaMode, c.OutDir, b(c.KeepTIFF), b(c.DeleteRAW), c.Align, b(c.LZW), c.SimScene, c.Slowest, legacy, evKey(bracketEVs[bi]), hdrscan.SanitizeRoll(c.Roll), c.Counter,
		dustKeys[clamp(c.Dust, len(dustKeys))], frame0Keys[clamp(c.Frame0, len(frame0Keys))], isoVals[clamp(c.ISO, len(isoVals))], c.Stock, stockTolVals[tol],
		mi, mergeKeys[mi], fixedShutterKeys[clamp(c.Shutter, len(fixedShutterKeys))], triModeKeys[clamp(c.ScanMode, len(triModeKeys))], []string{"0_2", "0_2_3"}[clamp(c.TriBracket, 2)])
	os.MkdirAll(configDir(), 0o755)
	os.WriteFile(hdrConfigFile(), []byte(s), 0o644)
}

// ---------- camera worker: every camera call runs on one locked OS thread ----------

type camWorker struct {
	jobs    chan func()
	onPanic func(r any, stack string) // logs and reports a job panic
}

func newCamWorker() *camWorker {
	w := &camWorker{jobs: make(chan func(), 64)}
	go func() {
		runtime.LockOSThread()
		for j := range w.jobs {
			func() {
				defer func() {
					if r := recover(); r != nil {
						st := string(debug.Stack())
						fmt.Println("camera job panic:", r)
						if w.onPanic != nil {
							w.onPanic(r, st)
						}
					}
				}()
				j()
			}()
		}
	}()
	return w
}

func (w *camWorker) do(f func()) { w.jobs <- f }

// ---------- UI ----------

type HDRUI struct {
	levelMetaCB  *walk.CheckBox
	levelMetaLbl *walk.Label
	mw           **walk.MainWindow
	cfg          HDRConfig
	log          *camera.Logger
	worker       *camWorker

	dev    camera.Device
	sess   *hdrscan.Session
	isSim  bool
	wpd    []camera.WPDDevice
	devIdx int

	// view
	view       *walk.CustomWidget // the live view in use (one of views)
	views      [4]*walk.CustomWidget
	lvBtns     [4]*walk.PushButton
	logTE2     *walk.TextEdit
	bmp        *walk.Bitmap
	viewWhat   string
	roi        hdrscan.Rect
	baseNorm   hdrscan.Rect
	dragging   bool
	dragA      hdrscan.Rect
	lvOn       atomic.Bool // live view loop should run
	lvRunning  atomic.Bool // live view goroutine alive
	lvVisible  bool        // a page containing a live view is visible (UI thread)
	lvWanted   bool        // user wants live view (UI thread); paused during camera jobs
	lvDrawing  atomic.Bool // a frame is waiting for the UI thread (drop new ones)
	statusBusy atomic.Bool // a status read is queued
	camJob     atomic.Bool // meter / capture / test shot / diagnostics running
	busy       bool
	processing bool

	subTabs                                         *pager
	hair                                            *hairUI // Remove hairs sub-tab
	subScroll                                       *walk.ScrollView
	devCB, targetCB, areaCB, tiffCB, rawCB, alignCB *walk.ComboBox
	mergeCB                                         *walk.ComboBox
	slowCB                                          *walk.ComboBox
	testBtn                                         *walk.PushButton
	// big exposure readout
	camISO, camSh, camF, f0ISO, f0Sh, p2Sh, baseLv, baseR, baseG, baseB *walk.LineEdit
	compCB                                                              *walk.ComboBox
	connBtn, diagBtn, lvBtn, meterBtn, capBtn                           *walk.PushButton
	camLbl, expLbl, baseLbl, viewLbl                                    *walk.Label
	brCB                                                                *walk.ComboBox
	outEd, rollEd                                                       *walk.LineEdit
	logTE, resTE                                                        *walk.TextEdit
	pb                                                                  *walk.ProgressBar

	// link to Develop > Density In
	sendBtn, stopBtn, mOpenBtn *walk.PushButton
	scanCancel                 context.CancelFunc // v0.83 Stop (white light scan)
	scanBefore                 *rgblight.State
	lastCap, lastMerge         *hdrscan.Result // last HDR from Camera capture / Merge two files
	sending                    bool
	// SendBase / OpenInDevelop are set by main and switch to Develop > Density In;
	SendBase      func(SentBase)
	OpenInDevelop func(path string)

	// merge two files
	m0Ed, m2Ed, m4Ed  *walk.LineEdit
	mAlignCB, mCompCB *walk.ComboBox
	mMergeCB          *walk.ComboBox
	mBtn              *walk.PushButton
	mRes              *walk.TextEdit

	// v0.63: dust removal, fixed frame 0, film-base DB, per-roll folder
	dustCB, f0CB, isoCB, stockCB, tolCB, mDustCB *walk.ComboBox
	shutCB                                       *walk.ComboBox // v0.81 base shutter
	tri                                          *TriUI         // v0.82 Trichrome page
	handoff                                      *baseHandoff   // v0.82 leveling / stored light handed to the lock
	autoLocking                                  bool
	lockSrc                                      string   // what locked frame 0 ("" = Meter base)
	lockLight                                    *[3]int  // light values when frame 0 was locked (nil = unknown)
	light                                        *LightUI // RGB light page (trichrome)
	recBtn, saveDBBtn, saveF0Btn, manageBtn      *walk.PushButton
	db                                           *filmdb.DB
	sessFirst                                    time.Time // first capture of this session (empty roll name folder)
	capExp                                       camera.Exposure
	capReading                                   hdrscan.Reading
	lastF0                                       *frame0Base
	lockBase                                     string  // base status of a fixed lock without the film stock line
	camFn, lockFn                                float64 // f-number the camera reported (0 = lens reports none)
}

func (u *HDRUI) win() *walk.MainWindow { return *u.mw }

func (u *HDRUI) ui(f func()) {
	if mw := u.win(); mw != nil {
		mw.Synchronize(func() {
			defer u.guard("UI update")
			f()
		})
	}
}

func (u *HDRUI) appendLog(s string) {
	line := time.Now().Format("15:04:05  ") + s + "\r\n"
	for _, te := range []*walk.TextEdit{u.logTE, u.logTE2} {
		if te == nil {
			continue
		}
		t := te.Text()
		if len(t) > 60000 {
			t = t[len(t)-40000:]
			if i := strings.Index(t, "\r\n"); i >= 0 {
				t = t[i+2:]
			}
			te.SetText(t)
		}
		te.AppendText(line)
		te.ScrollToCaret()
	}
}

// invalidateViews repaints Camera, Light, White light and Trichrome previews;
// they all show the same camera frame and normalized base rectangle.
func (u *HDRUI) invalidateViews() {
	for _, v := range u.views {
		if v != nil {
			v.Invalidate()
		}
	}
}

// liveView (v0.83) is live view k with its own base-region drag box and
// buttons. Mouse input makes it the view in use; painting uses its size.
func (u *HDRUI) liveView(k int, grey walk.Color) Widget {
	use := func() { u.view = u.views[k] }
	row := []Widget{
		PushButton{AssignTo: &u.lvBtns[k], Text: "Start live view", Enabled: false, OnClicked: func() { use(); u.toggleLiveView() }},
		PushButton{Text: "Reset to Auto", OnClicked: func() {
			u.clearBaseRectangle()
		}},
	}
	if k == 0 {
		row = append(row, Label{AssignTo: &u.viewLbl, Text: "", TextColor: grey})
	}
	return Composite{StretchFactor: 3, Layout: VBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
		CustomWidget{
			AssignTo: &u.views[k],
			Paint: func(c *walk.Canvas, r walk.Rectangle) error {
				prev := u.view
				u.view = u.views[k]
				defer func() { u.view = prev }()
				return u.paint(c, r)
			},
			PaintMode:           PaintBuffered,
			InvalidatesOnResize: true,
			MinSize:             Size{Width: 360, Height: 240},
			StretchFactor:       3,
			OnMouseDown:         func(x, y int, b walk.MouseButton) { use(); u.mouseDown(x, y, b) },
			OnMouseMove:         func(x, y int, b walk.MouseButton) { use(); u.mouseMove(x, y, b) },
			OnMouseUp:           func(x, y int, b walk.MouseButton) { use(); u.mouseUp(x, y, b) },
		},
		Composite{Layout: HBox{MarginsZero: true}, Children: append(row, HSpacer{})},
	}}
}

// All four previews share dev, sess, worker, bmp and roi. Hiding the live-view
// pages stops polling without clearing the user's preference or interrupting a job.
func (u *HDRUI) setLivePage(k int) {
	u.lvVisible = k >= 0
	if k < 0 {
		u.lvOn.Store(false)
		return
	}
	u.view = u.views[k]
	if u.lvWanted {
		u.startLiveView()
	}
	u.invalidateViews()
}

func (u *HDRUI) clearBaseRectangle() {
	if u.busy || (u.light != nil && u.light.level) {
		return
	}
	u.roi = hdrscan.Rect{}
	u.cfg.AreaMode = 0
	if u.areaCB != nil {
		u.areaCB.SetCurrentIndex(0)
	}
	u.save()
	if l := u.light; l != nil {
		l.cfg.Area, l.cfg.AreaMode, l.cfg.AreaPicked = toLightRect(hdrscan.Rect{}), 0, false
		if l.areaCB != nil {
			l.areaCB.SetCurrentIndex(0)
		}
		l.saveCfg()
		if l.view != nil {
			l.view.Invalidate()
		}
	}
	u.invalidateViews()
}

func (u *HDRUI) say(s string) {
	if u.log != nil {
		u.log.Printf("%s", s)
	} else {
		u.ui(func() { u.appendLog(s) })
	}
}

func (u *HDRUI) save() {
	if u.targetCB != nil {
		u.cfg.Target = u.targetCB.CurrentIndex()
	}
	if u.areaCB != nil {
		u.cfg.AreaMode = u.areaCB.CurrentIndex()
	}
	if u.tiffCB != nil {
		u.cfg.KeepTIFF = u.tiffCB.CurrentIndex() == 1
	}
	if u.rawCB != nil {
		u.cfg.DeleteRAW = u.rawCB.CurrentIndex() == 1
	}
	if u.alignCB != nil {
		u.cfg.Align = u.alignCB.CurrentIndex()
	}
	if u.mergeCB != nil {
		u.cfg.Merge = max(0, u.mergeCB.CurrentIndex())
	}
	if u.compCB != nil {
		u.cfg.LZW = u.compCB.CurrentIndex() == 1
	}
	if u.rollEd != nil {
		u.cfg.Roll = hdrscan.SanitizeRoll(u.rollEd.Text())
	}
	if u.outEd != nil {
		u.cfg.OutDir = u.outEd.Text()
	}
	if u.slowCB != nil {
		u.cfg.Slowest = max(0, u.slowCB.CurrentIndex())
	}
	if u.brCB != nil {
		u.cfg.Bracket = max(0, u.brCB.CurrentIndex())
	}
	if u.dustCB != nil {
		u.cfg.Dust = max(0, u.dustCB.CurrentIndex())
	}
	if u.f0CB != nil {
		u.cfg.Frame0 = max(0, u.f0CB.CurrentIndex())
	}
	if u.isoCB != nil {
		u.cfg.ISO = max(0, u.isoCB.CurrentIndex())
	}
	if u.shutCB != nil {
		u.cfg.Shutter = max(0, u.shutCB.CurrentIndex())
	}
	if u.stockCB != nil {
		prev := u.cfg.Stock
		u.cfg.Stock = ""
		if i := u.stockCB.CurrentIndex(); i > 0 {
			u.cfg.Stock = u.stockCB.Text()
		}
		if prev != u.cfg.Stock {
			defer u.stockRecheck()
		}
	}
	if u.tolCB != nil {
		u.cfg.StockTol = max(0, u.tolCB.CurrentIndex())
	}
	saveHDRConfig(u.cfg)
}

func (u *HDRUI) target() float64 {
	i := u.cfg.Target
	if i < 0 || i >= len(targetVals) {
		i = targetDefault
	}
	return targetVals[i]
}

func (u *HDRUI) updateButtons() {
	conn := u.dev != nil
	locked := conn && u.sess != nil && u.sess.Locked != nil
	u.connBtn.SetText(map[bool]string{true: "Disconnect", false: "Connect"}[conn])
	u.connBtn.SetEnabled(!u.busy)
	u.devCB.SetEnabled(!conn && !u.busy)
	u.diagBtn.SetEnabled(!u.busy)
	u.testBtn.SetEnabled(conn && !u.busy)
	for _, b := range u.lvBtns {
		if b != nil {
			b.SetEnabled(conn && !u.busy)
			b.SetText(map[bool]string{true: "Stop live view", false: "Start live view"}[u.lvWanted])
		}
	}
	u.meterBtn.SetEnabled(conn && !u.busy)
	u.meterBtn.SetText(map[bool]string{true: "Re-meter base", false: "Meter base"}[locked])
	u.capBtn.SetEnabled(locked && !u.busy && !u.processing)
	if u.sendBtn != nil {
		u.sendBtn.SetEnabled(!u.sending && !u.processing && locked && !u.busy)
		u.stopBtn.SetEnabled(u.scanCancel != nil)
	}
	if u.mOpenBtn != nil {
		u.mOpenBtn.SetEnabled(u.lastMerge != nil && u.lastMerge.HDR != "")
	}
	if u.recBtn != nil {
		u.recBtn.SetEnabled(conn && !u.busy)
		u.saveDBBtn.SetEnabled(locked && !u.busy)
		u.saveF0Btn.SetEnabled(u.lastF0 != nil && !u.busy)
		u.isoCB.SetEnabled(u.cfg.Frame0 == 1)
	}
	if u.shutCB != nil {
		u.shutCB.SetEnabled(!u.busy)
	}
	if u.handoff != nil && u.handoff.pending {
		u.autoLock()
	}
	u.refreshLevelingUI()
	if u.tri != nil {
		u.tri.updateButtons()
	}
}

// SentBase is a clear-base reading handed to Develop > Density In, on its scale
// (linear TIFF value / 65535, leak rejection, per-channel median).
type SentBase struct {
	RGB    [3]float64
	Source string
	// Trichrome base transfer disables mask/crosstalk in Develop preview.
	Trichrome bool
}

// Keep the existing default light-leak rejection after retiring its DCTL-page UI.
func (u *HDRUI) leak() float64 { return 20 }

// sendBase hands the clear base to Develop: from the last HDR
// capture after a merge (same values as frame 0 at the base), otherwise from
// the locked probe RAW decoded on the TIFF path (same exposure as frame 0).
func (u *HDRUI) sendBase() {
	if u.SendBase == nil || u.sending {
		return
	}
	if u.light != nil && u.light.cfg.UseLastLeveling {
		l := u.metadataLeveling()
		if !l.Matches(u.levelingValues()) {
			return
		}
		u.SendBase(SentBase{RGB: l.Base, Source: scanmeta.LevelingSource})
		return
	}
	if u.sess == nil || u.sess.Locked == nil {
		return
	}
	s := u.sess
	leak := u.leak()
	u.sending = true
	u.updateButtons()
	prevExp := u.expLbl.Text()
	setStatus(u.expLbl, "Measuring the base for Develop ...")
	go func() {
		rgb, n, err := s.ProbeBase(leak)
		u.ui(func() {
			u.sending = false
			setStatus(u.expLbl, prevExp)
			u.updateButtons()
			if err != nil {
				u.say("Send base failed: " + err.Error())
				walk.MsgBox(u.win(), "Send base to Develop", "Could not measure the base: "+err.Error(), walk.MsgBoxIconWarning)
				return
			}
			b := SentBase{RGB: rgb,
				Source: fmt.Sprintf("locked probe RAW at %s (frame 0 exposure), %d samples of the clear base", s.Locked, n)}
			u.say(fmt.Sprintf("Sent base to Develop: R %.4f G %.4f B %.4f (%s)", rgb[0], rgb[1], rgb[2], b.Source))
			u.SendBase(b)
		})
	}()
}

func (u *HDRUI) openInDevelop(r *hdrscan.Result) {
	if r == nil || r.HDR == "" || u.OpenInDevelop == nil {
		return
	}
	if _, err := os.Stat(r.HDR); err != nil {
		walk.MsgBox(u.win(), "Open in Develop", "The HDR TIFF is gone: "+r.HDR, walk.MsgBoxIconWarning)
		return
	}
	u.OpenInDevelop(r.HDR)
}

// ---------- device list / connect ----------

const simItem = "Simulated camera (test with a RAW file)"

func (u *HDRUI) deviceItems() []string {
	var items []string
	for _, d := range u.wpd {
		items = append(items, d.String())
	}
	items = append(items, simItem)
	return items
}

func (u *HDRUI) refreshDevices() {
	u.worker.do(func() {
		devs, err := camera.ListWPD()
		u.ui(func() {
			u.wpd = nil
			if err != nil {
				u.say("Portable device list: " + err.Error())
			} else {
				// Cameras first (Fujifilm on top).
				sort.SliceStable(devs, func(i, j int) bool {
					fi := strings.Contains(strings.ToLower(devs[i].Manufacturer+devs[i].Name), "fuji")
					fj := strings.Contains(strings.ToLower(devs[j].Manufacturer+devs[j].Name), "fuji")
					return fi && !fj
				})
				u.wpd = devs
				if len(devs) == 0 {
					u.say("No portable devices found. Connect the camera by USB, switch it on and set CONNECTION MODE to USB TETHER SHOOTING FIXED.")
				} else {
					for _, d := range devs {
						u.say("Found device: " + d.String())
					}
				}
			}
			u.devCB.SetModel(u.deviceItems())
			u.devCB.SetCurrentIndex(0)
		})
	})
}

func (u *HDRUI) connect() {
	if u.dev != nil {
		u.disconnect()
		return
	}
	i := u.devCB.CurrentIndex()
	sim := i < 0 || i >= len(u.wpd)
	scene := ""
	if sim {
		dlg := new(walk.FileDialog)
		dlg.Title = "Simulated camera: pick an uncompressed RAF to use as the scene"
		dlg.Filter = "Fujifilm RAW (*.raf)|*.raf|All files (*.*)|*.*"
		dlg.FilePath = u.cfg.SimScene
		if ok, _ := dlg.ShowOpen(u.win()); !ok {
			return
		}
		scene = dlg.FilePath
		u.cfg.SimScene = scene
		u.save()
	}
	var dev camera.WPDDevice
	if !sim {
		dev = u.wpd[i]
	}
	connISO := hdrscan.FixedISO
	if u.fixed() {
		connISO = u.fixedISO()
	}
	u.busy = true
	u.updateButtons()
	u.camLbl.SetText("Connecting ...")
	u.worker.do(func() {
		var d camera.Device
		var err error
		if sim {
			var s *camera.Sim
			s, err = camera.OpenSim(scene, u.log)
			if err == nil {
				// Scene file values = ISO 160, 1/125; start somewhere else so
				// metering has work to do.
				s.SetDials(camera.Exposure{ISO: 160, Shutter: 3906})
				d = s
			}
		} else {
			u.log.Printf("Opening %s (%s)", dev.String(), dev.ID)
			var t camera.Transport
			t, err = camera.OpenWPD(dev.ID)
			if err == nil {
				var f *camera.Fuji
				f, err = camera.OpenFuji(t, u.log, "WPD")
				if err != nil {
					t.Close()
				} else {
					d = f
				}
			}
		}
		var sess *hdrscan.Session
		var notManual error
		if err == nil {
			sess = &hdrscan.Session{Dev: d, Log: u.log, ProbeDir: filepath.Join(os.TempDir(), "NegativeScanStudio", "probe"), Dials: u.dialPrompt}
			// Auto frame 0: ISO 160 for the whole session; Fixed: the chosen ISO.
			if ierr := sess.EnsureConnectISO(connISO); errors.Is(ierr, camera.ErrNotManual) {
				notManual = ierr
				u.log.Printf("%v", ierr)
			} else if ierr != nil {
				u.log.Printf("Could not set ISO %d on connect: %v - set ISO %d on the camera (ISO dial, or A with ISO DIAL SETTING (A) = COMMAND)", connISO, ierr, connISO)
			}
		}
		var e camera.Exposure
		var eerr error
		if err == nil {
			e, eerr = d.Exposure()
		}
		u.ui(func() {
			u.busy = false
			if err != nil {
				u.camLbl.SetText("Not connected")
				u.say("Connect failed: " + err.Error())
				walk.MsgBox(u.win(), "Connect camera", "Could not connect:\n"+err.Error()+"\n\nCheck: camera on, USB cable direct to the PC, CONNECTION MODE = USB TETHER SHOOTING FIXED, and no other tether program (X Acquire, Lightroom, Capture One) running.\nUse Camera diagnostics for a detailed log.", walk.MsgBoxIconWarning)
				u.updateButtons()
				return
			}
			u.dev, u.isSim = d, sim
			u.sess = sess
			info := d.Info()
			cam := fmt.Sprintf("%s %s", info.Manufacturer, info.Model)
			if info.Driver != "" && !strings.Contains(cam, info.Driver) {
				cam += "  (" + info.Driver + ")"
			}
			u.camLbl.SetText("Connected: " + cam)
			if eerr != nil {
				setStatus(u.expLbl, "Exposure: could not read ("+eerr.Error()+")")
			} else {
				setStatus(u.expLbl, "Not metered yet  (camera "+e.String()+")")
			}
			setStatus(u.baseLbl, "Press Meter base. The base is found automatically (rebate strips, gaps between frames, sprocket area), or drag a rectangle over it.")
			u.clearLocked()
			u.lvWanted = true // live view starts by itself after connecting
			u.updateButtons()
			u.startLiveView()
			u.startStatusPoll(d)
			if notManual != nil {
				u.notManualMsg(notManual)
			}
		})
	})
}

func (u *HDRUI) disconnect() {
	u.lvOn.Store(false)
	u.lvWanted = false
	u.clearLocked()
	for _, e := range []*walk.LineEdit{u.camISO, u.camSh, u.camF} {
		e.SetText("")
	}
	d := u.dev
	u.dev, u.sess = nil, nil
	u.camLbl.SetText("Not connected")
	setStatus(u.expLbl, "")
	setStatus(u.baseLbl, "")
	u.updateButtons()
	if d != nil {
		u.worker.do(func() {
			d.StopLiveView()
			d.Close()
		})
	}
}

// notManualMsg tells the user to switch the body to M (A/P/S cannot be
// metered: the camera would change the shutter itself).
func (u *HDRUI) notManualMsg(err error) {
	setStatus(u.expLbl, "Set the camera to M (shutter dial T)")
	walk.MsgBox(u.win(), "Set the camera to M",
		"Set the camera to M (shutter dial T).\n\n"+err.Error()+
			"\n\nOn the X-T3: turn the shutter speed dial to T (and the aperture ring off A, or use a manual lens), "+
			"then press Meter base again. The program then sets ISO 160 and the shutter over USB.", walk.MsgBoxIconWarning)
}

func (u *HDRUI) dialPrompt(e camera.Exposure, why string) bool {
	ch := make(chan bool, 1)
	u.ui(func() {
		r := walk.MsgBox(u.win(), "Set the exposure on the camera",
			fmt.Sprintf("The camera does not accept exposure changes over USB, so set it by hand (%s):\n\n    ISO:      %d\n    Shutter:  %s\n\nLeave the aperture where it is. Press OK when the camera shows these values.",
				why, e.ISO, camera.ShutterLabel(e.Shutter)), walk.MsgBoxOKCancel|walk.MsgBoxIconInformation)
		ch <- r == walk.DlgCmdOK
	})
	return <-ch
}

func (u *HDRUI) diagnostics() {
	u.pauseLiveView()
	u.busy = true
	u.updateButtons()
	d := u.dev
	u.worker.do(func() {
		u.log.Printf("==== %s camera diagnostics, %s/%s ====", appVersion, runtime.GOOS, runtime.GOARCH)
		devs, err := camera.ListWPD()
		if err != nil {
			u.log.Printf("Windows Portable Devices: %v", err)
		} else {
			u.log.Printf("Windows Portable Devices: %d", len(devs))
			for _, x := range devs {
				u.log.Printf("  %s | manufacturer %q | description %q | id %s", x.Name, x.Manufacturer, x.Description, x.ID)
			}
		}
		if d != nil {
			if err := d.Diagnostics(); err != nil {
				u.log.Printf("Diagnostics error: %v", err)
			}
		} else {
			// Probe each device read-only: GetDeviceInfo only.
			for _, x := range devs {
				t, err := camera.OpenWPD(x.ID)
				if err != nil {
					u.log.Printf("Open %s: %v", x.Name, err)
					continue
				}
				p := &camera.PTP{T: t, Log: u.log, Verbose: false}
				info, raw, err := p.DeviceInfo()
				if err != nil {
					u.log.Printf("%s: GetDeviceInfo over MTP extension failed: %v", x.Name, err)
				} else {
					u.log.Printf("%s: %s %s fw %s, vendor ext 0x%08X %q, %d bytes DeviceInfo", x.Name, info.Manufacturer, info.Model, info.Version, info.VendorExt, info.VendorDesc, len(raw))
					u.log.Printf("  ops: %s", hexs(info.Ops))
					u.log.Printf("  props: %s", hexs(info.Props))
					u.log.Printf("  events: %s", hexs(info.Events))
				}
				if v, err := t.SupportedVendorOps(); err == nil {
					u.log.Printf("  vendor opcodes allowed by the driver: %s", hexs(v))
				}
				t.Close()
			}
			u.log.Printf("(Connect the camera first for the full property dump.)")
		}
		u.log.Printf("==== end ====")
		u.ui(func() {
			u.busy = false
			u.updateButtons()
			u.resumeLiveView()
			if walk.MsgBox(u.win(), "Camera diagnostics", "Written to:\n"+u.log.Path()+"\n\nOpen it now?", walk.MsgBoxYesNo|walk.MsgBoxIconInformation) == walk.DlgCmdYes {
				exec.Command("notepad.exe", u.log.Path()).Start()
			}
		})
	})
}

func hexs(a []uint16) string {
	var b strings.Builder
	for i, v := range a {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%04X", v)
	}
	return b.String()
}

// ---------- live view / image ----------

func (u *HDRUI) setImage(img image.Image, what string) {
	bmp, err := walk.NewBitmapFromImage(img)
	if err != nil {
		return
	}
	if u.bmp != nil {
		u.bmp.Dispose()
	}
	u.bmp = bmp
	u.viewWhat = what
	u.viewLbl.SetText(what)
	u.invalidateViews()
}

func (u *HDRUI) toggleLiveView() {
	if u.lvWanted {
		u.lvWanted = false
		u.lvOn.Store(false)
		u.updateButtons()
		return
	}
	u.lvWanted = true
	u.startLiveView()
	u.updateButtons()
}

// pauseLiveView stops the live view loop for a camera job; resumeLiveView
// restarts it afterwards if the user had it on. Camera calls are serialised on
// the worker thread, so the job always runs after the last live view frame.
func (u *HDRUI) pauseLiveView() {
	u.camJob.Store(true)
	u.lvOn.Store(false)
}

func (u *HDRUI) resumeLiveView() {
	u.camJob.Store(false)
	if u.lvWanted && u.dev != nil {
		u.startLiveView()
	}
}

// startLiveView runs the continuous live view loop: it polls frames as fast as
// the camera delivers them (capped at ~15 fps), decodes off the UI thread and
// drops frames while the previous one is still being drawn.
func (u *HDRUI) startLiveView() {
	if u.dev == nil || !u.lvWanted || !u.lvVisible || u.camJob.Load() {
		return
	}
	if u.lvRunning.Load() {
		if !u.lvOn.Load() {
			// previous loop still winding down: try again shortly
			time.AfterFunc(150*time.Millisecond, func() { u.ui(u.startLiveView) })
		}
		return
	}
	u.lvOn.Store(true)
	u.lvRunning.Store(true)
	d := u.dev
	go func() {
		defer u.lvRunning.Store(false)
		const minGap = 66 * time.Millisecond // <= 15 fps
		fails, frames := 0, 0
		var fps float64
		tWin := time.Now()
		for u.lvOn.Load() && u.dev == d {
			t0 := time.Now()
			done := make(chan struct{})
			var jpg []byte
			var err error
			u.worker.do(func() {
				if u.lvOn.Load() {
					jpg, err = d.LiveView()
				}
				close(done)
			})
			<-done
			if err != nil {
				fails++
				if fails >= 5 {
					u.say("Live view stopped: " + err.Error())
					u.lvOn.Store(false)
					u.ui(func() { u.lvWanted = false; u.updateButtons() })
					break
				}
				time.Sleep(300 * time.Millisecond)
				continue
			}
			fails = 0
			if jpg != nil && u.lvOn.Load() {
				frames++
				if el := time.Since(tWin); el >= time.Second {
					fps = float64(frames) / el.Seconds()
					frames, tWin = 0, time.Now()
				}
				if !u.lvDrawing.Load() {
					if img, e := jpeg.Decode(bytes.NewReader(jpg)); e == nil {
						u.lvDrawing.Store(true)
						lbl := "Live view  (measuring fps)"
						if fps > 0 {
							lbl = fmt.Sprintf("Live view  %.1f fps", fps)
						}
						u.ui(func() {
							defer u.lvDrawing.Store(false)
							if u.lvOn.Load() {
								u.setImage(img, lbl)
							}
						})
					}
				}
			}
			if gap := time.Since(t0); gap < minGap {
				time.Sleep(minGap - gap)
			}
		}
		u.worker.do(func() { d.StopLiveView() })
		u.ui(func() {
			if !u.lvOn.Load() && strings.HasPrefix(u.viewWhat, "Live view") {
				u.viewLbl.SetText("Live view paused")
			}
			u.updateButtons()
		})
	}()
}

// startStatusPoll keeps the "Camera now" readout current (every ~1.2 s,
// quiet reads, skipped while a camera job runs).
func (u *HDRUI) startStatusPoll(d camera.Device) {
	sr, ok := d.(camera.StatusReader)
	if !ok {
		return
	}
	go func() {
		for u.dev == d {
			if !u.camJob.Load() && !u.statusBusy.Load() {
				u.statusBusy.Store(true)
				u.worker.do(func() {
					defer u.statusBusy.Store(false)
					if u.dev != d || u.camJob.Load() {
						return
					}
					e, fn, err := sr.Status()
					if err != nil {
						return
					}
					u.ui(func() { u.showCamera(e, fn) })
				})
			}
			time.Sleep(1200 * time.Millisecond)
		}
	}()
}

func (u *HDRUI) showCamera(e camera.Exposure, fn float64) {
	if u.dev == nil {
		return
	}
	u.camFn = fn
	iso := camera.ISOLabel(e.ISO)
	u.camISO.SetText(iso)
	u.camSh.SetText(camera.ShutterLabel(e.Shutter))
	if fn > 0 {
		u.camF.SetText(fmt.Sprintf("f/%.1f", fn))
	} else {
		u.camF.SetText("f/-- (lens)")
	}
}

// stockRecheck redoes the film stock line of a fixed lock when the stock is
// changed after metering (QA L5), from the locked probe; nothing is reshot.
func (u *HDRUI) stockRecheck() {
	if u.sess == nil || u.sess.Locked == nil || u.lockBase == "" || u.baseLbl == nil {
		return
	}
	s, e, base := u.sess, *u.sess.Locked, u.lockBase
	ent, tol, leak := u.stock(), u.tol(), u.leak()
	if ent == nil {
		setStatus(u.baseLbl, base+"\nFilm stock: None (no DB check)")
		return
	}
	u.worker.do(func() {
		var t string
		if b1, _, err := s.ProbeBase(leak); err != nil {
			t = ent.Name + ": no base to compare (" + err.Error() + ")"
		} else {
			t, _ = compareText(ent, b1, e, tol)
		}
		u.ui(func() {
			if u.sess == s && u.lockBase == base {
				setStatus(u.baseLbl, base+"\n"+t)
			}
		})
	})
}

func (u *HDRUI) clearLocked() {
	u.lockBase = ""
	u.lockSrc, u.lockLight = "", nil
	for _, e := range []*walk.LineEdit{u.f0ISO, u.f0Sh, u.p2Sh, u.baseLv, u.baseR, u.baseG, u.baseB} {
		if e != nil {
			e.SetText("")
		}
	}
	if u.f0ISO != nil {
		u.f0ISO.SetText("not locked")
	}
}

func (u *HDRUI) imageRect() walk.Rectangle {
	cb := u.view.ClientBoundsPixels()
	if u.bmp == nil {
		return cb
	}
	s := u.bmp.Size()
	if s.Width == 0 || s.Height == 0 {
		return cb
	}
	sc := float64(cb.Width) / float64(s.Width)
	if f := float64(cb.Height) / float64(s.Height); f < sc {
		sc = f
	}
	w, h := int(float64(s.Width)*sc), int(float64(s.Height)*sc)
	return walk.Rectangle{X: cb.X + (cb.Width-w)/2, Y: cb.Y + (cb.Height-h)/2, Width: w, Height: h}
}

func (u *HDRUI) paint(c *walk.Canvas, _ walk.Rectangle) error {
	cb := u.view.ClientBoundsPixels()
	bg, _ := walk.NewSolidColorBrush(walk.RGB(22, 22, 25))
	defer bg.Dispose()
	c.FillRectanglePixels(bg, cb)
	if u.bmp == nil {
		f, _ := walk.NewFont("Segoe UI", 10, 0)
		if f != nil {
			defer f.Dispose()
			msg := "No image yet.\r\nConnect the camera and start live view, or press Meter base.\r\nDrag a rectangle here to choose the clear film base yourself."
			fmtx := walk.TextCenter | walk.TextWordbreak
			tr := cb
			if m, _, err := c.MeasureTextPixels(msg, f, cb, fmtx); err == nil && m.Height < cb.Height {
				tr.Y = cb.Y + (cb.Height-m.Height)/2
				tr.Height = m.Height + 2
			}
			c.DrawTextPixels(msg, f, walk.RGB(128, 128, 136), tr, fmtx)
		}
		return nil
	}
	ir := u.imageRect()
	c.DrawImageStretchedPixels(u.bmp, ir)
	toPx := func(r hdrscan.Rect) walk.Rectangle {
		x0 := ir.X + int(r.X0*float64(ir.Width))
		y0 := ir.Y + int(r.Y0*float64(ir.Height))
		x1 := ir.X + int(r.X1*float64(ir.Width))
		y1 := ir.Y + int(r.Y1*float64(ir.Height))
		if x1-x0 < 6 {
			cx := (x0 + x1) / 2
			x0, x1 = cx-3, cx+3
		}
		if y1-y0 < 6 {
			cy := (y0 + y1) / 2
			y0, y1 = cy-3, cy+3
		}
		return walk.Rectangle{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	}
	draw := func(r hdrscan.Rect, col walk.Color) {
		br, _ := walk.NewSolidColorBrush(col)
		if br == nil {
			return
		}
		defer br.Dispose()
		pen, _ := walk.NewGeometricPen(walk.PenSolid, 2, br)
		if pen == nil {
			return
		}
		defer pen.Dispose()
		c.DrawRectanglePixels(pen, toPx(r))
	}
	if !u.baseNorm.Empty() {
		draw(u.baseNorm, walk.RGB(60, 220, 120))
		if f, _ := walk.NewFont("Segoe UI", 8, walk.FontBold); f != nil {
			r := toPx(u.baseNorm)
			lr := walk.Rectangle{X: r.X, Y: r.Y - 16, Width: 120, Height: 15}
			if lr.Y < ir.Y {
				lr.Y = r.Y + r.Height + 2
			}
			c.DrawTextPixels("film base", f, walk.RGB(60, 220, 120), lr, walk.TextLeft|walk.TextSingleLine)
			f.Dispose()
		}
	}
	if u.dragging {
		draw(u.dragA, walk.RGB(255, 170, 40))
	} else if u.cfg.AreaMode == 1 && !u.roi.Empty() {
		draw(u.roi, walk.RGB(255, 170, 40))
	}
	return nil
}

func (u *HDRUI) norm(x, y int) (float64, float64) {
	ir := u.imageRect()
	fx := float64(x-ir.X) / float64(ir.Width)
	fy := float64(y-ir.Y) / float64(ir.Height)
	clamp := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	return clamp(fx), clamp(fy)
}

func (u *HDRUI) mouseDown(x, y int, b walk.MouseButton) {
	if b != walk.LeftButton || u.bmp == nil || u.busy {
		return
	}
	fx, fy := u.norm(x, y)
	u.dragging = true
	u.dragA = hdrscan.Rect{X0: fx, Y0: fy, X1: fx, Y1: fy}
}

var dragStart [2]float64

func (u *HDRUI) mouseMove(x, y int, b walk.MouseButton) {
	if !u.dragging {
		return
	}
	fx, fy := u.norm(x, y)
	if u.dragA.X1 == u.dragA.X0 && u.dragA.Y1 == u.dragA.Y0 {
		dragStart = [2]float64{u.dragA.X0, u.dragA.Y0}
	}
	ax, ay := dragStart[0], dragStart[1]
	u.dragA = hdrscan.Rect{X0: min(ax, fx), Y0: min(ay, fy), X1: max(ax, fx), Y1: max(ay, fy)}
	u.invalidateViews()
}

func (u *HDRUI) mouseUp(x, y int, b walk.MouseButton) {
	if !u.dragging {
		return
	}
	u.dragging = false
	if u.dragA.Empty() {
		u.invalidateViews()
		return
	}
	u.roi = u.dragA
	u.cfg.AreaMode = 1
	u.areaCB.SetCurrentIndex(1)
	u.save()
	if l := u.light; l != nil {
		l.cfg.Area = toLightRect(u.dragA)
		l.cfg.AreaMode = 1
		l.cfg.AreaPicked = true
		if l.areaCB != nil && l.areaCB.CurrentIndex() != 1 {
			l.areaCB.SetCurrentIndex(1)
		}
		l.saveCfg()
		if l.view != nil {
			l.view.Invalidate()
		}
	}
	setStatus(u.baseLbl, "Base rectangle set. Press Meter base.")
	u.invalidateViews()
}

// ---------- meter / capture ----------

// meter is the Meter base button (a manual lock supersedes a light handoff's
// label, the handoff's values stay the trichrome scan values).
func (u *HDRUI) meter() { u.meterSrc(nil) }

// meterSrc meters / locks frame 0; ho != nil is a leveling / Apply stored
// handoff (fixed lock at its exposure, no dialogs).
func (u *HDRUI) meterSrc(ho *baseHandoff) {
	if u.dev == nil || u.busy {
		return
	}
	if ho == nil && u.cfg.AreaMode == 1 && u.roi.Empty() {
		walk.MsgBox(u.win(), "Meter base", "Base area is set to \"Rectangle\": drag a rectangle over the clear film base on the image first (start live view to see the frame), or choose Auto.", walk.MsgBoxIconInformation)
		return
	}
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
	fixed, iso, leak := u.fixed(), u.fixedISO(), u.leak()
	src := ""
	if ho != nil {
		fixed, iso, src = true, ho.ISO, "from "+ho.Source
		u.log.Printf("==== Frame 0 lock from the light handoff (%s): fixed ISO %d, %s - no Meter base needed ====", ho.Source, ho.ISO, camera.ShutterLabel(ho.Shutter))
	}
	var lockLight *[3]int
	if l := u.light; l != nil && l.cl != nil {
		if cs := l.cl.CachedState(); cs != nil {
			v := cs.Value
			lockLight = &v
		}
	}
	ent, tol := u.stock(), u.tol()
	s.Fixed = u.fixedShutter()
	if ho != nil {
		s.Fixed = ho.Shutter
	}
	if fixed {
		setStatus(u.expLbl, fmt.Sprintf("Frame 0 fixed: ISO %d, %s ...", iso, camera.ShutterLabel(s.Fixed)))
	} else {
		setStatus(u.expLbl, "Metering ...")
	}
	u.lastCap = nil // a new metering supersedes the last capture's base
	u.worker.do(func() {
		var e camera.Exposure
		var r hdrscan.Reading
		var err error
		var warns []string
		var cmp string
		cmpOK := true
		if fixed {
			e, r, warns, err = s.LockFixed(iso)
			if err == nil {
				for _, w := range warns {
					u.log.Printf("Frame 0 fixed: WARNING: %s", w)
				}
				if ent != nil {
					if b1, _, berr := s.ProbeBase(leak); berr != nil {
						cmp, cmpOK = ent.Name+": no base to compare ("+berr.Error()+")", false
					} else {
						cmp, cmpOK = compareText(ent, b1, e, tol)
					}
					u.log.Printf("%s", cmp)
				}
			}
		} else {
			e, r, err = s.Meter()
		}
		var st camera.Exposure
		var fn float64
		var sterr error = errors.New("no status")
		if sr, ok := s.Dev.(camera.StatusReader); ok {
			st, fn, sterr = sr.Status()
		}
		var prev image.Image
		if s.LastProbe != nil {
			prev = hdrscan.Preview(s.LastProbe)
		}
		u.ui(func() {
			u.busy = false
			u.lockFn = 0
			if sterr == nil {
				u.showCamera(st, fn)
				u.lockFn = fn
			}
			if prev != nil {
				u.setImage(prev, "Last metering probe (RAW) - live view resumes in 4 s")
			}
			if r.Max > 0 {
				u.baseNorm = r.Norm
				u.showBase(r)
			}
			// keep the probe on screen for a moment so the base box can be seen
			time.AfterFunc(4*time.Second, func() { u.ui(u.resumeLiveView) })
			if err != nil {
				u.clearLocked()
				setStatus(u.expLbl, "NOT LOCKED")
				setStatus(u.baseLbl, "Metering failed: "+err.Error())
				u.say("Metering failed: " + err.Error())
				if ho != nil {
					u.say("Frame 0 lock from " + ho.Source + " failed - press Meter base, or level / load a preset again.")
				}
				if errors.Is(err, camera.ErrNotManual) {
					u.notManualMsg(err)
				}
				u.updateButtons()
				return
			}
			u.baseNorm = r.Norm
			u.lockSrc, u.lockLight = src, lockLight
			mode := "set over USB"
			if s.DialMode {
				mode = "set on the camera dials"
			}
			if src != "" {
				mode += ", " + src + " - ready to scan, no Meter base needed"
				u.say(fmt.Sprintf("Frame 0 LOCKED from %s: ISO %d, %s, base %.2f FS, R/G/B %.3f / %.3f / %.3f - ready to scan.",
					ho.Source, e.ISO, camera.ShutterLabel(e.Shutter), r.Max, r.RGB[0], r.RGB[1], r.RGB[2]))
			}
			if fixed {
				setStatus(u.expLbl, fmt.Sprintf("LOCKED  (fixed ISO %d, %s, %s)", e.ISO, camera.ShutterLabel(e.Shutter), mode))
				extra := ""
				for _, w := range warns {
					extra += "\nWARNING: " + w
				}
				u.lockBase = u.baseLbl.Text() + extra
				if cmp != "" {
					extra += "\n" + cmp
				} else if ent == nil {
					extra += "\nFilm stock: None (no DB check)"
				}
				setStatus(u.baseLbl, u.baseLbl.Text()+extra)
				if (len(warns) > 0 || !cmpOK) && ho == nil {
					walk.MsgBox(u.win(), "Frame 0", strings.TrimPrefix(extra, "\n"), walk.MsgBoxIconWarning)
				}
			} else {
				setStatus(u.expLbl, fmt.Sprintf("LOCKED  (%s, ISO stays 160)", mode))
			}
			u.f0ISO.SetText(camera.ISOLabel(e.ISO))
			u.f0Sh.SetText(camera.ShutterLabel(e.Shutter))
			u.showBracket()
			u.updateButtons()
		})
	})
}

// showBase fills the base readout and says where the base was found.
func (u *HDRUI) showBase(r hdrscan.Reading) {
	u.baseLv.SetText(fmt.Sprintf("%.2f FS", r.Max))
	u.baseR.SetText(fmt.Sprintf("%.3f", r.RGB[0]))
	u.baseG.SetText(fmt.Sprintf("%.3f", r.RGB[1]))
	u.baseB.SetText(fmt.Sprintf("%.3f", r.RGB[2]))
	where := "your rectangle"
	if r.Auto {
		where = "auto: " + r.Where
	}
	// Explicit short lines: the label does not get taller for wrapped text.
	t := "Base region (green box), " + where
	if r.Auto {
		t = "Base region (green box), auto:\n" + r.Where
		if i := strings.Index(t, ", median of"); i > 0 {
			t = t[:i] // cells and region counts are in the log
		}
	}
	if r.Cells > 0 && r.Lo > 0 {
		t += fmt.Sprintf("\nMedian over the whole base; %s min/max %.3f/%.3f (%.2f EV)",
			[]string{"R", "G", "B"}[r.Channel], r.Lo, r.Hi, math.Log2(r.Hi/r.Lo))
	} else {
		t += fmt.Sprintf("\n%dx%d px at x %d, y %d", r.Area.Dx(), r.Area.Dy(), r.Area.Min.X, r.Area.Min.Y)
	}
	if r.Warn != "" {
		t += "\nWARNING: " + r.Warn
	}
	setStatus(u.baseLbl, t)
}

// testShot takes one picture with the current camera settings, downloads it,
// deletes it from the camera and measures it - every step is logged.
func (u *HDRUI) testShot() {
	if u.dev == nil || u.busy {
		return
	}
	dir := u.outDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		walk.MsgBox(u.win(), "Test shot", err.Error(), walk.MsgBoxIconError)
		return
	}
	u.pauseLiveView()
	u.busy = true
	u.updateButtons()
	d := u.dev
	u.worker.do(func() {
		u.log.Printf("==== Test shot (%s) ====", appVersion)
		if fu, ok := d.(*camera.Fuji); ok {
			fu.SetVerbose(true)
			defer fu.SetVerbose(false)
		}
		if e, err := d.Exposure(); err == nil {
			u.log.Printf("Test shot: camera reports %s", e)
		}
		t0 := time.Now()
		path, err := d.Capture(dir, "testshot_")
		var prev image.Image
		var r hdrscan.Reading
		var merr error
		if err == nil {
			u.log.Printf("Test shot: downloaded %s in %.1f s", path, time.Since(t0).Seconds())
			if f, rerr := rawconv.ReadRawFrame(path); rerr != nil {
				u.log.Printf("Test shot: not a readable RAW (%v) - set the camera to RAW or RAW+JPEG", rerr)
			} else {
				prev = hdrscan.Preview(f)
				r, merr = hdrscan.MeasureBase(f, hdrscan.Rect{})
				if merr != nil {
					u.log.Printf("Test shot: base detection: %v", merr)
				} else {
					u.log.Printf("Test shot: %s", r)
					if r.Warn != "" {
						u.log.Printf("Test shot: note: %s", r.Warn)
					}
				}
			}
		} else {
			u.log.Printf("Test shot FAILED: %v", err)
		}
		u.log.Printf("==== end of test shot ====")
		u.ui(func() {
			u.busy = false
			u.updateButtons()
			if prev != nil {
				u.setImage(prev, "Test shot (RAW) - live view resumes in 4 s")
				if merr == nil {
					u.baseNorm = r.Norm
					u.showBase(r)
				}
			}
			time.AfterFunc(4*time.Second, func() { u.ui(u.resumeLiveView) })
			if err != nil {
				walk.MsgBox(u.win(), "Test shot", "The test shot failed:\n\n"+err.Error()+"\n\nThe full sequence is in "+u.log.Path(), walk.MsgBoxIconWarning)
			}
		})
	})
}

func (u *HDRUI) outDir() string {
	d := strings.TrimSpace(u.outEd.Text())
	if d == "" {
		d = defaultOutDir()
		u.outEd.SetText(d)
	}
	return d
}

func (u *HDRUI) capture() {
	if u.dev == nil || u.busy || u.sess == nil || u.sess.Locked == nil {
		return
	}
	u.save()
	dir, err := u.rollDir()
	if err != nil {
		walk.MsgBox(u.win(), "Output folder", err.Error(), walk.MsgBoxIconError)
		return
	}
	u.pauseLiveView()
	u.busy = true
	u.updateButtons()
	s := u.sess
	opts := u.processOptions(dir)
	opts.BaseArea = s.Reading.Area
	if !s.Reading.Region.Rect.Empty() {
		reg := s.Reading.Region
		opts.BaseRegion = &reg
	}
	evs := u.evs()
	opts.MeteredBase = s.Reading.RGB
	u.capExp, u.capReading = *s.Locked, s.Reading
	e0 := *s.Locked
	roll, last := u.cfg.Roll, u.cfg.Counter
	u.resTE.SetText(fmt.Sprintf("Capturing %s ...", bracketItems[u.cfg.Bracket]))
	ctx, cancel := context.WithCancel(context.Background())
	u.scanCancel = cancel
	opts.Ctx = ctx
	u.scanBefore = u.lightState()
	u.updateButtons()
	u.worker.do(func() {
		var num int
		shot, err := s.CaptureBracketEVCtx(ctx, dir, evs, func(t time.Time) string {
			b, n := hdrscan.NextShot(dir, roll, t, last)
			num = n
			return b
		})
		var raws []string
		if shot != nil {
			raws = shot.Raws
			opts.Merge.Exposure = shot.Exposures
			for i, r := range raws {
				if iso, sh, xerr := rawconv.ReadExposure(r); xerr == nil {
					u.log.Printf("RAF EXIF %s: ISO %.0f, %.5f s", filepath.Base(r), iso, sh)
				}
				if i == 0 {
					if c := hdrscan.EXIFCheck(r, e0); c != "" {
						u.log.Printf("Frame 0 %s", c)
					}
				}
			}
		}
		u.ui(func() {
			if num > 0 {
				u.cfg.Counter = num
				u.save()
			}
			u.busy = false
			u.resumeLiveView()
			if err != nil && errors.Is(err, context.Canceled) {
				u.scanStopped(raws, "capture")
				return
			}
			if err != nil {
				u.endScan()
				u.resTE.SetText("Capture failed: " + err.Error())
				u.say("Capture failed: " + err.Error())
				if errors.Is(err, camera.ErrNotManual) {
					u.notManualMsg(err)
				}
				u.updateButtons()
				return
			}
			var names []string
			for _, r := range raws {
				names = append(names, filepath.Base(r))
			}
			u.say("Downloaded " + strings.Join(names, ", ") + " to " + dir)
			u.process(raws, opts, nil, u.resTE)
			u.updateButtons()
		})
	})
}

// evs are the EV steps of the chosen bracket (0, 2[, 3 or 4]).
func (u *HDRUI) evs() []int {
	if u.cfg.Bracket >= 0 && u.cfg.Bracket < len(bracketEVs) {
		return bracketEVs[u.cfg.Bracket]
	}
	return bracketEVs[bracketDefault]
}

// showBracket updates the capture button and the brighter-frame shutters
// ("+2: 1/20,  +3: 1/10") for the chosen bracket and the locked frame 0.
func (u *HDRUI) showBracket() {
	evs := u.evs()
	if u.capBtn != nil {
		u.capBtn.SetText("Capture HDR  (" + evText(evs) + ")")
	}
	if u.p2Sh == nil {
		return
	}
	if u.sess == nil || u.sess.Locked == nil {
		u.p2Sh.SetText("")
		return
	}
	e0 := *u.sess.Locked
	var parts []string
	for _, ev := range evs[1:] {
		e, err := hdrscan.PlusEV(e0, ev)
		if err != nil {
			parts = append(parts, fmt.Sprintf("+%d: too slow", ev))
			continue
		}
		parts = append(parts, fmt.Sprintf("+%d: %s", ev, camera.ShutterLabel(e.Shutter)))
	}
	u.p2Sh.SetText(strings.Join(parts, ",  "))
}

func (u *HDRUI) processOptions(dir string) hdrscan.ProcessOptions {
	mo := hdrscan.DefaultOptions()
	mo.Align = alignModes[max(0, u.alignCB.CurrentIndex())]
	mo.Method = mergeModes[max(0, u.mergeCB.CurrentIndex())]
	return hdrscan.ProcessOptions{Merge: mo, OutDir: dir, KeepTIFF: u.cfg.KeepTIFF, DeleteRAW: u.cfg.DeleteRAW, LZW: u.cfg.LZW, Software: appVersion, Metadata: u.scanMetadata(), LeakPct: u.leak(), Dust: u.cfg.Dust}
}

// process converts + merges in the background (camera stays usable).
func (u *HDRUI) process(srcs []string, o hdrscan.ProcessOptions, lbl *walk.Label, te *walk.TextEdit) {
	u.processing = true
	u.updateButtons()
	if u.mBtn != nil {
		u.mBtn.SetEnabled(false)
	}
	u.pb.SetMarqueeMode(true)
	set := func(s string) {
		if lbl != nil {
			lbl.SetText(s)
		}
		if te != nil {
			te.SetText(strings.ReplaceAll(s, "\n", "\r\n"))
		}
	}
	o.Progress = func(m string) { u.ui(func() { set(m) }) }
	go func() {
		res, err := hdrscan.ProcessN(srcs, o)
		u.ui(func() {
			u.processing = false
			u.pb.SetMarqueeMode(false)
			u.pb.SetValue(0)
			if u.mBtn != nil {
				u.mBtn.SetEnabled(true)
			}
			u.updateButtons()
			if err != nil && o.Ctx != nil && errors.Is(err, context.Canceled) {
				u.scanStopped(srcs, "merge")
				return
			}
			if o.Ctx != nil {
				u.endScan()
			}
			if err != nil {
				set("Merge failed: " + err.Error())
				u.say("Merge failed: " + err.Error())
				return
			}
			msg := fmt.Sprintf("Saved %s  (%d frames, %.1f s)", res.HDR, res.Frames, res.Elapsed.Seconds())
			msg += "\n" + levelMetadataSummary(o.Metadata)
			for _, st := range res.StatsN {
				if st.Dropped {
					msg += fmt.Sprintf("\nWARNING: the %s frame could not be used - this is a %d-frame HDR.", st.Label, res.Frames)
				} else if st.Warning != "" {
					msg += fmt.Sprintf("\nNote: %s ratio taken from the exposure times (the frame is too clipped to measure).", st.Label)
				}
			}
			for _, st := range res.StatsN {
				msg += "\n" + st.String()
			}
			if res.OrderNote != "" {
				msg += "\nNOTE: " + res.OrderNote + "."
			}
			msg += "\nExposure times: " + res.ExposureSrc
			msg += "\n" + res.BaseCheck()
			if res.LightNote != "" {
				msg += "\nWARNING: " + res.LightNote
			}
			if ds := res.DustStats; ds.Level != hdrscan.DustOff {
				msg += fmt.Sprintf("\nDust removal (%s) applied in %s: %d spots (%d hairs), %d fibres, %d px changed, %d left alone, %.1f s.",
					hdrscan.DustName(ds.Level), filepath.Base(res.HDR), ds.Spots, ds.Hairs, ds.Fibres, ds.Pixels, ds.Rejected, ds.Elapsed.Seconds())
			}
			if len(res.TIFFs) > 0 {
				msg += "\nFrame TIFFs kept: " + strings.Join(res.TIFFs, ", ")
			}
			if te == u.mRes {
				u.lastMerge = res
			} else {
				u.lastCap = res
				u.setLastF0(res)
				if f := u.lastF0; f != nil && f.Pure {
					if t, _ := compareText(u.stock(), f.RGB, f.Exp, u.tol()); t != "" && u.fixed() {
						msg += "\n" + t
					}
				}
			}
			u.updateButtons()
			set(msg)
			u.say(strings.ReplaceAll(msg, "\n", " | "))
		})
	}()
}

// DropMerge fills the manual merge fields from dropped files.
func (u *HDRUI) DropFiles(files []string) {
	if u.subTabs == nil || u.subTabs.CurrentIndex() != 1 {
		return
	}
	var ok []string
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		if ext == ".raf" || ext == ".tif" || ext == ".tiff" || ext == ".dng" || ext == ".nef" || ext == ".cr3" || ext == ".arw" {
			ok = append(ok, f)
		}
	}
	sort.Strings(ok)
	eds := []*walk.LineEdit{u.m0Ed, u.m2Ed, u.m4Ed}
	if len(ok) >= 2 {
		for i, e := range eds {
			if i < len(ok) {
				e.SetText(ok[i])
			} else {
				e.SetText("")
			}
		}
	} else if len(ok) == 1 {
		for _, e := range eds {
			if e.Text() == "" {
				e.SetText(ok[0])
				break
			}
		}
	}
}

func (u *HDRUI) mergeFiles() {
	srcs := []string{strings.TrimSpace(u.m0Ed.Text()), strings.TrimSpace(u.m2Ed.Text())}
	if f4 := strings.TrimSpace(u.m4Ed.Text()); f4 != "" {
		srcs = append(srcs, f4)
	}
	for i, f := range srcs {
		if _, err := os.Stat(f); err != nil {
			msg := "Choose frame 0 and a brighter frame (+2 or +3 EV) first; the third file is optional."
			if i == 2 {
				msg = "The third file does not exist: " + f + "\nClear the field to merge two files."
			}
			walk.MsgBox(u.win(), "Merge files", msg, walk.MsgBoxIconInformation)
			return
		}
		for _, g := range srcs[:i] {
			if strings.EqualFold(f, g) {
				walk.MsgBox(u.win(), "Merge files", "The same file is chosen twice: "+filepath.Base(f), walk.MsgBoxIconWarning)
				return
			}
		}
	}
	mo := hdrscan.DefaultOptions()
	mo.Align = alignModes[max(0, u.mAlignCB.CurrentIndex())]
	mo.Method = mergeModes[max(0, u.mMergeCB.CurrentIndex())]
	o := hdrscan.ProcessOptions{Merge: mo, LZW: u.mCompCB.CurrentIndex() == 1, Software: appVersion, LeakPct: u.leak(), Dust: max(0, u.mDustCB.CurrentIndex())}
	u.process(srcs, o, nil, u.mRes)
}

// ---------- page ----------

// HDRPages (v0.83) are the HDR sub-pages, spread over the modules
// Camera & Light (Camera), HDR Scan (White light scan) and Crop & Merge
// (Merge files, Remove hairs, Batch RAW to TIFF). One HDRUI state backs all.
type HDRPages struct{ Camera, White, Merge, Hairs, Batch TabPage }

// Pages builds the HDR sub-pages.
func (u *HDRUI) Pages(batch TabPage) HDRPages {
	u.cfg = loadHDRConfig()
	u.log = camera.NewLogger(configDir())
	u.log.OnLine = func(s string) { u.ui(func() { u.appendLog(s) }) }
	u.worker = newCamWorker()
	u.worker.onPanic = func(r any, st string) { u.reportPanic("camera job", r, st) }
	u.loadDB()
	mw := u.win
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	save := func() { u.save() }
	bold := Font{Family: "Segoe UI", PointSize: 9, Bold: true}
	big := Font{Family: "Consolas", PointSize: 12, Bold: true}
	grey := walk.RGB(110, 110, 110)

	camGroups := []Widget{
		Card{
			Title:  "Camera",
			Icon:   iconCamera,
			Layout: VBox{Spacing: 3, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 4}},
			Children: []Widget{
				Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
					ComboBox{AssignTo: &u.devCB, Model: []string{simItem}, CurrentIndex: 0, StretchFactor: 1},
					PushButton{Text: "Refresh", MaxSize: Size{Width: 70}, OnClicked: u.refreshDevices},
					PushButton{Text: "Chart calibration...", OnClicked: u.calibrateChart, ToolTipText: calTip},
				}},
				Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
					PushButton{AssignTo: &u.connBtn, Text: "Connect", MinSize: Size{Width: 100}, OnClicked: u.connect},
					PushButton{AssignTo: &u.diagBtn, Text: "Camera diagnostics", OnClicked: u.diagnostics},
					PushButton{AssignTo: &u.testBtn, Text: "Test shot", Enabled: false, OnClicked: u.testShot,
						ToolTipText: "One capture with the current settings: download to the Save-to folder (testshot_*), delete from the camera, measure the base. Every step goes to the log."},
					HSpacer{},
				}},
				Label{AssignTo: &u.camLbl, Text: "Not connected", Font: bold},
			},
		},
		Card{
			Title:  "Meter the clear film base",
			Icon:   iconMeter,
			Layout: Grid{Columns: 2, Spacing: 3, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 4}},
			Children: []Widget{
				Label{Text: "Base level", MinSize: Size{Width: 92}},
				ComboBox{AssignTo: &u.targetCB, Model: targetItems, CurrentIndex: u.cfg.Target, OnCurrentIndexChanged: save,
					ToolTipText: "Median of the brightest channel over the clear base in frame 0. Locks from 1/3 EV below the target up to 1/6 EV above it, never above 0.87 (and never with the brightest part of the base near clipping). 0.50-0.60 leave more headroom."},
				Label{Text: "Base area", MinSize: Size{Width: 92}},
				ComboBox{AssignTo: &u.areaCB, Model: []string{"Auto: find the base (rebate, gaps, sprocket area)", "Rectangle drawn on the image"}, CurrentIndex: u.cfg.AreaMode, OnCurrentIndexChanged: save},
				Label{Text: "Slowest shutter", MinSize: Size{Width: 92}},
				ComboBox{AssignTo: &u.slowCB, Model: slowestItems, CurrentIndex: u.cfg.Slowest, OnCurrentIndexChanged: save,
					ToolTipText: "Auto frame 0: ISO is fixed at 160. If the base needs a slower frame-0 shutter than this, nothing is locked: open the aperture or brighten the light."},
				Label{Text: "Frame 0", MinSize: Size{Width: 92}},
				Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
					ComboBox{AssignTo: &u.f0CB, Model: frame0Items, CurrentIndex: u.cfg.Frame0, StretchFactor: 3, OnCurrentIndexChanged: func() { u.save(); u.updateButtons() },
						ToolTipText: "Auto: meter the shutter at ISO 160 (as before).\nFixed: no metering search - one probe at the chosen ISO and base shutter (1/60 s or 1/125 s) locks frame 0; the brighter frames change the shutter only.\nWarns when the base clips (> 0.95) or is very low; compares with the film stock."},
					Label{Text: "ISO", MinSize: Size{Width: 92}},
					ComboBox{AssignTo: &u.isoCB, Model: isoItems, CurrentIndex: u.cfg.ISO, StretchFactor: 1, OnCurrentIndexChanged: u.isoChanged,
						ToolTipText: "ISO of a fixed frame 0 (Fixed only; Auto always uses ISO 160).\nLinked with the leveling ISO on the Light page (v0.82)."},
				}},
				Label{Text: "Base shutter", MinSize: Size{Width: 92}},
				ComboBox{AssignTo: &u.shutCB, Model: fixedShutterItems, CurrentIndex: u.cfg.Shutter, OnCurrentIndexChanged: u.shutterChanged,
					ToolTipText: fixedShutterTip},
				Label{Text: "Film stock", MinSize: Size{Width: 92}},
				Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
					ComboBox{AssignTo: &u.stockCB, Model: u.stockItems(), CurrentIndex: u.stockIndex(u.cfg.Stock), StretchFactor: 3, OnCurrentIndexChanged: save,
						ToolTipText: "Film-base DB (film_base_db.json next to the settings). With Fixed frame 0, frame 0's base is compared with this stock\n(per channel, in EV) and an ISO or shutter mismatch is flagged."},
					Label{Text: "Tol."},
					ComboBox{AssignTo: &u.tolCB, Model: stockTolItems, CurrentIndex: u.cfg.StockTol, StretchFactor: 1, OnCurrentIndexChanged: save,
						ToolTipText: "Allowed deviation from the stored base per channel."},
				}},
				Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
					PushButton{AssignTo: &u.recBtn, Text: "Record base", Enabled: false, OnClicked: u.recordBase,
						ToolTipText: "Shoot a standalone fixed frame 0 at ISO 250 and the base shutter (set f/4 on the lens) of the clear base,\nkeep it as <roll>_..._BASE.RAF and save it as a new film stock or update one."},
					PushButton{AssignTo: &u.saveDBBtn, Text: "Save base to DB", Enabled: false, OnClicked: u.saveBaseToDB,
						ToolTipText: "Save the locked frame 0's base (metering probe) as a new film stock or update one."},
					PushButton{AssignTo: &u.saveF0Btn, Text: "Save last frame 0 to DB", Enabled: false, OnClicked: u.saveLastFrame0,
						ToolTipText: "Save the base of the last captured frame 0 without reshooting (only when that frame 0 was a clean, unclipped base)."},
					PushButton{AssignTo: &u.manageBtn, Text: "Manage", OnClicked: u.manageDB, ToolTipText: "Rename or delete film stocks."},
				}},
				PushButton{AssignTo: &u.meterBtn, ColumnSpan: 2, Text: "Meter base", Enabled: false, MinSize: Size{Height: 26}, Font: bold, OnClicked: u.meter},
				Composite{ColumnSpan: 2, Layout: Grid{Columns: 4, MarginsZero: true, Spacing: 3}, Children: []Widget{
					Label{Text: "Camera now", Font: bold},
					LineEdit{AssignTo: &u.camISO, ReadOnly: true, Font: big, MinSize: Size{Width: 92, Height: 26}},
					LineEdit{AssignTo: &u.camSh, ReadOnly: true, Font: big, MinSize: Size{Width: 92, Height: 26}},
					LineEdit{AssignTo: &u.camF, ReadOnly: true, Font: big, MinSize: Size{Width: 92, Height: 26}},
					Label{Text: "Frame 0", Font: bold, TextColor: walk.RGB(30, 110, 60)},
					LineEdit{AssignTo: &u.f0ISO, ReadOnly: true, Font: big, TextColor: walk.RGB(30, 110, 60), Text: "not locked"},
					LineEdit{AssignTo: &u.f0Sh, ReadOnly: true, Font: big, TextColor: walk.RGB(30, 110, 60)},
					Label{Text: "locked", TextColor: grey},
					Label{Text: "Brighter", Font: bold, ToolTipText: "Shutters of the brighter frames: shutter only, ISO 160, aperture unchanged."},
					LineEdit{AssignTo: &u.p2Sh, ReadOnly: true, Font: big, ColumnSpan: 3, ToolTipText: "Shutters of the brighter frames: shutter only, ISO 160, aperture unchanged."},
					Label{Text: "Base level", Font: bold},
					LineEdit{AssignTo: &u.baseLv, ReadOnly: true, Font: big},
					PushButton{AssignTo: &u.sendBtn, ColumnSpan: 2, Text: "Send base to Develop", Enabled: false, OnClicked: u.sendBase,
						ToolTipText: "Send the saved after-profile leveling base when the metadata option is on. Otherwise measure the locked probe explicitly."},
					Label{Text: "Base R / G / B", Font: bold},
					LineEdit{AssignTo: &u.baseR, ReadOnly: true, Font: big, TextColor: walk.RGB(200, 50, 50)},
					LineEdit{AssignTo: &u.baseG, ReadOnly: true, Font: big, TextColor: walk.RGB(40, 150, 60)},
					LineEdit{AssignTo: &u.baseB, ReadOnly: true, Font: big, TextColor: walk.RGB(50, 90, 200)},
				}},
				CheckBox{AssignTo: &u.levelMetaCB, ColumnSpan: 2, Text: "Film base for metadata: Last leveling (after profile)", OnClicked: func() { u.setUseLastLeveling(u.levelMetaCB.Checked()) }, ToolTipText: levelMetadataTip},
				Label{AssignTo: &u.levelMetaLbl, ColumnSpan: 2, Text: "Last leveling: unavailable"},
				Label{AssignTo: &u.expLbl, ColumnSpan: 2, Text: "", Font: bold, TextColor: walk.RGB(30, 110, 60)},
				Label{AssignTo: &u.baseLbl, ColumnSpan: 2, Text: "Frame 0 Auto: ISO 160, 1/60 s or faster. Fixed: chosen ISO and base shutter.\nAperture: the f-number the camera reports is stored with base records.", MinSize: Size{Height: 30}},
			},
		},
	}
	scanGroup := Card{
		Title: "Capture and merge",
		Icon:  iconHDR,
		OnReset: func() {
			u.brCB.SetCurrentIndex(bracketDefault)
			u.mergeCB.SetCurrentIndex(mergeDefault)
			u.compCB.SetCurrentIndex(1)
			u.dustCB.SetCurrentIndex(0)
			u.save()
			u.say("Capture and merge settings reset to defaults (bracket, merge, LZW, dust off).")
		},
		ResetTip: "Reset bracket, merge, compression and dust removal to the defaults (Save to and roll are kept).",
		Layout:   Grid{Columns: 2, Spacing: 3, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 4}},
		Children: []Widget{
			Label{Text: "Save to", MinSize: Size{Width: 96}},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				LineEdit{AssignTo: &u.outEd, Text: u.cfg.OutDir, OnEditingFinished: save},
				PushButton{Text: "...", MaxSize: Size{Width: 30}, OnClicked: func() {
					dlg := new(walk.FileDialog)
					dlg.Title = "Folder for the HDR scans"
					dlg.FilePath = u.outEd.Text()
					if ok, _ := dlg.ShowBrowseFolder(mw()); ok {
						u.outEd.SetText(dlg.FilePath)
						u.save()
					}
				}},
			}},
			Label{Text: "Roll name", MinSize: Size{Width: 96}},
			LineEdit{AssignTo: &u.rollEd, Text: u.cfg.Roll, CueBanner: "roll / folder name, e.g. Portra400_Kyoto",
				ToolTipText: "Captures go into <Save to>\\<roll name>\\ (created if missing; an existing folder is continued, nothing is overwritten).\nThe roll name is also the file-name prefix: <roll>_<date>_<time>_<NNNN>_HDR.tiff and _0EV/_+3EV/... .RAF.\nEmpty: folder <YYYYMMDD>_roll (date of the first capture of this session), no prefix. Characters not allowed in file names become _.",
				OnEditingFinished: func() {
					r := hdrscan.SanitizeRoll(u.rollEd.Text())
					if r != u.rollEd.Text() {
						u.rollEd.SetText(r)
					}
					u.save()
				}},
			Label{Text: "Bracket", MinSize: Size{Width: 96}},
			ComboBox{AssignTo: &u.brCB, Model: bracketItems, CurrentIndex: u.cfg.Bracket, OnCurrentIndexChanged: func() { u.save(); u.showBracket() },
				ToolTipText: "0 / +2 EV (default): frame 0 + one frame 2 EV brighter (4x frame 0's shutter).\n0 / +2 / +3 EV: adds a third frame 3 EV brighter (8x) for dense areas.\nBoth are merged with the Merge method below (Weighted by default). Shutter only, frame 0's ISO is kept."},
			Label{Text: "Frame TIFFs", MinSize: Size{Width: 96}},
			ComboBox{AssignTo: &u.tiffCB, Model: []string{"Delete (keep only the HDR)", "Keep all frame TIFFs"}, CurrentIndex: b2i(u.cfg.KeepTIFF), OnCurrentIndexChanged: save},
			Label{Text: "RAF files", MinSize: Size{Width: 96}},
			ComboBox{AssignTo: &u.rawCB, Model: []string{"Keep all RAF files", "Delete after a good merge"}, CurrentIndex: b2i(u.cfg.DeleteRAW), OnCurrentIndexChanged: save},
			Label{Text: "Alignment", MinSize: Size{Width: 96}},
			ComboBox{AssignTo: &u.alignCB, Model: alignItems, CurrentIndex: u.cfg.Align, OnCurrentIndexChanged: save},
			Label{Text: "Merge", MinSize: Size{Width: 96}},
			ComboBox{AssignTo: &u.mergeCB, Model: mergeItems, CurrentIndex: u.cfg.Merge, OnCurrentIndexChanged: save, ToolTipText: mergeTip},
			Label{Text: "Compression", MinSize: Size{Width: 96}},
			ComboBox{AssignTo: &u.compCB, Model: []string{"None (fastest)", "LZW + predictor (lossless)"}, CurrentIndex: b2i(u.cfg.LZW), OnCurrentIndexChanged: save},
			Label{Text: "Dust removal", MinSize: Size{Width: 96}},
			ComboBox{AssignTo: &u.dustCB, Model: dustItems, CurrentIndex: u.cfg.Dust, OnCurrentIndexChanged: save,
				ToolTipText: "Removes dust specks and hairs (also semi-transparent ones) from the merged HDR, judged against the local grain and image colour. Light: only clear outliers, leaves fine detail alone. Strong: also fainter dust, may touch some texture. The film base and rebate are left alone.\nApplied into the one output <name>_HDR.tiff (no uncleaned copy is kept). Off: the HDR is written exactly as before."},
			Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				PushButton{AssignTo: &u.capBtn, Text: "Capture HDR", Enabled: false, MinSize: Size{Height: 30}, StretchFactor: 2,
					Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}, OnClicked: u.capture},
				PushButton{AssignTo: &u.stopBtn, Text: "Stop", Enabled: false, MinSize: Size{Height: 30}, StretchFactor: 1, OnClicked: u.stopScan,
					ToolTipText: "Stop the scan in progress (capture or merge): the interrupted frame's partial RAFs/TIFFs are deleted, the light is restored to its pre-scan state. Earlier frames are kept."},
			}},
			ProgressBar{AssignTo: &u.pb, ColumnSpan: 2, MaxSize: Size{Height: 8}},
			TextEdit{AssignTo: &u.resTE, ColumnSpan: 2, ReadOnly: true, VScroll: true, MinSize: Size{Height: 44}, Text: "Output: <Save to>\\<roll>\\[roll_]YYYYMMDD_HHMMSS_NNNN_HDR.tiff (time of frame 0, NNNN counts up and never repeats in the folder), RAFs _0EV/_+3EV/..., Record base _BASE.RAF. One linear 16-bit TIFF per frame (dust-cleaned if Dust removal is on), frame 0 scale."},
		},
	}
	capture := TabPage{
		Title:  " Camera ",
		Layout: HBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 8},
		Children: []Widget{
			Composite{StretchFactor: 3, Layout: VBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				u.liveView(0, grey),
				Label{Text: "Drag on the image to mark the clear film base (orange). Green box = base region used by the last metering.\nCamera: CONNECTION MODE = USB TETHER SHOOTING FIXED, close X Acquire.", TextColor: grey},
				TextEdit{AssignTo: &u.logTE, ReadOnly: true, VScroll: true, StretchFactor: 1, MinSize: Size{Height: 90}},
			}},
			Composite{StretchFactor: 2, MinSize: Size{Width: 450}, MaxSize: Size{Width: 480}, Layout: VBox{MarginsZero: true, Spacing: 3},
				Children: append(camGroups, VSpacer{})},
		},
	}
	white := TabPage{
		Title:  " White light scan ",
		Layout: HBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 8},
		Children: []Widget{
			Composite{StretchFactor: 3, Layout: VBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				u.liveView(1, grey),
				Label{Text: "Drag on the image to mark the clear film base (orange). Frame 0 exposure and the base come from Camera & Light.", TextColor: grey},
				TextEdit{AssignTo: &u.logTE2, ReadOnly: true, VScroll: true, StretchFactor: 1, MinSize: Size{Height: 90}},
			}},
			Composite{StretchFactor: 2, MinSize: Size{Width: 450}, MaxSize: Size{Width: 480}, Layout: VBox{MarginsZero: true, Spacing: 3},
				Children: []Widget{scanGroup, VSpacer{}}},
		},
	}

	browse := func(ed **walk.LineEdit, title string) func() {
		return func() {
			dlg := new(walk.FileDialog)
			dlg.Title = title
			dlg.Filter = "RAW or 16-bit TIFF (*.raf;*.tif;*.tiff;*.dng)|*.raf;*.tif;*.tiff;*.dng;*.nef;*.cr2;*.cr3;*.arw|All files (*.*)|*.*"
			dlg.FilePath = (*ed).Text()
			if ok, _ := dlg.ShowOpen(mw()); ok {
				(*ed).SetText(dlg.FilePath)
			}
		}
	}
	merge := TabPage{
		Title:  " Merge files ",
		Layout: VBox{Margins: Margins{Left: 12, Top: 12, Right: 12, Bottom: 12}, Spacing: 8},
		Children: []Widget{
			Label{Text: "Merge a bracket you already have: frame 0 (base metered), the frame shot 2 EV brighter and optionally one 3 or 4 EV brighter.\nRAF files are converted with the same linear LibRaw settings as the batch converter; 16-bit TIFFs from this program\ncan be used directly. You can also drop 2 or 3 files here.", TextColor: grey},
			Composite{
				Layout: Grid{Columns: 3, Spacing: 8},
				Children: []Widget{
					Label{Text: "Frame 0", MinSize: Size{Width: 110}},
					LineEdit{AssignTo: &u.m0Ed},
					PushButton{Text: "Browse...", OnClicked: browse(&u.m0Ed, "Frame 0 (clear base metered)")},
					Label{Text: "Brighter frame\n(+2 or +3 EV)", MinSize: Size{Width: 110}},
					LineEdit{AssignTo: &u.m2Ed},
					PushButton{Text: "Browse...", OnClicked: browse(&u.m2Ed, "Frame shot 2 EV brighter")},
					Label{Text: "Frame +3 or +4 EV\n(optional)", MinSize: Size{Width: 110}},
					LineEdit{AssignTo: &u.m4Ed, ToolTipText: "Leave empty to merge two files. The EV step (+3 or +4) is detected from the RAW exposure times or the measured ratio."},
					PushButton{Text: "Browse...", OnClicked: browse(&u.m4Ed, "Frame shot 3 or 4 EV brighter (optional)")},
					Label{Text: "Alignment", MinSize: Size{Width: 110}},
					ComboBox{AssignTo: &u.mAlignCB, Model: alignItems, CurrentIndex: 0, MaxSize: Size{Width: 260}},
					HSpacer{},
					Label{Text: "Merge", MinSize: Size{Width: 110}},
					ComboBox{AssignTo: &u.mMergeCB, Model: mergeItems, CurrentIndex: mergeDefault, MaxSize: Size{Width: 260}, ToolTipText: mergeTip},
					HSpacer{},
					Label{Text: "Compression", MinSize: Size{Width: 110}},
					ComboBox{AssignTo: &u.mCompCB, Model: []string{"None (fastest)", "LZW + predictor (lossless)"}, CurrentIndex: 1, MaxSize: Size{Width: 260}},
					HSpacer{},
					Label{Text: "Dust removal", MinSize: Size{Width: 110}},
					ComboBox{AssignTo: &u.mDustCB, Model: dustItems, CurrentIndex: u.cfg.Dust, MaxSize: Size{Width: 260},
						ToolTipText: "Off / Light / Strong. Dust removal is applied into the one output <name>_HDR.tiff (no uncleaned copy is kept)."},
				},
			},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				PushButton{AssignTo: &u.mBtn, Text: "Merge to HDR TIFF", MinSize: Size{Width: 200, Height: 32}, Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}, OnClicked: u.mergeFiles},
				PushButton{AssignTo: &u.mOpenBtn, Text: "Open in Develop", Enabled: false, MinSize: Size{Height: 32}, OnClicked: func() { u.openInDevelop(u.lastMerge) },
					ToolTipText: "Open the merged HDR TIFF in Develop for preview."},
				HSpacer{},
			}},
			TextEdit{AssignTo: &u.mRes, ReadOnly: true, VScroll: true, MinSize: Size{Height: 80}, StretchFactor: 1,
				Text: "Output: <frame 0 name>_HDR.tiff next to frame 0 (a trailing _0EV is dropped; _2, _3 ... if that name exists). Always one file per frame: with Dust removal it is the cleaned merge (no uncleaned copy), linear 16-bit. The source files are never changed or deleted here."},
			Label{Text: "Merge: each frame's ratio is measured against the next darker frame on pixels unclipped in the brighter one and chained\n(+4/0 = +4/+2 x +2/0; ~4, ~8, ~16). If a frame is too clipped to measure, the RAW exposure times are used instead.\nFrame 0 gives the highlights (clear base), +2 the mid-dense areas, +3/+4 the densest; where a channel reaches 0.85 of\nfull scale (mask grown by 4 px) the darker data is kept, and the clear base always keeps frame 0's exact values.", TextColor: grey},
		},
	}
	batch.Title = " Batch RAW to TIFF "
	u.hair = &hairUI{u: u}
	hairs := u.hair.page(grey)
	return HDRPages{Camera: capture, White: white, Merge: merge, Hairs: hairs, Batch: batch}
}

// AfterCreate finishes setup once the window exists.
func (u *HDRUI) AfterCreate() {
	u.view = u.views[0]
	u.showBracket()
	u.updateButtons()
	u.say(appVersion + " HDR scan ready. Log file: " + u.log.Path())
	u.refreshDevices()
}

var _ = errors.New

// ---------- v0.83 Stop (white light scan) ----------

func (u *HDRUI) stopScan() {
	if u.scanCancel == nil {
		return
	}
	u.say("Stop pressed - the shot in progress finishes downloading, then this frame's partial RAFs/TIFFs are deleted (earlier frames are kept).")
	u.scanCancel()
}

func (u *HDRUI) endScan() {
	if u.scanCancel != nil {
		u.scanCancel()
	}
	u.scanCancel, u.scanBefore = nil, nil
	u.updateButtons()
}

// lightState is the light's last confirmed state (nil = not connected).
func (u *HDRUI) lightState() *rgblight.State {
	if u.light == nil || !u.light.connected() {
		return nil
	}
	if cs := u.light.cl.CachedState(); cs != nil {
		st := *cs
		return &st
	}
	return nil
}

// scanStopped deletes the interrupted frame's RAWs (partial TIFFs are removed
// by hdrscan) and puts the light back if it changed during the scan.
func (u *HDRUI) scanStopped(raws []string, phase string) {
	before := u.scanBefore
	u.endScan()
	n := 0
	for _, r := range raws {
		if os.Remove(r) == nil {
			n++
		}
	}
	msg := fmt.Sprintf("Scan stopped during the %s: %d RAF(s) of this frame deleted, partial TIFFs removed; earlier frames kept.", phase, n)
	now := u.lightState()
	switch {
	case before == nil || now == nil:
	case now.On == before.On && now.Value == before.Value:
		msg += " Light unchanged (" + now.String() + ")."
	default:
		b := *before
		msg += " Restoring the light to " + b.String() + " ..."
		u.light.lightJob("Restore pre-scan light (03, confirmed)", func(c *rgblight.Client) (rgblight.State, error) {
			return setAllBusyOnce(c, b.On, b.Value)
		})
	}
	u.resTE.SetText(msg)
	u.say(msg)
	u.updateButtons()
}
