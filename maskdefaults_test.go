package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"basetool/colorsci"
	"basetool/filmdb"
)

func TestXtMatrixFor(t *testing.T) {
	ek := &filmdb.Entry{Name: "Kodak Ektar 100"}
	cal := [9]float64{1.2, -0.2, 0, 0, 1, 0, 0, 0, 1}
	ekCal := &filmdb.Entry{Name: "Kodak Ektar 100", Crosstalk: &cal}
	odd := &filmdb.Entry{Name: "Fuji C200"}
	cases := []struct {
		mat  int
		st   *filmdb.Entry
		want colorsci.Mat3
		name string
	}{
		{xtMatAuto, ek, colorsci.Ektar100Dyes.Crosstalk(), "Ektar 100"},
		{xtMatAuto, ekCal, m3FromArr(cal), "(chart)"},
		{xtMatAuto, odd, colorsci.DefaultCrosstalk(), "no built-in match"},
		{xtMatAuto, nil, colorsci.DefaultCrosstalk(), "no film stock"},
		{1, odd, colorsci.Ektar100Dyes.Crosstalk(), "Ektar 100"},
		{2, nil, colorsci.Portra400Dyes.Crosstalk(), "Portra 400"},
		{3, nil, colorsci.UltraMax400Dyes.Crosstalk(), "UltraMax 400"},
		{4, nil, colorsci.Gold200Dyes.Crosstalk(), "Gold 200"},
		{xtMatVision3, ek, colorsci.DefaultCrosstalk(), "Vision3 5219"},
		{xtMatChart, ek, colorsci.Ektar100Dyes.Crosstalk(), "has no chart matrix"},
		{xtMatChart, ekCal, m3FromArr(cal), "(chart)"},
		{xtMatIdentity, ekCal, colorsci.Identity, "identity"},
	}
	for i, c := range cases {
		m, n := xtMatrixFor(c.mat, c.st)
		if m != c.want || !strings.Contains(n, c.name) {
			t.Errorf("case %d: %v %q", i, m, n)
		}
	}
	if len(xtMatItems) != len(xtMatKeys) || xtMatKeys[xtMatVision3] != "vision3_5219" || xtMatKeys[xtMatChart] != "chart" || xtMatKeys[xtMatIdentity] != "identity" {
		t.Fatal("xtMat tables out of sync")
	}
}

// The DCTL preset chosen for every matrix source holds the same matrix as
// xtMatrixFor (built-in rows are the colorsci matrices to 4 decimals).
func TestMaskPresetFor(t *testing.T) {
	cal := [9]float64{1.2, -0.2, 0, 0, 1, 0, 0, 0, 1}
	ents := []filmdb.Entry{{Name: "Other", Crosstalk: &cal}, {Name: "Kodak Ektar 100", Crosstalk: &cal}, {Name: "Kodak Portra 400"}, {Name: "Fuji C200"}, {Name: "Vision3 250D"}}
	dctl := buildMaskDCTL(ents)
	for mat := range xtMatItems {
		for si := -1; si < len(ents); si++ {
			var st *filmdb.Entry
			if si >= 0 {
				st = &ents[si]
			}
			id, _ := maskPresetFor(mat, st, ents)
			m, why := xtMatrixFor(mat, st)
			if id == 0 {
				if m != colorsci.Identity {
					t.Errorf("mat %d stock %d: Custom but %s", mat, si, why)
				}
				continue
			}
			if got, ok := dctlPresetRow(dctl, id); !ok || !close4(got, arrFromM3(m)) {
				t.Errorf("mat %d stock %d: preset %d %v does not hold %s", mat, si, id, got, why)
			}
		}
	}
	if id, _ := maskPresetFor(xtMatChart, &ents[1], ents); id != maskBuiltin+2 {
		t.Errorf("chart stock 2: preset %d", id)
	}
}

