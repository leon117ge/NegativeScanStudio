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

// grainLayer is one emulsion sublayer (fast / mid / slow).
type grainLayer struct {
	delta, sigma float64
}

// grainParams returns fast/mid/slow dye-cloud quanta and blur σ in pixels
// for a 36x24 mm frame of width refW. Cloud sizes are specified in microns
// and mapped through the pixel pitch (36000 µm / refW).
func grainParams(refW int, stock string, iso GrainISO, strength, sizeMul float64) (layers [3]grainLayer, prof GrainProfile) {
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
	// Physical dye-cloud diameters at ISO 400, then scaled by prof.Size.
	// Fast (large) records low exposure; slow (small) records high exposure.
	um := [3]float64{18.0 * prof.Size, 10.0 * prof.Size, 5.0 * prof.Size}
	delta0 := [3]float64{0.020 * prof.Amp, 0.012 * prof.Amp, 0.006 * prof.Amp}
	pxUM := 36000.0 / float64(refW)
	minSig := [3]float64{0.70, 0.50, 0.40}
	for i := 0; i < 3; i++ {
		sig := (um[i] * sizeMul) / pxUM
		if sig < minSig[i] {
			sig = minSig[i]
		}
		layers[i] = grainLayer{delta: delta0[i] * strength, sigma: sig}
	}
	return layers, prof
}

// layerWeights returns fast/mid/slow mixing weights at density D.
// High density (optically darker negative) shifts weight onto the
// slower, finer grains; thin negative areas are dominated by the
// larger fast grains. Weights always sum to 1.
func layerWeights(D float64) (wFast, wMid, wSlow float64) {
	t := D / 0.85
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	wFast = 0.55*(1-t) + 0.15*t
	wSlow = 0.15*(1-t) + 0.50*t
	wMid = 1 - wFast - wSlow
	if wMid < 0.10 {
		wMid = 0.10
		s := wFast + wSlow
		if s > 0 {
			k := (1 - wMid) / s
			wFast *= k
			wSlow *= k
		}
	}
	return wFast, wMid, wSlow
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
	layers, prof := grainParams(refW, stock, iso, strength, sizeMul)
	sigmaEq = layers[0].sigma * 2.0
	if sigmaEq < 2 {
		sigmaEq = 2
	}
	if w*h < 2_500_000 && sigmaEq > 5 {
		sigmaEq = 5 // preview: keep LF match, less blur work
	}

	n := w * h
	plane := [3][3][]float64{} // [layer][channel]
	for li := 0; li < 3; li++ {
		for c := 0; c < 3; c++ {
			plane[li][c] = make([]float64, n)
		}
	}
	shared := [3][]float64{make([]float64, n), make([]float64, n), make([]float64, n)}
	chN := [3][3][]float64{}
	for li := 0; li < 3; li++ {
		for c := 0; c < 3; c++ {
			chN[li][c] = make([]float64, n)
		}
	}
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < n; i++ {
		for li := 0; li < 3; li++ {
			shared[li][i] = rng.NormFloat64()
			for c := 0; c < 3; c++ {
				chN[li][c][i] = rng.NormFloat64()
			}
		}
	}
	load := prof.ChanLoad
	cs := prof.ChanScale

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
						wf, wm, ws := layerWeights(D)
						ww := [3]float64{wf, wm, ws}
						lc := load[c]
						if lc < 0 {
							lc = 0
						}
						if lc > 0.98 {
							lc = 0.98
						}
						sCorr := math.Sqrt(1 - lc*lc)
						scale := 1.0
						if prof.DensRef[c] > 0 {
							// Mild vs-density flatten (exponent 0.25, not
							// 0.5) so Poisson still varies with D.
							scale = math.Pow(prof.DensRef[c]/math.Max(D, 0.04), 0.25)
						}
						for li := 0; li < 3; li++ {
							del := layers[li].delta
							lam := ww[li] * D / del
							z := lc*shared[li][i] + sCorr*chN[li][c][i]
							nn := lam + math.Sqrt(math.Max(lam, 1e-8))*z
							if nn < 0 {
								nn = 0
							}
							plane[li][c][i] = (nn*del - ww[li]*D) * cs[c] * scale
						}
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
		for li := 0; li < 3; li++ {
			sig := layers[li].sigma * sz
			if sig < 0.35 {
				sig = 0.35
			}
			blurInPlaceFast(plane[li][c], w, h, sig, passes)
		}
	}

	// Combine sublayers and subtract the per-channel mean so clipping of
	// negative Poisson samples cannot lift or drop mean density.
	var sum [3]float64
	comb := [3][]float64{make([]float64, n), make([]float64, n), make([]float64, n)}
	for i := 0; i < n; i++ {
		for c := 0; c < 3; c++ {
			g := plane[0][c][i] + plane[1][c][i] + plane[2][c][i]
			comb[c][i] = g
			sum[c] += g
		}
	}
	invN := 1 / float64(n)
	for i := 0; i < n; i++ {
		for c := 0; c < 3; c++ {
			v := d[i*3+c] + comb[c][i] - sum[c]*invN
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
