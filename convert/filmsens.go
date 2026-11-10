package convert

import (
	"math"
	"strings"

	"basetool/colorsci"
)

// filmExposureMatrix maps white-balanced camera-native RGB to film-layer
// exposures, row-normalised so a camera grey stays a film-layer grey.
//
// The input is the same space as a scanned TIFF: demosaiced linear
// camera RGB (X-T3 / X-Pro3 X-Trans), not sRGB, Rec.709, ProPhoto or
// XYZ. Camera channel k is weighted by the measured X-T3 spectral
// sensitivity (colorsci.XT3Measured, Weta / rawtoaces-data, Apache-2.0).
// Each film layer c is weighted by that stock's spectral sensitivity.
// The 3x3 is the illuminant-weighted overlap
//
//	M[c][k] = Σ film_sens_c(λ) · I(λ) · cam_sens_k(λ)
//
// so film exposure is reached by a camera-to-film sensitivity mapping,
// not by converting through a standard colour space.
//
// Film sensitivities:
//   - Vision3 500T 5219: Kodak H-1-5219 log-sensitivity CSV
//   - Vision3 250D 5207: Kodak H-1-5207 log-sensitivity CSV
//   - Still films: Gaussian approximation of the published Kodak
//     spectral-sensitivity chart shapes (peaks near 650/550/430 nm).
//     Peaks and widths are character parameters, not digitised point
//     readings. A small secondary blue response (~8 % of the main
//     peak) is included on the red and green layers.
//
// Illuminants:
//   - 500T: Planck 3200 K (Kodak tungsten)
//   - Daylight / still stocks: Planck 5500 K as a D55 stand-in
func filmExposureMatrix(name string) colorsci.Mat3 {
	film := stockFilmSens(name)
	illumK := 5500.0
	n := strings.ToLower(name)
	if strings.Contains(n, "500t") || strings.Contains(n, "5219") {
		illumK = 3200
	}
	cam := colorsci.XT3Measured
	var m colorsci.Mat3
	for c := 0; c < 3; c++ {
		for k := 0; k < 3; k++ {
			var sum float64
			for wl := 380.0; wl <= 780; wl += 5 {
				sum += film.sens(c, wl) * colorsci.PlanckRelative(wl, illumK) * cam.Sens(k, wl)
			}
			m[c][k] = sum
		}
	}
	return m.NormalizeRows()
}

type filmSensFn struct {
	// Measured CSV (Vision3). logS may contain NaNs for missing samples.
	wl   []float64
	logS [3][]float64
	csv  bool
	// Gaussian fallback / still-film approximation.
	peak, sigma [3]float64
	// Secondary blue-region peak for R and G layers (still-film approx).
	secPeak, secSigma, secAmp [3]float64
}

func (f filmSensFn) sens(c int, wl float64) float64 {
	if f.csv && len(f.wl) > 0 {
		return colorsci.SensLinear(f.wl, f.logS[c], wl)
	}
	x := (wl - f.peak[c]) / f.sigma[c]
	s := math.Exp(-0.5 * x * x)
	if f.secAmp[c] > 0 && f.secSigma[c] > 0 {
		y := (wl - f.secPeak[c]) / f.secSigma[c]
		s += f.secAmp[c] * math.Exp(-0.5*y*y)
	}
	return s
}

func stockFilmSens(name string) filmSensFn {
	if wl, logS, ok := colorsci.LogSensitivity(name); ok {
		return filmSensFn{wl: wl, logS: logS, csv: true}
	}
	// Still-film Gaussian approximations of published chart shapes.
	// Secondary blue response ~8 % of the main peak (typical of the
	// small short-wavelength hump drawn on Kodak still-film charts).
	f := filmSensFn{
		peak:     [3]float64{650, 550, 430},
		sigma:    [3]float64{36, 32, 26},
		secPeak:  [3]float64{430, 430, 0},
		secSigma: [3]float64{22, 22, 1},
		secAmp:   [3]float64{0.08, 0.08, 0},
	}
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "ektar"):
		// Narrower bands: more selective layers, higher purity.
		f.peak = [3]float64{652, 548, 428}
		f.sigma = [3]float64{32, 28, 24}
		f.secAmp = [3]float64{0.05, 0.05, 0}
	case strings.Contains(n, "portra"):
		// Slightly broader green record (gentler skin / foliage).
		f.peak = [3]float64{648, 552, 432}
		f.sigma = [3]float64{40, 36, 28}
		f.secAmp = [3]float64{0.08, 0.09, 0}
	case strings.Contains(n, "gold"):
		// Red record shifted longer: warmer reds/yellows.
		f.peak = [3]float64{656, 550, 430}
		f.sigma = [3]float64{38, 34, 26}
		f.secAmp = [3]float64{0.07, 0.07, 0}
	case strings.Contains(n, "ultramax"):
		f.peak = [3]float64{654, 548, 428}
		f.sigma = [3]float64{36, 32, 26}
		f.secAmp = [3]float64{0.07, 0.07, 0}
	}
	return f
}