func TestMaskDefaultsOffIdentical(t *testing.T) {
	cal := [9]float64{1.2, -0.2, 0, 0, 1, 0, 0, 0, 1}
	for _, ents := range [][]filmdb.Entry{nil, {{Name: "A", R: 0.5, G: 0.4, B: 0.2, Crosstalk: &cal}}} {
		in := buildMaskDCTL(ents)
		// every Off combination of the other dropdowns leaves the file alone
		for bs := range maskBaseItems {
			for mat := range xtMatItems {
				for k := 0; k < 8; k++ {
					s := maskSettings{BaseSrc: bs, XtMat: mat, Stock: "A", BaseRGB: [3]float64{0.5, 0.4, 0.2}, Sensor: k & 1, Light: k >> 1 & 1, Form: k >> 2}
					d := resolveMask(s, ents)
					out, err := applyMaskDefaults(buildMaskDCTLExtra(ents, d.Extra), d)
					if err != nil || out != in || d.Extra != nil {
						t.Fatalf("Off settings changed the DCTL (%v)", err)
					}
				}
			}
		}
	}
	// no settings file = zero settings
	t.Setenv("APPDATA", t.TempDir())
	if loadMaskSettings() != (maskSettings{}) {
		t.Fatal("missing settings must be all Off")
	}
}

func TestMaskDefaultsSet(t *testing.T) {
	ents := []filmdb.Entry{{Name: "Kodak Ektar 100", R: 0.30, G: 0.25, B: 0.10}}
	in := buildMaskDCTL(ents)
	// v0.75: Gaussian (legacy v0.74) reproduces the v0.74 result
	s := maskSettings{MaskOn: true, BaseSrc: 0, BaseRGB: [3]float64{0.5, 0.4, 0.2}, Stock: "Kodak Ektar 100", Xt: 2, XtMat: xtMatAuto, Sensor: xtSensorLegacy}
	d := resolveMask(s, ents)
	if !d.MaskOn || d.MatPreset != 3 || d.Strength != 1 || math.Abs(d.BD[1]-math.Log10(0.5/0.4)) > 1e-12 || d.BD[0] != 0 {
		t.Fatalf("%+v", d)
	}
	out, err := applyMaskDefaults(in, d)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"DEFINE_UI_PARAMS(mask_on, Orange mask compensation, DCTLUI_CHECK_BOX, 1)":                  true,
		"DEFINE_UI_PARAMS(bd_r, Base density R, DCTLUI_SLIDER_FLOAT, 0.0000, -0.5, 3.0, 0.0001)":    true,
		"DEFINE_UI_PARAMS(bd_g, Base density G, DCTLUI_SLIDER_FLOAT, 0.0969, -0.5, 3.0, 0.0001)":    true,
		"DEFINE_UI_PARAMS(bd_b, Base density B, DCTLUI_SLIDER_FLOAT, 0.3979, -0.5, 3.0, 0.0001)":    true,
		"DEFINE_UI_PARAMS(strength, Crosstalk strength, DCTLUI_SLIDER_FLOAT, 1.00, 0.0, 1.0, 0.01)": true,
	}
	a, b := strings.Split(in, "\n"), strings.Split(out, "\n")
	// drop the inserted comment block, then the files line up 1:1
	var bf []string
	inBlock := false
	for _, l := range b {
		if strings.HasPrefix(l, "// Defaults set by NegativeScan Studio") {
			inBlock = true
			continue
		}
		if inBlock && strings.HasPrefix(l, "//   ") {
			continue
		}
		inBlock = false
		bf = append(bf, l)
	}
	if len(bf) != len(a) || len(b)-len(bf) != 1+len(d.Desc) {
		t.Fatalf("line counts %d %d %d", len(a), len(b), len(bf))
	}
	got := 0
	for i := range a {
		if a[i] == bf[i] {
			continue
		}
		if want[bf[i]] || strings.HasPrefix(bf[i], "DEFINE_UI_PARAMS(mat_preset, Crosstalk matrix preset, DCTLUI_COMBO_BOX, 3, {M_CUSTOM") {
			got++
		} else {
			t.Errorf("unexpected change %q -> %q", a[i], bf[i])
		}
	}
	if got != 6 {
		t.Errorf("changed %d define lines (want 6)", got)
	}
	// stock base, mask only: crosstalk untouched
	s = maskSettings{MaskOn: true, BaseSrc: 1, Stock: "kodak ektar 100"}
	d = resolveMask(s, ents)
	if !d.MaskOn || d.Strength != 0 || math.Abs(d.BD[2]-math.Log10(3)) > 1e-12 {
		t.Fatalf("%+v", d)
	}
	out, _ = applyMaskDefaults(in, d)
	if !strings.Contains(out, "DCTLUI_COMBO_BOX, 0, {M_CUSTOM") || !strings.Contains(out, "Crosstalk strength, DCTLUI_SLIDER_FLOAT, 0.0, ") {
		t.Error("mask only must keep the crosstalk defaults")
	}
	// mask on without any base: not set, file unchanged
	if d := resolveMask(maskSettings{MaskOn: true, BaseSrc: 1}, ents); d.MaskOn || len(d.Desc) != 1 {
		t.Errorf("no base: %+v", d)
	}
}

