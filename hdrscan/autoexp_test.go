package hdrscan

import (
	"math"
	"testing"
	"time"

	"basetool/hikcam"
)

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

func TestLiveAutoExpCapsShutterThenRaisesGain(t *testing.T) {
	cfg := LiveAutoExp()
	if cfg.MaxExposureUS != LiveMaxExposureUS || cfg.MaxGainDB != LiveMaxGainDB {
		t.Fatalf("live caps %+v", cfg)
	}
	// Scan metering must keep the long shutter; only Focus Check uses LiveAutoExp.
	if DefaultAutoExp().MaxExposureUS != 10_000_000 {
		t.Fatalf("DefaultAutoExp shutter cap changed")
	}
	cfg.Damping = 1
	cfg.MaxStep = 8
	cur := ExposureState{ExposureUS: LiveMaxExposureUS, GainDB: 0}
	samples := make([]float64, 50)
	for i := range samples {
		samples[i] = 0.02
	}
	next, why := AdjustExposure(samples, cur, cfg)
	if next.ExposureUS != LiveMaxExposureUS {
		t.Fatalf("live shutter should stay at cap, got %v (%s)", next.ExposureUS, why)
	}
	if next.GainDB <= 0 {
		t.Fatalf("gain should rise once live shutter is maxed, got %+v (%s)", next, why)
	}
	if next.GainDB > LiveMaxGainDB {
		t.Fatalf("gain above cap: %v", next.GainDB)
	}
}

func TestLiveAutoExpDoesNotRunAwayOnEmptySamples(t *testing.T) {
	cur := ExposureState{ExposureUS: 10_000, GainDB: 0}
	next, why := AdjustExposure(nil, cur, LiveAutoExp())
	if next != cur || why != "no samples" {
		t.Fatalf("got %+v %q", next, why)
	}
}

// lagSimCam wraps hikcam.Sim with an N-frame pipeline lag: Grab returns frames
// rendered with the exposure/gain that was active N frames ago, modeling a
// free-running USB3/GigE sensor whose in-flight frame precedes SetExposureGain.
type lagSimCam struct {
	sim     *hikcam.Sim
	lag     int
	history []hikcam.Settings
	writes  int
}

func newLagSimCam(startUS float64, lag int) *lagSimCam {
	sim := hikcam.OpenSim()
	sim.SetFlatField(true)
	st := hikcam.LiveViewSettings(hikcam.DefaultSettings(), 1)
	st.ExposureUS = startUS
	st.GainDB = 0
	_ = sim.Apply(st)
	_ = sim.StartGrab()
	return &lagSimCam{sim: sim, lag: lag}
}

func (l *lagSimCam) SetExposureGain(us, gain float64) error {
	l.writes++
	return l.sim.SetExposureGain(us, gain)
}

func (l *lagSimCam) Grab() (*hikcam.Frame, error) {
	cur := l.sim.Settings()
	l.history = append(l.history, cur)
	use := cur
	if idx := len(l.history) - 1 - l.lag; idx >= 0 {
		use = l.history[idx]
	} else if len(l.history) > 0 {
		use = l.history[0]
	}
	// Render frame using the delayed settings without changing l.sim's current settings.
	tmp := l.sim.Settings()
	_ = l.sim.SetExposureGain(use.ExposureUS, use.GainDB)
	fr, err := l.sim.Grab(time.Second)
	_ = l.sim.SetExposureGain(tmp.ExposureUS, tmp.GainDB)
	return fr, err
}

func applyHikDeadband(cam *lagSimCam, st ExposureState) (ExposureState, bool, error) {
	cur := cam.sim.Settings()
	if math.Abs(cur.ExposureUS-st.ExposureUS)/math.Max(cur.ExposureUS, 1) < 0.01 && math.Abs(cur.GainDB-st.GainDB) < 0.05 {
		return ExposureState{ExposureUS: cur.ExposureUS, GainDB: cur.GainDB}, false, nil
	}
	if err := cam.SetExposureGain(st.ExposureUS, st.GainDB); err != nil {
		return ExposureState{ExposureUS: cur.ExposureUS, GainDB: cur.GainDB}, false, err
	}
	after := cam.sim.Settings()
	return ExposureState{ExposureUS: after.ExposureUS, GainDB: after.GainDB}, true, nil
}

