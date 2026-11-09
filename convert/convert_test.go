package convert

import (
	"encoding/json"
	"math"
	"testing"

	"basetool/colorsci"
	"basetool/develop"
	"basetool/scanmeta"
)

func TestStocksPresent(t *testing.T) {
	if len(Stocks()) != 6 {
		t.Fatalf("want 6 stocks, got %d", len(Stocks()))
	}
}

func TestBaseIsHalf(t *testing.T) {
	s, _ := StockByName("Ektar 100")
	v := SceneToNeg([3]float64{1e-6, 1e-6, 1e-6}, Options{Stock: s})
	for c := 0; c < 3; c++ {
		if math.Abs(v[c]-BaseFS) > 0.02 {
			t.Fatalf("ch %d: deep shadow should be near base 0.5, got %v", c, v)
		}
	}
}

func TestMidGreyDarkerThanBase(t *testing.T) {
	s, _ := StockByName("Portra 400")
	v := SceneToNeg([3]float64{MidGrey, MidGrey, MidGrey}, Options{Stock: s})
	for c := 0; c < 3; c++ {
		if !(v[c] < BaseFS-0.05) {
			t.Fatalf("ch %d: mid grey denser than base, got %v", c, v)
		}
		if v[c] < 0.02 {
			t.Fatalf("ch %d: mid grey clipped: %v", c, v)
		}
	}
}

func TestHighlightNotClipped(t *testing.T) {
	s, _ := StockByName("Gold 200")
	mid := SceneToNeg([3]float64{MidGrey, MidGrey, MidGrey}, Options{Stock: s})
	v := SceneToNeg([3]float64{8, 8, 8}, Options{Stock: s})
	for c := 0; c < 3; c++ {
		if v[c] <= 0 {
			t.Fatalf("highlight clipped ch %d", c)
		}
		if !(v[c] < mid[c]) {
			t.Fatalf("highlight should be darker than mid ch %d", c)
		}
	}
}

func TestBorderIsBase(t *testing.T) {
	s, _ := StockByName("UltraMax 400")
	w, h := 100, 80
	rgb := make([]float64, w*h*3)
	for i := range rgb {
		rgb[i] = MidGrey
	}
	ProcessPixels(rgb, w, h, Options{Stock: s, BorderPct: 0.05, SoftBorder: false})
	for _, i := range []int{0, (w - 1) * 3, (h - 1) * w * 3} {
		for c := 0; c < 3; c++ {
			if math.Abs(rgb[i+c]-BaseFS) > 1e-9 {
				t.Fatalf("border not base: %v", rgb[i:i+3])
			}
		}
	}
	ci := ((h/2)*w + w/2) * 3
	if !(rgb[ci] < BaseFS-0.05) {
		t.Fatalf("centre not denser: %v", rgb[ci:ci+3])
	}
}

func TestMetadataFilmBase(t *testing.T) {
	s, _ := StockByName("Vision3 500T 5219")
	m := Metadata(Options{Stock: s})
	if m.FilmBase == nil || *m.FilmBase != [3]float64{0.5, 0.5, 0.5} {
		t.Fatalf("film_base: %#v", m.FilmBase)
	}
	if m.FilmBaseSpace != scanmeta.AfterProfile {
		t.Fatalf("space %q", m.FilmBaseSpace)
	}
	js, err := m.JSON("NSS test convert / " + s.Name)
	if err != nil {
		t.Fatal(err)
	}
	var got scanmeta.Metadata
	if err := json.Unmarshal([]byte(js), &got); err != nil {
		t.Fatal(err)
	}
	if got.Schema != scanmeta.Schema || got.Mode != "convert" || got.FilmBase == nil || *got.FilmBase != [3]float64{0.5, 0.5, 0.5} {
		t.Fatalf("json meta: %+v / %s", got, js)
	}
}

