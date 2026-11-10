package convert

import (
	"math"
	"strings"
	"sync"

	"basetool/colorsci"
)

// stockLook is the per-stock colour character that survives OFX inversion.
//
// OFX Positive-Out undoes a 1D H&D curve per channel (a gamma/fog model
// fitted to Kodak mid-slopes). Anything that is only a per-channel 1D curve
// is therefore approximately erased in the inverted positive. Film look that
// remains after a correct invert lives in the steps OFX does not undo:
//
//   - camera RGB -> film-layer exposure (spectral sensitivity mismatch)
//   - interlayer / DIR interimage, applied in log-exposure so a grey (equal
//     layer exposures) is unchanged — Status M curve separations are not
//     mistaken for chroma
//   - dye unwanted absorption applied only to the chromatic residual around
//     the grey-axis Status M densities (the published curves already include
//     dye mix for neutrals; remixing a grey would double-count and cast it)
//   - a modest system contrast on linear exposure (print-like snap); OFX
//     linearises the H&D only, so this contrast remains
//   - a per-channel density offset so a grey card inverts to grey under the
//     stock's OFX gamma triplet (the gamma model is not a perfect inverse
//     of the CSV / logistic curves)
//
// Interimage and system-contrast numbers are NOT Kodak datasheet readings:
// Kodak does not publish DIR coupler strengths. They are character
// parameters chosen from published design intent (Portra: natural skin,
// lower saturation; Ektar: high saturation; Gold: warm/yellow; UltraMax:
// punchy warm; Vision3: motion-picture, restrained). Dye spectra and
// H&D curves are from the digitised Kodak datasheets already in this repo.
type stockLook struct {
	Interimage float64 // log-exposure saturation (DIR proxy); 0 = off
	Warm       float64 // extra yellow in chromatic colours; 0 = neutral hue
	Contrast   float64 // linear-domain system contrast about mid-grey; 1 = off
	Chroma     float64 // scale of dye-mix chromatic residual; 1 = off
	DensOff    [3]float64
	OffLUT     [3]densLUT // per-channel offset vs logRel; mid-grey matches DensOff
	DyeMix     colorsci.Mat3
	FilmSens   colorsci.Mat3
}

var (
	lookOnce sync.Once
	lookMap  map[string]stockLook
	filmSens colorsci.Mat3
)

func lookFor(name string) stockLook {
	lookOnce.Do(initLooks)
	if lk, ok := lookMap[name]; ok {
		return lk
	}
	return makeLook(name, filmSens)
}

func initLooks() {
	filmSens = cameraToFilmMatrix()
	lookMap = map[string]stockLook{}
	for _, s := range Stocks() {
		lookMap[s.Name] = makeLook(s.Name, filmSens)
	}
}

func makeLook(name string, fm colorsci.Mat3) stockLook {
	// Published Status M H&D curves already include dye unwanted
	// absorption along the grey scale. Remixing chromatic residuals
	// through the scanner Jacobian therefore double-applies that mix
	// and rotates greens toward yellow (olive/mustard). Leave DyeMix
	// at identity; film colour character comes from spectral
	// sensitivity, DIR, warmth, contrast, and the per-stock curves.
	lk := stockLook{
		Interimage: 0.16,
		Warm:       0.02,
		Contrast:   1.16,
		Chroma:     1,
		DyeMix:     colorsci.Identity,
		FilmSens:   fm,
	}
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "ektar"):
		// Kodak E-4046: "finest, most saturated color" / vivid colour.
		lk.Interimage, lk.Warm, lk.Contrast, lk.Chroma = 0.32, 0.00, 1.24, 1
	case strings.Contains(n, "portra"):
		// Kodak E-4050: natural skin-tone reproduction, not drab.
		lk.Interimage, lk.Warm, lk.Contrast, lk.Chroma = 0.16, 0.04, 1.18, 1
	case strings.Contains(n, "gold"):
		// Kodak E-7022: warm, saturated consumer colour.
		lk.Interimage, lk.Warm, lk.Contrast, lk.Chroma = 0.22, 0.10, 1.20, 1
	case strings.Contains(n, "ultramax"):
		// Kodak E-7023: rich, saturated, slightly warm consumer colour.
		lk.Interimage, lk.Warm, lk.Contrast, lk.Chroma = 0.28, 0.06, 1.24, 1
	case strings.Contains(n, "500t") || strings.Contains(n, "5219"):
		// H-1-5219: tungsten motion-picture stock, wide latitude, restrained.
		lk.Interimage, lk.Warm, lk.Contrast, lk.Chroma = 0.14, 0.02, 1.12, 1
	case strings.Contains(n, "250d") || strings.Contains(n, "5207"):
		// H-1-5207: daylight Vision3 sibling of 500T.
		lk.Interimage, lk.Warm, lk.Contrast, lk.Chroma = 0.16, 0.00, 1.14, 1
	}
	s, err := StockByName(name)
	if err == nil {
		lk.DensOff = alignDensOffsets(s)
		lk.OffLUT = buildOffLUT(s)
	}
	return lk
}

