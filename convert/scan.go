package convert

// Camera-native encoding of Convert's complementary (Status M) densities
// so a file behaves like a levelled trichrome scan: the plugin applies
// the CameraCal matrix first, then Density In sees film_base 0.5 after
// profile and the designed per-channel densities.
//
// A peak-normalised dye × LED × X-T3 integral that treats those Status M
// densities as isolated Y/M/C peak OD double-counts scanner crosstalk
// (the same class of error as the old DyeMix Jacobian) and rotates
// greens toward yellow. The first-order inverse of that forward model
// is this encoding: T_after = 0.5 * 10^(-D), T_native = M^{-1} * T_after.
// LED assumption (repo ScanLightNarrowband, not a Light Air measurement):
// 660/540/450 nm, FWHM 20/30/20. X-T3 sensitivities: XT3Measured
// (Weta/rawtoaces-data, Apache-2.0).

import (
	"math"

	"basetool/colorsci"
)

// DefaultXT3Profile is the 2026-11-02 X-T3 trichrome chart fit
// (camera -> linear Rec.709: IT8 XYZ D50, Bradford to D65).
const DefaultXT3Profile = "XT3 trichrome 2026-11-02"

// DefaultXT3Trichrome is the 2026-11-02 chart matrix, row-major
// (R, G, B rows). Used when the app has no enabled camera profile.
var DefaultXT3Trichrome = colorsci.Mat3{
	{0.86426, 0.10467, -0.01969},
	{-0.05068, 0.98176, 0.01644},
	{0.11287, -0.14623, 1.04095},
}

func scanMatrixOf(o Options) (colorsci.Mat3, float64) {
	m := o.ScanMatrix
	if m == (colorsci.Mat3{}) {
		m = DefaultXT3Trichrome
	}
	k := o.ScanStrength
	if k <= 0 {
		k = 1
	}
	if k > 1 {
		k = 1
	}
	return m, k
}

// ScanPreBase is the camera-native film-base triplet that the CameraCal
// matrix maps to (0.5, 0.5, 0.5) in after_profile space.
func ScanPreBase(m colorsci.Mat3, k float64) [3]float64 {
	a := m.Lerp(k)
	inv, err := a.Inverse()
	if err != nil {
		return [3]float64{BaseFS, BaseFS, BaseFS}
	}
	pre := inv.Apply([3]float64{BaseFS, BaseFS, BaseFS})
	for i := range pre {
		if pre[i] < 1e-4 {
			pre[i] = 1e-4
		}
	}
	return pre
}

func matrix9(m colorsci.Mat3) [9]float64 {
	return [9]float64{
		m[0][0], m[0][1], m[0][2],
		m[1][0], m[1][1], m[1][2],
		m[2][0], m[2][1], m[2][2],
	}
}

// ScanTransmit encodes complementary densities as camera-native
// transmittance, levelled so the plugin's CameraCal yields
// T_after = 0.5 * 10^(-D) (film base 0.5 when D = 0).
func ScanTransmit(d [3]float64, o Options) [3]float64 {
	var t [3]float64
	for c := 0; c < 3; c++ {
		dc := d[c]
		if dc < 0 {
			dc = 0
		}
		t[c] = BaseFS * math.Pow(10, -dc)
	}
	m, k := scanMatrixOf(o)
	inv, err := m.Lerp(k).Inverse()
	if err != nil {
		return t
	}
	return inv.Apply(t)
}

func scanProfileName(o Options) string {
	if o.ScanProfile != "" {
		return o.ScanProfile
	}
	if o.ScanMatrix == (colorsci.Mat3{}) {
		return DefaultXT3Profile
	}
	return o.ScanProfile
}
