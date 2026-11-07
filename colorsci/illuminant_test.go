package colorsci

import (
	"math"
	"testing"
)

func TestVision3Illuminant(t *testing.T) {
	z, e := Vision3Illuminant(3200)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range z.Density {
		if math.Abs(v) > 1e-12 {
			t.Fatal(z)
		}
	}
	r, e := Vision3Illuminant(5500)
	if e != nil {
		t.Fatal(e)
	}
	if r.Density[0] >= 0 || r.Density[1] != 0 || r.Density[2] <= 0 {
		t.Fatal(r)
	}
	if math.Abs(r.Mired-130.6818) > .001 {
		t.Fatal(r)
	}
	t.Logf("5500K: logH=%v gamma=%v extraD=%v mired=%g", r.LogExposure, r.Gamma, r.Density, r.Mired)
	prev := r.Density[2]
	for _, k := range []float64{6500, 8000, 10000} {
		r, _ := Vision3Illuminant(k)
		if r.Density[2] <= prev {
			t.Fatal(r)
		}
		prev = r.Density[2]
	}
	for _, k := range []float64{0, 2400, 10001, math.NaN(), math.Inf(1)} {
		if _, e := Vision3Illuminant(k); e == nil {
			t.Fatalf("accepted %g", k)
		}
	}
}
func TestPlanckNormalisation(t *testing.T) {
	for _, k := range []float64{2500, 3200, 5500, 10000} {
		if math.Abs(PlanckRelative(550, k)-1) > 1e-12 {
			t.Fatal(k)
		}
	}
}
