package main

// v0.75 QA: dumps FilmScan_MaskCrosstalk.dctl for many settings through
// writeMaskDCTL into $DCTL_DUMP (compared byte-for-byte with the same dump
// of the v0.74 sources). DCTL_DUMP_V074=1 marks the v0.74 tree copy.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"basetool/filmdb"
)

type dumpCase struct {
	name string
	s    maskSettings
	ents []filmdb.Entry
}

func dumpCases() []dumpCase {
	cal := [9]float64{1.25, -0.2, -0.05, -0.1, 1.3, -0.2, 0.02, -0.32, 1.3}
	entSets := [][]filmdb.Entry{
		nil,
		{{Name: "Kodak Portra 400", R: 0.31, G: 0.22, B: 0.09}, {Name: "Kodak Ektar 100", R: 0.30, G: 0.25, B: 0.10}, {Name: "Vision3 250D", R: 0.2, G: 0.15, B: 0.08}, {Name: "Fuji C200", R: 0.3, G: 0.2, B: 0.1}},
		{{Name: "Roll 12", R: 0.3, G: 0.2, B: 0.1, Crosstalk: &cal}, {Name: "Kodak Portra 400", R: 0.31, G: 0.22, B: 0.09}, {Name: "Kodak Gold 200", R: 0.3, G: 0.24, B: 0.1, Crosstalk: &cal}},
	}
	var cs []dumpCase
	for ei, ents := range entSets {
		stocks := []string{""}
		for _, e := range ents {
			stocks = append(stocks, e.Name)
		}
		for si, st := range stocks {
			for mask := 0; mask < 2; mask++ {
				for bs := 0; bs < 2; bs++ {
					for ref := 0; ref < 2; ref++ {
						for xt := 0; xt < 3; xt++ {
							for mat := 0; mat < 8; mat++ {
								s := maskSettings{MaskOn: mask == 1, BaseSrc: bs, Ref: ref, BaseRGB: [3]float64{0.5, 0.4, 0.2}, Stock: st, Xt: xt, XtMat: mat}
								cs = append(cs, dumpCase{fmt.Sprintf("e%d_s%d_m%d_b%d_r%d_x%d_t%d", ei, si, mask, bs, ref, xt, mat), s, ents})
							}
						}
					}
				}
			}
		}
	}
	return cs
}

func TestDumpDCTLs(t *testing.T) {
	dir := os.Getenv("DCTL_DUMP")
	if dir == "" {
		t.Skip("DCTL_DUMP not set")
	}
	t.Setenv("APPDATA", t.TempDir())
	os.MkdirAll(configDir(), 0o755)
	dp := filepath.Join(t.TempDir(), "FilmScan_Density_In.dctl")
	out := filepath.Join(filepath.Dir(dp), maskDCTLName)
	for _, c := range dumpCases() {
		for _, v := range dumpVariants(c.s) {
			os.Remove(out)
			if err := saveMaskSettings(v.s); err != nil {
				t.Fatal(err)
			}
			if err := writeMaskDCTL(dp, c.ents); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(dir, c.name+v.tag+".dctl"), b, 0o644)
		}
	}
}
