package convert

import (
	"math"
	"math/rand"
	"sync"
)

// GrainISO selects Off / ISO 100 / ISO 400 film grain.
type GrainISO int

const (
	GrainOff GrainISO = iota
	GrainISO100
	GrainISO400
)

func (g GrainISO) Label() string {
	switch g {
	case GrainISO100:
		return "ISO 100"
	case GrainISO400:
		return "ISO 400"
	default:
		return "Off"
	}
}

// stockGrainScale: relative dye-cloud density quantum (smaller = finer /
// more grains). Tuned; Ektar extremely fine. Not a Kodak PGI table copy.
func stockGrainScale(name string) float64 {
	switch name {
	case "Ektar 100":
		return 0.55
	case "Portra 400":
		return 1.0
	case "Gold 200":
		return 1.05
	case "UltraMax 400":
		return 1.2
	case "Vision3 500T 5219":
		return 1.15
	case "Vision3 250D 5207":
		return 1.05
	default:
		return 1.0
	}
}

// grainParams returns dye-cloud density quantum δ (fast/slow) and blur σ in
// pixels for a 36x24 mm frame at width w.
func grainParams(w int, stock string, iso GrainISO, strength, sizeMul float64) (deltaFast, deltaSlow, sigmaFast, sigmaSlow float64) {
	if strength <= 0 {
		strength = 1
	}
	if sizeMul <= 0 {
		sizeMul = 1
	}
	// Base quantum (Status-M density per dye cloud at Portra / ISO100).
	df, ds := 0.012, 0.004
	umFast, umSlow := 18.0, 7.0
	if iso == GrainISO400 {
		df, ds = 0.028, 0.009
		umFast, umSlow = 32.0, 12.0
	}
	s := stockGrainScale(stock) * strength
	df *= s
	ds *= s
	pxUM := 36000.0 / float64(w)
	sigmaFast = (umFast * sizeMul) / pxUM
	sigmaSlow = (umSlow * sizeMul) / pxUM
	if sigmaFast < 0.8 {
		sigmaFast = 0.8
	}
	if sigmaSlow < 0.5 {
		sigmaSlow = 0.5
	}
	return df, ds, sigmaFast, sigmaSlow
}

// applyGrainPoisson replaces density-above-Dmin samples with a Poisson /
// normal dye-cloud realisation (fast+slow sublayers), then lightly blurs.
// Expected density is preserved before the transmittance equalisation step
// in ProcessPixels. Approach inspired by spektrafilm's stochastic negative
// grain (GPLv3 — ideas only, no code copied).
func applyGrainPoisson(d []float64, w, h int, stock string, iso GrainISO, seed int64, strength, sizeMul float64) (sigmaEq float64) {
	if iso == GrainOff || w < 8 || h < 8 {
		return 0
	}
	df, ds, sigF, sigS := grainParams(w, stock, iso, strength, sizeMul)
	// Equalisation blur a bit larger than the coarse cloud so LF tone is matched.
	sigmaEq = sigF * 2.0
	if sigmaEq < 2 {
		sigmaEq = 2
	}
	if w*h < 2_500_000 && sigmaEq > 5 {
		sigmaEq = 5 // preview: keep LF match, less blur work
	}

	n := w * h
	// Work planes: realised density per channel.
	planes := [3][]float64{make([]float64, n), make([]float64, n), make([]float64, n)}
	// Shared / per-channel unit normals for partial correlation of counts.
	sharedF := make([]float64, n)
	sharedS := make([]float64, n)
	chF := [3][]float64{make([]float64, n), make([]float64, n), make([]float64, n)}
	chS := [3][]float64{make([]float64, n), make([]float64, n), make([]float64, n)}
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < n; i++ {
		sharedF[i] = rng.NormFloat64()
		sharedS[i] = rng.NormFloat64()
		for c := 0; c < 3; c++ {
			chF[c][i] = rng.NormFloat64()
			chS[c][i] = rng.NormFloat64()
		}
	}
	const corr = 0.35
	sCorr := math.Sqrt(1 - corr*corr)

	// Split mean density ~60% slow (fine) / 40% fast (coarse) dye clouds.
	const wFast, wSlow = 0.40, 0.60

	var wg sync.WaitGroup
	workers := 8
	if h < workers {
		workers = h
	}
	chunk := (h + workers - 1) / workers
	for wi := 0; wi < workers; wi++ {
		y0 := wi * chunk
		y1 := y0 + chunk
		if y1 > h {
			y1 = h
		}
		if y0 >= y1 {
			continue
		}
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			for y := y0; y < y1; y++ {
				for x := 0; x < w; x++ {
					i := y*w + x
					for c := 0; c < 3; c++ {
						D := math.Max(d[i*3+c], 0)
						// Expected cloud counts from mean density.
						lamF := wFast * D / df
						lamS := wSlow * D / ds
						zF := corr*sharedF[i] + sCorr*chF[c][i]
						zS := corr*sharedS[i] + sCorr*chS[c][i]
						// Normal approx to Poisson: N = λ + √λ Z, E[N]=λ.
						nF := lamF + math.Sqrt(math.Max(lamF, 1e-8))*zF
						nS := lamS + math.Sqrt(math.Max(lamS, 1e-8))*zS
						if nF < 0 {
							nF = 0
						}
						if nS < 0 {
							nS = 0
						}
						planes[c][i] = nF*df + nS*ds
					}
				}
			}
		}(y0, y1)
	}
	wg.Wait()

	// Dye-cloud spatial extent: blur each channel (fast then mix with slow).
	// Approximate two scales by blurring the whole plane at an intermediate σ,
	// then blending with a finer-blurred copy.
	passes := 3
	if w*h < 2_500_000 {
		passes = 2
	}
	for c := 0; c < 3; c++ {
		fine := append([]float64(nil), planes[c]...)
		blurInPlaceFast(planes[c], w, h, sigF, passes)
		blurInPlaceFast(fine, w, h, sigS, passes)
		for i := 0; i < n; i++ {
			// Re-weight so mean of blend ≈ mean of pre-blur (box blur preserves mean).
			planes[c][i] = wFast*planes[c][i] + wSlow*fine[i]
		}
	}

	for i := 0; i < n; i++ {
		for c := 0; c < 3; c++ {
			d[i*3+c] = planes[c][i]
		}
	}
	return sigmaEq
}

// equalizeTransmittance scales grainy T so its local average matches clean T.
// This keeps mean tone and low-frequency contrast; only HF grain remains.
func equalizeTransmittance(grainy, clean []float64, w, h int, sigma float64) {
	if sigma < 1 {
		sigma = 1
	}
	n := w * h
	bg := make([]float64, n)
	bc := make([]float64, n)
	for c := 0; c < 3; c++ {
		for i := 0; i < n; i++ {
			bg[i] = grainy[i*3+c]
			bc[i] = clean[i*3+c]
		}
		blurInPlaceFast(bg, w, h, sigma, 2)
		blurInPlaceFast(bc, w, h, sigma, 2)
		for i := 0; i < n; i++ {
			g := bg[i]
			if g < 1e-8 {
				g = 1e-8
			}
			grainy[i*3+c] *= bc[i] / g
			if grainy[i*3+c] < 0 {
				grainy[i*3+c] = 0
			}
			if grainy[i*3+c] > 1 {
				grainy[i*3+c] = 1
			}
		}
	}
}

// Separable box-filter passes approximate a Gaussian (mean-preserving).
func blurInPlace(buf []float64, w, h int, sigma float64) {
	blurInPlaceFast(buf, w, h, sigma, 3)
}

