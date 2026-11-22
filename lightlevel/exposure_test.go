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

// nonLinearExpoCam wraps expoCam with an optional per-channel brightness curve
// override so tests can simulate real LED droop / low-code boost where the
// physical driver differs from ledDrive(code).
type nonLinearExpoCam struct {
	*expoCam
	curve func(ch, code int) float64
}

func (c *nonLinearExpoCam) Shoot(ctx context.Context, ch int, phase string) (*Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	on, v := c.Synth.Lights()
	lv := 0.0
	if on[ch] {
		lv = c.baseGain[ch] * c.curve(ch, v[ch]) * (c.us / c.ref)
	}
	im := &Image{W: 20, H: 20, V: make([]float32, 400), Clip: make([]bool, 400)}
	for i := range im.V {
		val := lv
		if val >= 1 {
			val = 1
			im.Clip[i] = true
		}
		im.V[i] = float32(val)
	}
	return im, nil
}

func runHardwareScenario(t *testing.T, name string, startUS float64, gain [3]float64, tol float64, curve func(ch, code int) float64) Result {
	t.Helper()
	d, c := setup(t)
	syn := NewSynth(d.Lights)
	syn.Gain = gain
	baseCam := &expoCam{Synth: syn, us: startUS, ref: startUS, baseGain: syn.Gain}
	var cam VariableExposure = baseCam
	if curve != nil {
		cam = &nonLinearExpoCam{expoCam: baseCam, curve: curve}
	}
	res, err := Run(context.Background(), simLight{c}, cam, Params{
		Target:  0.50,
		Tol:     tol,
		MaxIter: 10,
		Settle:  time.Millisecond,
		Start:   [3]int{50, 50, 50},
	}, [3]bool{true, true, true}, func(s string) { t.Logf("[%s tol=%.0f%%] %s", name, tol*100, s) })
	if err != nil {
		t.Fatalf("%s (tol %.1f%%): %v", name, tol*100, err)
	}
	if !res.Converged() {
		t.Fatalf("%s (tol %.1f%%) did not converge: %v (exposure %.0f -> %.0f us, values %v, post %v, fit %s)",
			name, tol*100, res.Problems(), res.Fit.StartedUS, res.Fit.FinalUS, res.Values, res.Post, res.Fit.Reason)
	}
	for ch, code := range res.Values {
		if code < 1 || code > 100 {
			t.Fatalf("%s: channel %c code %d out of 1..100", name, "RGB"[ch], code)
		}
	}
	worst := 0.0
	for ch, v := range res.Post {
		e := math.Abs(v/0.50 - 1)
		if e > worst {
			worst = e
		}
		if e > tol {
			t.Fatalf("%s: %c reading %.5f exceeds %.1f%% tol (values %v, %.0f us)",
				name, "RGB"[ch], v, tol*100, res.Values, res.Fit.FinalUS)
		}
	}
	t.Logf("SCENARIO_SUMMARY name=%q tol=%.1f%% start_us=%.0f final_us=%.0f values=R%d/G%d/B%d post=%.5f/%.5f/%.5f worst=%.3f%% rounds=%d",
		name, tol*100, startUS, res.Fit.FinalUS, res.Values[0], res.Values[1], res.Values[2],
		res.Post[0], res.Post[1], res.Post[2], worst*100, res.Rounds)
	return res
}

func TestRegression_V107DimGBAt10000us(t *testing.T) {
	// Exact v1.07 real-hardware numbers back-calculated to startUS = 10000 us:
	// at 9269 us, G@100 = 0.44215 FS, B@100 = 0.43360 FS, R@13 ~= 0.500 FS.
	// So at 10000 us: G@100 = 0.47702 FS, B@100 = 0.46780 FS, R@13 ~= 0.5394 FS.
	// Exposure MUST go UP (never down below 10000 us) so G and B stay <= 100.
	const startUS = 10000.0
	gain := [3]float64{
		0.50 * (10000.0 / 9269.0) / ledDrive(13),
		0.44215 * (10000.0 / 9269.0) / ledDrive(100),
		0.43360 * (10000.0 / 9269.0) / ledDrive(100),
	}
	for _, tol := range []float64{0.01, 0.02} {
		res := runHardwareScenario(t, "v1.07_dimGB_10000us", startUS, gain, tol, nil)
		if res.Fit.FinalUS <= startUS {
			t.Fatalf("tol %.0f%%: exposure must increase above %.0f us when G/B are below target at 100, got %.0f us",
				tol*100, startUS, res.Fit.FinalUS)
		}
	}
}

