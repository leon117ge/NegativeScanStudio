package hdrscan

import "testing"

func constSamples(n int, v float64) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = v
	}
	return s
}

func TestMeterScanExposureReducesWhenHighlightsClip(t *testing.T) {
	cur := ExposureState{ExposureUS: 10_000, GainDB: 0}
	next, why := MeterScanExposure(constSamples(100, 0.97), constSamples(20, 0.80), cur, ScanMeterConfig())
	if next.ExposureUS >= cur.ExposureUS && next.GainDB >= cur.GainDB {
		t.Fatalf("expected a shorter exposure, got %+v (%s)", next, why)
	}
	if why != "reduce (highlight clip)" {
		t.Fatalf("reason %q", why)
	}
}

func TestMeterScanExposureRaisesDarkBaseTowardHalf(t *testing.T) {
	cur := ExposureState{ExposureUS: 4_000, GainDB: 0}
	full := constSamples(80, 0.22)
	base := constSamples(20, 0.18)
	next, why := MeterScanExposure(full, base, cur, ScanMeterConfig())
	if next.ExposureUS <= cur.ExposureUS {
		t.Fatalf("expected a longer exposure, got %+v (%s)", next, why)
	}
	if why != "increase toward film base 0.50" && why != "increase (near black)" {
		t.Fatalf("reason %q", why)
	}
}

func TestMeterScanExposureHoldsWhenBaseIsHalf(t *testing.T) {
	cur := ExposureState{ExposureUS: 12_000, GainDB: 0}
	next, why := MeterScanExposure(constSamples(80, 0.48), constSamples(20, 0.50), cur, ScanMeterConfig())
	if why != "on target" {
		t.Fatalf("reason %q next=%+v", why, next)
	}
	if next.ExposureUS != cur.ExposureUS || next.GainDB != cur.GainDB {
		t.Fatalf("should hold, got %+v", next)
	}
}

func TestMeterScanExposureLimitsRaiseByHighlightHeadroom(t *testing.T) {
	cur := ExposureState{ExposureUS: 8_000, GainDB: 0}
	// Base is dark (would like ~2.5x) but highlights are already at 0.70;
	// 0.88/0.70 ≈ 1.26 so the raise must be smaller than 2x.
	next, why := MeterScanExposure(constSamples(80, 0.70), constSamples(20, 0.20), cur, ScanMeterConfig())
	if why != "match base, limited by highlight headroom" {
		t.Fatalf("reason %q next=%+v", why, next)
	}
	if next.ExposureUS > cur.ExposureUS*1.6 {
		t.Fatalf("raise too large: %+v", next)
	}
	if next.ExposureUS <= cur.ExposureUS {
		t.Fatalf("expected a modest raise, got %+v", next)
	}
}

func TestFrameSamplesRespectsRectangle(t *testing.T) {
	const w, h = 40, 20
	pix := make([]uint16, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x >= 20 {
				pix[y*w+x] = 40000
			} else {
				pix[y*w+x] = 1000
			}
		}
	}
	left := FrameSamples(pix, w, h, Rect{X0: 0, Y0: 0, X1: 0.5, Y1: 1})
	right := FrameSamples(pix, w, h, Rect{X0: 0.5, Y0: 0, X1: 1, Y1: 1})
	if len(left) == 0 || len(right) == 0 {
		t.Fatal("empty samples")
	}
	if percentile(left, 50) > 0.1 {
		t.Fatalf("left median %v", percentile(left, 50))
	}
	if percentile(right, 50) < 0.5 {
		t.Fatalf("right median %v", percentile(right, 50))
	}
}
