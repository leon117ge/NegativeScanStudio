package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"basetool/colorsci"
	"basetool/filmdb"
)

// v0.78: Vision3 500T film-stock preset (H-1-5219 dyes + D-min, measured
// 500T / X-T3 neutral axis).
func TestVision3500TPreset(t *testing.T) {
	v := colorsci.Vision3500TDyes
	if _, ok := colorsci.KodakDmin[v.Name]; !ok {
		t.Fatal("no 500T D-min")
	}
	if xtMatKeys[xtMatV3_500T] != "vision3_500t" || xtMatItems[xtMatV3_500T] != "Vision3 500T" {
		t.Fatal("500T dropdown entry")
	}
	// legacy Gaussian matrix unchanged (same dyes), X-T3 matrix base-weighted
	if v.Crosstalk() != colorsci.DefaultCrosstalk() {
		t.Fatal("500T legacy matrix must equal 5219")
	}
	a, b := v.MeasuredCrosstalk(0, colorsci.FormMaskAware), colorsci.Vision3Dyes.MeasuredCrosstalk(0, colorsci.FormMaskAware)
	if math.Abs(a[0][1]-b[0][1]) < 0.01 {
		t.Fatalf("500T D-min not used: %v vs %v", a, b)
	}
	ents := []filmdb.Entry{{Name: "Kodak Vision3 500T", R: 0.86, G: 0.86, B: 0.28}, {Name: "Vision3 250D"}, {Name: "Kodak Ektar 100"}}
	for _, s := range []maskSettings{{Xt: 2, XtMat: xtMatV3_500T}, {Xt: 2, Stock: "Kodak Vision3 500T"}} {
		d := resolveMask(s, ents)
		out, err := applyMaskDefaults(buildMaskDCTLExtra(ents, d.Extra), d)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(d.Extra[0].Label, "Vision3 500T - X-T3 measured White LED mask-aware") || d.NeutAxis != [2]float64{0.6808, 1.2052} {
			t.Fatalf("%+v: %q %v", s, d.Extra[0].Label, d.NeutAxis)
		}
		for _, w := range []string{"neut_r, Neutral axis density R per G, DCTLUI_SLIDER_FLOAT, 0.6808,", "neut_b, Neutral axis density B per G, DCTLUI_SLIDER_FLOAT, 1.2052,", "Vision3 500T / X-T3 scans", "Minimum Density curve"} {
			if !strings.Contains(out, w) {
				t.Fatalf("%+v: missing %q", s, w)
			}
		}
		if got, ok := dctlPresetRow(out, d.MatPreset); !ok || !close4(got, arrFromM3(a)) {
			t.Fatalf("preset row %v", got)
		}
	}
	// other ECN stocks keep 5219 without D-min and the default axis
	if d := resolveMask(maskSettings{Xt: 2, Stock: "Vision3 250D"}, ents); !strings.HasPrefix(d.Extra[0].Label, "Vision3 5219 -") || d.NeutAxis != defaultNeutAxis {
		t.Fatalf("250D: %q %v", d.Extra[0].Label, d.NeutAxis)
	}
	if d := resolveMask(maskSettings{Xt: 2, Stock: "Kodak Ektar 100"}, ents); d.NeutAxis != defaultNeutAxis {
		t.Fatalf("Ektar axis %v", d.NeutAxis)
	}
	// a measured axis applies to its stock only; a v0.76 one (no stock) to all
	m := maskSettings{Xt: 2, XtMat: xtMatV3_500T, NeutAxis: [2]float64{0.9, 1.1}, NeutSrc: "x.tif", NeutDyes: "Ektar 100"}
	if d := resolveMask(m, ents); d.NeutAxis != [2]float64{0.6808, 1.2052} {
		t.Fatalf("Ektar measurement used for 500T: %v", d.NeutAxis)
	}
	m.NeutDyes = ""
	if d := resolveMask(m, ents); d.NeutAxis != [2]float64{0.9, 1.1} {
		t.Fatalf("v0.76 measurement not used: %v", d.NeutAxis)
	}
	m.NeutDyes = v.Name
	if d := resolveMask(m, ents); d.NeutAxis != [2]float64{0.9, 1.1} {
		t.Fatalf("500T measurement not used: %v", d.NeutAxis)
	}
	t.Setenv("APPDATA", t.TempDir())
	os.MkdirAll(configDir(), 0o755)
	saveMaskSettings(m)
	if s := loadMaskSettings(); s.NeutDyes != v.Name || s.XtMat != xtMatV3_500T {
		t.Fatalf("round trip %+v", s)
	}
}

// TestWrite500TFiles writes the 500T DCTLs to $V500T_OUT for the numpy check.
func TestWrite500TFiles(t *testing.T) {
	out := os.Getenv("V500T_OUT")
	if out == "" {
		t.Skip("V500T_OUT not set")
	}
	for name, s := range map[string]maskSettings{
		"500T_WL_maskaware_keep.dctl": {Xt: 2, XtMat: xtMatV3_500T},
		"500T_WL_full_keep.dctl":      {Xt: 2, XtMat: xtMatV3_500T, Form: xtFormFull},
		"500T_NB_maskaware_keep.dctl": {Xt: 2, XtMat: xtMatV3_500T, Light: 1},
		"500T_WL_maskaware_off.dctl":  {Xt: 2, XtMat: xtMatV3_500T, Neut: 1},
		"default_off.dctl":            {},
	} {
		d := resolveMask(s, nil)
		dctl, err := applyMaskDefaults(buildMaskDCTLExtra(nil, d.Extra), d)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, name), []byte(dctl), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
