package colorsci

import "math"

// v0.75: crosstalk matrices from the MEASURED X-T3 spectral sensitivity
// (measured_data.go, Weta / rawtoaces-data, Apache-2.0) instead of the
// Gaussian XT3Approx model. Same method as spectral.go (Jacobian of the
// camera channel densities at 0.5 D of each dye, columns scaled to diag 1,
// inverse with rows normalised to 1), plus:
//
//   - the stock's D-min (base + orange mask, KodakDmin) weights the
//     spectrum the camera sees (density is still measured above base);
//     Vision3 5219 has no D-min data and is computed without base;
//   - narrowband RGB light: three Gaussian LEDs 450/540/660 nm, FWHM
//     20/30/20 nm (ASSUMED shapes), with the three powers balanced so the
//     camera R, G and B get equal signal through the film base (what you do
//     when setting the LED currents on the bare base);
//   - White LED is the phosphor-LED model of spectral.go (LightLED).
//
// Reference implementation: prototype xt3_crosstalk/xt3_dye_crosstalk.py
// (the Go results match it to < 1e-6, see measured_test.go).
//
// The dye curves of Ektar / Portra / UltraMax / Gold are ESTIMATED (see
// stocks.go); the D-min curves are digitised from chart images. The matrices
// are a model-based starting point, not a calibration of your setup.

// Sensitivity is a camera's relative spectral sensitivity per channel (R G B).
type Sensitivity interface {
	Sens(c int, wl float64) float64
}

// SampledCamera is a measured sensitivity table (linear interpolation, zero
// outside the measured range, negatives clipped to zero).
type SampledCamera struct {
	WL []float64
	Ch [3][]float64
}

// Sens returns channel c's sensitivity at wl.
func (s SampledCamera) Sens(c int, wl float64) float64 {
	n := len(s.WL)
	if wl < s.WL[0] || wl > s.WL[n-1] {
		return 0
	}
	v := s.Ch[c]
	for i := 1; i < n; i++ {
		if wl <= s.WL[i] {
			t := (wl - s.WL[i-1]) / (s.WL[i] - s.WL[i-1])
			return math.Max(v[i-1]+t*(v[i]-v[i-1]), 0)
		}
	}
	return math.Max(v[0], 0)
}

// XT3Measured is the measured Fujifilm X-T3 sensitivity (Weta, Apache-2.0).
var XT3Measured = SampledCamera{WL: xt3MeasuredWL, Ch: [3][]float64{xt3MeasuredR, xt3MeasuredG, xt3MeasuredB}}

// Scan light sources of the measured model.
const (
	ScanLightWhiteLED   = iota // phosphor white LED (LightSPD(LightLED))
	ScanLightNarrowband        // RGB LEDs 450/540/660 nm, balanced on the film base
)

// Matrix forms.
const (
	FormFull      = iota // full inverse of the forward model
	FormMaskAware        // zero G<-C, B<-C, B<-M before inverting (masking couplers)
)

var narrowLines = [3][2]float64{{660, 20}, {540, 30}, {450, 20}} // R, G, B: centre, FWHM nm

func gaussLine(c, fwhm, wl float64) float64 {
	s := fwhm / 2.3548
	x := (wl - c) / s
	return math.Exp(-0.5 * x * x)
}

// baseT is the film base transmission of d (1 when no D-min is known).
func (d DyeSet) baseT(wl float64) float64 {
	if b, ok := KodakDmin[d.Name]; ok {
		return math.Pow(10, -b.At(wl))
	}
	return 1
}

// scanSPD returns the light reaching the film (before the base) on the
// 380..800/2 nm grid used by the density integrals.
func (d DyeSet) scanSPD(cam Sensitivity, light int) func(wl float64) float64 {
	if light != ScanLightNarrowband {
		return func(wl float64) float64 { return LightSPD(LightLED, wl) }
	}
	var a Mat3
	for k := 0; k < 3; k++ {
		for j := 0; j < 3; j++ {
			for wl := 380.0; wl <= 800; wl += 2 {
				a[k][j] += cam.Sens(k, wl) * gaussLine(narrowLines[j][0], narrowLines[j][1], wl) * d.baseT(wl)
			}
		}
	}
	var pw [3]float64
	if inv, err := a.Inverse(); err == nil {
		pw = inv.Apply([3]float64{1, 1, 1})
	}
	if pw[0] <= 0 || pw[1] <= 0 || pw[2] <= 0 {
		for j := range pw {
			pw[j] = 1 / a[j][j]
		}
	}
	return func(wl float64) float64 {
		var s float64
		for j := 0; j < 3; j++ {
			s += pw[j] * gaussLine(narrowLines[j][0], narrowLines[j][1], wl)
		}
		return s
	}
}

func (d DyeSet) measuredDensity(cam Sensitivity, spd func(float64) float64, c int, amt [3]float64) float64 {
	var num, den float64
	for wl := 380.0; wl <= 800; wl += 2 {
		w := cam.Sens(c, wl) * spd(wl) * d.baseT(wl)
		dd := amt[0]*d.Y.At(wl) + amt[1]*d.M.At(wl) + amt[2]*d.C.At(wl)
		num += w * math.Pow(10, -dd)
		den += w
	}
	return -math.Log10(num / den)
}

// MeasuredForward is the forward mixing matrix (rows R G B, columns C M Y,
// diag 1) for camera cam under light at 0.5 D of each dye, base-weighted.
func (d DyeSet) MeasuredForward(cam Sensitivity, light int) Mat3 {
	spd := d.scanSPD(cam, light)
	dyeOf := [3]int{2, 1, 0}
	amt := [3]float64{0.5, 0.5, 0.5}
	var J Mat3
	const h = 1e-4
	for k := 0; k < 3; k++ {
		for i := 0; i < 3; i++ {
			a1, a2 := amt, amt
			a1[dyeOf[i]] -= h
			a2[dyeOf[i]] += h
			J[k][i] = (d.measuredDensity(cam, spd, k, a2) - d.measuredDensity(cam, spd, k, a1)) / (2 * h)
		}
	}
	for i := 0; i < 3; i++ {
		dg := J[i][i]
		for k := 0; k < 3; k++ {
			J[k][i] /= dg
		}
	}
	return J
}

// MeasuredCrosstalk is the correction matrix (inverse, rows normalised to 1)
// from the measured X-T3 sensitivity for light and form.
func (d DyeSet) MeasuredCrosstalk(light, form int) Mat3 {
	m := d.MeasuredForward(XT3Measured, light)
	if form == FormMaskAware {
		m = MaskAware(m)
	}
	inv, err := m.Inverse()
	if err != nil {
		return Identity
	}
	return inv.NormalizeRows()
}
