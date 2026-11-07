package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"basetool/colorsci"
	"basetool/filmdb"
)

func TestMaskDCTL(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir()) // no Mask & Crosstalk settings: all Off
	base := buildMaskDCTL(nil)
	for _, want := range []string{"M_UNMASKED, M_KODAK1, M_KODAK2, M_KODAK3, M_KODAK4}", "Ektar 100 - datasheet spectral, Portra 400 - datasheet spectral, UltraMax 400 - datasheet spectral, Gold 200 - datasheet spectral}", "if (id == 6)"} {
		if !strings.Contains(base, want) {
			t.Errorf("built-in presets: missing %q", want)
		}
	}
	m := [9]float64{1.2, -0.15, -0.05, -0.1, 1.3, -0.2, 0, -0.3, 1.3}
	s := buildMaskDCTL([]filmdb.Entry{{Name: "Plain"}, {Name: "Portra, 400 {x}", Crosstalk: &m}})
	for _, want := range []string{"M_KODAK4, M_STOCK1}", "Gold 200 - datasheet spectral, Portra  400 (x) - chart}", "if (id == 7)", "make_float3(1.2000f, -0.1500f, -0.0500f)"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Count(s, "{") != strings.Count(s, "}") {
		t.Error("unbalanced braces")
	}
	dir := t.TempDir()
	dp := filepath.Join(dir, "FilmScan_Density_In.dctl")
	if err := writeMaskDCTL(dp, []filmdb.Entry{{Name: "A", Crosstalk: &m}}); err != nil {
		t.Fatal(err)
	}
	mp := filepath.Join(dir, maskDCTLName)
	if b, _ := os.ReadFile(mp); !strings.Contains(string(b), "A - chart") {
		t.Fatal("written DCTL lacks the stock")
	}
	os.WriteFile(mp, []byte("user edit"), 0o644)
	if err := writeMaskDCTL(dp, nil); err == nil {
		t.Fatal("a foreign file must not be overwritten")
	}
	if b, _ := os.ReadFile(mp); string(b) != "user edit" {
		t.Fatal("foreign file changed")
	}
}

// TestWriteMaskDCTLFile writes the stand-alone DCTL (built-in presets, no
// chart stocks) to $MASKDCTL_OUT for distribution next to the other DCTLs.
func TestWriteMaskDCTLFile(t *testing.T) {
	if p := os.Getenv("MASKDCTL_OUT"); p != "" {
		if err := os.WriteFile(p, []byte(buildMaskDCTL(nil)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMaskTemplateInSync (v0.71): the embedded FilmScan_MaskCrosstalk.dctl
// is the shipped stand-alone DCTL; its built-in presets 3-6 must be the
// datasheet matrices of colorsci (so the file cannot go stale again).
func TestMaskTemplateInSync(t *testing.T) {
	for i, d := range colorsci.StockDyeSets[:4] {
		if row := maskRow(i+3, arrFromM3(d.Crosstalk())); !strings.Contains(maskDCTLTemplate, row) {
			t.Errorf("preset %d (%s) differs from colorsci:\n%s", i+3, d.Name, row)
		}
	}
	if buildMaskDCTL(nil) != maskDCTLTemplate {
		t.Error("no DB stocks must give the embedded file unchanged")
	}
}
