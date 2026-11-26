package appstate

// HDR scan settings (hdr_config.txt), item lists and their parsing. The
// declarations between the "copied" markers are copied verbatim from the
// Windows UI (../../hdrpage.go, ../../tripage.go, ../../monoopts_ui.go) so
// both front ends read and write the same file; TestCopiedDeclsMatchWindows
// fails if they drift. Only configDir differs: os.UserConfigDir is
// ~/Library/Application Support on macOS.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"basetool/camera"
	"basetool/hdrscan"
	"basetool/hikbase"
	"basetool/monocap"
)

// copied from ../../hdrpage.go (begin)
const appName = "NegativeScan Studio"
const appVersion = appName + " v1.13"

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
	CenterMark bool   // live-view optical-centre / flatness overlay (default on)
	// Shared Hikrobot exposure (us, gain 0 dB) for leveling and Mono Trichrome.
	HikExposureUS float64
	HikMeterEach  bool    // per-channel auto-meter; default off
	HikLeveledUS  float64 // exposure recorded at the last successful hik leveling
	HikLeveledRGB [3]int
	HikLeveledAt  string // RFC3339; empty if never leveled
	// Mono capture options (v1.10, v1.11). The merge noise model is the
	// MV-CS240-90UM sensor model and no longer read from the config: v1.10
	// wrote HIK_FULL_WELL_E=9000 / HIK_READ_NOISE_E=2.00 estimates, which
	// are ignored now, as are HIK_APPLY_DARK / HIK_APPLY_FLAT.
	HikFramesPerExp int  // same-exposure grabs averaged per bracket step (1 = off)
	HikHDR          bool // Mono page mode: HDR bracket (default) or single frame
	HikBracket      int  // index into monoBracketEVs
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
	c := HDRConfig{Target: targetDefault, OutDir: defaultOutDir(), LZW: true, StockTol: stockTolDefault, Bracket: bracketDefault, Merge: mergeDefault, CenterMark: true, HikExposureUS: hikbase.DefaultExposureUS, HikHDR: true, HikBracket: monoBracketDefault}
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
		case "CENTER_MARK":
			c.CenterMark = v != "0"
		case "HIK_EXPOSURE":
			if f, err := strconv.ParseFloat(v, 64); err == nil && f >= hikbase.ExpMinUS {
				c.HikExposureUS = hikbase.ClampExposureUS(f)
			}
		case "HIK_METER_EACH":
			c.HikMeterEach = v != "0"
		case "HIK_FRAMES_PER_EXP":
			if n >= framesPerExpMin && n <= framesPerExpMax {
				c.HikFramesPerExp = n
			}
		case "HIK_MODE":
			c.HikHDR = v != "single"
		case "HIK_BRACKET":
			if i := monoBracketIndex(v); i >= 0 {
				c.HikBracket = i
			}
		case "HIK_LEVELED_US":
			if f, err := strconv.ParseFloat(v, 64); err == nil && f >= hikbase.ExpMinUS {
				c.HikLeveledUS = hikbase.ClampExposureUS(f)
			}
		case "HIK_LEVELED_RGB":
			if rgb, ok := hikbase.ParseRGB(v); ok {
				c.HikLeveledRGB = rgb
			}
		case "HIK_LEVELED_AT":
			if hikbase.ParseLeveledAt(v).IsZero() == false {
				c.HikLeveledAt = v
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
	s := fmt.Sprintf("TARGETFS=%.2f\r\nAREA=%d\r\nOUTDIR=%s\r\nKEEP_TIFF=%s\r\nDELETE_RAW=%s\r\nALIGN=%d\r\nLZW=%s\r\nSIM_SCENE=%s\r\nSLOWEST=%d\r\nBRACKET=%d\r\nBRACKET_EV=%s\r\nROLL=%s\r\nSHOT_COUNTER=%d\r\nDUST=%s\r\nFRAME0=%s\r\nISO=%d\r\nSTOCK=%s\r\nSTOCK_TOL=%.2f\r\nMERGE=%d\r\nMERGE_METHOD=%s\r\nFIXED_SHUTTER=%s\r\nTRI_MODE=%s\r\nTRI_BRACKET=%s\r\nCENTER_MARK=%s\r\n",
		targetVals[ti], c.AreaMode, c.OutDir, b(c.KeepTIFF), b(c.DeleteRAW), c.Align, b(c.LZW), c.SimScene, c.Slowest, legacy, evKey(bracketEVs[bi]), hdrscan.SanitizeRoll(c.Roll), c.Counter,
		dustKeys[clamp(c.Dust, len(dustKeys))], frame0Keys[clamp(c.Frame0, len(frame0Keys))], isoVals[clamp(c.ISO, len(isoVals))], c.Stock, stockTolVals[tol],
		mi, mergeKeys[mi], fixedShutterKeys[clamp(c.Shutter, len(fixedShutterKeys))], triModeKeys[clamp(c.ScanMode, len(triModeKeys))], []string{"0_2", "0_2_3"}[clamp(c.TriBracket, 2)], b(c.CenterMark))
	hikUS := c.HikExposureUS
	if hikUS < hikbase.ExpMinUS {
		hikUS = hikbase.DefaultExposureUS
	}
	s += fmt.Sprintf("HIK_EXPOSURE=%.0f\r\nHIK_METER_EACH=%s\r\n", hikbase.ClampExposureUS(hikUS), b(c.HikMeterEach))
	hikMode := "hdr"
	if !c.HikHDR {
		hikMode = "single"
	}
	s += fmt.Sprintf("HIK_FRAMES_PER_EXP=%d\r\nHIK_MODE=%s\r\nHIK_BRACKET=%s\r\n",
		max(1, c.HikFramesPerExp), hikMode, evKey(monoBracketEVs[clampIdx(c.HikBracket, len(monoBracketEVs))]))
	if c.HikLeveledAt != "" {
		s += fmt.Sprintf("HIK_LEVELED_US=%.0f\r\nHIK_LEVELED_RGB=%s\r\nHIK_LEVELED_AT=%s\r\n", hikbase.ClampExposureUS(c.HikLeveledUS), hikbase.FormatRGB(c.HikLeveledRGB), c.HikLeveledAt)
	}
	os.MkdirAll(configDir(), 0o755)
	os.WriteFile(hdrConfigFile(), []byte(s), 0o644)
}

