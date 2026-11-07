package colorsci

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// TestMeasuredMatchesPython compares the Go matrices with the Python
// reference (xt3_dye_crosstalk.py via v075_reference.py).
func TestMeasuredMatchesPython(t *testing.T) {
	b, err := os.ReadFile("testdata/v075_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref map[string]map[string][3][3]float64
	if err := json.Unmarshal(b, &ref); err != nil {
		t.Fatal(err)
	}
	worst := 0.0
	for _, d := range StockDyeSets {
		for li, ln := range []string{"white_led", "narrowband"} {
			r, ok := ref[d.Name+"|"+ln]
			if !ok {
				t.Fatalf("no reference for %s|%s", d.Name, ln)
			}
			got := map[string]Mat3{"forward": d.MeasuredForward(XT3Measured, li), "full": d.MeasuredCrosstalk(li, FormFull), "maskaware": d.MeasuredCrosstalk(li, FormMaskAware)}
			for k, m := range got {
				for i := 0; i < 3; i++ {
					for j := 0; j < 3; j++ {
						e := math.Abs(m[i][j] - r[k][i][j])
						worst = math.Max(worst, e)
						if e > 1e-4 {
							t.Errorf("%s|%s %s [%d][%d]: go %.6f py %.6f", d.Name, ln, k, i, j, m[i][j], r[k][i][j])
						}
					}
				}
			}
		}
	}
	t.Logf("max |Go - Python| = %.3g", worst)
}

// TestLegacyUnchanged: the v0.74 Gaussian matrices are untouched.
func TestLegacyUnchanged(t *testing.T) {
	m := DefaultCrosstalk()
	want := Mat3{{1.6258, -0.8166, 0.1908}, {0, 1.5437, -0.5437}, {0, 0, 1}}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if math.Abs(m[i][j]-want[i][j]) > 5e-5 {
				t.Fatalf("DefaultCrosstalk changed: %v", m)
			}
		}
	}
}
