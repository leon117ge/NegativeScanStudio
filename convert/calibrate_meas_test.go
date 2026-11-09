package convert

import (
	"fmt"
	"math"
	"testing"
)

func measPatch(t *testing.T, name string, meanD [3]float64, g GrainISO) {
	s, _ := StockByName("Vision3 500T 5219")
	w, h := 512, 512
	// Build flat transmittance from target D: T=0.5*10^(-D)
	// ProcessPixels expects linear scene then makes density - easier to inject density via fake:
	// Use ProcessPixelsFast on constant mid grey with EV so resulting mean D matches roughly.
	// Simpler: manually set density, apply grain only.
	d := make([]float64, w*h*3)
	for i := 0; i < w*h; i++ {
		for c := 0; c < 3; c++ {
			d[i*3+c] = meanD[c]
		}
	}
	applyGrainPoisson(d, w, h, s.Name, g, 42, 1, 1)
	// high-pass
	plane := func(c int) []float64 {
		p := make([]float64, w*h)
		for i := 0; i < w*h; i++ {
			p[i] = d[i*3+c]
		}
		return p
	}
	blur := func(p []float64, rad int) []float64 {
		tmp := make([]float64, len(p))
		out := make([]float64, len(p))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				var s float64
				var n float64
				for k := -rad; k <= rad; k++ {
					xx := x + k
					if xx < 0 {
						xx = 0
					}
					if xx >= w {
						xx = w - 1
					}
					s += p[y*w+xx]
					n++
				}
				tmp[y*w+x] = s / n
			}
		}
		for x := 0; x < w; x++ {
			for y := 0; y < h; y++ {
				var s float64
				var n float64
				for k := -rad; k <= rad; k++ {
					yy := y + k
					if yy < 0 {
						yy = 0
					}
					if yy >= h {
						yy = h - 1
					}
					s += tmp[yy*w+x]
					n++
				}
				out[y*w+x] = s / n
			}
		}
		return out
	}
	HF := [3][]float64{}
	for c := 0; c < 3; c++ {
		p := plane(c)
		lf := blur(p, 15)
		hf := make([]float64, w*h)
		for i := range hf {
			hf[i] = p[i] - lf[i]
		}
		HF[c] = hf
	}
	rms := func(a []float64) float64 {
		var s float64
		for _, v := range a {
			s += v * v
		}
		return math.Sqrt(s / float64(len(a)))
	}
	corr := func(a, b []float64) float64 {
		n := float64(len(a))
		var sa, sb float64
		for i := range a {
			sa += a[i]
			sb += b[i]
		}
		sa /= n
		sb /= n
		var sab, a2, b2 float64
		for i := range a {
			da, db := a[i]-sa, b[i]-sb
			sab += da * db
			a2 += da * da
			b2 += db * db
		}
		return sab / math.Sqrt(a2*b2)
	}
	t.Logf("[%s] RMS=%.5f/%.5f/%.5f corr=%.3f/%.3f/%.3f", name,
		rms(HF[0]), rms(HF[1]), rms(HF[2]),
		corr(HF[0], HF[1]), corr(HF[1], HF[2]), corr(HF[0], HF[2]))
	fmt.Printf("[%s] RMS=%.5f/%.5f/%.5f corr=%.3f/%.3f/%.3f\n", name,
		rms(HF[0]), rms(HF[1]), rms(HF[2]),
		corr(HF[0], HF[1]), corr(HF[1], HF[2]), corr(HF[0], HF[2]))
}

func TestMeasOldSim(t *testing.T) {
	measPatch(t, "old_mid", [3]float64{0.19, 0.38, 0.44}, GrainISO400)
	measPatch(t, "old_dense", [3]float64{0.51, 0.85, 0.96}, GrainISO400)
}
