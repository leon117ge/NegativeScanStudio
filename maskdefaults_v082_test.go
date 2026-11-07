package main

import (
	"strings"
	"testing"
)

// v0.82: a trichrome base sent to Film base turns orange mask compensation
// off and the crosstalk strength to 0 in the written MaskCrosstalk DCTL.
func TestTrichromeMaskDefaults(t *testing.T) {
	in := maskSettings{MaskOn: true, BaseRGB: [3]float64{0.5, 0.3, 0.2}, Xt: 2, XtMat: 1, Stock: "Kodak Ektar 100"}
	out, changed := trichromeMask(in)
	if !changed || out.MaskOn || out.Xt != 0 || out.XtMat != 1 || out.Stock != in.Stock || out.BaseRGB != in.BaseRGB {
		t.Fatalf("trichromeMask %+v changed=%v", out, changed)
	}
	if _, again := trichromeMask(out); again {
		t.Fatal("second call changed it again")
	}
	d := resolveMask(out, nil)
	if d.MaskOn || d.Strength != 0 || d.changed() {
		t.Fatalf("resolved %+v", d)
	}
	body, err := applyMaskDefaults(buildMaskDCTLExtra(nil, d.Extra), d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, mdMaskOn) || !strings.Contains(body, mdStrength) {
		t.Fatal("DCTL does not have mask compensation off and strength 0.0")
	}
	// the white-light settings would have written them on
	if dw := resolveMask(in, nil); !dw.MaskOn || dw.Strength != 1 {
		t.Fatalf("white-light %+v", dw)
	}
}
