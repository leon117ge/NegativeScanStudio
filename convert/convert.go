// Package convert turns calibrated linear camera RGB into a simulated
// colour-negative (transmittance) image.
//
// Algorithm (see docs/convert-design.md): LibRaw linear RGB -> optional
// Chart CamCal (colorsci.ApplyCam) -> EV exposure -> per-layer H&D density
// (Kodak datasheet mid-slopes) -> dye mixing via MeasuredForward on our
// digitised dye spectra -> T = 10^(-D). Inspired by the spektrafilm pipeline
// shape; no spektrafilm code or CC BY-SA profile data is used.
package convert

import (
	"fmt"
	"math"
	"sync"

	"basetool/colorsci"
)

// Stock is one film model available in Convert.
type Stock struct {
	Name   string
	Source string // human-readable citation
	Gamma  [3]float64
	Dyes   colorsci.DyeSet
}

// Options control one conversion.
type Options struct {
	Stock          Stock
	EV             float64 // exposure compensation in stops
	CamMatrix      colorsci.Mat3
	CamStrength    float64 // 0..1; 0 skips ApplyCam
	ApplyCamProfile bool
}

// MidGrey is the linear mid-grey reference for log exposure (18% grey).
const MidGrey = 0.18

// Density range above Dmin for the soft H&D curve (Status-M-ish).
const densityRange = 2.4

var (
	stockOnce sync.Once
	stockList []Stock
	fwdCache  = map[string]colorsci.Mat3{}
	dminCache = map[string][3]float64{}
)

// Stocks returns the built-in Convert stocks (order = UI order).
func Stocks() []Stock {
	stockOnce.Do(func() {
		stockList = []Stock{
			{Name: "Vision3 500T 5219", Source: "Gamma: OFX Vision3 Status M; dyes+Dmin: Kodak H-1-5219",
				Gamma: [3]float64{0.4482, 0.5266, 0.5096}, Dyes: colorsci.Vision3500TDyes},
			{Name: "Vision3 250D 5207", Source: "Gamma: OFX 250D preset; dyes: H-1-5219 shapes (proxy for 5207)",
				Gamma: [3]float64{0.48148148, 0.56018519, 0.54166667}, Dyes: colorsci.Vision3Dyes},
			{Name: "UltraMax 400", Source: "Gamma: OFX still_gammas (Kodak E-7023 polylines); dyes: E-7023 decomposed",
				Gamma: [3]float64{0.518590585, 0.536316940, 0.604826092}, Dyes: colorsci.UltraMax400Dyes},
			{Name: "Portra 400", Source: "Gamma: OFX still_gammas (Kodak E-4050 polylines); dyes: E-4050 decomposed",
				Gamma: [3]float64{0.555934584, 0.549589137, 0.640031943}, Dyes: colorsci.Portra400Dyes},
			{Name: "Gold 200", Source: "Gamma: OFX still_gammas (Kodak E-7022 polylines); dyes: E-7022 decomposed",
				Gamma: [3]float64{0.550723702, 0.564449843, 0.610388122}, Dyes: colorsci.Gold200Dyes},
			{Name: "Ektar 100", Source: "Gamma: OFX still_gammas (Kodak E-4046 polylines); dyes: E-4046 decomposed",
				Gamma: [3]float64{0.580572786, 0.579564388, 0.663740443}, Dyes: colorsci.Ektar100Dyes},
		}
	})
	return stockList
}

// StockByName finds a stock or returns an error.
func StockByName(name string) (Stock, error) {
	for _, s := range Stocks() {
		if s.Name == name {
			return s, nil
		}
	}
	return Stock{}, fmt.Errorf("unknown film stock %q", name)
}

func forwardOf(s Stock) colorsci.Mat3 {
	if m, ok := fwdCache[s.Name]; ok {
		return m
	}
	m := s.Dyes.MeasuredForward(colorsci.XT3Measured, colorsci.ScanLightWhiteLED)
	fwdCache[s.Name] = m
	return m
}

// dminRGB is absolute optical density of the unexposed base (orange mask)
// as seen by XT3Measured under white LED. Falls back to a mild warm base
// when the stock has no KodakDmin curve.
func dminRGB(s Stock) [3]float64 {
	if v, ok := dminCache[s.Name]; ok {
		return v
	}
	var out [3]float64
	curve, ok := colorsci.KodakDmin[s.Dyes.Name]
	if !ok {
		out = [3]float64{0.15, 0.45, 0.55} // R,G,B typical orange-mask-ish
		dminCache[s.Name] = out
		return out
	}
	for c := 0; c < 3; c++ {
		var num, den float64
		for wl := 380.0; wl <= 800; wl += 2 {
			w := colorsci.XT3Measured.Sens(c, wl) * colorsci.LightSPD(colorsci.LightLED, wl)
			num += w * math.Pow(10, -curve.At(wl))
			den += w
		}
		if den > 0 {
			out[c] = -math.Log10(num / den)
		}
	}
	dminCache[s.Name] = out
	return out
}

// hd maps mid-grey-relative log10 exposure to density above zero using a
// logistic whose maximum slope equals gamma (Kodak mid-tone slope).
func hd(logH, gamma float64) float64 {
	if gamma < 1e-6 {
		gamma = 1e-6
	}
	k := 4 * gamma / densityRange
	return densityRange / (1 + math.Exp(-k*logH))
}

// ProcessPixels converts linear camera RGB (0..1 per sample already scaled
// from 16-bit / 65535) in place into negative transmittance 0..1.
func ProcessPixels(rgb []float64, o Options) {
	exp := math.Pow(2, o.EV)
	fwd := forwardOf(o.Stock)
	base := dminRGB(o.Stock)
	g := o.Stock.Gamma
	applyCam := o.ApplyCamProfile && o.CamStrength > 0
	for i := 0; i+2 < len(rgb); i += 3 {
		v := [3]float64{rgb[i], rgb[i+1], rgb[i+2]}
		if applyCam {
			v = colorsci.ApplyCam(o.CamMatrix, o.CamStrength, v)
		}
		var layer [3]float64 // cyan, magenta, yellow (R,G,B-forming)
		for c := 0; c < 3; c++ {
			h := math.Max(v[c]*exp, 1e-10)
			layer[c] = hd(math.Log10(h/MidGrey), g[c])
		}
		// Optical density in camera RGB: Forward * [C,M,Y] + Dmin.
		od := fwd.Apply(layer)
		for c := 0; c < 3; c++ {
			d := od[c] + base[c]
			t := math.Pow(10, -d)
			if t < 0 {
				t = 0
			}
			if t > 1 {
				t = 1
			}
			rgb[i+c] = t
		}
	}
}

// Process16 converts interleaved RGB uint16 pixels (camera-linear) to a
// new negative uint16 buffer.
func Process16(pix []uint16, o Options) []uint16 {
	n := len(pix)
	tmp := make([]float64, n)
	for i, v := range pix {
		tmp[i] = float64(v) / 65535
	}
	ProcessPixels(tmp, o)
	out := make([]uint16, n)
	for i, v := range tmp {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		out[i] = uint16(v*65535 + 0.5)
	}
	return out
}

// NeutralNegDensity returns OD of an 18% grey at EV=0 for tests.
func NeutralNegDensity(o Options) [3]float64 {
	rgb := []float64{MidGrey, MidGrey, MidGrey}
	ProcessPixels(rgb, o)
	var d [3]float64
	for c := 0; c < 3; c++ {
		d[c] = -math.Log10(math.Max(rgb[c], 1e-12))
	}
	return d
}
