package colorsci

import "math"

// Spectral model behind the default crosstalk matrix.
//
// DYE CURVES: peak-normalised spectral dye density of the yellow, magenta
// and cyan image dyes, read by eye (every 25 nm, plus the peaks) from the
// "Spectral Dye Density Curves" chart of Kodak publication H-1-5219,
// KODAK VISION3 500T Color Negative Film 5219/7219 (Revised 3-26), p.4,
// "Process: ECN-2; D-mins subtracted". Reading error is about +-0.03 D.
// Kodak Portra (E-4050) publishes a chart of the same kind; Vision3 and
// Portra use closely related dye sets, but Portra was not digitised here,
// so for Portra rolls this is an approximation.
//
// IMPORTANT LIMITATION: the chart shows image dyes with D-min subtracted.
// The masking couplers (the orange mask) are built to cancel part of the
// magenta and cyan dyes' unwanted absorption *for printing density*
// (narrow print-stock sensitivities, roughly 445/545/690 nm). That
// cancellation is not in these curves, so a matrix derived from them
// overstates crosstalk. How far it overstates it for a camera scan
// cannot be derived from public data. See DefaultCrosstalk.
//
// CAMERA: Fujifilm does not publish X-T3 (X-Trans 4) spectral
// sensitivities. Modelled as Gaussians typical of published measurements
// of modern Bayer/X-Trans CMOS cameras (e.g. the Jiang et al. 2013 camera
// spectral sensitivity database): B peak 460 nm, G 530 nm, R 600 nm, times
// an IR-cut filter edge at about 655 nm. ASSUMPTION, not a measurement.
//
// LIGHT: either an equal-energy (flat) source, or a high-CRI phosphor
// white LED (blue pump at 450 nm plus a broad phosphor band). ASSUMPTION.

// SpectralCurve is a sampled curve on wavelength in nm (ascending).
type SpectralCurve struct {
	WL []float64
	V  []float64
}

// At interpolates linearly (clamped at the ends, negatives clipped to 0).
func (s SpectralCurve) At(wl float64) float64 {
	n := len(s.WL)
	if wl <= s.WL[0] {
		return math.Max(s.V[0], 0)
	}
	if wl >= s.WL[n-1] {
		return math.Max(s.V[n-1], 0)
	}
	for i := 1; i < n; i++ {
		if wl <= s.WL[i] {
			t := (wl - s.WL[i-1]) / (s.WL[i] - s.WL[i-1])
			return math.Max(s.V[i-1]+t*(s.V[i]-s.V[i-1]), 0)
		}
	}
	return 0
}

// Vision3 dye curves (yellow, magenta, cyan), see the file comment.
var (
	DyeYellow = SpectralCurve{
		WL: []float64{400, 425, 445, 450, 475, 500, 525, 550, 575, 600, 625, 650, 700, 750, 800},
		V:  []float64{0.50, 0.86, 1.00, 1.00, 0.83, 0.42, 0.20, 0.12, 0.07, 0.035, 0.02, 0.015, 0.01, 0.01, 0.01},
	}
	DyeMagenta = SpectralCurve{
		WL: []float64{400, 425, 450, 475, 500, 525, 540, 550, 575, 600, 625, 650, 700, 750, 800},
		V:  []float64{0, 0, 0, 0.15, 0.56, 0.90, 1.00, 0.93, 0.52, 0.20, 0.10, 0.07, 0.05, 0.03, 0.01},
	}
	DyeCyan = SpectralCurve{
		WL: []float64{400, 425, 450, 475, 500, 525, 550, 575, 600, 625, 650, 675, 685, 700, 725, 750, 775, 800},
		V:  []float64{0.20, 0.10, 0.04, 0.02, 0.02, 0.04, 0.09, 0.20, 0.40, 0.64, 0.85, 0.98, 1.00, 0.98, 0.85, 0.62, 0.40, 0.15},
	}
)

// CameraModel is a Gaussian approximation of camera RGB sensitivity.
type CameraModel struct {
	Peak  [3]float64 // nm, R G B
	Sigma [3]float64 // nm
	IRCut float64    // 50% point of the IR-cut edge, nm (0 = none)
}

// XT3Approx is the assumed X-T3 sensitivity (see file comment).
var XT3Approx = CameraModel{Peak: [3]float64{600, 530, 460}, Sigma: [3]float64{30, 38, 28}, IRCut: 655}

// Sens returns channel c's sensitivity at wl.
func (cm CameraModel) Sens(c int, wl float64) float64 {
	x := (wl - cm.Peak[c]) / cm.Sigma[c]
	s := math.Exp(-0.5 * x * x)
	if cm.IRCut > 0 {
		s /= 1 + math.Exp((wl-cm.IRCut)/10)
	}
	return s
}

// Light sources.
const (
	LightFlat = iota // equal energy
	LightLED         // phosphor white LED, high CRI
)

