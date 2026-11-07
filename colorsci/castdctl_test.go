package colorsci

import (
	"os"
	"strings"
	"testing"
)

func TestCastDCTLEmission(t *testing.T) {
	b, e := os.ReadFile("../template.dctl")
	if e != nil {
		t.Fatal(e)
	}
	r, _ := Vision3Illuminant(5500)
	off := [3]float64{-r.Density[0], -r.Density[1], -r.Density[2]}
	s, e := ApplyCastDCTL(string(b), true, off)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(s, "DCTLUI_CHECK_BOX, 1)") || !strings.Contains(s, "r += cast_r / (dens_step * full_code)") {
		t.Fatal("wrong density emission")
	}
	s, e = ApplyCastDCTL(s, false, [3]float64{})
	if e != nil || !strings.Contains(s, "cast_on, Apply colour cast correction, DCTLUI_CHECK_BOX, 0)") {
		t.Fatal(e)
	}
	if _, e := ApplyCastDCTL("", true, off); e == nil {
		t.Fatal("accepted missing params")
	}
}