func TestLiveAEConvergesAndLocksWithPipelineLag(t *testing.T) {
	cfg := LiveAutoExp()

	// 1. Reproduce v1.12 failure mode under 2-frame pipeline lag: without settle
	// skip, deadband sync, and lock, exposure keeps changing in the tail window.
	legacy := newLagSimCam(150_000, 2)
	legacyState := ExposureState{ExposureUS: 150_000, GainDB: 0}
	writesBeforeTail := 0
	for i := 0; i < 60; i++ {
		if i == 30 {
			writesBeforeTail = legacy.writes
		}
		fr, err := legacy.Grab()
		if err != nil {
			t.Fatal(err)
		}
		samples := RegionSamples(fr.Pix, fr.W, fr.H, fr.W/4, fr.H/4, 3*fr.W/4, 3*fr.H/4, 4)
		next, _ := AdjustExposure(samples, legacyState, cfg)
		if _, _, err := applyHikDeadband(legacy, next); err != nil {
			t.Fatal(err)
		}
		legacyState = next // v1.12 overwrote aeState even when deadband skipped
	}
	if legacy.writes-writesBeforeTail == 0 {
		t.Fatalf("expected legacy loop without settle-skip/lock to keep oscillating under lag")
	}

	// 2. Verify LiveAE converges and locks with zero late writes for both 1-frame
	// and 2-frame lag from overexposed and underexposed starting points.
	for _, tc := range []struct {
		name    string
		startUS float64
		lag     int
	}{
		{"overexposed_lag1", 150_000, 1},
		{"overexposed_lag2", 150_000, 2},
		{"underexposed_lag2", 1_000, 2},
		{"zero_lag_sim", 150_000, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cam := newLagSimCam(tc.startUS, tc.lag)
			var ae LiveAE
			ae.Reset(ExposureState{ExposureUS: tc.startUS, GainDB: 0}, cfg)
			writesAtLock := -1
			for i := 0; i < 80; i++ {
				fr, err := cam.Grab()
				if err != nil {
					t.Fatal(err)
				}
				samples := RegionSamples(fr.Pix, fr.W, fr.H, fr.W/4, fr.H/4, 3*fr.W/4, 3*fr.H/4, 4)
				next, why, epoch, needApply := ae.Prepare(samples, cfg)
				if needApply {
					applied, changed, err := applyHikDeadband(cam, next)
					if err != nil {
						t.Fatal(err)
					}
					ae.Record(samples, cfg, why, epoch, applied, changed)
				}
				if ae.Locked && writesAtLock < 0 {
					writesAtLock = cam.writes
				}
			}
			if !ae.Locked || ae.Why != "locked" {
				t.Fatalf("did not lock: state=%+v why=%q locked=%v", ae.State, ae.Why, ae.Locked)
			}
			if cam.writes != writesAtLock {
				t.Fatalf("exposure changed %d times after locking", cam.writes-writesAtLock)
			}
			// FlatField is 0.20 at 10_000 us, so 0.42 target is ~21_000 us.
			if ae.State.ExposureUS < 18_000 || ae.State.ExposureUS > 24_500 {
				t.Fatalf("unexpected converged exposure: %.0f us", ae.State.ExposureUS)
			}
		})
	}
}

