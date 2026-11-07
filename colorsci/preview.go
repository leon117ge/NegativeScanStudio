package colorsci

import (
	"math"
	"sort"
)

// Display transforms used only for the comparison previews (not part of
// the app's output). All return display-referred sRGB in 0..1.

// SRGBEncode is the sRGB OETF with clamping.
func SRGBEncode(v float64) float64 {
	v = math.Min(math.Max(v, 0), 1)
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

// ACESFilm is Krzysztof Narkowicz's fit of the ACES RRT+sRGB ODT tone
// curve (2016), applied per channel with his 0.6 input pre-scale. It is
// used here as the "print-film-like" S-curve after Cineon log2lin: toe in
// the shadows, shoulder in the highlights, ~1.0 mid-tone slope in log.
func ACESFilm(x float64) float64 {
	x *= 0.6
	return math.Min(math.Max((x*(2.51*x+0.03))/(x*(2.43*x+0.59)+0.14), 0), 1)
}

// Levels maps per-channel lo..hi to 0..1 (no clamp).
type Levels struct{ Lo, Hi [3]float64 }

func (l Levels) apply(c int, v float64) float64 { return (v - l.Lo[c]) / (l.Hi[c] - l.Lo[c]) }

// SimpleInversion is "current-style" naive linear inversion: positive =
// base/reading (= 10^D) per channel, per-channel black/white levels lv
// (taken from the same quantity), then sRGB.
func SimpleInversion(v, base [3]float64, lv Levels) [3]float64 {
	var o [3]float64
	for c := 0; c < 3; c++ {
		p := base[c] / math.Max(v[c], 0.5)
		o[c] = SRGBEncode(lv.apply(c, p))
	}
	return o
}

// Cineon "printer lights + contrast" normalisation for previews: per
// channel, map code lo -> 180 (2% black card) and hi -> 700 (a little
// above the 90% white card at 685). This is a per-channel gain/offset in
// density - the log-domain equivalent of SimpleInversion's levels.
const (
	PreviewBlackCode = 180.0
	PreviewWhiteCode = 700.0
)

// PrintNormalise is the per-channel levels step alone (code -> code).
func PrintNormalise(code float64, c int, lv Levels) float64 {
	return PreviewBlackCode + lv.apply(c, code)*(PreviewWhiteCode-PreviewBlackCode)
}

// CineonPrintPreview: per-channel normalise codes with lv, add a global
// exposure offset (codes, same for all channels; 90 codes = 1 stop), Cineon
// log2lin (95/685/0.6), ACESFilm S-curve, sRGB.
func CineonPrintPreview(code [3]float64, lv Levels, g float64) [3]float64 {
	var o [3]float64
	for c := 0; c < 3; c++ {
		n := PreviewBlackCode + g*(PrintNormalise(code[c], c, lv)-PreviewBlackCode)
		o[c] = SRGBEncode(ACESFilm(math.Max(CineonToLinear(n), 0)))
	}
	return o
}

// Percentile of a sample (p in 0..1), sorting a copy.
func Percentile(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	i := int(math.Round(p * float64(len(s)-1)))
	return s[min(max(i, 0), len(s)-1)]
}