func TestRegression_V107DimGBHotLowRedNonLinear(t *testing.T) {
	// Same setup at 10000 us, but R at low codes (<= 20) runs 12% hotter than
	// ledDrive predicts (realistic LED PWM nonlinearity near low codes).
	// v1.07 dropped exposure to ~9269 us during confirm-trim to put R on target,
	// leaving G and B stranded below 0.50 at code 100. v1.08 must step R down
	// instead and raise exposure above 10000 us.
	const startUS = 10000.0
	gain := [3]float64{
		0.50 / ledDrive(13),
		0.44215 * (10000.0 / 9269.0) / ledDrive(100),
		0.43360 * (10000.0 / 9269.0) / ledDrive(100),
	}
	curve := func(ch, code int) float64 {
		base := ledDrive(code)
		if ch == 0 && code <= 20 {
			// Extra +12% around code 10-15 fading to 0 by code 25.
			return base * 1.12
		}
		return base
	}
	for _, tol := range []float64{0.01, 0.02} {
		res := runHardwareScenario(t, "v1.07_hotLowRed_10000us", startUS, gain, tol, curve)
		if res.Fit.FinalUS <= startUS {
			t.Fatalf("tol %.0f%%: exposure must go UP above %.0f us even with hot low-code R, got %.0f us",
				tol*100, startUS, res.Fit.FinalUS)
		}
	}
}

func TestRegression_R7G85B86_11567us(t *testing.T) {
	// Earlier hardware scenario: R~7, G~85, B~86 at 11567 us (v1.06 2.489% miss).
	const startUS = 11567.0
	gain := [3]float64{
		0.48756 / ledDrive(7),
		0.49805 / ledDrive(85),
		0.50172 / ledDrive(86),
	}
	for _, tol := range []float64{0.01, 0.02} {
		runHardwareScenario(t, "R7G85B86_11567us", startUS, gain, tol, nil)
	}
}

func TestRegression_R5G68B67_14289us(t *testing.T) {
	// Earlier hardware scenario: R~5, G~68, B~67 at 14289 us.
	const startUS = 14289.0
	gain := [3]float64{
		0.50 / ledDrive(5),
		0.50 / ledDrive(68),
		0.50 / ledDrive(67),
	}
	for _, tol := range []float64{0.01, 0.02} {
		runHardwareScenario(t, "R5G68B67_14289us", startUS, gain, tol, nil)
	}
}

func TestChooseCoarsePlanNeverDropsBelowDimFloor(t *testing.T) {
	cur := 10000.0
	pts := [3][]probePt{
		{{code: 13, level: 0.54}, {code: 25, level: 0.92}},
		{{code: 100, level: 0.477}},
		{{code: 100, level: 0.468}},
	}
	plan, ok := chooseCoarsePlan(cur, pts, 0.50)
	if !ok {
		t.Fatal("expected coarse plan")
	}
	usDim100, _, _, _, _, okB := exposureBounds(cur, pts, 0.50)
	if !okB {
		t.Fatal("expected bounds")
	}
	if plan.us < usDim100*0.999 {
		t.Fatalf("plan exposure %.0f us dropped below dim-channel floor %.0f us", plan.us, usDim100)
	}
	if plan.floorUS < usDim100*0.999 {
		t.Fatalf("plan floorUS %.0f us below dim-channel floor %.0f us", plan.floorUS, usDim100)
	}
}

func TestSolverRecoveryRaisesExposureWhenGBLimitedMidSolve(t *testing.T) {
	// Simulate a non-linear G/B channel that is slightly dimmer near code 100
	// than expected from mid-codes, so the solver initially picks an exposure
	// where G/B hit code 100 Limited during solving and must raise exposure.
	const startUS = 10000.0
	gain := [3]float64{
		0.50 / ledDrive(13),
		0.50 / ledDrive(92),
		0.50 / ledDrive(92),
	}
	curve := func(ch, code int) float64 {
		base := ledDrive(code)
		if ch > 0 && code >= 80 {
			// Thermal droop near high duty cycle: 14% dimmer at 80..100.
			return base * 0.86
		}
		return base
	}
	for _, tol := range []float64{0.01, 0.02} {
		res := runHardwareScenario(t, "solver_droop_GB_10000us", startUS, gain, tol, curve)
		if res.Fit.FinalUS <= startUS {
			t.Fatalf("tol %.0f%%: expected exposure > %.0f us after mid-solve recovery, got %.0f us",
				tol*100, startUS, res.Fit.FinalUS)
		}
	}
}
