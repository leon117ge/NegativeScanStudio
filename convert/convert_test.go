package convert

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"basetool/colorsci"
	"basetool/develop"
	"basetool/scanmeta"
)

func TestStocksAndCurves(t *testing.T) {
	if len(Stocks()) != 6 {
		t.Fatal(len(Stocks()))
	}
	for _, name := range []string{"Ektar 100", "Portra 400", "Gold 200", "UltraMax 400"} {
		if CurveFor(name) == nil {
			t.Fatalf("missing full curve for %s", name)
		}
	}
	if CurveFor("Vision3 500T 5219") != nil || CurveFor("Vision3 250D 5207") != nil {
		t.Fatal("Vision3 should use logistic fallback until full D-logH is digitised")
	}
	t.Log("\n" + curveStatus())
}

func TestPCHIPMonotoneMid(t *testing.T) {
	cs := CurveFor("Ektar 100")
	d0 := cs.densityAbove(cs.LogHMid, 1)
	if math.Abs(d0-0.75) > 0.08 {
		t.Fatalf("G at LogHMid: got %.3f want ~0.75", d0)
	}
	// Increasing logH -> increasing density
	dLo := cs.densityAbove(cs.LogHMid-1, 1)
	dHi := cs.densityAbove(cs.LogHMid+1, 1)
	if !(dLo < d0 && d0 < dHi) {
		t.Fatalf("not monotone: %.3f %.3f %.3f", dLo, d0, dHi)
	}
}

func TestBaseAndMid(t *testing.T) {
	s, _ := StockByName("Ektar 100")
	v := SceneToNeg([3]float64{1e-6, 1e-6, 1e-6}, Options{Stock: s})
	for c := 0; c < 3; c++ {
		if math.Abs(v[c]-BaseFS) > 0.05 {
			t.Fatalf("shadow near base: %v", v)
		}
	}
	m := SceneToNeg([3]float64{MidGrey, MidGrey, MidGrey}, Options{Stock: s})
	for c := 0; c < 3; c++ {
		if !(m[c] < BaseFS-0.05) {
			t.Fatalf("mid denser than base: %v", m)
		}
	}
}

func TestBorderAndNaming(t *testing.T) {
	s, _ := StockByName("Portra 400")
	dir := t.TempDir()
	src := filepath.Join(dir, "DSCF1234.RAF")
	os.WriteFile(src, []byte("x"), 0644)
	p1 := OutputPath(src, s)
	if filepath.Base(p1) != "DSCF1234_Portra400.tiff" {
		t.Fatalf("got %s", p1)
	}
	os.WriteFile(p1, []byte("y"), 0644)
	p2 := OutputPath(src, s)
	if filepath.Base(p2) != "DSCF1234_Portra400_2.tiff" {
		t.Fatalf("got %s", p2)
	}
}

func TestMetadata(t *testing.T) {
	s, _ := StockByName("Gold 200")
	js, err := Metadata(Options{Stock: s, Grain: GrainISO400}).JSON("test")
	if err != nil {
		t.Fatal(err)
	}
	var got scanmeta.Metadata
	json.Unmarshal([]byte(js), &got)
	if got.FilmBase == nil || *got.FilmBase != [3]float64{0.5, 0.5, 0.5} {
		t.Fatalf("%+v", got)
	}
}

func pluginInvert(negR, negG, negB float32, gam [3]float64, expo float32) (float32, float32, float32) {
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
	return ch.Compile(&develop.Context{}, -1).Pixel(negR, negG, negB)
}

func findExpo(s Stock) float32 {
	neg := SceneToNeg([3]float64{MidGrey, MidGrey, MidGrey}, Options{Stock: s})
	bestE, bestErr := float32(1), float32(1e9)
	for e := float32(-1); e <= 4; e += 0.05 {
		lr, lg, lb := pluginInvert(float32(neg[0]), float32(neg[1]), float32(neg[2]), s.Gamma, e)
		err := float32(math.Abs(float64((lr+lg+lb)/3) - MidGrey))
		if err < bestErr {
			bestErr, bestE = err, e
		}
	}
	return bestE
}