func TestLiveAEDoesNotSawtoothOnQuantizedSteps(t *testing.T) {
	cfg := LiveAutoExp()
	// 1/3-stop shutter steps like Fujifilm X-T3 (15625, 20000, 25000 us) where
	// the ideal 21000 us sits between two discrete steps.
	steps := []float64{5000, 6250, 8000, 10000, 12500, 15625, 20000, 25000, 31250, 40000}
	quantize := func(us float64) float64 {
		best := steps[0]
		bestD := math.Abs(math.Log(us / best))
		for _, s := range steps[1:] {
			if d := math.Abs(math.Log(us / s)); d < bestD {
				best, bestD = s, d
			}
		}
		return best
	}
	cam := newLagSimCam(40000, 1)
	var ae LiveAE
	ae.Reset(ExposureState{ExposureUS: 40000, GainDB: 0}, cfg)
	writesAtLock := -1
	for i := 0; i < 60; i++ {
		fr, err := cam.Grab()
		if err != nil {
			t.Fatal(err)
		}
		samples := RegionSamples(fr.Pix, fr.W, fr.H, fr.W/4, fr.H/4, 3*fr.W/4, 3*fr.H/4, 4)
		next, why, epoch, needApply := ae.Prepare(samples, cfg)
		if needApply {
			q := quantize(next.ExposureUS)
			cur := cam.sim.Settings().ExposureUS
			changed := q != cur
			if changed {
				_ = cam.SetExposureGain(q, 0)
			}
			ae.Record(samples, cfg, why, epoch, ExposureState{ExposureUS: q, GainDB: 0}, changed)
		}
		if ae.Locked && writesAtLock < 0 {
			writesAtLock = cam.writes
		}
	}
	if !ae.Locked {
		t.Fatalf("expected quantized shutter loop to lock, got state=%+v why=%q", ae.State, ae.Why)
	}
	if cam.writes != writesAtLock {
		t.Fatalf("quantized shutter changed %d times after locking", cam.writes-writesAtLock)
	}
}

func TestLiveAEUnlocksOnSceneShiftOrUserAction(t *testing.T) {
	cfg := LiveAutoExp()
	cam := newLagSimCam(21_000, 1)
	var ae LiveAE
	ae.Reset(ExposureState{ExposureUS: 21_000, GainDB: 0}, cfg)
	for i := 0; i < 20; i++ {
		fr, _ := cam.Grab()
		samples := RegionSamples(fr.Pix, fr.W, fr.H, fr.W/4, fr.H/4, 3*fr.W/4, 3*fr.H/4, 4)
		if next, why, epoch, ok := ae.Prepare(samples, cfg); ok {
			applied, changed, _ := applyHikDeadband(cam, next)
			ae.Record(samples, cfg, why, epoch, applied, changed)
		}
	}
	if !ae.Locked {
		t.Fatalf("expected initial lock")
	}
	// Explicit Unlock (e.g. zoom/pan/light switch) clears lock cleanly.
	ae.Unlock()
	if ae.Locked {
		t.Fatalf("Unlock did not clear Locked")
	}
	// Re-lock, then simulate a bright scene shift (grid image has higher p90).
	for i := 0; i < 20; i++ {
		fr, _ := cam.Grab()
		samples := RegionSamples(fr.Pix, fr.W, fr.H, fr.W/4, fr.H/4, 3*fr.W/4, 3*fr.H/4, 4)
		if next, why, epoch, ok := ae.Prepare(samples, cfg); ok {
			applied, changed, _ := applyHikDeadband(cam, next)
			ae.Record(samples, cfg, why, epoch, applied, changed)
		}
	}
	if !ae.Locked {
		t.Fatalf("expected re-lock")
	}
	cam.sim.SetFlatField(false)
	writesBeforeShift := cam.writes
	for i := 0; i < 40; i++ {
		fr, _ := cam.Grab()
		samples := RegionSamples(fr.Pix, fr.W, fr.H, fr.W/4, fr.H/4, 3*fr.W/4, 3*fr.H/4, 4)
		if next, why, epoch, ok := ae.Prepare(samples, cfg); ok {
			applied, changed, _ := applyHikDeadband(cam, next)
			ae.Record(samples, cfg, why, epoch, applied, changed)
		}
	}
	if cam.writes == writesBeforeShift {
		t.Fatalf("expected scene shift to unlock and adjust exposure")
	}
	if !ae.Locked {
		t.Fatalf("expected LiveAE to re-lock after scene shift, got why=%q", ae.Why)
	}
}
