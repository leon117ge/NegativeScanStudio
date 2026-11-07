package hdrscan

import (
	"math"
	"math/rand"
	"testing"
)

// synth makes frame 0 as a horizontal log ramp (brightest channel from
// lo to hi of full scale) and brighter frames at the true ratios, with
// photon-like noise, clipped at white.
func synth(w, h int, lo, hi float64, ratios []float64, seed int64) []*Plane {
	rng := rand.New(rand.NewSource(seed))
	col := [3]float64{1, 0.7, 0.25}
	var out []*Plane
	for _, r := range ratios {
		p := &Plane{W: w, H: h, Pix: make([]uint16, w*h*3)}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				v := lo * math.Pow(hi/lo, float64(x)/float64(w-1)) * 65535
				for c := 0; c < 3; c++ {
					s := v * col[c] * r
					s += rng.NormFloat64() * (math.Sqrt(s*0.4) + 1.5)
					if s > 65535 {
						s = 65535
					}
					if s < 0 {
						s = 0
					}
					p.Pix[(y*w+x)*3+c] = uint16(s + 0.5)
				}
			}
		}
		out = append(out, p)
	}
	return out
}

func clippedPct(p *Plane, T float64) float64 {
	n := 0
	for i := 0; i < p.W*p.H; i++ {
		if float64(max3(p.Pix[3*i], p.Pix[3*i+1], p.Pix[3*i+2])) >= T {
			n++
		}
	}
	return 100 * float64(n) / float64(p.W*p.H)
}

func TestChainedRatio(t *testing.T) {
	T := 0.85 * 65535
	// frame 0 brightest channel from 0.045 to 0.979 FS (log ramp): +2 (x4.05) clips above
	// 0.21 FS (~50 % of the frame), +4 (x16.2) above 0.0525 (~95 %).
	for _, tc := range []struct {
		name  string
		evs   []int
		truth []float64 // true exposure ratios
		nom   []float64 // exposures given to the merge (nil = unknown)
	}{
		{"0/+2/+4 with exposure", []int{0, 2, 4}, []float64{1, 4.05, 16.2}, []float64{1, 4, 16}},
		{"0/+2/+4 no exposure", []int{0, 2, 4}, []float64{1, 4.05, 16.2}, nil},
		{"0/+2/+3 with exposure", []int{0, 2, 3}, []float64{1, 4.05, 8.1}, []float64{1, 4, 8}},
		{"0/+2/+3 no exposure", []int{0, 2, 3}, []float64{1, 4.05, 8.1}, nil},
	} {
		fr := synth(1600, 600, 0.045, 0.979, tc.truth, 7)
		t.Logf("%s: clipped in +2 %.1f%%, in %+d %.1f%%", tc.name, clippedPct(fr[1], T), tc.evs[2], clippedPct(fr[2], T))
		o := DefaultOptions()
		o.Exposure = tc.nom
		_, sts, err := MergeN(fr, o)
		if err != nil {
			t.Fatal(err)
		}
		for k, st := range sts {
			want := tc.truth[k+1]
			t.Logf("  %s", st)
			if st.Dropped || math.Abs(st.Ratio/want-1) > 0.02 {
				t.Errorf("%s %s: ratio %.4f, want %.4f ±2%%", tc.name, st.Label, st.Ratio, want)
			}
			if st.Label != EVLabel(float64(tc.evs[k+1])) {
				t.Errorf("%s: label %q, want +%d", tc.name, st.Label, tc.evs[k+1])
			}
		}
	}
}

// A brighter frame clipped everywhere: exposure fallback, or left out.
func TestRatioFallback(t *testing.T) {
	fr := synth(800, 400, 0.22, 0.3, []float64{1, 4.05, 16.2}, 3) // pure base: +2 and +4 fully clipped
	o := DefaultOptions()
	o.Exposure = []float64{1, 4, 16}
	out, sts, err := MergeN(fr, o)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range sts {
		t.Logf("%s", st)
	}
	if sts[1].Dropped || math.Abs(sts[1].Ratio-16) > 0.01 || sts[1].Warning == "" {
		t.Errorf("+4 fallback: %+v", sts[1])
	}
	for i := range out {
		if out[i] != fr[0].Pix[i] {
			t.Fatalf("pixel %d differs from frame 0", i/3)
		}
	}
	o.Exposure = nil
	_, sts, err = MergeN(fr, o)
	if err != nil {
		t.Fatal(err)
	}
	if sts[0].Ratio != 4 || !sts[1].Dropped {
		t.Errorf("no exposure: want +2 assumed 4 and +4 left out, got %v / %v", sts[0], sts[1])
	}
}
