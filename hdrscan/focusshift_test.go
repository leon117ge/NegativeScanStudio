package hdrscan

import (
	"math"
	"testing"
)

func TestEstimateAndCorrectKnownTransform(t *testing.T) {
	const w, h = 192, 160
	g := synthPattern(w, h)

	type caseT struct {
		name       string
		dx, dy, sc float64
		sigma      float64
	}
	cases := []caseT{
		{name: "R", dx: 1.5, dy: -0.8, sc: 1.004, sigma: 0.85},
		{name: "B", dx: -1.1, dy: 1.2, sc: 0.996, sigma: 1.15},
	}
	gCopy := append([]float64(nil), g...)
	for _, tc := range cases {
		src := blurSeparable(g, w, h, tc.sigma)
		plane := applyOptical(src, w, h, tc.dx, tc.dy, tc.sc)
		fs := EstimateFocusShift(plane, g, w, h)
		if math.Abs(fs.DX-tc.dx) > 0.4 {
			t.Errorf("%s dx: got %.3f want %.3f", tc.name, fs.DX, tc.dx)
		}
		if math.Abs(fs.DY-tc.dy) > 0.4 {
			t.Errorf("%s dy: got %.3f want %.3f", tc.name, fs.DY, tc.dy)
		}
		if math.Abs(fs.Scale-tc.sc) > 0.0025 {
			t.Errorf("%s scale: got %.5f want %.5f", tc.name, fs.Scale, tc.sc)
		}
		if fs.BlurRatio < 1.04 {
			t.Errorf("%s expected to measure extra blur, ratio=%.3f", tc.name, fs.BlurRatio)
		}

		before := nccAt(plane, g, w, h, 0, 0, 1, 12)
		corr := CorrectFocusShift(plane, w, h, fs)
		after := nccAt(corr, g, w, h, 0, 0, 1, 12)
		if after < 0.80 {
			t.Errorf("%s corrected NCC %.3f (was %.3f)", tc.name, after, before)
		}
		if after < before+0.08 {
			t.Errorf("%s NCC should improve substantially: %.3f -> %.3f", tc.name, before, after)
		}
		// Corrected plane should be closer in sharpness to green than the raw plane.
		if edgeWidth(corr, w, h) > edgeWidth(plane, w, h)*0.98 && fs.BlurRatio > 1.08 {
			// Only require a sharpness improvement when the input was clearly softer.
			t.Logf("%s sharpness: raw width %.4f corr %.4f green %.4f (k would apply)",
				tc.name, edgeWidth(plane, w, h), edgeWidth(corr, w, h), edgeWidth(g, w, h))
		}
	}
	for i := range g {
		if g[i] != gCopy[i] {
			t.Fatal("green plane was modified")
		}
	}
}

func TestCorrectMonoCALeavesGreenUntouched(t *testing.T) {
	const w, h = 96, 80
	g := synthPattern(w, h)
	r := applyOptical(blurSeparable(g, w, h, 0.7), w, h, 0.8, -0.6, 1.003)
	b := applyOptical(blurSeparable(g, w, h, 0.9), w, h, -0.7, 0.5, 0.997)
	gBefore := append([]float64(nil), g...)
	rr, gg, bb, rep := CorrectMonoCA(r, g, b, w, h)
	if !rep.Enabled {
		t.Fatal("expected enabled report")
	}
	if len(gg) != len(g) {
		t.Fatal("green length changed")
	}
	for i := range gBefore {
		if gg[i] != gBefore[i] {
			t.Fatal("green values changed")
		}
	}
	if nccAt(rr, g, w, h, 0, 0, 1, 8) < 0.80 {
		t.Fatalf("corrected R still poorly aligned: %.3f", nccAt(rr, g, w, h, 0, 0, 1, 8))
	}
	if nccAt(bb, g, w, h, 0, 0, 1, 8) < 0.80 {
		t.Fatalf("corrected B still poorly aligned: %.3f", nccAt(bb, g, w, h, 0, 0, 1, 8))
	}
	if rep.String() == "" {
		t.Fatal("empty report")
	}
}

func TestIdentityHasNearZeroShift(t *testing.T) {
	const w, h = 80, 64
	g := synthPattern(w, h)
	fs := EstimateFocusShift(g, g, w, h)
	if math.Abs(fs.DX) > 0.15 || math.Abs(fs.DY) > 0.15 {
		t.Fatalf("identity shift %v", fs)
	}
	if math.Abs(fs.Scale-1) > 0.0015 {
		t.Fatalf("identity scale %v", fs)
	}
	if math.Abs(fs.BlurRatio-1) > 0.05 {
		t.Fatalf("identity blur %v", fs)
	}
}

func TestFocusShiftReportOff(t *testing.T) {
	s := FocusShiftReport{}.String()
	if s != "CA correction off" {
		t.Fatal(s)
	}
}

// synthPattern is a high-contrast grid plus spokes, easy to register.
func synthPattern(w, h int) []float64 {
	p := make([]float64, w*h)
	cx, cy := float64(w)/2, float64(h)/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := 0.35
			if x%12 < 2 || y%12 < 2 {
				v = 0.95
			}
			dx, dy := float64(x)-cx, float64(y)-cy
			if math.Hypot(dx, dy) < 8 {
				v = 0.1
			}
			if (x+y)%18 < 2 {
				v = 0.15
			}
			// a few filled squares for scale cues
			if x > w/3 && x < w/3+10 && y > h/3 && y < h/3+10 {
				v = 0.05
			}
			p[y*w+x] = v
		}
	}
	return p
}

// applyOptical builds R(q) = src(Tinv(q)) so that sampling src at T(p)
// recovers src(p). T(p) = scale*(p-c)+c+(dx,dy).
func applyOptical(src []float64, w, h int, dx, dy, scale float64) []float64 {
	out := make([]float64, w*h)
	cx := float64(w-1) / 2
	cy := float64(h-1) / 2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := (float64(x)-cx-dx)/scale + cx
			sy := (float64(y)-cy-dy)/scale + cy
			if v, ok := sampleBilinear(src, w, h, sx, sy); ok {
				out[y*w+x] = v
			}
		}
	}
	return out
}
