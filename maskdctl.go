package main

// FilmScan_MaskCrosstalk.dctl (v0.66): written next to the exported
// FilmScan_Density_In.dctl; film stocks with a chart-calibrated crosstalk
// matrix are added to its "Crosstalk matrix preset" dropdown.

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"basetool/filmdb"
)

//go:embed FilmScan_MaskCrosstalk.dctl
var maskDCTLTemplate string

const maskDCTLName = "FilmScan_MaskCrosstalk.dctl"

// The embedded file is the stand-alone DCTL itself (v0.71: identical to
// the shipped FilmScan_MaskCrosstalk.dctl, presets 1-6 built in). Chart
// calibrated stocks of the film base DB are appended after preset 6.
const (
	maskEnumOld  = "{M_CUSTOM, M_DEFAULT, M_UNMASKED, M_KODAK1, M_KODAK2, M_KODAK3, M_KODAK4}"
	maskLabelOld = "{Custom - use sliders below, Vision3 5219 mask-aware - suggest 50 pct, Vision3 5219 unmasked - strong, Ektar 100 - datasheet spectral, Portra 400 - datasheet spectral, UltraMax 400 - datasheet spectral, Gold 200 - datasheet spectral}"
	maskRowsOld  = "    return slider;\n}"
	maskBuiltin  = 6 // presets 1..6 of the embedded file
)

// dctlLabel keeps a stock name safe inside a DCTL combo label.
func dctlLabel(s string) string {
	r := strings.NewReplacer(",", " ", "{", "(", "}", ")", "\"", "'", "\n", " ", "\r", " ", "%", " pct")
	return strings.TrimSpace(r.Replace(s))
}

// maskRow is the preset row block of matrix m for preset id.
func maskRow(id int, m [9]float64) string {
	return fmt.Sprintf("    if (id == %d)\n    {\n        if (row == 0) return make_float3(%.4ff, %.4ff, %.4ff);\n        if (row == 1) return make_float3(%.4ff, %.4ff, %.4ff);\n        return make_float3(%.4ff, %.4ff, %.4ff);\n    }\n",
		id, m[0], m[1], m[2], m[3], m[4], m[5], m[6], m[7], m[8])
}

// buildMaskDCTL returns the DCTL with one extra preset per calibrated stock.
func buildMaskDCTL(entries []filmdb.Entry) string { return buildMaskDCTLExtra(entries, nil) }

// buildMaskDCTLExtra also appends extra presets after the chart presets
// (v0.75: the X-T3 measured presets, only when crosstalk is on).
func buildMaskDCTLExtra(entries []filmdb.Entry, extra []dctlPreset) string {
	s := maskDCTLTemplate
	var enums, labels, rows []string
	k := 0
	for _, e := range entries {
		if e.Crosstalk == nil {
			continue
		}
		k++
		enums = append(enums, fmt.Sprintf("M_STOCK%d", k))
		labels = append(labels, dctlLabel(e.Name)+" - chart")
		rows = append(rows, maskRow(maskBuiltin+k, *e.Crosstalk))
	}
	for _, p := range extra {
		k++
		enums = append(enums, p.Enum)
		labels = append(labels, p.Label)
		rows = append(rows, maskRow(maskBuiltin+k, arrFromM3(p.M)))
	}
	if k == 0 || !strings.Contains(s, maskEnumOld) || !strings.Contains(s, maskLabelOld) || !strings.Contains(s, maskRowsOld) {
		return s
	}
	s = strings.Replace(s, maskEnumOld, strings.TrimSuffix(maskEnumOld, "}")+", "+strings.Join(enums, ", ")+"}", 1)
	s = strings.Replace(s, maskLabelOld, strings.TrimSuffix(maskLabelOld, "}")+", "+strings.Join(labels, ", ")+"}", 1)
	s = strings.Replace(s, maskRowsOld, strings.Join(rows, "")+maskRowsOld, 1)
	if len(extra) > 0 {
		s = strings.Replace(s, maskProtoNote, maskProtoNote+"\n"+maskXT3Note, 1)
		s = addNeutralParams(s)
	}
	return s
}

