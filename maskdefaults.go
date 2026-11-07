package main

// v0.73: DCTL > Mask & Crosstalk. The former HDR-page colour options are
// DCTL-generation settings now: they set the parameter defaults written
// into FilmScan_MaskCrosstalk.dctl (the image files are not changed). All
// Off (the default) writes the DCTL exactly as before.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"basetool/colorsci"
	"basetool/filmdb"
)

var maskItems = []string{"Off", "On"}
var maskBaseItems = []string{"Base: Film base result", "Base: film stock"}
var maskBaseKeys = []string{"result", "stock"}

// The reference FilmScan_Density_In divides by: Base density = log10(ref / base).
var maskRefItems = []string{"Density_In base: brightest base channel", "Density_In base: 1.0 (full scale)"}
var maskRefKeys = []string{"brightest", "full"}
var xtItems = []string{"Off", "50%", "100%"}
var xtKeys = []string{"off", "50", "100"}
var xtVals = []float64{0, 0.5, 1}
var xtMatItems = []string{"Auto (by film stock)", "Ektar 100", "Portra 400", "UltraMax 400", "Gold 200", "Vision3 5219", "Vision3 500T", "Calibrated (chart)", "Identity"}
var xtMatKeys = []string{"auto", "ektar100", "portra400", "ultramax400", "gold200", "vision3_5219", "vision3_500t", "chart", "identity"}

// v0.75: sensor model / light source / matrix form of the built-in
// spectral matrices (not used for Calibrated (chart) and Identity).
var xtSensorItems = []string{"X-T3 measured (default)", "Gaussian (legacy v0.74)"}
var xtSensorKeys = []string{"xt3", "gauss"}
var xtLightItems = []string{"White LED (default)", "Narrowband RGB 450/540/660"}
var xtLightKeys = []string{"whiteled", "narrow"}
var xtFormItems = []string{"Mask-aware (default)", "Full inverse (no masking)"}
var xtFormKeys = []string{"maskaware", "full"}

// v0.76: neutral axis of the crosstalk matrix. Keep: every matrix row is
// rescaled so the film's grey axis (density ratios R/G, B/G above the base)
// comes out unchanged; only colour separation changes. Off: v0.75 rows.
var xtNeutItems = []string{"Keep neutral axis (default)", "Off (v0.75 matrix rows)"}
var xtNeutKeys = []string{"keep", "off"}

// defaultNeutAxis: density ratios R/G, B/G of the grey axis measured on a
// Kodak Ektar 100 frame scanned with a Fujifilm X-T3 (user_hdr.tiff, v0.76
// QA, neutralAxisFromImage). A measurement of one setup, not a datasheet value.
var defaultNeutAxis = [2]float64{0.8072, 1.1045}

const defaultNeutSrc = "measured on an Ektar 100 / X-T3 scan (v0.76 default)"

// stockNeutAxis: per-stock grey axis used when no axis was measured for the
// stock (v0.78). Vision3 500T: pooled over six Vision3 500T frames scanned
// with a Fujifilm X-T3 on a white backlight (constant border base, 507847
// near-neutral pixels; per-frame R/G 0.61-0.89, leave-one-frame-out R/G
// 0.649-0.737, B/G 1.148-1.228). No grey card was on the roll, so this is
// the scene-average grey axis, not a datasheet value. Other stocks use
// defaultNeutAxis.
var stockNeutAxis = map[string]struct {
	Axis [2]float64
	Src  string
}{
	colorsci.Ektar100Dyes.Name:    {defaultNeutAxis, defaultNeutSrc},
	colorsci.Vision3500TDyes.Name: {[2]float64{0.6808, 1.2052}, "measured on six Vision3 500T / X-T3 scans (v0.78 default)"},
}

const (
	xtSensorMeasured = 0
	xtSensorLegacy   = 1
	xtFormMaskAware  = 0
	xtFormFull       = 1
)

const (
	xtMatAuto     = 0
	xtMatVision3  = 5
	xtMatV3_500T  = 6 // v0.78
	xtMatChart    = 7
	xtMatIdentity = 8
)

const maskConfigName = "mask_config.txt"

