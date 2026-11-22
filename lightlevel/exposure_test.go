package lightlevel

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// expoCam wraps Synth so tests can raise a common monochrome exposure.
type expoCam struct {
	*Synth
	us, ref  float64
	baseGain [3]float64
}

func (e *expoCam) Mono() bool { return true }

func (e *expoCam) GetExposure() (float64, float64) { return e.us, 0 }

func (e *expoCam) SetExposure(us, _ float64) error {
	e.us = clampExposureUS(us)
	s := e.us / e.ref
	for i := range e.Gain {
		e.Gain[i] = e.baseGain[i] * s
	}
	return nil
}

func (e *expoCam) Prepare() (string, error) {
	if err := e.SetExposure(e.us, 0); err != nil {
		return "", err
	}
	return fmt.Sprintf("%.0f us, 0.00 dB", e.us), nil
}

func TestFitCommonExposureRaisesForDimGreen(t *testing.T) {
	d, c := setup(t)
	syn := NewSynth(d.Lights)
	syn.Gain = [3]float64{1.10, 0.40, 0.95}
	cam := &expoCam{Synth: syn, us: 10_000, ref: 10_000, baseGain: syn.Gain}
	res, err := Run(context.Background(), simLight{c}, cam, Params{
		Target: 0.50, Tol: 0.02, Settle: time.Millisecond, Start: [3]int{29, 100, 54},
	}, [3]bool{true, true, true}, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.Mono {
		t.Fatal("mono flag not set")
	}
	if !res.Fit.Changed || res.Fit.FinalUS <= res.Fit.StartedUS {
		t.Fatalf("expected a raised exposure, got %+v", res.Fit)
	}
	if !res.Converged() {
		t.Fatalf("should converge after the exposure bump: %v (exposure %.0f us, values %v, post %v)",
			res.Problems(), res.Fit.FinalUS, res.Values, res.Post)
	}
	want := 10_000 * 0.50 / (0.40 * ledDrive(FitHeadroomCode))
	if math.Abs(res.Fit.FinalUS-want)/want > 0.25 {
		t.Fatalf("final exposure %.0f us, expected around %.0f us", res.Fit.FinalUS, want)
	}
	if res.Values[1] > 95 {
		t.Fatalf("green should have headroom below 100, got %d", res.Values[1])
	}
}

func TestLevelContinuesWhenGreenCappedAtStart(t *testing.T) {
	d, c := setup(t)
	cam := NewSynth(d.Lights)
	cam.Gain = [3]float64{1.20, 0.40, 0.95}
	res, err := Run(context.Background(), simLight{c}, cam, Params{
		Target: 0.50, Tol: 0.02, Settle: time.Millisecond, Start: [3]int{29, 100, 54},
	}, [3]bool{}, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.Limited[1] || res.Values[1] != 100 {
		t.Fatalf("green should stay limited at 100: %+v", res)
	}
	if !res.Done[0] || !res.Done[2] {
		t.Fatalf("R and B should still be leveled: values %v post %v problems %v", res.Values, res.Post, res.Problems())
	}
	if res.Done[1] {
		t.Fatal("green must not be reported as done")
	}
	if math.Abs(res.Post[0]/0.50-1) > 0.02 || math.Abs(res.Post[2]/0.50-1) > 0.02 {
		t.Fatalf("R/B not within tolerance: %v", res.Post)
	}
}

func TestMonoWordingUsesRawBase(t *testing.T) {
	r := Result{
		Mono:     true,
		Verified: true,
		Values:   [3]int{29, 100, 54},
		Post:     [3]float64{0.45753, 0.42799, 0.52515},
		Captured: [3]bool{true, true, true},
		Done:     [3]bool{false, false, false},
		Limited:  [3]bool{false, true, false},
		Boundary: [3]float64{0, 0.42799, 0},
		Reason:   "Closest reachable within search budget: unreachable: G too dark at code 100; increase camera exposure; worst raw base error 14.354% (tolerance 2.000%)",
	}
	s := strings.Join(r.Problems(), "\n")
	if strings.Contains(s, "after-profile") {
		t.Fatalf("mono problems still say after-profile: %s", s)
	}
	if !strings.Contains(s, "R raw base 0.45753 FS at 29") {
		t.Fatalf("missing raw-base wording for R: %s", s)
	}
	if !strings.Contains(s, "B raw base 0.52515 FS at 54") {
		t.Fatalf("missing raw-base wording for B: %s", s)
	}
	if !strings.Contains(s, "G too dark at brightness 100") {
		t.Fatalf("missing G limited wording: %s", s)
	}
	if profilePhrase(false) != "after-profile" || profilePhrase(true) != "raw base" {
		t.Fatal(profilePhrase(false), profilePhrase(true))
	}
}
