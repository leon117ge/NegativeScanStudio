package convert

import (
	"fmt"
	"math"
	"testing"
)

// Flat mid-density target from visually flat 192-256 px patches of the
// 0212 500T scan (crop_mid peach wall / floor), not the whole 512 crop
// which includes shoe and bar edges.
var (
	targetMidD   = [3]float64{0.167, 0.396, 0.554}
	targetMidRMS = [3]float64{0.0154, 0.0222, 0.0546}
	targetMidRG  = 0.194
	targetMidGB  = 0.045
	targetMidRB  = 0.055
	targetMidAC  = [3]float64{2.1, 1.9, 1.8}

	targetHighD   = [3]float64{0.400, 0.796, 1.047}
	targetHighRMS = [3]float64{0.0133, 0.0238, 0.0520}

	targetLowD   = [3]float64{0.109, 0.278, 0.388}
	targetLowRMS = [3]float64{0.0194, 0.0321, 0.0706}
)

func TestGrainCalibrationISO400(t *testing.T) {
	s, _ := StockByName("Vision3 500T 5219")
	w, h := 512, 512
	_, st := SimulateFlatGrain(targetMidD, w, h, ScanFramePx, s.Name, GrainISO400, 42)
	t.Logf("ISO400 mid  RMS=%.5f/%.5f/%.5f corr=%.3f/%.3f/%.3f ac50=%.1f/%.1f/%.1f meanD=%.3f/%.3f/%.3f",
		st.RMS[0], st.RMS[1], st.RMS[2], st.CorrRG, st.CorrGB, st.CorrRB,
		st.AC50[0], st.AC50[1], st.AC50[2], st.MeanD[0], st.MeanD[1], st.MeanD[2])
	fmt.Printf("[ISO400 mid] RMS=%.5f/%.5f/%.5f corr=%.3f/%.3f/%.3f ac50=%.1f/%.1f/%.1f\n",
		st.RMS[0], st.RMS[1], st.RMS[2], st.CorrRG, st.CorrGB, st.CorrRB,
		st.AC50[0], st.AC50[1], st.AC50[2])

	const tol = 0.18 // 18 % — a shade looser than 15 % so seeds can vary
	for c, name := range []string{"R", "G", "B"} {
		rel := math.Abs(st.RMS[c]-targetMidRMS[c]) / targetMidRMS[c]
		if rel > tol {
			t.Errorf("RMS %s: got %.5f want %.5f (rel %.1f%%)", name, st.RMS[c], targetMidRMS[c], rel*100)
		}
	}
	checkCorr := func(got, want float64, name string) {
		// Absolute tolerance 0.08 or 18% relative, whichever is larger.
		lim := math.Max(0.08, math.Abs(want)*tol)
		if math.Abs(got-want) > lim {
			t.Errorf("corr %s: got %.3f want %.3f", name, got, want)
		}
	}
	checkCorr(st.CorrRG, targetMidRG, "RG")
	checkCorr(st.CorrGB, targetMidGB, "GB")
	checkCorr(st.CorrRB, targetMidRB, "RB")
	for c, name := range []string{"R", "G", "B"} {
		if math.Abs(st.AC50[c]-targetMidAC[c]) > 1.2 {
			t.Errorf("ac50 %s: got %.1f want %.1f", name, st.AC50[c], targetMidAC[c])
		}
	}

	_, hi := SimulateFlatGrain(targetHighD, w, h, ScanFramePx, s.Name, GrainISO400, 42)
	t.Logf("ISO400 high RMS=%.5f/%.5f/%.5f corr=%.3f/%.3f/%.3f ac50=%.1f/%.1f/%.1f",
		hi.RMS[0], hi.RMS[1], hi.RMS[2], hi.CorrRG, hi.CorrGB, hi.CorrRB,
		hi.AC50[0], hi.AC50[1], hi.AC50[2])
	fmt.Printf("[ISO400 high] RMS=%.5f/%.5f/%.5f corr=%.3f/%.3f/%.3f ac50=%.1f/%.1f/%.1f\n",
		hi.RMS[0], hi.RMS[1], hi.RMS[2], hi.CorrRG, hi.CorrGB, hi.CorrRB,
		hi.AC50[0], hi.AC50[1], hi.AC50[2])

	_, lo := SimulateFlatGrain(targetLowD, w, h, ScanFramePx, s.Name, GrainISO400, 42)
	t.Logf("ISO400 low  RMS=%.5f/%.5f/%.5f corr=%.3f/%.3f/%.3f ac50=%.1f/%.1f/%.1f",
		lo.RMS[0], lo.RMS[1], lo.RMS[2], lo.CorrRG, lo.CorrGB, lo.CorrRB,
		lo.AC50[0], lo.AC50[1], lo.AC50[2])
	fmt.Printf("[ISO400 low] RMS=%.5f/%.5f/%.5f corr=%.3f/%.3f/%.3f ac50=%.1f/%.1f/%.1f\n",
		lo.RMS[0], lo.RMS[1], lo.RMS[2], lo.CorrRG, lo.CorrGB, lo.CorrRB,
		lo.AC50[0], lo.AC50[1], lo.AC50[2])
}

func TestGrainISO100Ratio(t *testing.T) {
	s, _ := StockByName("Vision3 500T 5219")
	w, h := 512, 512
	_, a := SimulateFlatGrain(targetMidD, w, h, ScanFramePx, s.Name, GrainISO400, 42)
	_, b := SimulateFlatGrain(targetMidD, w, h, ScanFramePx, s.Name, GrainISO100, 42)
	t.Logf("ISO400 RMS=%.5f/%.5f/%.5f ac50=%.1f/%.1f/%.1f", a.RMS[0], a.RMS[1], a.RMS[2], a.AC50[0], a.AC50[1], a.AC50[2])
	t.Logf("ISO100 RMS=%.5f/%.5f/%.5f ac50=%.1f/%.1f/%.1f", b.RMS[0], b.RMS[1], b.RMS[2], b.AC50[0], b.AC50[1], b.AC50[2])
	// PGI-derived RMS ratio is 0.74; finer clouds leak a bit more HF, so
	// allow 0.55–0.90 around G.
	ratio := b.RMS[1] / a.RMS[1]
	if ratio < 0.55 || ratio > 0.90 {
		t.Errorf("ISO100/ISO400 G RMS ratio %.2f, expected ~0.74", ratio)
	}
	if b.AC50[1] >= a.AC50[1]*0.95 {
		t.Errorf("ISO100 should be finer: ac50 G %.1f vs ISO400 %.1f", b.AC50[1], a.AC50[1])
	}
}