// maskSettings is the Mask & Crosstalk page (zero value = all Off).
type maskSettings struct {
	MaskOn   bool
	BaseSrc  int        // maskBaseKeys
	BaseRGB  [3]float64 // Film base result taken on the page (fractions of full scale)
	Ref      int        // maskRefKeys
	Stock    string     // film stock of the DB ("" = none)
	Xt       int        // xtKeys
	XtMat    int        // xtMatKeys
	Sensor   int        // xtSensorKeys (v0.75)
	Light    int        // xtLightKeys (v0.75)
	Form     int        // xtFormKeys (v0.75)
	Neut     int        // xtNeutKeys (v0.76, 0 = keep neutral axis)
	NeutAxis [2]float64 // v0.76: grey-axis density ratios R/G, B/G (0 = defaultNeutAxis)
	NeutSrc  string     // v0.76: where NeutAxis came from
	NeutDyes string     // v0.78: dye set NeutAxis was measured for ("" = every stock, v0.76)
}

// neutAxis is the grey axis in use for dye set ds and where it came from:
// the axis measured on the page (for ds, or for every stock when it was
// stored by v0.76), else the stock's built-in axis, else defaultNeutAxis.
func (s maskSettings) neutAxis(ds colorsci.DyeSet) ([2]float64, string) {
	if s.NeutAxis[0] > 0 && s.NeutAxis[1] > 0 && (s.NeutDyes == "" || s.NeutDyes == ds.Name) {
		src := s.NeutSrc
		if src == "" {
			src = "stored"
		}
		return s.NeutAxis, src
	}
	if a, ok := stockNeutAxis[ds.Name]; ok {
		return a.Axis, a.Src
	}
	return defaultNeutAxis, defaultNeutSrc
}

// xtResolvedDyes is the dye set of the X-T3 measured presets (resolveMask):
// xtDyeSet, with Ektar 100 for Auto without a film stock (v0.76).
func xtResolvedDyes(s maskSettings, st *filmdb.Entry) (ds colorsci.DyeSet, ok, ektarDefault bool) {
	ds, ok = xtDyeSet(s.XtMat, st)
	if ok && s.Sensor != xtSensorLegacy && s.XtMat == xtMatAuto && st == nil {
		ds, ektarDefault = colorsci.Ektar100Dyes, true
	}
	return ds, ok, ektarDefault
}

func maskConfigPath() string { return filepath.Join(configDir(), maskConfigName) }

func loadMaskSettings() maskSettings {
	var s maskSettings
	b, err := os.ReadFile(maskConfigPath())
	if err != nil {
		return s
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "MASK_ON":
			s.MaskOn = v == "1"
		case "MASK_BASE":
			s.BaseSrc = keyIndex(maskBaseKeys, v, 0)
		case "MASK_REF":
			s.Ref = keyIndex(maskRefKeys, v, 0)
		case "MASK_BASE_RGB":
			f := strings.Split(v, ",")
			if len(f) == 3 {
				for i := range f {
					x, err := strconv.ParseFloat(strings.TrimSpace(f[i]), 64)
					if err == nil && x > 0 && !math.IsInf(x, 0) {
						s.BaseRGB[i] = x
					}
				}
			}
		case "STOCK":
			s.Stock = v
		case "CROSSTALK":
			s.Xt = keyIndex(xtKeys, v, 0)
		case "XT_MATRIX":
			s.XtMat = keyIndex(xtMatKeys, v, 0)
		case "XT_SENSOR":
			s.Sensor = keyIndex(xtSensorKeys, v, 0)
		case "XT_LIGHT":
			s.Light = keyIndex(xtLightKeys, v, 0)
		case "XT_FORM":
			s.Form = keyIndex(xtFormKeys, v, 0)
		case "XT_NEUTRAL":
			s.Neut = keyIndex(xtNeutKeys, v, 0)
		case "XT_NEUT_AXIS":
			f := strings.Split(v, ",")
			if len(f) == 2 {
				r, e1 := strconv.ParseFloat(strings.TrimSpace(f[0]), 64)
				b, e2 := strconv.ParseFloat(strings.TrimSpace(f[1]), 64)
				if e1 == nil && e2 == nil && r > 0.2 && r < 5 && b > 0.2 && b < 5 {
					s.NeutAxis = [2]float64{r, b}
				}
			}
		case "XT_NEUT_SRC":
			s.NeutSrc = v
		case "XT_NEUT_DYES":
			s.NeutDyes = v
		}
	}
	return s
}

