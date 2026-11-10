package convert

import (
	"math"
	"testing"

	"basetool/develop"
)

func invertPixel(neg [3]float64, gam [3]float64, expo float32) [3]float64 {
	ch := develop.DefaultChain()
	d := &ch.Nodes[0]
	d.V["base_r"], d.V["base_g"], d.V["base_b"] = BaseFS, BaseFS, BaseFS
	d.V["preset"], d.V["cast_on"], d.V["out_trans"], d.V["in_di"] = 0, 0, 0, 0
	ch.Nodes[1].Bypass = true
	p := &ch.Nodes[2]
	p.V["gam_r"], p.V["gam_g"], p.V["gam_b"] = gam[0], gam[1], gam[2]
	p.V["expo"] = float64(expo)
	p.V["bal_r"], p.V["bal_g"], p.V["bal_b"] = 0, 0, 0
	p.V["film_wb"], p.V["gray_on"], p.V["contrast"], p.V["out_mode"] = 0, 0, 0, 4
	for i := range ch.Nodes {
		if i >= 3 {
			ch.Nodes[i].Bypass = true
		}
	}
	r, g, b := ch.Compile(&develop.Context{}, -1).Pixel(float32(neg[0]), float32(neg[1]), float32(neg[2]))
	return [3]float64{float64(r), float64(g), float64(b)}
}

func stockExpo(s Stock) float32 {
	neg := SceneToNeg([3]float64{MidGrey, MidGrey, MidGrey}, Options{Stock: s})
	bestE, bestErr := float32(1), float32(1e9)
	for e := float32(-1); e <= 4; e += 0.05 {
		v := invertPixel(neg, s.Gamma, e)
		err := float32(math.Abs((v[0]+v[1]+v[2])/3 - MidGrey))
		if err < bestErr {
			bestErr, bestE = err, e
		}
	}
	return bestE
}

func TestGreyInvertsNeutral(t *testing.T) {
	for _, s := range Stocks() {
		expo := stockExpo(s)
		neg := SceneToNeg([3]float64{MidGrey, MidGrey, MidGrey}, Options{Stock: s})
		v := invertPixel(neg, s.Gamma, expo)
		dev := math.Abs(v[0]-v[1]) + math.Abs(v[1]-v[2])
		t.Logf("%s expo=%.2f neg=%.4f/%.4f/%.4f inv=%.4f/%.4f/%.4f dev=%.4f",
			s.FileTag, expo, neg[0], neg[1], neg[2], v[0], v[1], v[2], dev)
		if dev > 0.025 {
			t.Errorf("%s grey card not neutral after invert: %v (dev=%.4f)", s.Name, v, dev)
		}
		mean := (v[0] + v[1] + v[2]) / 3
		if math.Abs(mean-MidGrey) > 0.03 {
			t.Errorf("%s grey mean %.4f want ~%.2f", s.Name, mean, MidGrey)
		}
	}
}

func chromaOf(v [3]float64) float64 {
	// linear sRGB -> approx chroma in Lab a*b*
	X := 0.4124564*v[0] + 0.3575761*v[1] + 0.1804375*v[2]
	Y := 0.2126729*v[0] + 0.7151522*v[1] + 0.0721750*v[2]
	Z := 0.0193339*v[0] + 0.1191920*v[1] + 0.9503041*v[2]
	f := func(t float64) float64 {
		if t > 216.0/24389.0 {
			return math.Cbrt(t)
		}
		return (24389.0/27.0*t + 16) / 116
	}
	fx, fy, fz := f(X/0.95047), f(Y), f(Z/1.08883)
	a, b := 500*(fx-fy), 200*(fy-fz)
	return math.Hypot(a, b)
}

func TestStockDifferentiation(t *testing.T) {
	// Light skin (approx sRGB 194,150,130 linearised) and a saturated red.
	lin := func(c float64) float64 {
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	skin := [3]float64{lin(194.0 / 255), lin(150.0 / 255), lin(130.0 / 255)}
	red := [3]float64{lin(175.0 / 255), lin(54.0 / 255), lin(60.0 / 255)}

	portra, _ := StockByName("Portra 400")
	ektar, _ := StockByName("Ektar 100")
	gold, _ := StockByName("Gold 200")
	inv := func(s Stock, src [3]float64) [3]float64 {
		return invertPixel(SceneToNeg(src, Options{Stock: s}), s.Gamma, stockExpo(s))
	}
	ps, es, gs := inv(portra, skin), inv(ektar, skin), inv(gold, skin)
	pr, er := inv(portra, red), inv(ektar, red)
	t.Logf("skin Portra C*=%.2f RGB=%.3f/%.3f/%.3f", chromaOf(ps), ps[0], ps[1], ps[2])
	t.Logf("skin Ektar  C*=%.2f RGB=%.3f/%.3f/%.3f", chromaOf(es), es[0], es[1], es[2])
	t.Logf("skin Gold   C*=%.2f RGB=%.3f/%.3f/%.3f", chromaOf(gs), gs[0], gs[1], gs[2])
	t.Logf("red  Portra C*=%.2f  Ektar C*=%.2f", chromaOf(pr), chromaOf(er))

	if chromaOf(er) <= chromaOf(pr) {
		t.Errorf("Ektar red should be more saturated than Portra: %.2f vs %.2f", chromaOf(er), chromaOf(pr))
	}
	if chromaOf(es) <= chromaOf(ps)*0.95 {
		t.Errorf("Ektar skin should not be duller than Portra: %.2f vs %.2f", chromaOf(es), chromaOf(ps))
	}
	// Gold should be warmer than Portra: higher R relative to B on skin.
	goldRB := gs[0] / math.Max(gs[2], 1e-6)
	portraRB := ps[0] / math.Max(ps[2], 1e-6)
	t.Logf("skin R/B Gold=%.3f Portra=%.3f", goldRB, portraRB)
	if goldRB < portraRB {
		t.Errorf("Gold skin should be warmer (higher R/B) than Portra: %.3f vs %.3f", goldRB, portraRB)
	}
}

func TestSceneToNegMatchesFast(t *testing.T) {
	s, _ := StockByName("Ektar 100")
	o := Options{Stock: s, BorderPct: 0, Grain: GrainOff}
	src := [][3]float64{
		{0.18, 0.18, 0.18},
		{0.4, 0.25, 0.18},
		{0.05, 0.08, 0.20},
		{0.9, 0.85, 0.10},
	}
	for _, v := range src {
		a := SceneToNeg(v, o)
		rgb := []float64{v[0], v[1], v[2]}
		ProcessPixelsFast(rgb, 1, 1, o)
		for c := 0; c < 3; c++ {
			if math.Abs(a[c]-rgb[c]) > 2e-3 {
				t.Fatalf("mismatch at %v: SceneToNeg %v fast %v", v, a, rgb)
			}
		}
	}
}

func TestLookNeutralsUnchangedByDensityMix(t *testing.T) {
	lk := lookFor("Portra 400")
	d := [3]float64{0.7, 0.7, 0.7}
	out := applyLook(d, lk)
	for c := 0; c < 3; c++ {
		if math.Abs(out[c]-0.7) > 1e-6 {
			t.Fatalf("neutral density moved: %v", out)
		}
	}
}
