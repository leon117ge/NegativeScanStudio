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

// grainParams returns dye-cloud density quantum δ (fast/slow) and blur σ in
// pixels for a 36x24 mm frame of width refW.
func grainParams(refW int, stock string, iso GrainISO, strength, sizeMul float64) (deltaFast, deltaSlow, sigmaFast, sigmaSlow float64, prof GrainProfile) {
	if strength <= 0 {
		strength = 1
	}
	if sizeMul <= 0 {
		sizeMul = 1
	}
	if refW < 8 {
		refW = 8
	}
	prof = grainProfile(stock, iso)
	// Base quantum; Amp from 500T calibration.
	df, ds := 0.018*prof.Amp, 0.006*prof.Amp
	umFast, umSlow := 14.0*prof.Size, 5.5*prof.Size
	df *= strength
	ds *= strength
	pxUM := 36000.0 / float64(refW)
	sigmaFast = (umFast * sizeMul) / pxUM
	sigmaSlow = (umSlow * sizeMul) / pxUM
	if sigmaFast < 0.7 {
		sigmaFast = 0.7
	}
	if sigmaSlow < 0.45 {
		sigmaSlow = 0.45
	}
	return df, ds, sigmaFast, sigmaSlow, prof
}

// applyGrainPoisson replaces density-above-Dmin samples with a Poisson /
// normal dye-cloud realisation (fast+slow sublayers), then blurs only the
// zero-mean fluctuation planes so image detail stays sharp.
// Expected density is preserved before the transmittance equalisation step
// in ProcessPixels.
func applyGrainPoisson(d []float64, w, h int, stock string, iso GrainISO, seed int64, strength, sizeMul float64) (sigmaEq float64) {
	return applyGrainAtScale(d, w, h, w, stock, iso, seed, strength, sizeMul)
}

// applyGrainAtScale is applyGrainPoisson but computes dye-cloud size from
// refW (pixels across 36 mm) instead of the buffer width. Used so a 512 px
// calibration patch can be measured at the scan's 6246 px scale.
func applyGrainAtScale(d []float64, w, h, refW int, stock string, iso GrainISO, seed int64, strength, sizeMul float64) (sigmaEq float64) {
	if iso == GrainOff || w < 8 || h < 8 {
		return 0
	}
	if refW < 8 {
		refW = w
	}
	df, ds, sigF, sigS, prof := grainParams(refW, stock, iso, strength, sizeMul)
	// Equalisation blur a bit larger than the coarse cloud so LF tone is matched.
	sigmaEq = sigF * 2.0
	if sigmaEq < 2 {
		sigmaEq = 2
	}
	if w*h < 2_500_000 && sigmaEq > 5 {
		sigmaEq = 5 // preview: keep LF match, less blur work
	}

	n := w * h
	fastP := [3][]float64{make([]float64, n), make([]float64, n), make([]float64, n)}
	slowP := [3][]float64{make([]float64, n), make([]float64, n), make([]float64, n)}
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
	load := prof.ChanLoad
	cs := prof.ChanScale

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
						lamF := wFast * D / df
						lamS := wSlow * D / ds
						lc := load[c]
						if lc < 0 {
							lc = 0
						}
						if lc > 0.98 {
							lc = 0.98
						}
						sCorr := math.Sqrt(1 - lc*lc)
						zF := lc*sharedF[i] + sCorr*chF[c][i]
						zS := lc*sharedS[i] + sCorr*chS[c][i]
						// Normal approx to Poisson: N = λ + √λ Z, E[N]=λ.
						nF := lamF + math.Sqrt(math.Max(lamF, 1e-8))*zF
						nS := lamS + math.Sqrt(math.Max(lamS, 1e-8))*zS
						if nF < 0 {
							nF = 0
						}
						if nS < 0 {
							nS = 0
						}
						// Store only the zero-mean grain fluctuation; the image
						// density itself must not be blurred (v0.96 blurred the
						// whole frame, which made exports soft).
						scale := 1.0
						if prof.DensRef[c] > 0 {
							scale = math.Sqrt(prof.DensRef[c] / math.Max(D, 0.04))
						}
						fastP[c][i] = (nF*df - wFast*D) * cs[c] * scale
						slowP[c][i] = (nS*ds - wSlow*D) * cs[c] * scale
					}
				}
			}
		}(y0, y1)
	}
	wg.Wait()

	passes := 3
	if w*h < 2_500_000 {
		passes = 2
	}
	for c := 0; c < 3; c++ {
		sz := chanSizeMul(prof, c)
		sigFc := sigF * sz
		sigSc := sigS * sz
		if sigFc < 0.7 {
			sigFc = 0.7
		}
		if sigSc < 0.45 {
			sigSc = 0.45
		}
		blurInPlaceFast(fastP[c], w, h, sigFc, passes)
		blurInPlaceFast(slowP[c], w, h, sigSc, passes)
	}

	for i := 0; i < n; i++ {
		for c := 0; c < 3; c++ {
			v := d[i*3+c] + fastP[c][i] + slowP[c][i]
			if v < 0 {
				v = 0
			}
			d[i*3+c] = v
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