// trichromeMask (v0.82): a trichrome scan separates the dyes optically, and
// FilmScan_Density_In divides by its film base: orange mask compensation
// off and crosstalk strength 0. changed reports whether s had them on.
func trichromeMask(s maskSettings) (out maskSettings, changed bool) {
	changed = s.MaskOn || s.Xt != 0
	s.MaskOn, s.Xt = false, 0
	return s, changed
}

func saveMaskSettings(s maskSettings) error {
	clamp := func(i, n int) int {
		if i < 0 || i >= n {
			return 0
		}
		return i
	}
	on := 0
	if s.MaskOn {
		on = 1
	}
	t := fmt.Sprintf("MASK_ON=%d\r\nMASK_BASE=%s\r\nMASK_REF=%s\r\nMASK_BASE_RGB=%.6f,%.6f,%.6f\r\nSTOCK=%s\r\nCROSSTALK=%s\r\nXT_MATRIX=%s\r\nXT_SENSOR=%s\r\nXT_LIGHT=%s\r\nXT_FORM=%s\r\n",
		on, maskBaseKeys[clamp(s.BaseSrc, len(maskBaseKeys))], maskRefKeys[clamp(s.Ref, len(maskRefKeys))], s.BaseRGB[0], s.BaseRGB[1], s.BaseRGB[2], s.Stock,
		xtKeys[clamp(s.Xt, len(xtKeys))], xtMatKeys[clamp(s.XtMat, len(xtMatKeys))],
		xtSensorKeys[clamp(s.Sensor, len(xtSensorKeys))], xtLightKeys[clamp(s.Light, len(xtLightKeys))], xtFormKeys[clamp(s.Form, len(xtFormKeys))])
	t += "XT_NEUTRAL=" + xtNeutKeys[clamp(s.Neut, len(xtNeutKeys))] + "\r\n"
	if s.NeutAxis[0] > 0 && s.NeutAxis[1] > 0 {
		t += fmt.Sprintf("XT_NEUT_AXIS=%.4f,%.4f\r\nXT_NEUT_SRC=%s\r\n", s.NeutAxis[0], s.NeutAxis[1], strings.NewReplacer("\r", " ", "\n", " ").Replace(s.NeutSrc))
		if s.NeutDyes != "" {
			t += "XT_NEUT_DYES=" + s.NeutDyes + "\r\n"
		}
	}
	return os.WriteFile(maskConfigPath(), []byte(t), 0o644)
}

func m3FromArr(a [9]float64) colorsci.Mat3 {
	return colorsci.Mat3{{a[0], a[1], a[2]}, {a[3], a[4], a[5]}, {a[6], a[7], a[8]}}
}

func arrFromM3(m colorsci.Mat3) [9]float64 {
	return [9]float64{m[0][0], m[0][1], m[0][2], m[1][0], m[1][1], m[1][2], m[2][0], m[2][1], m[2][2]}
}

// xtMatrixFor resolves the matrix-source dropdown (xtMatItems) for the
// selected stock (may be nil). The name records what was actually used.
func xtMatrixFor(mat int, st *filmdb.Entry) (colorsci.Mat3, string) {
	builtin := func(d colorsci.DyeSet, why string) (colorsci.Mat3, string) {
		return d.Crosstalk(), d.Name + " (spectral, mask-aware" + why + ")"
	}
	switch mat {
	case xtMatIdentity:
		return colorsci.Identity, "identity"
	case xtMatVision3:
		return builtin(colorsci.Vision3Dyes, "")
	case xtMatV3_500T:
		return builtin(colorsci.Vision3500TDyes, "")
	case xtMatChart, xtMatAuto:
		if st != nil && st.Crosstalk != nil {
			return m3FromArr(*st.Crosstalk), st.Name + " (chart)"
		}
		why := "; no film stock selected"
		if mat == xtMatChart {
			why = "; Calibrated (chart) unavailable: no film stock selected"
		}
		if st != nil {
			if d, ok := colorsci.MatchStock(st.Name); ok {
				if mat == xtMatChart {
					return builtin(d, "; Calibrated (chart) unavailable: "+st.Name+" has no chart matrix")
				}
				return builtin(d, "; auto from "+st.Name)
			}
			why = "; no built-in match for " + st.Name
			if mat == xtMatChart {
				why = "; Calibrated (chart) unavailable: " + st.Name + " has no chart matrix" + why
			}
		}
		return builtin(colorsci.Vision3Dyes, why)
	}
	if mat > 0 && mat < xtMatVision3 {
		return builtin(colorsci.StockDyeSets[mat-1], "")
	}
	return builtin(colorsci.Vision3Dyes, "")
}

