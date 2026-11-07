package colorsci

import "math"

// Kodak Cineon / SMPTE conventions for 10-bit printing-density encoding.
//
// Sources:
//   - Kodak, "Cineon Image File Format" (4.5 draft) and "Scanning
//     recommendations for extended dynamic range camera films": 1023 code
//     values, 0.002 density per code value, D-min (film base) at code 95.
//   - SMPTE tutorial "Gray-scale transformations of digital film data"
//     (Kennel/Snider, doi:10.5594/j01652): normal negative -> 90% white
//     card at 685, 18% grey at 470, 2% black at 180; negative gamma 0.6, so
//     one stop = 0.3*0.6/0.002 = 90 code values.
//   - Nuke Log2Lin defaults black 95 / white 685 / gamma 0.6, which is the
//     same curve Film Base Meter's FilmScan_Positive_Out.dctl uses for its
//     Cineon output (685 + 300*log10(lin*0.98921 + 0.01079)).
const (
	CineonBlack      = 95.0   // code of film base (D-min)
	CineonWhite      = 685.0  // code of a 90% white card on a normal negative
	CineonGrey       = 470.0  // code of an 18% grey card
	CineonStep       = 0.002  // density per code value
	CineonMaxCode    = 1023.0 // 10-bit full scale
	CineonNegGamma   = 0.6    // nominal negative gamma for log<->lin
	minTransmittance = 1e-7   // floor so log10 stays finite (D <= 7)
)

// DensityAboveBase returns -log10(v/base) per channel. v and base are
// linear readings in the same scale. A reading brighter than the base gives
// a small negative density (kept, not clamped: it is real data such as
// illumination fall-off or flare).
func DensityAboveBase(v, base [3]float64) [3]float64 {
	var d [3]float64
	for c := 0; c < 3; c++ {
		t := v[c] / base[c]
		if !(t > minTransmittance) { // also catches NaN
			t = minTransmittance
		}
		d[c] = -math.Log10(t)
	}
	return d
}

// TransmittanceFromDensity is the inverse of DensityAboveBase.
func TransmittanceFromDensity(d, base [3]float64) [3]float64 {
	return [3]float64{
		base[0] * math.Pow(10, -d[0]),
		base[1] * math.Pow(10, -d[1]),
		base[2] * math.Pow(10, -d[2]),
	}
}

// CineonCode converts density above base to a 10-bit Cineon code value
// (95 + D/0.002), clamped to 0..1023. It is a float: the 16-bit output
// keeps sub-code precision.
func CineonCode(d float64) float64 {
	c := CineonBlack + d/CineonStep
	return math.Min(math.Max(c, 0), CineonMaxCode)
}

// CineonDensity is the inverse of CineonCode (no clamp).
func CineonDensity(code float64) float64 { return (code - CineonBlack) * CineonStep }

// CineonTo16 stores a 10-bit code in 16 bits as round(code/1023*65535),
// i.e. the normalised value code/1023 that Resolve (and every other
// application) reads from a 16-bit integer TIFF. Resolve's "Cineon Film
// Log" gamma interprets that normalised value as code/1023.
func CineonTo16(code float64) uint16 {
	v := math.Round(code / CineonMaxCode * 65535)
	return uint16(math.Min(math.Max(v, 0), 65535))
}

// CineonFrom16 is the inverse of CineonTo16.
func CineonFrom16(v uint16) float64 { return float64(v) / 65535 * CineonMaxCode }

// CineonToLinear is the standard Cineon log-to-linear (Kodak/Nuke
// Log2Lin, black 95, white 685, gamma 0.6): code 95 -> 0, 685 -> 1,
// 470 -> ~0.18. Scene-referred, unbounded above 1.
func CineonToLinear(code float64) float64 {
	off := math.Pow(10, (CineonBlack-CineonWhite)*CineonStep/CineonNegGamma)
	return (math.Pow(10, (code-CineonWhite)*CineonStep/CineonNegGamma) - off) / (1 - off)
}

// LinearToCineon is the inverse of CineonToLinear (unclamped).
func LinearToCineon(lin float64) float64 {
	off := math.Pow(10, (CineonBlack-CineonWhite)*CineonStep/CineonNegGamma)
	x := lin*(1-off) + off
	if x < 1e-10 {
		x = 1e-10
	}
	return CineonWhite + math.Log10(x)*CineonNegGamma/CineonStep
}
