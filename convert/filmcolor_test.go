package convert

import (
	"math"
	"testing"

	"basetool/colorsci"
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

func afterProfile(neg [3]float64) [3]float64 {
	return colorsci.ApplyCam(DefaultXT3Trichrome, 1, neg)
}

func invertScanned(neg [3]float64, gam [3]float64, expo float32) [3]float64 {
	return invertPixel(afterProfile(neg), gam, expo)
}

func stockExpo(s Stock) float32 {
	neg := SceneToNeg([3]float64{MidGrey, MidGrey, MidGrey}, Options{Stock: s})
	bestE, bestErr := float32(1), float32(1e9)
	for e := float32(-1); e <= 4; e += 0.05 {
		v := invertScanned(neg, s.Gamma, e)
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
		v := invertScanned(neg, s.Gamma, expo)
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
		return invertScanned(SceneToNeg(src, Options{Stock: s}), s.Gamma, stockExpo(s))
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

func TestAsShotWBFixesTungstenGrey(t *testing.T) {
	s, _ := StockByName("Portra 400")
	expo := stockExpo(s)
	// Tungsten-ish camera-RGB of a grey card: raw red high, blue low.
	raw := [3]float64{0.36, 0.18, 0.072}
	// cam_mul that restores the grey (green-normalized: G/R, 1, G/B).
	mul := [4]float32{float32(raw[1] / raw[0]), 1, float32(raw[1] / raw[2]), 1}

	noWB := invertScanned(SceneToNeg(raw, Options{Stock: s}), s.Gamma, expo)
	with := invertScanned(SceneToNeg(raw, Options{Stock: s, AsShotWB: true, CamMul: mul}), s.Gamma, expo)
	ref := invertScanned(SceneToNeg([3]float64{MidGrey, MidGrey, MidGrey}, Options{Stock: s}), s.Gamma, expo)

	t.Logf("no WB  inv=%.3f/%.3f/%.3f  R/B=%.2f", noWB[0], noWB[1], noWB[2], noWB[0]/math.Max(noWB[2], 1e-6))
	t.Logf("with WB inv=%.3f/%.3f/%.3f", with[0], with[1], with[2])
	t.Logf("ref     inv=%.3f/%.3f/%.3f", ref[0], ref[1], ref[2])

	if !(noWB[0] > noWB[2]*1.5) {
		t.Fatalf("expected yellow-brown tungsten cast without WB, got %v", noWB)
	}
	for c := 0; c < 3; c++ {
		if math.Abs(with[c]-ref[c]) > 0.03 {
			t.Fatalf("as-shot WB should match a balanced grey: got %v want %v", with, ref)
		}
	}

	// ProcessPixelsFast must match SceneToNeg.
	buf := []float64{raw[0], raw[1], raw[2]}
	ProcessPixelsFast(buf, 1, 1, Options{Stock: s, AsShotWB: true, CamMul: mul, BorderPct: 0})
	neg := SceneToNeg(raw, Options{Stock: s, AsShotWB: true, CamMul: mul})
	for c := 0; c < 3; c++ {
		if math.Abs(buf[c]-neg[c]) > 2e-3 {
			t.Fatalf("fast vs SceneToNeg: %v vs %v", buf, neg)
		}
	}
}

func TestAsShotScalesRejectJunk(t *testing.T) {
	if _, _, _, ok := asShotScales([4]float32{0, 0, 0, 0}); ok {
		t.Fatal("zero cam_mul should be rejected")
	}
	if _, _, _, ok := asShotScales([4]float32{1, 0, 1, 0}); ok {
		t.Fatal("zero green should be rejected")
	}
	rm, gm, bm, ok := asShotScales([4]float32{2.4, 1.2, 1.8, 1.2})
	if !ok || math.Abs(rm-2) > 1e-6 || gm != 1 || math.Abs(bm-1.5) > 1e-6 {
		t.Fatalf("got %v %v %v ok=%v", rm, gm, bm, ok)
	}
}

func TestAutoEVScaleLeavesMidGrey(t *testing.T) {
	const w, h = 16, 16
	rgb := make([]float64, w*h*3)
	for i := range rgb {
		rgb[i] = MidGrey
	}
	got := AutoEVScale(rgb, w, h)
	if math.Abs(got-1) > 0.08 {
		t.Fatalf("mid-grey frame should stay put, scale=%v", got)
	}
}

func TestAutoEVScaleLiftsDarkFrame(t *testing.T) {
	const w, h = 16, 16
	rgb := make([]float64, w*h*3)
	for i := range rgb {
		rgb[i] = 0.02
	}
	got := AutoEVScale(rgb, w, h)
	want := MidGrey / 0.02
	if math.Abs(got-want) > 0.8 {
		t.Fatalf("dark frame scale=%v want ~%v", got, want)
	}
}

func TestSceneRGBSkipsSecondWB(t *testing.T) {
	s, _ := StockByName("Portra 400")
	grey := [3]float64{MidGrey, MidGrey, MidGrey}
	mul := [4]float32{2, 1, 3, 1}
	with := SceneToNeg(grey, Options{Stock: s, AsShotWB: true, CamMul: mul, SceneRGB: true})
	plain := SceneToNeg(grey, Options{Stock: s})
	for c := 0; c < 3; c++ {
		if math.Abs(with[c]-plain[c]) > 1e-6 {
			t.Fatalf("SceneRGB must not re-apply cam_mul: %v vs %v", with, plain)
		}
	}
}

func TestGreenStaysGreenAfterInvert(t *testing.T) {
	// Linearised lime-green (the frog body). Must not rotate to mustard.
	src := [3]float64{0.143, 0.159, 0.045}
	srcRG := src[0] / src[1]
	for _, name := range []string{"Portra 400", "Ektar 100", "Gold 200"} {
		s, _ := StockByName(name)
		inv := invertScanned(SceneToNeg(src, Options{Stock: s}), s.Gamma, stockExpo(s))
		rg := inv[0] / inv[1]
		t.Logf("%s inv=%.3f/%.3f/%.3f R/G=%.3f (src R/G=%.3f)", name, inv[0], inv[1], inv[2], rg, srcRG)
		if rg > srcRG+0.12 {
			t.Errorf("%s rotated green toward yellow: R/G=%.3f src=%.3f inv=%v", name, rg, srcRG, inv)
		}
	}
}

func TestDIRPreservesGreyRamp(t *testing.T) {
	for _, name := range []string{"Portra 400", "Ektar 100", "Gold 200", "UltraMax 400", "Vision3 500T 5219", "Vision3 250D 5207"} {
		lk := lookFor(name)
		for _, e := range []float64{-1.5, -0.5, 0, 0.5, 1.2} {
			rel := [3]float64{e, e, e}
			out := applyLogDIR(rel, lk)
			for c := 0; c < 3; c++ {
				if math.Abs(out[c]-e) > 1e-9 {
					t.Fatalf("%s DIR moved a grey at rel=%.2f: %v", name, e, out)
				}
			}
		}
	}
}

func TestFilmSensKeepsGrey(t *testing.T) {
	grey := [3]float64{0.18, 0.18, 0.18}
	for _, s := range Stocks() {
		m := filmExposureMatrix(s.Name)
		v := m.Apply(grey)
		dev := math.Abs(v[0]-v[1]) + math.Abs(v[1]-v[2])
		if dev > 1e-9 {
			t.Errorf("%s film matrix moved a grey: %v", s.Name, v)
		}
	}
}

func TestStillPaperKeepsGrey(t *testing.T) {
	for _, lin := range []float64{0.04, 0.18, 0.50} {
		c := CineonEncode(lin)
		r, g, b := ApplyStillPaper(c, c, c)
		dev := math.Abs(r-g) + math.Abs(g-b)
		if dev > 1e-9 {
			t.Fatalf("paper LUT split a grey at lin=%.2f: %v %v %v", lin, r, g, b)
		}
		if r <= 0 || r >= 1 {
			t.Fatalf("paper LUT grey out of range at lin=%.2f: %v", lin, r)
		}
	}
	// Mid-grey should land near a display mid (~0.4-0.6 in gamma 2.4).
	c := CineonEncode(MidGrey)
	r, _, _ := ApplyStillPaper(c, c, c)
	if r < 0.35 || r > 0.65 {
		t.Fatalf("paper mid-grey display %.3f, expected ~0.5", r)
	}
}

func TestCamXYZNotApplied(t *testing.T) {
	// A colour-space matrix must not run: Convert stays in camera-native RGB.
	s, _ := StockByName("Portra 400")
	src := [3]float64{0.05, 0.40, 0.02}
	fake := [4][3]float32{
		{2, -0.8, -0.2},
		{-0.4, 1.6, -0.2},
		{-0.1, -0.5, 1.6},
		{0, 0, 0},
	}
	a := SceneToNeg(src, Options{Stock: s, AsShotWB: true, CamMul: [4]float32{1, 1, 1, 1}, CamXYZ: fake})
	b := SceneToNeg(src, Options{Stock: s})
	for c := 0; c < 3; c++ {
		if math.Abs(a[c]-b[c]) > 1e-9 {
			t.Fatalf("CamXYZ must not change camera-native input: %v vs %v", a, b)
		}
	}
}
