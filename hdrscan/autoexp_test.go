package hdrscan

import "testing"

func TestAdjustExposureIncreasesWhenDark(t *testing.T) {
	cfg := DefaultAutoExp()
	cur := ExposureState{ExposureUS: 1000, GainDB: 0}
	samples := make([]float64, 100)
	for i := range samples {
		samples[i] = 0.05
	}
	next, why := AdjustExposure(samples, cur, cfg)
	if next.LinearGain() <= cur.LinearGain() {
		t.Fatalf("expected brighter, got %+v (%s)", next, why)
	}
}

func TestAdjustExposureDecreasesWhenClipped(t *testing.T) {
	cfg := DefaultAutoExp()
	cur := ExposureState{ExposureUS: 20_000, GainDB: 0}
	samples := make([]float64, 100)
	for i := range samples {
		samples[i] = 0.98
	}
	next, why := AdjustExposure(samples, cur, cfg)
	if next.ExposureUS >= cur.ExposureUS {
		t.Fatalf("expected shorter exposure, got %+v (%s)", next, why)
	}
}

func TestAdjustExposureHoldsWhenOnTarget(t *testing.T) {
	cfg := DefaultAutoExp()
	cur := ExposureState{ExposureUS: 8000, GainDB: 0}
	samples := make([]float64, 100)
	for i := range samples {
		samples[i] = cfg.TargetLevel
	}
	// A few brighter pixels still under the clip limit.
	samples[99] = 0.7
	next, why := AdjustExposure(samples, cur, cfg)
	rel := next.ExposureUS / cur.ExposureUS
	if rel < 0.9 || rel > 1.1 {
		t.Fatalf("should hold near current, got %+v (%s) rel=%.3f", next, why, rel)
	}
}

func TestAdjustExposureUsesGainAfterMaxShutter(t *testing.T) {
	cfg := DefaultAutoExp()
	cfg.MaxExposureUS = 10_000
	cfg.Damping = 1
	cfg.MaxStep = 8
	cur := ExposureState{ExposureUS: 10_000, GainDB: 0}
	samples := make([]float64, 50)
	for i := range samples {
		samples[i] = 0.05
	}
	next, why := AdjustExposure(samples, cur, cfg)
	if next.ExposureUS != cfg.MaxExposureUS {
		t.Fatalf("shutter should sit at max, got %v (%s)", next, why)
	}
	if next.GainDB <= cur.GainDB {
		t.Fatalf("gain should rise once shutter is maxed, got %+v (%s)", next, why)
	}
}

func TestAdjustExposureEmptySamples(t *testing.T) {
	cur := ExposureState{ExposureUS: 5000, GainDB: 2}
	next, why := AdjustExposure(nil, cur, DefaultAutoExp())
	if next != cur || why != "no samples" {
		t.Fatalf("got %+v %q", next, why)
	}
}

func TestPercentile(t *testing.T) {
	s := []float64{0, 1, 2, 3, 4}
	if v := percentile(s, 50); v != 2 {
		t.Fatalf("median %v", v)
	}
	if v := percentile(s, 0); v != 0 {
		t.Fatalf("p0 %v", v)
	}
	if v := percentile(s, 100); v != 4 {
		t.Fatalf("p100 %v", v)
	}
}

func TestRegionSamples(t *testing.T) {
	pix := make([]uint16, 4*4)
	pix[1+1*4] = 65535
	s := RegionSamples(pix, 4, 4, 1, 1, 2, 2, 1)
	if len(s) != 1 || s[0] != 1 {
		t.Fatalf("got %v", s)
	}
}