func TestMaskSettingsRoundTrip(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	os.MkdirAll(configDir(), 0o755)
	s := maskSettings{MaskOn: true, BaseSrc: 1, BaseRGB: [3]float64{0.1933, 0.1883, 0.0593}, Stock: "Kodak Ektar 100", Xt: 1, XtMat: xtMatChart, Sensor: 1, Light: 1, Form: 1}
	if err := saveMaskSettings(s); err != nil {
		t.Fatal(err)
	}
	if g := loadMaskSettings(); g != s {
		t.Fatalf("%+v", g)
	}
	os.WriteFile(maskConfigPath(), []byte("MASK_ON=x\r\nCROSSTALK=bogus\r\nXT_MATRIX=zzz\r\nXT_SENSOR=?\r\nXT_LIGHT=\r\nXT_FORM=x\r\nMASK_BASE_RGB=1,2\r\nJUNK\r\n"), 0o644)
	if g := loadMaskSettings(); g != (maskSettings{}) {
		t.Fatalf("bad values: %+v", g)
	}
	// written DCTL uses the stored settings
	dp := filepath.Join(t.TempDir(), "FilmScan_Density_In.dctl")
	saveMaskSettings(maskSettings{Xt: 1, XtMat: xtMatVision3, Sensor: xtSensorLegacy})
	if err := writeMaskDCTL(dp, nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(dp), maskDCTLName))
	if !strings.Contains(string(b), "DCTLUI_COMBO_BOX, 1, {M_CUSTOM") || !strings.Contains(string(b), "Crosstalk strength, DCTLUI_SLIDER_FLOAT, 0.50, ") {
		t.Error("settings not written")
	}
	// v0.75 default sensor: Vision3 X-T3 measured, narrowband, full inverse = preset 6+1+3
	saveMaskSettings(maskSettings{Xt: 2, XtMat: xtMatVision3, Light: 1, Form: xtFormFull})
	if err := writeMaskDCTL(dp, nil); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(filepath.Dir(dp), maskDCTLName))
	if !strings.Contains(string(b), "DCTLUI_COMBO_BOX, 10, {M_CUSTOM") || !strings.Contains(string(b), "Vision3 5219 - X-T3 measured Narrowband RGB 450-540-660 full inverse") {
		t.Error("measured settings not written")
	}
	if got, ok := dctlPresetRow(string(b), 10); !ok || !close4(got, arrFromM3(colorsci.Vision3Dyes.MeasuredCrosstalk(colorsci.ScanLightNarrowband, colorsci.FormFull))) {
		t.Errorf("preset 10 = %v", got)
	}
}

// TestMeasuredPresets: with the X-T3 measured sensor the selected preset of
// the written DCTL holds MeasuredCrosstalk of the resolved stock, light and
// form; chart and identity sources are unchanged from v0.74.
func TestMeasuredPresets(t *testing.T) {
	cal := [9]float64{1.2, -0.2, 0, 0, 1, 0, 0, 0, 1}
	ents := []filmdb.Entry{{Name: "Other", Crosstalk: &cal}, {Name: "Kodak Ektar 100", Crosstalk: &cal}, {Name: "Kodak Portra 400"}, {Name: "Fuji C200"}, {Name: "Vision3 250D"}}
	for mat := range xtMatItems {
		for si := -1; si < len(ents); si++ {
			stock := ""
			var st *filmdb.Entry
			if si >= 0 {
				stock, st = ents[si].Name, &ents[si]
			}
			for l := 0; l < 2; l++ {
				for f := 0; f < 2; f++ {
					d := resolveMask(maskSettings{Xt: 2, XtMat: mat, Stock: stock, Light: l, Form: f}, ents)
					dctl, err := applyMaskDefaults(buildMaskDCTLExtra(ents, d.Extra), d)
					if err != nil {
						t.Fatal(err)
					}
					ds, spectral := xtDyeSet(mat, st)
					if spectral && mat == xtMatAuto && st == nil {
						ds = colorsci.Ektar100Dyes // v0.76: Auto without a stock
					}
					if !spectral {
						legacy := resolveMask(maskSettings{Xt: 2, XtMat: mat, Stock: stock, Sensor: xtSensorLegacy}, ents)
						if d.Extra != nil || d.MatPreset != legacy.MatPreset {
							t.Errorf("mat %d stock %d: chart/identity changed", mat, si)
						}
						continue
					}
					form := colorsci.FormMaskAware
					if f == xtFormFull {
						form = colorsci.FormFull
					}
					want := ds.MeasuredCrosstalk(l, form)
					if d.MatPreset != maskBuiltin+2+1+2*l+f {
						t.Errorf("mat %d stock %d l%d f%d: preset %d", mat, si, l, f, d.MatPreset)
					}
					got, ok := dctlPresetRow(dctl, d.MatPreset)
					if !ok || !close4(got, arrFromM3(want)) || !strings.Contains(dctl, fmt.Sprintf("DCTLUI_COMBO_BOX, %d, {M_CUSTOM", d.MatPreset)) {
						t.Errorf("mat %d stock %d l%d f%d: preset row %v want %v", mat, si, l, f, got, want)
					}
					if strings.Count(dctl, "M_XT3_") != 4 || !strings.Contains(dctl, ds.Name+" - X-T3 measured") {
						t.Errorf("mat %d stock %d: presets not appended", mat, si)
					}
				}
			}
		}
	}
}

