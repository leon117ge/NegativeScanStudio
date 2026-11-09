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

// stockGrainScale is relative RMS strength (tuned; Ektar extremely fine,
// Portra/Gold/UltraMax fine, Vision3 motion stocks a bit coarser). Not a
// direct copy of Kodak print-grain index tables.
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

// applyGrain adds density-domain grain to D-above arrays (same layout as rgb
// but holding density above Dmin). w,h are image size; seed is deterministic.
// Inspired by the spektrafilm idea of stochastic grain on the negative in
// density space (GPLv3 — approach only, no code copied).
func applyGrain(d []float64, w, h int, stock string, iso GrainISO, seed int64) {
	if iso == GrainOff || w < 8 || h < 8 {
		return
	}
	// Frame assumes 36x24 mm. Correlation length in µm, then to pixels.
	grainUM := 12.0 // ISO 100 clump scale
	amp := 0.035    // RMS density at mid for Portra-ish
	if iso == GrainISO400 {
		grainUM = 22.0
		amp = 0.055
	}
	amp *= stockGrainScale(stock)
	pxUM := 36000.0 / float64(w) // µm per pixel along long edge
	sigma := grainUM / pxUM
	if sigma < 0.6 {
		sigma = 0.6
	}
	if sigma > 12 {
		sigma = 12
	}

	n := w * h
	shared := make([]float64, n)
	chN := [3][]float64{make([]float64, n), make([]float64, n), make([]float64, n)}
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < n; i++ {
		shared[i] = rng.NormFloat64()
		for c := 0; c < 3; c++ {
			chN[c][i] = rng.NormFloat64()
		}
	}
	blurInPlace(shared, w, h, sigma)
	for c := 0; c < 3; c++ {
		blurInPlace(chN[c], w, h, sigma*0.85)
	}

	const corr = 0.45 // partial cross-channel correlation
	sCorr := math.Sqrt(1 - corr*corr)
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
						di := i*3 + c
						dd := d[di] // density above Dmin
						// RMS peaks in mid densities (Selwyn / published RMS behaviour).
						shape := math.Sqrt(math.Max(dd, 1e-6)) * math.Exp(-dd/1.1)
						noise := corr*shared[i] + sCorr*chN[c][i]
						d[di] = math.Max(dd+amp*shape*noise, 0)
					}
				}
			}
		}(y0, y1)
	}
	wg.Wait()
}

// Separable box-filter passes approximate a Gaussian (fast enough at 26 MP).
func blurInPlace(buf []float64, w, h int, sigma float64) {
	radius := int(math.Ceil(sigma * 1.2))
	if radius < 1 {
		return
	}
	tmp := make([]float64, len(buf))
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	for pass := 0; pass < 3; pass++ {
		for y := 0; y < h; y++ {
			row := y * w
			for x := 0; x < w; x++ {
				var sum float64
				var n float64
				for k := -radius; k <= radius; k++ {
					sum += buf[row+clamp(x+k, 0, w-1)]
					n++
				}
				tmp[row+x] = sum / n
			}
		}
		copy(buf, tmp)
		for x := 0; x < w; x++ {
			for y := 0; y < h; y++ {
				var sum float64
				var n float64
				for k := -radius; k <= radius; k++ {
					sum += buf[clamp(y+k, 0, h-1)*w+x]
					n++
				}
				tmp[y*w+x] = sum / n
			}
		}
		copy(buf, tmp)
	}
}