// copied from ../../hdrpage.go (end)

// copied from ../../tripage.go (begin)
var fixedShutterVals = []uint32{camera.Shutter1_60, camera.Shutter1_125}
var fixedShutterItems = []string{"1/60 s (default)", "1/125 s (\"1/120\", 1 EV faster)"}
var fixedShutterKeys = []string{"1/60", "1/125"}

const fixedShutterTip = "Base shutter of a Fixed frame 0, Record base, the RGB light's auto film-base leveling and trichrome scans.\n" +
	"1/125 s is one stop faster than 1/60 s (the X-T3 has no 1/120 s; 1/125 s is its nearest step).\n" +
	"Saved with the light values in the film-base DB; a preset on Camera and Light > Light restores it."

func fixedShutterIndex(v string) int {
	switch strings.TrimSpace(v) {
	case "1/125", "1/120", "7812":
		return 1
	}
	return 0
}

// fixedShutterIndexOf is the selection for a stored shutter (-1 = none).
func fixedShutterIndexOf(us uint32) int {
	for i, v := range fixedShutterVals {
		if v == us {
			return i
		}
	}
	return -1
}

// copied from ../../tripage.go (end)

var triModeKeys = []string{"tri", "tri_hdr"}

// copied from ../../monoopts_ui.go (begin)
var monoBracketEVs = [][]int{monocap.DefaultEVs, {0, 2}, {0, 2, 3}}
var monoBracketItems = []string{"0 / +2 / +3 / +5 EV (recommended)", "0 / +2 EV (fast)", "0 / +2 / +3 EV"}

const monoBracketDefault = 0

// monoBracketIndex finds a saved HIK_BRACKET key; -1 when unknown.
func monoBracketIndex(key string) int {
	for i, e := range monoBracketEVs {
		if evKey(e) == key {
			return i
		}
	}
	return -1
}

// Frames-per-exposure range shown on the Mono page (v1.10).
const (
	framesPerExpMin = 1
	framesPerExpMax = 16
)

// copied from ../../monoopts_ui.go (end)

func clampIdx(i, n int) int {
	if i < 0 || i >= n {
		return 0
	}
	return i
}

// configDir is the settings folder, as presets.go has it.
func configDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "NegativeScanStudio")
}
