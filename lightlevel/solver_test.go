package lightlevel

import (
	"basetool/colorsci"
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

type responseRig struct {
	values      [3]int
	gain        [3]float64
	power       float64
	hot         bool
	noise       float64
	shots       int
	verifyDrift float64
	codes       [3][]int
}

func (r *responseRig) SetAll(_ [3]bool, v [3]int) error { r.values = v; return nil }
func (r *responseRig) Name() string                     { return "deterministic response rig" }
func (r *responseRig) Mono() bool                       { return false }
func (r *responseRig) Prepare() (string, error)         { return "fixed test exposure", nil }
func (r *responseRig) Shoot(_ context.Context, ch int, prefix string) (*Image, error) {
	r.shots++
	r.codes[ch] = append(r.codes[ch], r.values[ch])
	level := r.gain[ch] * math.Pow(float64(r.values[ch])/100, r.power)
	level += r.noise * math.Sin(float64(r.shots)*1.7)
	if strings.Contains(prefix, "verify") {
		level += r.verifyDrift
	}
	im := &Image{W: 20, H: 20, V: make([]float32, 400), Clip: make([]bool, 400)}
	for i := range im.V {
		v := level
		if r.hot && i%20 == 0 {
			v = 3.4 * level
		}
		if v >= 1 {
			v = 1
			im.Clip[i] = true
		}
		im.V[i] = float32(v)
	}
	return im, nil
}
func runRig(t *testing.T, r *responseRig, p Params) Result {
	t.Helper()
	p.Settle = time.Nanosecond
	p.Area = Rect{0, 0, 1, 1}
	res, err := Run(context.Background(), r, r, p, [3]bool{true, true, true}, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	return res
}
func TestHotSpotDoesNotCause100To50Bounce(t *testing.T) {
	r := &responseRig{gain: [3]float64{1, .5, 1}, power: 1, hot: true}
	res := runRig(t, r, Params{Start: [3]int{50, 100, 50}, MaxIter: 10})
	if !res.Converged() {
		t.Fatalf("isolated 5%% hot spots blocked reachable base: %+v", res.Problems())
	}
	for _, v := range r.codes[1] {
		if v < 95 {
			t.Fatalf("hot spot halved a correctly exposed base: %v", r.codes[1])
		}
	}
}
func TestCoarseIntegerCodeMustNotClaimSuccess(t *testing.T) {
	r := &responseRig{gain: [3]float64{3, 1, 1}, power: 1}
	res := runRig(t, r, Params{Start: [3]int{17, 50, 50}, Tol: .005, MaxIter: 10})
	if res.Converged() {
		t.Fatalf("0.48/0.51 cannot reach 0.5 +/-0.5%%, got %v", res.Readings)
	}
}
func TestLastRoundNeverReportsAnUncapturedCode(t *testing.T) {
	r := &responseRig{gain: [3]float64{1, 1, 1}, power: 2}
	res := runRig(t, r, Params{Start: [3]int{20, 20, 20}, MaxIter: 1})
	for c := 0; c < 3; c++ {
		want := r.gain[c] * math.Pow(float64(res.Values[c])/100, r.power)
		if math.Abs(res.Readings[c]-want) > 1e-6 {
			t.Fatalf("reported code %d has reading %.4f, actually %.4f", res.Values[c], res.Readings[c], want)
		}
	}
}

func TestNonlinearPWMAndNoise(t *testing.T) {
	for _, power := range []float64{.65, 1.8, 2.6} {
		r := &responseRig{gain: [3]float64{1.3, .9, .7}, power: power, noise: .0008}
		res := runRig(t, r, Params{Start: [3]int{30, 30, 30}, MaxIter: 10})
		if !res.Converged() {
			t.Fatalf("power %.2f: %v", power, res.Problems())
		}
		for c, codes := range r.codes {
			if len(codes) < 2 || codes[len(codes)-1] != res.Values[c] {
				t.Fatalf("missing fresh final capture: %v", codes)
			}
			if math.Abs(res.Post[c]-.5) > .01 {
				t.Fatalf("false success: %v", res.Post)
			}
		}
	}
}
func TestUnreachableRangeStopsWithoutBurningRounds(t *testing.T) {
	for _, tc := range []struct {
		gain    float64
		code    int
		message string
	}{{.2, 100, "increase camera exposure"}, {200, 1, "reduce camera exposure"}, {0, 100, "increase camera exposure"}} {
		r := &responseRig{gain: [3]float64{tc.gain, 1, 1}, power: 1}
		res := runRig(t, r, Params{Start: [3]int{tc.code, 50, 50}, MaxIter: 10})
		if res.Converged() || res.Rounds != 1 || !res.Verified || !strings.Contains(res.Reason, tc.message) {
			t.Fatalf("range failure not clear/early: %+v", res)
		}
	}
}
func TestMatrixCrosstalkAndIntegerNeighbourSelection(t *testing.T) {
	r := &responseRig{gain: [3]float64{1.8, 1.4, 1}, power: 1}
	m := colorsci.Mat3{{3, -2, 0}, {-.5, 1.5, 0}, {0, 0, 1}}
	res := runRig(t, r, Params{Start: [3]int{28, 35, 50}, MaxIter: 10, Matrix: m})
	if !res.Converged() || deviation(m.Apply(res.Readings), .5) > .02 {
		t.Fatalf("matrix-after-profile did not converge: %v %v", res.Post, res.Problems())
	}
	// Independently enumerate the measured response model, not solver internals.
	best := math.Inf(1)
	for a := res.Values[0] - 1; a <= res.Values[0]+1; a++ {
		for b := res.Values[1] - 1; b <= res.Values[1]+1; b++ {
			for c := res.Values[2] - 1; c <= res.Values[2]+1; c++ {
				v := [3]float64{float64(float32(1.8 * float64(a) / 100)), float64(float32(1.4 * float64(b) / 100)), float64(float32(float64(c) / 100))}
				best = math.Min(best, deviation(m.Apply(v), .5))
			}
		}
	}
	if deviation(res.Post, .5) > best+1e-6 {
		t.Fatalf("neighbour refinement not minimax: %.6f vs %.6f", deviation(res.Post, .5), best)
	}
}
func TestFinalVerificationRejectsChangedMeasurement(t *testing.T) {
	r := &responseRig{gain: [3]float64{1, 1, 1}, power: 1, verifyDrift: .03}
	res := runRig(t, r, Params{Start: [3]int{50, 50, 50}, MaxIter: 10})
	if res.Converged() || !res.Verified || !strings.Contains(res.Reason, "measurement changed") {
		t.Fatalf("stale cached success: %+v", res)
	}
}
func TestDefaultTenRounds(t *testing.T) {
	var p Params
	p.Defaults()
	if p.MaxIter != 10 {
		t.Fatal(p.MaxIter)
	}
}

func TestResponseSweepFromBadStartingCodes(t *testing.T) {
	for n := 0; n < 40; n++ {
		r := &responseRig{power: .65 + float64(n%9)*.25}
		for c := range r.gain {
			r.gain[c] = .55 + float64((n*3+c*5)%17)*.1
		}
		feasible := true
		for c := range r.gain {
			best := 1.0
			for code := 1; code <= 100; code++ {
				best = math.Min(best, math.Abs(r.gain[c]*math.Pow(float64(code)/100, r.power)/.5-1))
			}
			if best > .015 {
				feasible = false
			}
		}
		if !feasible {
			continue
		}
		start := [3]int{1, 50, 100}
		if n%2 == 1 {
			start = [3]int{100, 1, 20}
		}
		res := runRig(t, r, Params{Start: start, MaxIter: 10})
		if !res.Converged() {
			t.Fatalf("reachable sweep %d power %.2f gain %v start %v: %v", n, r.power, r.gain, start, res.Problems())
		}
	}
}

func TestDeadChannelDetectsMeasuredUpperBound(t *testing.T) {
	r := &responseRig{gain: [3]float64{0, 1, 1}, power: 1}
	res := runRig(t, r, Params{Start: [3]int{50, 50, 50}, MaxIter: 10})
	if !res.Limited[0] || res.Rounds > 3 || !strings.Contains(res.Reason, "code 100") {
		t.Fatalf("dead channel burned rounds: %+v", res)
	}
}

func TestDefaultToleranceRejectsCoarseCodes(t *testing.T) {
	r := &responseRig{gain: [3]float64{5.3, 1, 1}, power: 1}
	res := runRig(t, r, Params{Start: [3]int{10, 50, 50}, MaxIter: 10})
	if res.Converged() || math.Abs(res.Post[0]-.5) <= .01 || !strings.Contains(res.Reason, "Closest reachable") {
		t.Fatalf("coarse code falsely accepted at default 2%%: %+v", res)
	}
}

func TestAutoAreaSelectedOnceAfterSafeDeclip(t *testing.T) {
	r := &responseRig{gain: [3]float64{3, 3, 3}, power: 1}
	calls := 0
	area := Rect{.2, .2, .8, .8}
	p := Params{Start: [3]int{50, 50, 50}, MaxIter: 10, SelectArea: func(images [3]*Image) (Rect, error) {
		calls++
		for _, im := range images {
			rd, err := Measure(im, Centre)
			if err != nil || rd.BaseClipped() {
				t.Fatal("auto selected before usable samples")
			}
		}
		return area, nil
	}}
	res := runRig(t, r, p)
	if calls != 1 || res.Area != area || !res.Converged() {
		t.Fatalf("auto rectangle was not frozen: calls %d area %v problems %v", calls, res.Area, res.Problems())
	}
}
