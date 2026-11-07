package colorsci

import (
	"os"
	"testing"

	"basetool/rawconv"
)

// TestWriteSynthChart writes a synthetic chart negative HDR TIFF to
// $CHART_OUT (for UI screenshots); skipped otherwise.
func TestWriteSynthChart(t *testing.T) {
	p := os.Getenv("CHART_OUT")
	if p == "" {
		t.Skip("CHART_OUT not set")
	}
	F := Mat3{{1, 0.35, 0.08}, {0.05, 1, 0.3}, {0.02, 0.1, 1}}
	pix, w, h, c := synthChart(F, [3]float64{52000, 38000, 12000}, 0.08, 0.004)
	if err := rawconv.WriteTIFF16(p, w, h, pix, false, "", "", "synthetic ColorChecker negative"); err != nil {
		t.Fatal(err)
	}
	t.Logf("corners %v", c)
}
