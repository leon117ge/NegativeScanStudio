package colorsci

import (
	"math"
	"math/rand"
	"testing"
)

// synthChart renders a 24-patch chart negative with forward crosstalk F
// (density domain), gamma 0.6, the given base and noise; patch pitch 60 px,
// with a rotation of rot radians about the image centre.
func synthChart(F Mat3, base [3]float64, rot float64, noise float64) ([]uint16, int, int, ChartCorners) {
	w, h := 520, 400
	pix := make([]uint16, w*h*3)
	rng := rand.New(rand.NewSource(3))
	cx, cy := float64(w)/2, float64(h)/2
	cs, sn := math.Cos(rot), math.Sin(rot)
	toImg := func(u, v float64) (float64, float64) { // chart coords (centre 0)
		return cx + u*cs - v*sn, cy + u*sn + v*cs
	}
	var c ChartCorners
	c[0][0], c[0][1] = toImg(-150, -90)
	c[1][0], c[1][1] = toImg(150, -90)
	c[2][0], c[2][1] = toImg(150, 90)
	c[3][0], c[3][1] = toImg(-150, 90)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			u, v := dx*cs+dy*sn, -dx*sn+dy*cs
			col := int(math.Floor((u + 180) / 60))
			row := int(math.Floor((v + 120) / 60))
			var d [3]float64
			fu, fv := math.Mod(u+180, 60), math.Mod(v+120, 60)
			if col >= 0 && col < 6 && row >= 0 && row < 4 && fu > 6 && fu < 54 && fv > 6 && fv < 54 {
				i := row*6 + col
				for k := 0; k < 3; k++ {
					le := math.Log10(math.Max(SRGBToLinear(ColorChecker24sRGB[i][k]/255), 1e-4))
					d[k] = 0.6*le + 1.4
				}
			} else {
				d = [3]float64{0.9, 0.9, 0.9} // chart frame
			}
			d = F.Apply(d)
			j := (y*w + x) * 3
			for k := 0; k < 3; k++ {
				val := base[k] * math.Pow(10, -d[k]) * (1 + noise*rng.NormFloat64())
				pix[j+k] = uint16(math.Max(0, math.Min(65535, math.Round(val))))
			}
		}
	}
	return pix, w, h, c
}

func TestCalibrateChartRecoversMatrix(t *testing.T) {
	F := Mat3{{1, 0.35, 0.08}, {0.05, 1, 0.3}, {0.02, 0.1, 1}}
	inv, _ := F.Inverse()
	want := inv.NormalizeRows()
	base := [3]float64{30000, 34000, 10000}
	for _, rot := range []float64{0, 0.08, math.Pi} {
		pix, w, h, c := synthChart(F, base, rot, 0.004)
		s := SampleChart(pix, w, h, c, 0.25)
		// a wrong base must not change the matrix (centred fit)
		for _, b := range [][3]float64{base, {20000, 50000, 9000}} {
			res, err := CalibrateChart(s, b, 0)
			if err != nil {
				t.Fatal(err)
			}
			for r := 0; r < 3; r++ {
				for k := 0; k < 3; k++ {
					if math.Abs(res.Neutral[r][k]-want[r][k]) > 0.03 {
						t.Fatalf("rot %.2f base %v: Neutral\n%v\nwant\n%v", rot, b, res.Neutral, want)
					}
				}
			}
			if len(res.Warn) > 0 {
				t.Fatalf("unexpected warnings %v", res.Warn)
			}
		}
	}
	// ridge 0.01 stays close
	pix, w, h, c := synthChart(F, base, 0, 0.004)
	res, _ := CalibrateChart(SampleChart(pix, w, h, c, 0.25), base, 0.01)
	if math.Abs(res.Neutral[0][1]-want[0][1]) > 0.1 {
		t.Fatalf("ridge: %v want %v", res.Neutral, want)
	}
	// corners in the wrong order (mirrored) give a warning or a clearly different matrix
	m := ChartCorners{c[1], c[0], c[3], c[2]}
	bad, err := CalibrateChart(SampleChart(pix, w, h, m, 0.25), base, 0.01)
	if err == nil && len(bad.Warn) == 0 && math.Abs(bad.Neutral[0][1]-want[0][1]) < 0.1 {
		t.Fatalf("mirrored corners were not noticed: %v", bad.Neutral)
	}
	t.Logf("mirrored: RMS %v warn %v", bad.RMS, bad.Warn)
}
