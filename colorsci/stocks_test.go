package colorsci

import (
	"math"
	"testing"
)

func TestStockCrosstalk(t *testing.T) {
	if Vision3Dyes.Crosstalk() != DefaultCrosstalk() {
		t.Fatal("Vision3 set must reproduce DefaultCrosstalk")
	}
	def := DefaultCrosstalk()
	for _, d := range StockDyeSets {
		m := d.Crosstalk()
		diff := 0.0
		for i := 0; i < 3; i++ {
			s := 0.0
			for j := 0; j < 3; j++ {
				if math.IsNaN(m[i][j]) || math.Abs(m[i][j]) > 3 {
					t.Fatalf("%s: bad matrix %v", d.Name, m)
				}
				s += m[i][j]
				diff = math.Max(diff, math.Abs(m[i][j]-def[i][j]))
			}
			if math.Abs(s-1) > 1e-9 {
				t.Fatalf("%s row %d sums to %v", d.Name, i, s)
			}
		}
		if d.Name != Vision3Dyes.Name && diff < 0.01 {
			t.Fatalf("%s matrix identical to 5219", d.Name)
		}
		if len(d.Y.WL) != len(d.Y.V) || len(d.M.WL) != len(d.M.V) || len(d.C.WL) != len(d.C.V) {
			t.Fatalf("%s: table length mismatch", d.Name)
		}
	}
}

func TestMatchStock(t *testing.T) {
	cases := map[string]string{"Kodak Ektar 100": "Ektar 100", "Portra 160": "Portra 400", "PORTRA400": "Portra 400",
		"Kodak UltraMax 400": "UltraMax 400", "Ultra Max": "UltraMax 400", "Gold 200": "Gold 200",
		"Vision3 500T 5219": "Vision3 500T", "Kodak 7219": "Vision3 500T", "250D": "Vision3 5219", "Vision3 200T": "Vision3 5219", "Fuji C200": ""}
	for in, want := range cases {
		d, ok := MatchStock(in)
		if (want == "") == ok || d.Name != want {
			t.Errorf("%q -> %q ok=%v, want %q", in, d.Name, ok, want)
		}
	}
}