func TestRoundTripEktarCSV(t *testing.T) {
	s, _ := StockByName("Ektar 100")
	expo := findExpo(s)
	t.Logf("Ektar 100 (full CSV) expo=%.2f", expo)
	prev := float32(-1)
	for _, stops := range []float64{-4, -3, -2, -1, 0, 1, 2, 3} {
		L := MidGrey * math.Pow(2, stops)
		neg := SceneToNeg([3]float64{L, L, L}, Options{Stock: s})
		or, og, ob := pluginInvert(float32(neg[0]), float32(neg[1]), float32(neg[2]), s.Gamma, expo)
		mean := (or + og + ob) / 3
		t.Logf("stops=%+.0f L=%.4f neg=%.4f/%.4f/%.4f out=%.4f/%.4f/%.4f mean=%.4f",
			stops, L, neg[0], neg[1], neg[2], or, og, ob, mean)
		if prev >= 0 && mean+1e-3 < prev {
			t.Fatalf("non-monotonic at %v", stops)
		}
		prev = mean
	}
}

func TestGrainChangesImage(t *testing.T) {
	s, _ := StockByName("Portra 400")
	w, h := 64, 48
	mk := func(g GrainISO) []float64 {
		rgb := make([]float64, w*h*3)
		for i := 0; i < w*h; i++ {
			rgb[i*3], rgb[i*3+1], rgb[i*3+2] = MidGrey, MidGrey, MidGrey
		}
		ProcessPixels(rgb, w, h, Options{Stock: s, Grain: g, GrainSeed: 42, BorderPct: 0})
		return rgb
	}
	a, b := mk(GrainOff), mk(GrainISO400)
	diff := 0.0
	for i := range a {
		diff += math.Abs(a[i] - b[i])
	}
	if diff < 1e-3 {
		t.Fatal("grain should change pixels")
	}
}

func TestApplyCam(t *testing.T) {
	s, _ := StockByName("Gold 200")
	m := colorsci.Mat3{{1.15, -0.08, 0}, {-0.04, 1.08, -0.04}, {0, -0.06, 1.12}}
	a := SceneToNeg([3]float64{0.2, 0.2, 0.2}, Options{Stock: s})
	b := SceneToNeg([3]float64{0.2, 0.2, 0.2}, Options{Stock: s, ApplyCamProfile: true, CamMatrix: m, CamStrength: 1})
	if a == b {
		t.Fatal("cam")
	}
}

func TestGrainContrastNeutral(t *testing.T) {
	s, _ := StockByName("Portra 400")
	w, h := 256, 128
	mk := func(g GrainISO) []float64 {
		rgb := make([]float64, w*h*3)
		for y := 0; y < h; y++ {
			L := math.Pow(10, -2+4*float64(y)/float64(h-1)) // log ramp
			for x := 0; x < w; x++ {
				i := (y*w + x) * 3
				rgb[i], rgb[i+1], rgb[i+2] = L, L, L
			}
		}
		ProcessPixels(rgb, w, h, Options{Stock: s, Grain: g, GrainSeed: 7, BorderPct: 0, GrainStrength: 1})
		return rgb
	}
	off, on := mk(GrainOff), mk(GrainISO400)
	// Global mean within 0.5%
	var mo, mn float64
	for i := range off {
		mo += off[i]
		mn += on[i]
	}
	mo /= float64(len(off))
	mn /= float64(len(on))
	rel := math.Abs(mn-mo) / math.Max(mo, 1e-9)
	t.Logf("mean off=%.6f on=%.6f rel=%.4f%%", mo, mn, rel*100)
	if rel > 0.005 {
		t.Fatalf("mean shifted by %.3f%% > 0.5%%", rel*100)
	}
	// Local-average tone curve: blur both at grain scale, compare per-row means
	blur := func(src []float64) []float64 {
		p := append([]float64(nil), src...)
		// planar G channel blur proxy: average RGB then blur as mono plane
		plane := make([]float64, w*h)
		for i := 0; i < w*h; i++ {
			plane[i] = (p[i*3] + p[i*3+1] + p[i*3+2]) / 3
		}
		blurInPlace(plane, w, h, 6)
		return plane
	}
	bo, bn := blur(off), blur(on)
	var maxRowRel float64
	for y := 0; y < h; y++ {
		var ro, rn float64
		for x := 0; x < w; x++ {
			ro += bo[y*w+x]
			rn += bn[y*w+x]
		}
		ro /= float64(w)
		rn /= float64(w)
		r := math.Abs(rn-ro) / math.Max(ro, 1e-9)
		if r > maxRowRel {
			maxRowRel = r
		}
	}
	t.Logf("max row local-avg rel=%.4f%%", maxRowRel*100)
	if maxRowRel > 0.005 {
		t.Fatalf("local tone curve shifted by %.3f%% > 0.5%%", maxRowRel*100)
	}
}