// cameraToFilmMatrix maps camera-linear RGB to film-layer exposures.
//
// Scene-referred input is linear sRGB (as-shot WB + camera matrix).
// Approximate scene-RGB sensitivities as Gaussians around 625/545/460 nm
// (display-referred RGB, not raw X-T3 camera RGB). Film layers: Gaussian
// approximation of published colour-negative spectral-sensitivity charts
// (Kodak H-1-5219 p.3 and still-film charts): red-sensitive ~650 nm,
// green-sensitive ~550 nm, blue-sensitive ~430 nm. Row normalisation keeps
// a scene grey as a film-layer grey.
func cameraToFilmMatrix() colorsci.Mat3 {
	film := colorsci.CameraModel{Peak: [3]float64{650, 550, 430}, Sigma: [3]float64{36, 32, 26}}
	cam := colorsci.CameraModel{Peak: [3]float64{625, 545, 460}, Sigma: [3]float64{40, 34, 28}, IRCut: 700}
	var m colorsci.Mat3
	for c := 0; c < 3; c++ {
		for k := 0; k < 3; k++ {
			var sum float64
			for wl := 380.0; wl <= 780; wl += 5 {
				sum += film.Sens(c, wl) * cam.Sens(k, wl)
			}
			m[c][k] = sum
		}
	}
	return m.NormalizeRows()
}

// ofxLin is the OFX Positive-Out linear reconstruction of one channel
// (matching dctl_math.hpp / develop.compilePositive with default white_code
// 696, no balance, no grey picker). Density In maps T = 0.5*10^(-D) with
// base 0.5 back to D, so the input here is density above base.
func ofxLin(D, gam, expo float64) float64 {
	if gam < 1e-6 {
		gam = 1e-6
	}
	const dw = (696.0 - 95.0) * 0.002 // 1.202
	fog := math.Pow(10, -dw/gam)
	core := math.Pow(10, (D-dw)/gam)
	lr := math.Exp2(expo) * (core - fog) / (1 - fog)
	if lr < 0 {
		return 0
	}
	return lr
}

// ofxDens is the inverse of ofxLin(..., expo=0): density that reconstructs lin.
func ofxDens(lin, gam float64) float64 {
	if gam < 1e-6 {
		gam = 1e-6
	}
	const dw = (696.0 - 95.0) * 0.002
	fog := math.Pow(10, -dw/gam)
	x := lin*(1-fog) + fog
	if x <= 1e-12 {
		return 0
	}
	return dw + gam*math.Log10(x)
}

func buildOffLUT(s Stock) [3]densLUT {
	cs := CurveFor(s.Name)
	o := Options{Stock: s}
	const n = 1024
	lo, hi := -5.0, 4.0
	step := (hi - lo) / float64(n-1)
	var out [3]densLUT
	for c := 0; c < 3; c++ {
		v := make([]float64, n)
		for i := 0; i < n; i++ {
			rel := lo + float64(i)*step
			h := MidGrey * math.Pow(10, rel)
			dG := densityAboveFor(h, 1, o, cs)
			target := ofxLin(dG, s.Gamma[1], 0)
			dC := densityAboveFor(h, c, o, cs)
			v[i] = ofxDens(target, s.Gamma[c]) - dC
		}
		out[c] = densLUT{lo: lo, step: step, v: v}
	}
	return out
}

// alignDensOffsets finds per-channel density offsets so a mid-grey card
// inverts to equal RGB under the stock's OFX gamma triplet. Green is the
// reference (offset 0). Applied after H&D so DIR / dye (which preserve
// greys) do not undo the match.
func alignDensOffsets(s Stock) [3]float64 {
	cs := CurveFor(s.Name)
	o := Options{Stock: s}
	var dMid [3]float64
	for c := 0; c < 3; c++ {
		dMid[c] = densityAboveFor(MidGrey, c, o, cs)
	}
	target := ofxLin(dMid[1], s.Gamma[1], 0)
	var off [3]float64
	for c := 0; c < 3; c++ {
		if c == 1 {
			continue
		}
		lo, hi := -1.5, 2.5
		for k := 0; k < 40; k++ {
			mid := 0.5 * (lo + hi)
			if ofxLin(mid, s.Gamma[c], 0) < target {
				lo = mid
			} else {
				hi = mid
			}
		}
		off[c] = 0.5*(lo+hi) - dMid[c]
	}
	return off
}

