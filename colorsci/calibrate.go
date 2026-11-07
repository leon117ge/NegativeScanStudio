package colorsci

import (
	"errors"
	"math"
)

// ColorChecker24sRGB are the commonly published 8-bit sRGB (D65) values of
// the 24 X-Rite ColorChecker Classic patches (X-Rite/BabelColor), row by
// row from dark skin to black. Approximate; for best results use the
// values printed for your chart's production run.
var ColorChecker24sRGB = [24][3]float64{
	{115, 82, 68}, {194, 150, 130}, {98, 122, 157}, {87, 108, 67}, {133, 128, 177}, {103, 189, 170},
	{214, 126, 44}, {80, 91, 166}, {193, 90, 99}, {94, 60, 108}, {157, 188, 64}, {224, 163, 46},
	{56, 61, 150}, {70, 148, 73}, {175, 54, 60}, {231, 199, 31}, {187, 86, 149}, {8, 133, 161},
	{243, 243, 242}, {200, 200, 200}, {160, 160, 160}, {122, 122, 121}, {85, 85, 85}, {52, 52, 52},
}

// SRGBToLinear decodes one 0..1 sRGB value.
func SRGBToLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// ReferenceLogExposure turns reference linear RGB of the chart patches
// into the target the solver fits: log10(linear). An ideal negative with
// no crosstalk has density above base = gamma·log10(E) + k per channel; the
// per-channel gamma and k are absorbed by the solver's diagonal and offset.
func ReferenceLogExposure(lin [][3]float64) [][3]float64 {
	out := make([][3]float64, len(lin))
	for i, v := range lin {
		for c := 0; c < 3; c++ {
			out[i][c] = math.Log10(math.Max(v[c], 1e-4))
		}
	}
	return out
}

// Calibration is the result of SolveCrosstalk.
type Calibration struct {
	Matrix   Mat3       // correction, density above base -> corrected density
	Offset   [3]float64 // fitted per-channel offset (exposure/printer lights; not applied by the app)
	RMS      [3]float64 // per-channel residual RMS in the fitted units
	Patches  int
	Neutral  Mat3 // Matrix scaled to unit row sums (saturation-only form)
	CondNote string
}

// ErrTooFewPatches means fewer than 6 usable patches were given.
var ErrTooFewPatches = errors.New("colorsci: need at least 6 chart patches")

// SolveCrosstalk fits ref ≈ A·meas + b by least squares, one row of A at a
// time. meas are measured patch densities above base (from the negative
// scan); ref are the targets (e.g. ReferenceLogExposure of the chart's
// reference values, or the patches' densities on a crosstalk-free
// reference). ridge >= 0 pulls A towards a scaled identity, which keeps
// the fit stable with few or noisy patches (0 = plain least squares;
// ~0.01 is a sensible default for a 24-patch chart).
//
// Only the matrix is meant to be stored per film stock; the offset
// depends on the chart's exposure and is reported for diagnostics.
func SolveCrosstalk(meas, ref [][3]float64, ridge float64) (Calibration, error) {
	n := len(meas)
	if n != len(ref) {
		return Calibration{}, errors.New("colorsci: meas and ref lengths differ")
	}
	if n < 6 {
		return Calibration{}, ErrTooFewPatches
	}
	// Normal equations on centred data: centring removes the offset term
	// so the ridge only acts on the matrix.
	var mm, mr [3]float64
	for i := 0; i < n; i++ {
		for c := 0; c < 3; c++ {
			mm[c] += meas[i][c] / float64(n)
			mr[c] += ref[i][c] / float64(n)
		}
	}
	var XtX Mat3
	var XtY [3][3]float64 // [input j][output row r]
	for i := 0; i < n; i++ {
		var x, y [3]float64
		for c := 0; c < 3; c++ {
			x[c] = meas[i][c] - mm[c]
			y[c] = ref[i][c] - mr[c]
		}
		for a := 0; a < 3; a++ {
			for b := 0; b < 3; b++ {
				XtX[a][b] += x[a] * x[b]
			}
			for r := 0; r < 3; r++ {
				XtY[a][r] += x[a] * y[r]
			}
		}
	}
	var cal Calibration
	cal.Patches = n
	tr := (XtX[0][0] + XtX[1][1] + XtX[2][2]) / 3
	for r := 0; r < 3; r++ {
		// Pull row r towards s·e_r where s is the plain per-channel slope.
		s := XtY[r][r] / math.Max(XtX[r][r], 1e-12)
		A := XtX
		var rhs [3]float64
		for a := 0; a < 3; a++ {
			rhs[a] = XtY[a][r]
		}
		lam := ridge * tr
		for a := 0; a < 3; a++ {
			A[a][a] += lam
		}
		rhs[r] += lam * s
		inv, err := A.Inverse()
		if err != nil {
			return Calibration{}, errors.New("colorsci: chart patches are degenerate (all the same colour?)")
		}
		row := inv.Apply(rhs)
		cal.Matrix[r] = row
		cal.Offset[r] = mr[r] - (row[0]*mm[0] + row[1]*mm[1] + row[2]*mm[2])
	}
	for i := 0; i < n; i++ {
		p := cal.Matrix.Apply(meas[i])
		for r := 0; r < 3; r++ {
			e := p[r] + cal.Offset[r] - ref[i][r]
			cal.RMS[r] += e * e / float64(n)
		}
	}
	for r := 0; r < 3; r++ {
		cal.RMS[r] = math.Sqrt(cal.RMS[r])
	}
	cal.Neutral = cal.Matrix.NormalizeRows()
	if math.Abs(cal.Matrix.Det()) < 1e-6 {
		cal.CondNote = "fitted matrix is (nearly) singular - check the patch readings"
	}
	return cal, nil
}