// xtDyeSet is the built-in dye set the matrix source resolves to (false for
// Identity and for a stock with a chart matrix): same rules as xtMatrixFor.
func xtDyeSet(mat int, st *filmdb.Entry) (colorsci.DyeSet, bool) {
	switch {
	case mat == xtMatIdentity:
		return colorsci.DyeSet{}, false
	case mat > 0 && mat < xtMatVision3:
		return colorsci.StockDyeSets[mat-1], true
	case mat == xtMatV3_500T:
		return colorsci.Vision3500TDyes, true
	case mat == xtMatAuto || mat == xtMatChart:
		if st != nil && st.Crosstalk != nil {
			return colorsci.DyeSet{}, false
		}
		if st != nil {
			if d, ok := colorsci.MatchStock(st.Name); ok {
				return d, true
			}
		}
	}
	return colorsci.Vision3Dyes, true
}

// dctlPreset is a matrix preset appended to the DCTL combo.
type dctlPreset struct {
	Enum, Label string
	M           colorsci.Mat3
}

// measuredPresets are the four X-T3 measured presets of d appended to the
// DCTL (v0.75): White LED / Narrowband x mask-aware / full inverse.
func measuredPresets(d colorsci.DyeSet) []dctlPreset {
	var p []dctlPreset
	for li, ln := range []string{"White LED", "Narrowband RGB 450-540-660"} {
		for fi, fn := range []string{"mask-aware", "full inverse"} {
			form := colorsci.FormMaskAware
			if fi == xtFormFull {
				form = colorsci.FormFull
			}
			p = append(p, dctlPreset{Enum: fmt.Sprintf("M_XT3_%d", len(p)+1), Label: dctlLabel(d.Name + " - X-T3 measured " + ln + " " + fn), M: d.MeasuredCrosstalk(li, form)})
		}
	}
	return p
}

func fmtM3(m colorsci.Mat3) string {
	return fmt.Sprintf("[%.4f %.4f %.4f; %.4f %.4f %.4f; %.4f %.4f %.4f]", m[0][0], m[0][1], m[0][2], m[1][0], m[1][1], m[1][2], m[2][0], m[2][1], m[2][2])
}

// maskDefaults are the DCTL parameter defaults derived from maskSettings.
type maskDefaults struct {
	MaskOn    bool
	BD        [3]float64 // base density above the Density_In reference
	MatPreset int        // "Crosstalk matrix preset" index of the written DCTL
	Strength  float64
	Desc      []string     // what was set, for the page and the DCTL comment
	Extra     []dctlPreset // v0.75: presets appended after the chart presets
	Neut      bool         // v0.76: keep the neutral axis (X-T3 presets only)
	NeutAxis  [2]float64   // v0.76: grey-axis density ratios R/G, B/G
}

func (d maskDefaults) changed() bool { return d.MaskOn || d.Strength > 0 }

func findEntry(entries []filmdb.Entry, name string) *filmdb.Entry {
	if name == "" {
		return nil
	}
	for i := range entries {
		if strings.EqualFold(entries[i].Name, name) {
			return &entries[i]
		}
	}
	return nil
}

// dctlPresetNames are the combo labels 0..6 of the embedded DCTL.
var dctlPresetNames = []string{"Custom", "Vision3 5219 mask-aware", "Vision3 5219 unmasked", "Ektar 100", "Portra 400", "UltraMax 400", "Gold 200"}

// maskPresetFor maps the matrix source to a preset index of the DCTL built
// from entries (buildMaskDCTL: built-in 1..6, chart stocks from 7 on).
func maskPresetFor(mat int, st *filmdb.Entry, entries []filmdb.Entry) (int, string) {
	_, why := xtMatrixFor(mat, st)
	if mat == xtMatIdentity {
		return 0, "Custom (sliders = identity)"
	}
	if strings.HasSuffix(why, " (chart)") && st != nil {
		k := 0
		for _, e := range entries {
			if e.Crosstalk == nil {
				continue
			}
			k++
			if strings.EqualFold(e.Name, st.Name) {
				return maskBuiltin + k, dctlLabel(e.Name) + " - chart"
			}
		}
	}
	id := 1 // Vision3 5219 mask-aware
	if mat > 0 && mat < xtMatVision3 {
		id = mat + 2
	} else if mat == xtMatAuto || mat == xtMatChart {
		if st != nil {
			if d, ok := colorsci.MatchStock(st.Name); ok {
				for i, ds := range colorsci.StockDyeSets {
					if ds.Name == d.Name && i < 4 {
						id = i + 3
					}
				}
			}
		}
	}
	if strings.HasPrefix(why, dctlPresetNames[id]) {
		return id, why
	}
	return id, dctlPresetNames[id] + " (" + why + ")"
}