func applyContrast(v [3]float64, p float64) [3]float64 {
	if p <= 0 || p == 1 {
		return v
	}
	for c := 0; c < 3; c++ {
		if v[c] > 0 {
			v[c] = MidGrey * math.Pow(v[c]/MidGrey, p)
		}
	}
	return v
}

// applyLogDIR applies interimage and warmth in log10-exposure (relative to
// mid-grey). Equal layer exposures (a grey) are unchanged.
func applyLogDIR(rel [3]float64, lk stockLook) [3]float64 {
	mean := (rel[0] + rel[1] + rel[2]) / 3
	s := lk.Interimage
	out := [3]float64{
		mean + (1+s)*(rel[0]-mean),
		mean + (1+s)*(rel[1]-mean),
		mean + (1+s)*(rel[2]-mean),
	}
	if lk.Warm != 0 {
		out[2] -= lk.Warm * (out[0] + out[1] - 2*out[2]) / 2
	}
	return out
}

// applyDyeChroma remixes only the chromatic residual around the grey-axis
// Status M densities at the mean log exposure. A grey (equal logH) has a
// zero residual and is left as the published per-channel curve values.
func applyDyeChroma(d, rel [3]float64, o Options, cs *CurveSet, lk stockLook) [3]float64 {
	mean := (rel[0] + rel[1] + rel[2]) / 3
	hMean := MidGrey * math.Pow(10, mean)
	var axis [3]float64
	for c := 0; c < 3; c++ {
		axis[c] = densityAboveFor(hMean, c, o, cs)
	}
	chroma := [3]float64{d[0] - axis[0], d[1] - axis[1], d[2] - axis[2]}
	chroma = scaleChroma(lk.DyeMix.Apply(chroma), lk.Chroma)
	return [3]float64{axis[0] + chroma[0], axis[1] + chroma[1], axis[2] + chroma[2]}
}

func applyDyeChromaLUT(d, rel [3]float64, luts [3]densLUT, lk stockLook) [3]float64 {
	mean := (rel[0] + rel[1] + rel[2]) / 3
	var axis [3]float64
	for c := 0; c < 3; c++ {
		axis[c] = luts[c].at(mean)
	}
	chroma := [3]float64{d[0] - axis[0], d[1] - axis[1], d[2] - axis[2]}
	chroma = scaleChroma(lk.DyeMix.Apply(chroma), lk.Chroma)
	return [3]float64{axis[0] + chroma[0], axis[1] + chroma[1], axis[2] + chroma[2]}
}

func scaleChroma(chroma [3]float64, g float64) [3]float64 {
	if g <= 0 || g == 1 {
		return chroma
	}
	return [3]float64{chroma[0] * g, chroma[1] * g, chroma[2] * g}
}

// sceneToDensity is the shared Convert colour path.
func sceneToDensity(v [3]float64, o Options, cs *CurveSet, lk stockLook) [3]float64 {
	v = lk.FilmSens.Apply(v)
	for c := 0; c < 3; c++ {
		if v[c] < 0 {
			v[c] = 0
		}
	}
	v = applyContrast(v, lk.Contrast)
	var rel [3]float64
	for c := 0; c < 3; c++ {
		rel[c] = math.Log10(math.Max(v[c], 1e-12) / MidGrey)
	}
	rel = applyLogDIR(rel, lk)
	var d [3]float64
	for c := 0; c < 3; c++ {
		h := MidGrey * math.Pow(10, rel[c])
		d[c] = densityAboveFor(h, c, o, cs)
	}
	d = applyDyeChroma(d, rel, o, cs, lk)
	for c := 0; c < 3; c++ {
		if len(lk.OffLUT[c].v) > 0 {
			d[c] += lk.OffLUT[c].at(rel[c])
		} else {
			d[c] += lk.DensOff[c]
		}
		if d[c] < 0 {
			d[c] = 0
		}
	}
	return d
}

func transmittance(d [3]float64) [3]float64 {
	var t [3]float64
	for c := 0; c < 3; c++ {
		x := BaseFS * math.Pow(10, -d[c])
		if x < 0 {
			x = 0
		}
		if x > 1 {
			x = 1
		}
		t[c] = x
	}
	return t
}

// applyLook keeps a density-domain no-op on neutrals for tests that feed
// already-equal densities through the dye matrix.
func applyLook(d [3]float64, lk stockLook) [3]float64 {
	return lk.DyeMix.Apply(d)
}
