package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"basetool/colorsci"
)

// TestWriteV076Standalone writes the v0.76 Ektar 100 X-T3 DCTLs to $V076_DCTL
// (crosstalk 100 %, keep neutral axis with the default axis, mask comp Off).
func TestWriteV076Standalone(t *testing.T) {
	dir := os.Getenv("V076_DCTL")
	if dir == "" {
		t.Skip("V076_DCTL not set")
	}
	t.Setenv("APPDATA", t.TempDir())
	os.MkdirAll(configDir(), 0o755)
	tmp := t.TempDir()
	dp := filepath.Join(tmp, "FilmScan_Density_In.dctl")
	for _, c := range []struct {
		name string
		s    maskSettings
	}{
		{"FilmScan_MaskCrosstalk_XT3_Ektar100_WhiteLED.dctl", maskSettings{Xt: 2, XtMat: 1, Light: 0, Form: xtFormMaskAware}},
		{"FilmScan_MaskCrosstalk_XT3_Ektar100_Narrowband.dctl", maskSettings{Xt: 2, XtMat: 1, Light: 1, Form: xtFormMaskAware}},
		{"qa_auto_nostock.dctl", maskSettings{Xt: 2, XtMat: 0}},
		{"qa_ektar_neutoff.dctl", maskSettings{Xt: 2, XtMat: 1, Neut: 1}},
		{"qa_ektar_wl_full.dctl", maskSettings{Xt: 2, XtMat: 1, Form: xtFormFull}},
		{"qa_legacy_ektar.dctl", maskSettings{Xt: 2, XtMat: 1, Sensor: xtSensorLegacy}},
		{"qa_maskon.dctl", maskSettings{MaskOn: true, BaseRGB: [3]float64{0.52714, 0.56878, 0.18674}}},
	} {
		os.Remove(filepath.Join(tmp, maskDCTLName))
		saveMaskSettings(c.s)
		if err := writeMaskDCTL(dp, nil); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(filepath.Join(tmp, maskDCTLName))
		if err := os.WriteFile(filepath.Join(dir, c.name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNeutralParams(t *testing.T) {
	// Off and legacy: no neutral parameters (byte-identical to v0.75).
	for _, s := range []maskSettings{{}, {Xt: 2, XtMat: 1, Sensor: xtSensorLegacy}, {MaskOn: true, BaseRGB: [3]float64{0.5, 0.5, 0.2}}} {
		d := resolveMask(s, nil)
		out, err := applyMaskDefaults(buildMaskDCTLExtra(nil, d.Extra), d)
		if err != nil || strings.Contains(out, "neut_on") {
			t.Fatalf("%+v: neutral params present (err %v)", s, err)
		}
	}
	d := resolveMask(maskSettings{Xt: 2, XtMat: 1}, nil)
	out, err := applyMaskDefaults(buildMaskDCTLExtra(nil, d.Extra), d)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"DEFINE_UI_PARAMS(neut_on, Keep neutral axis, DCTLUI_CHECK_BOX, 1)", "neut_r, Neutral axis density R per G, DCTLUI_SLIDER_FLOAT, 0.8072,", "neut_b, Neutral axis density B per G, DCTLUI_SLIDER_FLOAT, 1.1045,", "r0 = keep_neutral(r0, gn, neut_r);"} {
		if !strings.Contains(out, w) {
			t.Fatalf("missing %q", w)
		}
	}
	d = resolveMask(maskSettings{Xt: 2, XtMat: 1, Neut: 1}, nil)
	out, _ = applyMaskDefaults(buildMaskDCTLExtra(nil, d.Extra), d)
	if !strings.Contains(out, "DEFINE_UI_PARAMS(neut_on, Keep neutral axis, DCTLUI_CHECK_BOX, 0)") {
		t.Fatal("neutral off not written as 0")
	}
	// Auto without a stock: Ektar 100 X-T3 presets.
	d = resolveMask(maskSettings{Xt: 2}, nil)
	if !strings.HasPrefix(d.Extra[0].Label, colorsci.Ektar100Dyes.Name) {
		t.Fatalf("auto default %q", d.Extra[0].Label)
	}
	// Settings round trip of the neutral axis.
	t.Setenv("APPDATA", t.TempDir())
	os.MkdirAll(configDir(), 0o755)
	saveMaskSettings(maskSettings{Neut: 1, NeutAxis: [2]float64{0.81, 1.12}, NeutSrc: "x.tif"})
	s := loadMaskSettings()
	if s.Neut != 1 || s.NeutAxis != [2]float64{0.81, 1.12} || s.NeutSrc != "x.tif" {
		t.Fatalf("round trip %+v", s)
	}
}