// resolveMask turns the page settings into DCTL defaults.
func resolveMask(s maskSettings, entries []filmdb.Entry) maskDefaults {
	var d maskDefaults
	st := findEntry(entries, s.Stock)
	if s.MaskOn {
		base, src := s.BaseRGB, "Film base result"
		if s.BaseSrc == 1 {
			base, src = [3]float64{}, "film stock (none selected)"
			if st != nil {
				base, src = [3]float64{st.R, st.G, st.B}, "film stock "+st.Name
			}
		}
		if base[0] > 0 && base[1] > 0 && base[2] > 0 {
			ref, rn := math.Max(base[0], math.Max(base[1], base[2])), "brightest base channel"
			if s.Ref == 1 {
				ref, rn = 1, "1.0"
			}
			for c := range base {
				d.BD[c] = math.Log10(ref / base[c])
			}
			d.MaskOn = true
			d.Desc = append(d.Desc, fmt.Sprintf("Orange mask compensation On, base density R %.4f G %.4f B %.4f (log10(ref / base), Density_In base %s; base R %.4f G %.4f B %.4f from %s)",
				d.BD[0], d.BD[1], d.BD[2], rn, base[0], base[1], base[2], src),
				"Note (v0.76): only for a FilmScan_Density_In whose Base R/G/B is that reference. The Density_In exported here divides by the film base",
				"per channel, so the mask is already removed there and this subtracts it a second time (strong yellow cast). Turn it off in that case.")
		} else {
			d.Desc = append(d.Desc, "Orange mask compensation NOT set: no base ("+src+")")
		}
	}
	if s.Xt > 0 && s.Xt < len(xtVals) {
		d.Strength = xtVals[s.Xt]
		var name string
		// v0.76: Auto without a film stock uses Ektar 100 for the X-T3 presets.
		ds, ok, ektarDefault := xtResolvedDyes(s, st)
		if ok && s.Sensor != xtSensorLegacy {
			// v0.75 default: measured X-T3 sensitivity. The four presets
			// of the resolved stock go after the chart presets.
			_, why := xtMatrixFor(s.XtMat, st)
			if i := strings.Index(why, "; "); i >= 0 {
				why = " (" + strings.TrimSuffix(why[i+2:], ")") + ")"
			} else {
				why = ""
			}
			if ektarDefault {
				why = " (Auto: no film stock selected - Ektar 100 is the v0.76 default)"
			}
			d.Extra = measuredPresets(ds)
			light := max(0, min(s.Light, 1))
			k := 2*light + b2i(s.Form == xtFormFull)
			d.MatPreset = maskBuiltin + chartPresets(entries) + 1 + k
			d.Desc = append(d.Desc, fmt.Sprintf("Crosstalk matrix preset %d = %s%s, strength %.2f", d.MatPreset, d.Extra[k].Label, why, d.Strength),
				"Matrix "+fmtM3(d.Extra[k].M)+" (density space, rows R G B)",
				"Sensor: Fujifilm X-T3 spectral sensitivity measured by Weta Digital (rawtoaces-data, Apache-2.0); dyes: "+ds.Name+" ("+dyeNote(ds)+"); base-weighted D-min: "+dminNote(ds))
			if s.Neut == 0 {
				d.Neut = true
				var src string
				d.NeutAxis, src = s.neutAxis(ds)
				d.Desc = append(d.Desc, fmt.Sprintf("Keep neutral axis On: rows rescaled so density R:G:B = %.4f : 1 : %.4f above the base is unchanged (%s)", d.NeutAxis[0], d.NeutAxis[1], src))
				d.Desc = append(d.Desc, "Effective matrix with the neutral axis "+fmtM3(colorsci.KeepNeutral(d.Extra[k].M, d.NeutAxis[0], d.NeutAxis[1])))
			} else {
				d.Desc = append(d.Desc, "Keep neutral axis Off (v0.75 rows: only equal R=G=B densities are unchanged; film greys shift towards blue/cyan)")
			}
		} else {
			d.MatPreset, name = maskPresetFor(s.XtMat, st, entries)
			d.Desc = append(d.Desc, fmt.Sprintf("Crosstalk matrix preset %d = %s, strength %.2f", d.MatPreset, name, d.Strength))
		}
	}
	return d
}

