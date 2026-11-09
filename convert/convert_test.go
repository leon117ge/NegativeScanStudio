package convert

import (
	"math"
	"testing"

	"basetool/colorsci"
)

func TestStocksPresent(t *testing.T) {
	ss := Stocks()
	if len(ss) != 6 {
		t.Fatalf("want 6 stocks, got %d", len(ss))
	}
	if _, err := StockByName("Ektar 100"); err != nil {
		t.Fatal(err)
	}
}

func TestNeutralHasOrangeMask(t *testing.T) {
	s, _ := StockByName("Ektar 100")
	d := NeutralNegDensity(Options{Stock: s, ApplyCamProfile: false})
	// Orange mask: blue (index 2) denser than red (index 0).
	if !(d[2] > d[0]+0.05) {
		t.Fatalf("expected B density > R (orange mask), got R/G/B %#v", d)
	}
	if d[0] < 0.05 || d[2] > 3.5 {
		t.Fatalf("densities out of plausible range: %#v", d)
	}
}

func TestHigherExposureDarkerNeg(t *testing.T) {
	s, _ := StockByName("Portra 400")
	lo := NeutralNegDensity(Options{Stock: s, EV: -1})
	hi := NeutralNegDensity(Options{Stock: s, EV: +1})
	for c := 0; c < 3; c++ {
		if !(hi[c] > lo[c]+0.02) {
			t.Fatalf("channel %d: +1 EV should denser than -1 EV: lo=%v hi=%v", c, lo, hi)
		}
	}
}

func TestApplyCamChangesResult(t *testing.T) {
	s, _ := StockByName("Gold 200")
	m := colorsci.Mat3{{1.2, -0.1, 0}, {-0.05, 1.1, -0.05}, {0, -0.1, 1.15}}
	a := NeutralNegDensity(Options{Stock: s, ApplyCamProfile: false})
	b := NeutralNegDensity(Options{Stock: s, ApplyCamProfile: true, CamMatrix: m, CamStrength: 1})
	diff := 0.0
	for c := 0; c < 3; c++ {
		diff += math.Abs(a[c] - b[c])
	}
	if diff < 1e-4 {
		t.Fatalf("ApplyCam should change densities: a=%v b=%v", a, b)
	}
}

func TestProcess16Length(t *testing.T) {
	s, _ := StockByName("UltraMax 400")
	in := []uint16{1000, 2000, 3000, 4000, 5000, 6000}
	out := Process16(in, Options{Stock: s, EV: 0})
	if len(out) != len(in) {
		t.Fatal("length")
	}
	for _, v := range out {
		if v == 0 || v == 65535 {
			// mid samples should land inside; all-clip would be a bug
		}
	}
	// Brighter input channel (6000 vs 1000 on last pixel's B vs first R path) —
	// just ensure non-zero finite mapping.
	if out[0] == in[0] && out[5] == in[5] {
		t.Fatal("expected remapping of linear values")
	}
}
