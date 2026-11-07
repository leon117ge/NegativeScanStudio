package develop

import (
	"fmt"
	"strconv"

	"basetool/colorsci"
)

// Round returns v as printed with %.<prec>f (what the generated DCTLs
// contain), so the node uses exactly the DCTL constants.
func Round(v float64, prec int) float64 {
	x, _ := strconv.ParseFloat(fmt.Sprintf("%.*f", prec, v), 64)
	return x
}

// BuiltinMatrices are the crosstalk presets 1..6 of FilmScan_MaskCrosstalk.dctl:
// Vision3 mask-aware, Vision3 unmasked, then the datasheet stocks (%.4f).
func BuiltinMatrices() []NamedMat {
	out := []NamedMat{
		{"Vision3 5219 mask-aware - suggest 50 pct", [9]float64{1.6258, -0.8166, 0.1908, 0, 1.5437, -0.5437, 0, 0, 1}},
		{"Vision3 5219 unmasked - strong", [9]float64{1.6804, -0.8959, 0.2154, -0.5202, 2.2989, -0.7787, -0.0990, -0.2828, 1.3818}},
	}
	for _, d := range colorsci.StockDyeSets[:4] {
		m := d.Crosstalk()
		var a [9]float64
		for i := 0; i < 9; i++ {
			a[i] = Round(m[i/3][i%3], 4)
		}
		out = append(out, NamedMat{d.Name + " - datasheet spectral", a})
	}
	return out
}