const (
	mdMatPreset = "DEFINE_UI_PARAMS(mat_preset, Crosstalk matrix preset, DCTLUI_COMBO_BOX, 0, "
	mdMaskOn    = "DEFINE_UI_PARAMS(mask_on, Orange mask compensation, DCTLUI_CHECK_BOX, 0)"
	mdBD        = "DEFINE_UI_PARAMS(bd_%s, Base density %s, DCTLUI_SLIDER_FLOAT, 0.0, "
	mdStrength  = "DEFINE_UI_PARAMS(strength, Crosstalk strength, DCTLUI_SLIDER_FLOAT, 0.0, "
	mdPresets   = "// ===== MATRIX PRESETS ====="
)

// applyMaskDefaults rewrites the parameter defaults of a FilmScan_MaskCrosstalk
// DCTL. Unchanged defaults return s as it is.
func applyMaskDefaults(s string, d maskDefaults) (string, error) {
	if !d.changed() {
		return s, nil
	}
	rep := func(old, nw string) error {
		if strings.Count(s, old) != 1 {
			return fmt.Errorf("DCTL template: %q not found", old)
		}
		s = strings.Replace(s, old, nw, 1)
		return nil
	}
	if d.MaskOn {
		if err := rep(mdMaskOn, strings.TrimSuffix(mdMaskOn, "0)")+"1)"); err != nil {
			return "", err
		}
		for c, n := range []string{"R", "G", "B"} {
			old := fmt.Sprintf(mdBD, strings.ToLower(n), n)
			if err := rep(old, strings.TrimSuffix(old, "0.0, ")+fmt.Sprintf("%.4f, ", d.BD[c])); err != nil {
				return "", err
			}
		}
	}
	if d.Strength > 0 {
		if err := rep(mdMatPreset, strings.TrimSuffix(mdMatPreset, "0, ")+fmt.Sprintf("%d, ", d.MatPreset)); err != nil {
			return "", err
		}
		if err := rep(mdStrength, strings.TrimSuffix(mdStrength, "0.0, ")+fmt.Sprintf("%.2f, ", d.Strength)); err != nil {
			return "", err
		}
	}
	if d.Neut && d.Strength > 0 {
		if err := rep(mdNeutOn, strings.TrimSuffix(mdNeutOn, "0)")+"1)"); err != nil {
			return "", err
		}
		if err := rep(mdNeutR, strings.TrimSuffix(mdNeutR, "1.0, ")+fmt.Sprintf("%.4f, ", d.NeutAxis[0])); err != nil {
			return "", err
		}
		if err := rep(mdNeutB, strings.TrimSuffix(mdNeutB, "1.0, ")+fmt.Sprintf("%.4f, ", d.NeutAxis[1])); err != nil {
			return "", err
		}
	}
	var c strings.Builder
	c.WriteString("// Defaults set by NegativeScan Studio (DCTL > Mask & Crosstalk):\n")
	for _, l := range d.Desc {
		c.WriteString("//   " + l + "\n")
	}
	if err := rep(mdPresets, c.String()+mdPresets); err != nil {
		return "", err
	}
	return s, nil
}

// chartPresets is the number of chart presets buildMaskDCTL appends.
func chartPresets(entries []filmdb.Entry) int {
	k := 0
	for _, e := range entries {
		if e.Crosstalk != nil {
			k++
		}
	}
	return k
}

func dyeNote(d colorsci.DyeSet) string {
	if d.Name == colorsci.Vision3Dyes.Name || d.Name == colorsci.Vision3500TDyes.Name {
		return "Kodak H-1-5219 dye curves"
	}
	return "estimated from the Kodak datasheet neutral curve"
}

func dminNote(d colorsci.DyeSet) string {
	if d.Name == colorsci.Vision3500TDyes.Name {
		return "Kodak H-1-5219 Minimum Density curve, digitised by eye (estimate)"
	}
	if _, ok := colorsci.KodakDmin[d.Name]; ok {
		return "digitised Kodak datasheet curve"
	}
	return "none (no data, computed without base)"
}