// LightSPD returns the relative spectral power of a light model.
func LightSPD(kind int, wl float64) float64 {
	if kind == LightLED {
		b := (wl - 450) / 11
		p := (wl - 610) / 75
		return 0.9*math.Exp(-0.5*b*b) + 1.0*math.Exp(-0.5*p*p)
	}
	return 1
}

// ChannelDensity is the density camera channel c reads through dye
// amounts amt (Y, M, C peak densities) above base, under light kind:
// -log10( ∫S·L·10^-(Σ amt·dye) / ∫S·L ).
func ChannelDensity(cm CameraModel, light, c int, amt [3]float64) float64 {
	return Vision3Dyes.channelDensity(cm, light, c, amt)
}

func (d DyeSet) channelDensity(cm CameraModel, light, c int, amt [3]float64) float64 {
	var num, den float64
	for wl := 380.0; wl <= 800; wl += 2 {
		w := cm.Sens(c, wl) * LightSPD(light, wl)
		dd := amt[0]*d.Y.At(wl) + amt[1]*d.M.At(wl) + amt[2]*d.C.At(wl)
		num += w * math.Pow(10, -dd)
		den += w
	}
	return -math.Log10(num / den)
}

// CrosstalkJacobian is the forward mixing matrix M linearised at dye
// amounts amt: M[k][i] = dD_k/d(dye i) with channels k = R,G,B and the
// dyes ordered to match (cyan->R, magenta->G, yellow->B), with each column
// scaled so M[i][i] = 1 (dye amounts expressed in "density in their own
// channel").
func CrosstalkJacobian(cm CameraModel, light int, amt [3]float64) Mat3 {
	return Vision3Dyes.Jacobian(cm, light, amt)
}

// Jacobian is CrosstalkJacobian for this dye set.
func (d DyeSet) Jacobian(cm CameraModel, light int, amt [3]float64) Mat3 {
	// dye index for channel: R<-cyan(2), G<-magenta(1), B<-yellow(0)
	dyeOf := [3]int{2, 1, 0}
	var J Mat3
	const h = 1e-4
	for k := 0; k < 3; k++ {
		for i := 0; i < 3; i++ {
			a1, a2 := amt, amt
			a1[dyeOf[i]] -= h
			a2[dyeOf[i]] += h
			J[k][i] = (d.channelDensity(cm, light, k, a2) - d.channelDensity(cm, light, k, a1)) / (2 * h)
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

// DerivedCrosstalk returns the correction matrix (M^-1, rows normalised
// to 1 so neutrals are untouched) for one camera/light model at a
// mid-scale working point of 0.5 D of each dye.
func DerivedCrosstalk(cm CameraModel, light int) (corr, forward Mat3) {
	m := CrosstalkJacobian(cm, light, [3]float64{0.5, 0.5, 0.5})
	inv, err := m.Inverse()
	if err != nil {
		return Identity, m
	}
	return inv.NormalizeRows(), m
}

// MaskAware removes from a forward matrix the unwanted absorptions that a
// colour negative's coloured masking couplers are designed to cancel:
// cyan dye seen by the G and B channels (orange-coloured cyan coupler) and
// magenta dye seen by the B channel (yellow-coloured magenta coupler). The
// remaining terms (magenta seen by camera R, yellow seen by camera G,
// yellow seen by camera R) come from the camera's broad, shifted bands and
// are not masked, because masking is tuned for print-stock bands. This is
// a physically motivated approximation, not a measurement.
func MaskAware(fwd Mat3) Mat3 {
	m := fwd
	m[1][0], m[2][0], m[2][1] = 0, 0, 0
	return m
}

// SpectralForward is the forward model at the default assumptions (flat
// light, XT3Approx), without masking.
func SpectralForward() Mat3 {
	return CrosstalkJacobian(XT3Approx, LightFlat, [3]float64{0.5, 0.5, 0.5})
}

// DefaultCrosstalk is the built-in correction matrix (applied in density
// above base, R G B): inverse of the mask-aware spectral forward model,
// rows normalised so neutrals keep their density. With the default
// assumptions it is about
//
//	[ 1.63 -0.82  0.19 ]
//	[ 0     1.54 -0.54 ]
//	[ 0     0     1    ]
//
// Treat it as a starting point: the camera curves are assumed and the
// masking is modelled crudely. A matrix from SolveCrosstalk on a
// ColorChecker shot on the user's own film, light and camera replaces it.
func DefaultCrosstalk() Mat3 {
	inv, err := MaskAware(SpectralForward()).Inverse()
	if err != nil {
		return Identity
	}
	return inv.NormalizeRows()
}

// UnmaskedCrosstalk is the inverse of the full spectral model with no
// masking (overstates crosstalk; kept for comparison).
func UnmaskedCrosstalk() Mat3 {
	inv, err := SpectralForward().Inverse()
	if err != nil {
		return Identity
	}
	return inv.NormalizeRows()
}