func pluginInvert(negR, negG, negB float32, gam [3]float64, expo float32) (float32, float32, float32) {
	ch := develop.DefaultChain()
	d := &ch.Nodes[0]
	d.V["base_r"], d.V["base_g"], d.V["base_b"] = BaseFS, BaseFS, BaseFS
	d.V["preset"] = 0
	d.V["cast_on"] = 0
	d.V["out_trans"] = 0
	d.V["in_di"] = 0
	ch.Nodes[1].Bypass = true
	p := &ch.Nodes[2]
	p.V["gam_r"], p.V["gam_g"], p.V["gam_b"] = gam[0], gam[1], gam[2]
	p.V["expo"] = float64(expo)
	p.V["bal_r"], p.V["bal_g"], p.V["bal_b"] = 0, 0, 0
	p.V["film_wb"] = 0
	p.V["gray_on"] = 0
	p.V["contrast"] = 0
	p.V["out_mode"] = 4 // Linear
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
	for e := float32(-1); e <= 3.5; e += 0.05 {
		lr, lg, lb := pluginInvert(float32(neg[0]), float32(neg[1]), float32(neg[2]), s.Gamma, e)
		mean := (lr + lg + lb) / 3
		err := float32(math.Abs(float64(mean) - MidGrey))
		if err < bestErr {
			bestErr, bestE = err, e
		}
	}
	return bestE
}

func TestRoundTripGreyRamp(t *testing.T) {
	s, _ := StockByName("Ektar 100")
	expo := findExpo(s)
	t.Logf("Ektar 100 tuned expo=%.2f for mid grey", expo)

	type row struct {
		Stops                         float64
		L, negR, negG, negB           float64
		outR, outG, outB, mean        float64
	}
	var rows []row
	prev := float32(-1)
	for _, stops := range []float64{-4, -3, -2, -1, 0, 1, 2, 3} {
		L := MidGrey * math.Pow(2, stops)
		neg := SceneToNeg([3]float64{L, L, L}, Options{Stock: s})
		or, og, ob := pluginInvert(float32(neg[0]), float32(neg[1]), float32(neg[2]), s.Gamma, expo)
		mean := float64((or + og + ob) / 3)
		rows = append(rows, row{stops, L, neg[0], neg[1], neg[2], float64(or), float64(og), float64(ob), mean})
		if prev >= 0 && float32(mean)+1e-4 < prev {
			t.Fatalf("non-monotonic at stops %v: prev=%v mean=%v", stops, prev, mean)
		}
		prev = float32(mean)
		for _, c := range []float32{or, og, ob} {
			if mean > 1e-4 && math.Abs(float64(c)/mean-1) > 0.15 {
				t.Fatalf("stops %v not neutral: %+v", stops, rows[len(rows)-1])
			}
		}
	}
	for _, r := range rows {
		t.Logf("stops=%+.0f L=%.4f neg=%.4f/%.4f/%.4f out=%.4f/%.4f/%.4f mean=%.4f",
			r.Stops, r.L, r.negR, r.negG, r.negB, r.outR, r.outG, r.outB, r.mean)
	}
	mid := rows[4]
	if math.Abs(mid.mean-MidGrey) > 0.05 {
		t.Fatalf("mid recovered %.4f want ~0.18", mid.mean)
	}
	if rows[0].mean > 0.06 {
		t.Fatalf("deep shadow not near black: %+v", rows[0])
	}
}

func TestApplyCamStillUsed(t *testing.T) {
	s, _ := StockByName("Gold 200")
	m := colorsci.Mat3{{1.15, -0.08, 0}, {-0.04, 1.08, -0.04}, {0, -0.06, 1.12}}
	a := SceneToNeg([3]float64{0.2, 0.2, 0.2}, Options{Stock: s, ApplyCamProfile: false})
	b := SceneToNeg([3]float64{0.2, 0.2, 0.2}, Options{Stock: s, ApplyCamProfile: true, CamMatrix: m, CamStrength: 1})
	if a == b {
		t.Fatal("ApplyCam should change result")
	}
}
