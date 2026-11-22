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

func TestFitCommonExposureLowersWhenRedCodeTooLow(t *testing.T) {
	// At 14289 us, R would land near code 5 (one integer step ~20%).
	// G and B are close enough that lowering exposure can lift R to >= 25
	// while still keeping them reachable.
	const startUS = 14289.0
	const ref = 10000.0
	scale := startUS / ref
	// effective gain at startUS so Gain*ledDrive(code)*scale/scale wait:
	// expoCam: Gain[i] = baseGain[i] * us / ref, Shoot uses Gain * ledDrive.
	// Want at startUS: Gain_eff * ledDrive(5) = 0.50 for R.
	d, c := setup(t)
	syn := NewSynth(d.Lights)
	syn.Gain = [3]float64{
		0.50 / ledDrive(5) / scale,  // R needs ~5
		0.50 / ledDrive(15) / scale, // G needs ~15
		0.50 / ledDrive(18) / scale, // B needs ~18
	}
	cam := &expoCam{Synth: syn, us: startUS, ref: ref, baseGain: syn.Gain}
	res, err := Run(context.Background(), simLight{c}, cam, Params{
		Target: 0.50, Tol: 0.02, Settle: time.Millisecond, Start: [3]int{5, 15, 18},
	}, [3]bool{true, true, true}, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.Fit.Changed || res.Fit.FinalUS >= startUS {
		t.Fatalf("expected a lowered exposure from %.0f us, got %+v", startUS, res.Fit)
	}
	if !res.Converged() {
		t.Fatalf("should converge after lowering exposure: %v (exposure %.0f us, values %v, post %v)",
			res.Problems(), res.Fit.FinalUS, res.Values, res.Post)
	}
	if res.Values[0] < 25 {
		t.Fatalf("R should land at or above 25 after the exposure drop, got %d at %.0f us", res.Values[0], res.Fit.FinalUS)
	}
	if res.Values[0] > FitHeadroomCode || res.Values[1] > 100 || res.Values[2] > 100 {
		t.Fatalf("codes out of range: %v at %.0f us", res.Values, res.Fit.FinalUS)
	}
}

func TestChooseCoarsePlanLowersForBrightRed(t *testing.T) {
	// At 14289 us, R is already on target at code 5; G/B need 15 and 18.
	// The plan should lower exposure so R's integer code is as high as
	// possible while G and B stay at or below FitHeadroomMax.
	cur := 14289.0
	pts := [3][]probePt{
		{{code: 5, level: 0.50}},
		{{code: 15, level: 0.50}},
		{{code: 18, level: 0.50}},
	}
	plan, ok := chooseCoarsePlan(cur, pts, 0.50)
	if !ok {
		t.Fatal("expected a coarse plan")
	}
	if plan.ch != 0 {
		t.Fatalf("coarsest should be R, got %c", "RGB"[plan.ch])
	}
	if plan.us >= cur {
		t.Fatalf("should lower exposure, got %.0f (%s)", plan.us, plan.reason)
	}
	if plan.code < 25 {
		t.Fatalf("R should be raised well above 5, got code %d at %.0f us (%s)", plan.code, plan.us, plan.reason)
	}
	for c, start := range []int{15, 18} {
		n, ok := neededCodeAt(pts[c+1], cur, plan.us, 0.50)
		if !ok || n > float64(FitHeadroomMax)+1.5 {
			t.Fatalf("other channel %c should stay at or below %d, needed %.1f (from %d) at %.0f us",
				"RGB"[c+1], FitHeadroomMax, n, start, plan.us)
		}
	}
}

func TestFitExposureFineTunesCoarseRed(t *testing.T) {
	// Hardware-like ratio: at 11567 us, R needs ~7, G ~85, B ~86.
	// The 25-85 window is impossible (~12x channel ratio). Exposure
	// must fine-tune the coarsest integer (R) so all three land within 2%.
	const startUS = 11567.0
	const ref = 10000.0
	scale := startUS / ref
	d, c := setup(t)
	syn := NewSynth(d.Lights)
	syn.Gain = [3]float64{
		0.50 / ledDrive(7) / scale,
		0.50 / ledDrive(85) / scale,
		0.50 / ledDrive(86) / scale,
	}
	cam := &expoCam{Synth: syn, us: startUS, ref: ref, baseGain: syn.Gain}
	res, err := Run(context.Background(), simLight{c}, cam, Params{
		Target: 0.50, Tol: 0.02, Settle: time.Millisecond, Start: [3]int{7, 85, 86},
	}, [3]bool{true, true, true}, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.Converged() {
		t.Fatalf("R@7 vs G/B@85 should converge by fine-tuning exposure: %v (exposure %.0f us, values %v, post %v, fit %s)",
			res.Problems(), res.Fit.FinalUS, res.Values, res.Post, res.Fit.Reason)
	}
	if res.Values[0] < 1 || res.Values[0] > 25 {
		t.Fatalf("R should stay on a low integer code (ratio cannot reach 25-85), got %v at %.0f us", res.Values, res.Fit.FinalUS)
	}
	if res.Values[1] > 100 || res.Values[2] > 100 {
		t.Fatalf("G/B must stay reachable, got %v", res.Values)
	}
	for c, v := range res.Post {
		if math.Abs(v/0.50-1) > 0.02 {
			t.Fatalf("%c post %.5f is outside 2%% at values %v, exposure %.0f us", "RGB"[c], v, res.Values, res.Fit.FinalUS)
		}
	}
}

func TestChooseCoarsePlanHardwareRatio(t *testing.T) {
	// Measurements matching the v1.06 hardware miss: R at 7 is 2.5% low,
	// G/B are already on target at 85/86, 11567 us.
	cur := 11567.0
	pts := [3][]probePt{
		{{code: 7, level: 0.48756}},
		{{code: 85, level: 0.49805}},
		{{code: 86, level: 0.50172}},
	}
	plan, ok := chooseCoarsePlan(cur, pts, 0.50)
	if !ok {
		t.Fatal("expected a coarse plan")
	}
	if plan.ch != 0 {
		t.Fatalf("coarsest should be R, got %c (%s)", "RGB"[plan.ch], plan.reason)
	}
	if plan.code < 5 || plan.code > 20 {
		t.Fatalf("R code should stay low, got %d at %.0f us (%s)", plan.code, plan.us, plan.reason)
	}
	// Exposure for R at that integer should be near cur * 0.50/0.48756 if code stays 7.
	if plan.code == 7 {
		want := cur * 0.50 / 0.48756
		if math.Abs(plan.us-want)/want > 0.05 {
			t.Fatalf("R@7 exposure %.0f, want around %.0f (%s)", plan.us, want, plan.reason)
		}
	}
	for c := 1; c < 3; c++ {
		n, ok := neededCodeAt(pts[c], cur, plan.us, 0.50)
		if !ok || n > 100 {
			t.Fatalf("%c should stay reachable, needed %.1f at %.0f us", "RGB"[c], n, plan.us)
		}
	}
}
