package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteV075Standalone writes the stand-alone X-T3 measured DCTLs to
// $V075_DCTL through writeMaskDCTL (Portra 400, crosstalk 100 %, mask comp
// Off = its default; the four X-T3 presets are all in each file).
func TestWriteV075Standalone(t *testing.T) {
	dir := os.Getenv("V075_DCTL")
	if dir == "" {
		t.Skip("V075_DCTL not set")
	}
	t.Setenv("APPDATA", t.TempDir())
	os.MkdirAll(configDir(), 0o755)
	tmp := t.TempDir()
	dp := filepath.Join(tmp, "FilmScan_Density_In.dctl")
	for _, c := range []struct {
		name string
		s    maskSettings
	}{
		{"FilmScan_MaskCrosstalk_XT3_WhiteLED.dctl", maskSettings{Xt: 2, XtMat: 2, Light: 0, Form: xtFormMaskAware}},
		{"FilmScan_MaskCrosstalk_XT3_Narrowband.dctl", maskSettings{Xt: 2, XtMat: 2, Light: 1, Form: xtFormMaskAware}},
		{"qa_WhiteLED_full.dctl", maskSettings{Xt: 2, XtMat: 2, Light: 0, Form: xtFormFull}},
		{"qa_legacy_portra.dctl", maskSettings{Xt: 2, XtMat: 2, Sensor: xtSensorLegacy}},
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