// TestWriteMaskSetFiles writes DCTLs with settings to $MASKSET_OUT for the
// numpy check against qa070/fbm_ref.py (Ektar: the fbm_ref film base).
func TestWriteMaskSetFiles(t *testing.T) {
	dir := os.Getenv("MASKSET_OUT")
	if dir == "" {
		t.Skip("MASKSET_OUT not set")
	}
	cal := [9]float64{1.25, -0.2, -0.05, -0.1, 1.3, -0.2, 0.02, -0.32, 1.3}
	var eb [3]float64 // Ektar rebate base (fbm_ref / gen_ref.py), from $MASKSET_BASE
	fmt.Sscanf(os.Getenv("MASKSET_BASE"), "%g,%g,%g", &eb[0], &eb[1], &eb[2])
	ents := []filmdb.Entry{{Name: "Kodak Ektar 100", R: eb[0], G: eb[1], B: eb[2]}, {Name: "Roll 12", R: eb[0], G: eb[1], B: eb[2], Crosstalk: &cal}}
	sets := []maskSettings{
		{MaskOn: true, BaseSrc: 1, Ref: 1, Stock: "Kodak Ektar 100"},
		{MaskOn: true, BaseSrc: 1, Ref: 1, Stock: "Kodak Ektar 100", Xt: 1, XtMat: xtMatAuto},
		{MaskOn: true, BaseSrc: 0, BaseRGB: eb, Ref: 0, Stock: "Kodak Ektar 100", Xt: 1, XtMat: 2},
		{Xt: 2, XtMat: xtMatVision3},
		{MaskOn: true, BaseSrc: 1, Ref: 1, Stock: "Roll 12", Xt: 2, XtMat: xtMatChart},
		{},
		{MaskOn: true, BaseSrc: 1, Ref: 0, Stock: "Kodak Ektar 100", Xt: 2, XtMat: xtMatAuto},
	}
	for i, s := range sets {
		out, err := applyMaskDefaults(buildMaskDCTL(ents), resolveMask(s, ents))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("set%d.dctl", i)), []byte(out), 0o644)
	}
}

// dctlPresetRow reads the 3x3 matrix of preset id from a mask DCTL.
func dctlPresetRow(dctl string, id int) ([9]float64, bool) {
	var m [9]float64
	i := strings.Index(dctl, fmt.Sprintf("    if (id == %d)\n", id))
	if i < 0 {
		return m, false
	}
	n := 0
	for _, l := range strings.Split(dctl[i:], "\n")[1:6] {
		j := strings.Index(l, "make_float3(")
		if j < 0 {
			continue
		}
		f := strings.Split(strings.TrimSuffix(strings.TrimSpace(l[j+12:]), ");"), ",")
		for _, x := range f {
			fmt.Sscanf(strings.TrimSuffix(strings.TrimSpace(x), "f"), "%g", &m[n])
			n++
		}
	}
	return m, n == 9
}

func close4(a, b [9]float64) bool {
	for i := range a {
		if math.Abs(a[i]-b[i]) > 0.00005+1e-9 {
			return false
		}
	}
	return true
}
