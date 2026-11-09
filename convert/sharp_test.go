package convert

import (
	"math"
	"testing"
)

// Grain must not blur image detail: a hard edge stays hard with grain on.
func TestGrainKeepsEdgeSharp(t *testing.T) {
	s, _ := StockByName("Portra 400")
	w, h := 6000, 500 // > 2.5 MP so the full-resolution grain path runs
	pix := make([]uint16, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint16(3000)
			if x >= w/2 {
				v = 20000
			}
			for c := 0; c < 3; c++ {
				pix[(y*w+x)*3+c] = v
			}
		}
	}
	width := func(g GrainISO) float64 {
		out := Process16Fast(append([]uint16(nil), pix...), w, h, Options{Stock: s, Grain: g, GrainSeed: 3, BorderPct: 0})
		prof := make([]float64, w)
		for y := h / 4; y < 3*h/4; y++ {
			for x := 0; x < w; x++ {
				prof[x] += float64(out[(y*w+x)*3+1])
			}
		}
		lo, hi := prof[w/2-200], prof[w/2+200]
		a, b := lo+0.1*(hi-lo), lo+0.9*(hi-lo)
		var x10, x90 int
		for x := w/2 - 200; x < w/2+200; x++ {
			if (prof[x]-a)*(prof[x-1]-a) <= 0 {
				x10 = x
			}
			if (prof[x]-b)*(prof[x-1]-b) <= 0 {
				x90 = x
			}
		}
		return math.Abs(float64(x90 - x10))
	}
	off, on := width(GrainOff), width(GrainISO400)
	t.Logf("10-90%% edge width: grain off %.0f px, ISO400 %.0f px", off, on)
	if on > off+2 {
		t.Fatalf("grain blurs the image: edge %v px vs %v px", on, off)
	}
}
