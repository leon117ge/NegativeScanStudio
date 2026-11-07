package main

// v0.78 QA: export the Mask & Crosstalk DCTL for a set of configs given as
// mask_config.txt text (key-based, so it means the same in v0.77 and v0.78)
// to $V078_DUMP, for a byte comparison between the two versions.

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"basetool/filmdb"
)

func TestV078Dump(t *testing.T) {
	out := os.Getenv("V078_DUMP")
	if out == "" {
		t.Skip("V078_DUMP not set")
	}
	cal := [9]float64{1.5, -0.4, -0.1, -0.2, 1.4, -0.2, 0, -0.3, 1.3}
	ents := []filmdb.Entry{{Name: "Kodak Ektar 100", R: 0.19, G: 0.19, B: 0.06, Crosstalk: &cal}, {Name: "Kodak Portra 400", R: 0.2, G: 0.18, B: 0.07}, {Name: "Vision3 250D", R: 0.8, G: 0.8, B: 0.3}, {Name: "Roll 12", R: 0.3, G: 0.3, B: 0.1}}
	cfgs := []string{
		"",
		"MASK_ON=1\r\nMASK_BASE=stock\r\nSTOCK=Kodak Ektar 100\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=auto\r\n",
		"CROSSTALK=50\r\nXT_MATRIX=auto\r\nSTOCK=Kodak Ektar 100\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=ektar100\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=ektar100\r\nXT_LIGHT=narrow\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=ektar100\r\nXT_FORM=full\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=ektar100\r\nXT_NEUTRAL=off\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=ektar100\r\nXT_SENSOR=gauss\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=ektar100\r\nXT_NEUT_AXIS=0.810000,1.100000\r\nXT_NEUT_SRC=a.tif\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=portra400\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=vision3_5219\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=auto\r\nSTOCK=Vision3 250D\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=chart\r\nSTOCK=Kodak Ektar 100\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=chart\r\nSTOCK=Roll 12\r\n",
		"CROSSTALK=100\r\nXT_MATRIX=identity\r\n",
		"MASK_ON=1\r\nCROSSTALK=100\r\nXT_MATRIX=chart\r\nSTOCK=Kodak Ektar 100\r\nMASK_BASE=stock\r\n",
	}
	t.Setenv("APPDATA", t.TempDir())
	os.MkdirAll(configDir(), 0o755)
	for i, c := range cfgs {
		os.WriteFile(maskConfigPath(), []byte(c), 0o644)
		s := loadMaskSettings()
		d := resolveMask(s, ents)
		dctl, err := applyMaskDefaults(buildMaskDCTLExtra(ents, d.Extra), d)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(out, "cfg"+strconv.Itoa(i)+".dctl"), []byte(dctl), 0o644)
		saveMaskSettings(s)
		b, _ := os.ReadFile(maskConfigPath())
		os.WriteFile(filepath.Join(out, "cfg"+strconv.Itoa(i)+".saved.txt"), b, 0o644)
	}
}