// v0.76: "Keep neutral axis". The X-T3 measured matrices have rows summing
// to 1, so they keep only R=G=B densities; the film's greys sit on a rising
// density axis (R < G < B above the base) and were pushed towards blue/cyan.
// With neut_on each row i is scaled by g_i / (row_i . g), g = (neut_r, 1,
// neut_b): M g = g, so the grey axis is unchanged for every strength and
// only the colour separation changes. Added only with the X-T3 presets
// (crosstalk on), so Off and legacy output stay as before. The inserted
// defaults (off, 1, 1) are the v0.75 behaviour; applyMaskDefaults sets them.
const (
	neutParamsAt = "DEFINE_UI_PARAMS(strength, Crosstalk strength, DCTLUI_SLIDER_FLOAT, 0.0, 0.0, 1.0, 0.01)\n"
	mdNeutOn     = "DEFINE_UI_PARAMS(neut_on, Keep neutral axis, DCTLUI_CHECK_BOX, 0)"
	mdNeutR      = "DEFINE_UI_PARAMS(neut_r, Neutral axis density R per G, DCTLUI_SLIDER_FLOAT, 1.0, "
	mdNeutB      = "DEFINE_UI_PARAMS(neut_b, Neutral axis density B per G, DCTLUI_SLIDER_FLOAT, 1.0, "
	neutFuncAt   = "__DEVICE__ float3 transform("
	neutCodeAt   = "    float s = strength;\n"
)

const neutFunc = `// Keep neutral axis (v0.76): scale a matrix row so that row . gn = gi.
__DEVICE__ float3 keep_neutral(float3 r, float3 gn, float gi)
{
    float t = r.x * gn.x + r.y * gn.y + r.z * gn.z;
    if (_fabs(t) < 1e-4f) return r;
    float k = gi / t;
    return make_float3(r.x * k, r.y * k, r.z * k);
}

`

const neutCode = `    if (neut_on != 0)
    {
        float3 gn = make_float3(neut_r, 1.0f, neut_b);
        r0 = keep_neutral(r0, gn, neut_r);
        r1 = keep_neutral(r1, gn, 1.0f);
        r2 = keep_neutral(r2, gn, neut_b);
    }
`

func addNeutralParams(s string) string {
	if strings.Count(s, neutParamsAt) != 1 || strings.Count(s, neutFuncAt) != 1 || strings.Count(s, neutCodeAt) != 1 {
		return s
	}
	params := "// Keep neutral axis: the film's grey axis (density ratios R/G and B/G above the base) is left unchanged by the matrix.\n" +
		mdNeutOn + "\n" + mdNeutR + "0.2, 3.0, 0.0001)\n" + mdNeutB + "0.2, 3.0, 0.0001)\n"
	s = strings.Replace(s, neutParamsAt, neutParamsAt+params, 1)
	s = strings.Replace(s, neutFuncAt, neutFunc+neutFuncAt, 1)
	return strings.Replace(s, neutCodeAt, neutCode+neutCodeAt, 1)
}

// v0.75: added under the template's preset note when the X-T3 measured
// presets are appended (crosstalk on only, so Off/legacy output is unchanged).
const maskProtoNote = "// Prototype values from colorsci (Kodak 5219 dye curves x approximate X-T3 sensitivities, flat white light)."
const maskXT3Note = "// (That note is for the built-in presets 1-6.) The \"X-T3 measured\" presets use the Fujifilm X-T3 spectral sensitivity measured by Weta Digital (rawtoaces-data, Apache-2.0), the selected light, and the film base weighting; dye curves are estimates."

// writeMaskDCTL writes FilmScan_MaskCrosstalk.dctl into the folder of
// densityPath (the exported FilmScan_Density_In.dctl). It is only replaced
// when it is missing or was written by this program.
func writeMaskDCTL(densityPath string, entries []filmdb.Entry) error {
	if densityPath == "" {
		return nil
	}
	p := filepath.Join(filepath.Dir(densityPath), maskDCTLName)
	// v0.73: the DCTL > Mask & Crosstalk settings set the parameter
	// defaults (all Off = the DCTL exactly as built).
	d := resolveMask(loadMaskSettings(), entries)
	body, err := applyMaskDefaults(buildMaskDCTLExtra(entries, d.Extra), d)
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(p); err == nil {
		if string(old) == body {
			return nil
		}
		if !strings.HasPrefix(string(old), "// FilmScan_MaskCrosstalk") {
			return fmt.Errorf("%s exists and was not written by this program - left unchanged", p)
		}
	}
	return os.WriteFile(p, []byte(body), 0o644)
}

// calibratedStocks reads the film base DB entries.
func calibratedStocks() []filmdb.Entry {
	db, err := filmdb.Load(filmdb.Path(configDir()))
	if err != nil {
		return nil
	}
	return db.Entries
}

// syncMaskDCTL rewrites FilmScan_MaskCrosstalk.dctl next to the Film base
// page's exported DCTL when auto-update is on ("" = nothing to do).
func syncMaskDCTL() string {
	c := loadConfig()
	if c.DCTLPath == "" || !c.AutoSync {
		return ""
	}
	if err := writeMaskDCTL(c.DCTLPath, calibratedStocks()); err != nil {
		return maskDCTLName + ": " + err.Error()
	}
	return maskDCTLName + " updated."
}
