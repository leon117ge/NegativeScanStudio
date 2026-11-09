package convert

import (
	"math"
	"runtime"
	"sync"

)

// ProcessPixelsFast is a tiled, LUT-accelerated Convert pipeline.
// Same result family as ProcessPixels; used for preview and export.
func ProcessPixelsFast(rgb []float64, w, h int, o Options) {
	o.normalize()
	nPix := w * h
	if nPix == 0 {
		return
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	if workers > 16 {
		workers = 16
	}

	// Optional CamCal (cheap 3x3) in tiles.
	if o.ApplyCamProfile && o.CamStrength > 0 {
		parallelRows(h, workers, func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				for x := 0; x < w; x++ {
					i := (y*w + x) * 3
					v := colorsciApply(o, [3]float64{rgb[i], rgb[i+1], rgb[i+2]})
					rgb[i], rgb[i+1], rgb[i+2] = v[0], v[1], v[2]
				}
			}
		})
	}

	cs := CurveFor(o.Stock.Name)
	// Density via LUT on logH for full curves, or logistic eval.
	luts := buildDensityLUTs(o, cs)
	parallelRows(h, workers, func(y0, y1 int) {
		exp := math.Pow(2, o.EV)
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				i := (y*w + x) * 3
				for c := 0; c < 3; c++ {
					hlin := math.Max(rgb[i+c]*exp, 1e-12)
					rgb[i+c] = luts[c].at(math.Log10(hlin / MidGrey))
				}
			}
		}
	})

	if o.Grain != GrainOff {
		cleanT := make([]float64, len(rgb))
		parallelRows(h, workers, func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				for x := 0; x < w; x++ {
					i := (y*w + x) * 3
					for c := 0; c < 3; c++ {
						cleanT[i+c] = BaseFS * math.Pow(10, -rgb[i+c])
					}
				}
			}
		})
		sig := applyGrainPoisson(rgb, w, h, o.Stock.Name, o.Grain, o.GrainSeed, o.GrainStrength, o.GrainSize)
		parallelRows(h, workers, func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				for x := 0; x < w; x++ {
					i := (y*w + x) * 3
					for c := 0; c < 3; c++ {
						t := BaseFS * math.Pow(10, -rgb[i+c])
						if t < 0 {
							t = 0
						}
						if t > 1 {
							t = 1
						}
						rgb[i+c] = t
					}
				}
			}
		})
		equalizeTransmittance(rgb, cleanT, w, h, sig)
	} else {
		parallelRows(h, workers, func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				for x := 0; x < w; x++ {
					i := (y*w + x) * 3
					for c := 0; c < 3; c++ {
						t := BaseFS * math.Pow(10, -rgb[i+c])
						if t < 0 {
							t = 0
						}
						if t > 1 {
							t = 1
						}
						rgb[i+c] = t
					}
				}
			}
		})
	}
	applyBorder(rgb, w, h, o)
}

func colorsciApply(o Options, v [3]float64) [3]float64 {
	return applyCamMatrix(o.CamMatrix, o.CamStrength, v)
}

type densLUT struct {
	lo, step float64
	v        []float64
}

func (l densLUT) at(logRel float64) float64 {
	if len(l.v) == 0 {
		return 0
	}
	t := (logRel - l.lo) / l.step
	if t <= 0 {
		return l.v[0]
	}
	if t >= float64(len(l.v)-1) {
		return l.v[len(l.v)-1]
	}
	i := int(t)
	f := t - float64(i)
	return l.v[i]*(1-f) + l.v[i+1]*f
}

func buildDensityLUTs(o Options, cs *CurveSet) [3]densLUT {
	const n = 1024
	lo, hi := -5.0, 4.0
	step := (hi - lo) / float64(n-1)
	var out [3]densLUT
	for c := 0; c < 3; c++ {
		v := make([]float64, n)
		for i := 0; i < n; i++ {
			rel := lo + float64(i)*step
			if cs != nil && cs.HasFull {
				v[i] = cs.densityAbove(cs.LogHMid+rel, c)
			} else {
				v[i] = densityAboveLogistic(rel, o.Stock.Gamma[c], o.MidDensity, o.MaxDensity)
			}
		}
		out[c] = densLUT{lo: lo, step: step, v: v}
	}
	return out
}

func parallelRows(h, workers int, fn func(y0, y1 int)) {
	if workers < 1 {
		workers = 1
	}
	if h < workers {
		workers = h
	}
	if workers <= 1 {
		fn(0, h)
		return
	}
	var wg sync.WaitGroup
	chunk := (h + workers - 1) / workers
	for w := 0; w < workers; w++ {
		y0 := w * chunk
		y1 := y0 + chunk
		if y1 > h {
			y1 = h
		}
		if y0 >= y1 {
			continue
		}
		wg.Add(1)
		go func(a, b int) { defer wg.Done(); fn(a, b) }(y0, y1)
	}
	wg.Wait()
}

// Process16Fast converts uint16 linear RGB using the fast path.
func Process16Fast(pix []uint16, w, h int, o Options) []uint16 {
	n := len(pix)
	tmp := make([]float64, n)
	for i, v := range pix {
		tmp[i] = float64(v) / 65535
	}
	ProcessPixelsFast(tmp, w, h, o)
	out := make([]uint16, n)
	for i, v := range tmp {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		out[i] = uint16(v*65535 + 0.5)
	}
	return out
}
