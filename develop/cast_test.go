package develop

import (
	"math"
	"testing"
)

func TestDensityCastCorrection(t *testing.T) {
	n := NewNode(TDensity)
	for _, key := range []string{"base_r", "base_g", "base_b"} {
		n.V[key] = .5
	}
	n.V["clamp01"] = 0
	// A synthetic neutral with an extra -0.12 R / +0.21 B layer density.
	f := n.Compile(&Context{})
	r0, g0, b0 := f(.5*float32(math.Pow(10, -.58)), .5*float32(math.Pow(10, -.7)), .5*float32(math.Pow(10, -.91)))
	n.V["cast_on"] = 1
	n.V["cast_r"] = .12
	n.V["cast_b"] = -.21
	f = n.Compile(&Context{})
	r, g, b := f(.5*float32(math.Pow(10, -.58)), .5*float32(math.Pow(10, -.7)), .5*float32(math.Pow(10, -.91)))
	for _, v := range []float32{r - g, b - g} {
		if math.Abs(float64(v)) > 1e-6 {
			t.Fatalf("neutral not restored: %g %g %g", r, g, b)
		}
	}
	if r <= r0 || b >= b0 || g != g0 {
		t.Fatal("wrong correction sign")
	}
	// Diagnostic transmittance output MUST bypass density-only corrections.
	n.V["out_trans"] = 1
	f = n.Compile(&Context{})
	r, g, b = f(.1, .2, .3)
	if math.Abs(float64(r-.2)) > 1e-6 || math.Abs(float64(g-.4)) > 1e-6 || math.Abs(float64(b-.6)) > 1e-6 {
		t.Fatal(r, g, b)
	}
}
func TestDensityCastOffIsLegacy(t *testing.T) {
	a := NewNode(TDensity)
	b := NewNode(TDensity)
	b.V["cast_r"] = .9
	b.V["cast_g"] = -.8
	b.V["cast_b"] = .1
	f, g := a.Compile(&Context{}), b.Compile(&Context{})
	for _, v := range []float32{0, .001, .1, .5, 1} {
		r1, g1, b1 := f(v, v/2, v/3)
		r2, g2, b2 := g(v, v/2, v/3)
		if r1 != r2 || g1 != g2 || b1 != b2 {
			t.Fatal("disabled correction changes legacy output")
		}
	}
}
