package convert

import "math"

// GrainStats is high-pass density grain measured the same way on real
// scans and on simulated patches.
type GrainStats struct {
	MeanD                  [3]float64
	RMS                    [3]float64
	CorrRG, CorrGB, CorrRB float64
	AC50                   [3]float64
}

const grainHPRadius = 15

// MeasureDensityGrain high-pass filters each density channel with a
// (2*rad+1) box and reports RMS, pairwise correlation and the lag at
// which the mean of horizontal and vertical autocorrelation falls to 0.5.
func MeasureDensityGrain(d []float64, w, h int) GrainStats {
	return measureDensityGrainRad(d, w, h, grainHPRadius)
}

func measureDensityGrainRad(d []float64, w, h, rad int) GrainStats {
	var st GrainStats
	n := w * h
	if n == 0 {
		return st
	}
	planes := [3][]float64{make([]float64, n), make([]float64, n), make([]float64, n)}
	for i := 0; i < n; i++ {
		for c := 0; c < 3; c++ {
			planes[c][i] = d[i*3+c]
			st.MeanD[c] += d[i*3+c]
		}
	}
	nf := float64(n)
	for c := 0; c < 3; c++ {
		st.MeanD[c] /= nf
	}
	hf := [3][]float64{}
	for c := 0; c < 3; c++ {
		lf := boxBlurRad(planes[c], w, h, rad)
		hfc := make([]float64, n)
		for i := 0; i < n; i++ {
			hfc[i] = planes[c][i] - lf[i]
		}
		hf[c] = hfc
		st.RMS[c] = rmsOf(hfc)
		st.AC50[c] = acHalf(hfc, w, h, 80)
	}
	st.CorrRG = corrOf(hf[0], hf[1])
	st.CorrGB = corrOf(hf[1], hf[2])
	st.CorrRB = corrOf(hf[0], hf[2])
	return st
}

func rmsOf(a []float64) float64 {
	var s float64
	for _, v := range a {
		s += v * v
	}
	return math.Sqrt(s / float64(len(a)))
}

func corrOf(a, b []float64) float64 {
	n := float64(len(a))
	var sa, sb float64
	for i := range a {
		sa += a[i]
		sb += b[i]
	}
	sa /= n
	sb /= n
	var sab, a2, b2 float64
	for i := range a {
		da, db := a[i]-sa, b[i]-sb
		sab += da * db
		a2 += da * da
		b2 += db * db
	}
	den := math.Sqrt(a2 * b2)
	if den < 1e-18 {
		return 0
	}
	return sab / den
}

func acHalf(plane []float64, w, h, maxLag int) float64 {
	n := w * h
	var mean float64
	for _, v := range plane {
		mean += v
	}
	mean /= float64(n)
	var varr float64
	for _, v := range plane {
		d := v - mean
		varr += d * d
	}
	varr /= float64(n)
	if varr < 1e-18 {
		return 0
	}
	if maxLag > w-2 {
		maxLag = w - 2
	}
	if maxLag > h-2 {
		maxLag = h - 2
	}
	prev := 1.0
	for lag := 1; lag <= maxLag; lag++ {
		var sh, sv float64
		var nh, nv float64
		for y := 0; y < h; y++ {
			row := y * w
			for x := 0; x < w-lag; x++ {
				sh += (plane[row+x] - mean) * (plane[row+x+lag] - mean)
				nh++
			}
		}
		for y := 0; y < h-lag; y++ {
			for x := 0; x < w; x++ {
				sv += (plane[y*w+x] - mean) * (plane[(y+lag)*w+x] - mean)
				nv++
			}
		}
		ac := 0.5 * (sh/nh + sv/nv) / varr
		if ac <= 0.5 {
			t := (0.5 - prev) / (ac - prev + 1e-18)
			return float64(lag-1) + t
		}
		prev = ac
	}
	return float64(maxLag)
}

func boxBlurRad(p []float64, w, h, rad int) []float64 {
	tmp := make([]float64, len(p))
	out := make([]float64, len(p))
	copy(out, p)
	// horizontal
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var s float64
			var n float64
			for k := -rad; k <= rad; k++ {
				xx := x + k
				if xx < 0 {
					xx = 0
				}
				if xx >= w {
					xx = w - 1
				}
				s += p[y*w+xx]
				n++
			}
			tmp[y*w+x] = s / n
		}
	}
	// vertical
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			var s float64
			var n float64
			for k := -rad; k <= rad; k++ {
				yy := y + k
				if yy < 0 {
					yy = 0
				}
				if yy >= h {
					yy = h - 1
				}
				s += tmp[yy*w+x]
				n++
			}
			out[y*w+x] = s / n
		}
	}
	return out
}

// SimulateFlatGrain builds a flat density patch, applies ISO grain at
// refW physical scale, converts to transmittance, equalises, and
// converts back to density so the metric matches a linear scan TIFF.
func SimulateFlatGrain(meanD [3]float64, w, h, refW int, stock string, iso GrainISO, seed int64) ([]float64, GrainStats) {
	d := make([]float64, w*h*3)
	cleanT := make([]float64, w*h*3)
	for i := 0; i < w*h; i++ {
		for c := 0; c < 3; c++ {
			d[i*3+c] = meanD[c]
			cleanT[i*3+c] = BaseFS * math.Pow(10, -meanD[c])
		}
	}
	sig := applyGrainAtScale(d, w, h, refW, stock, iso, seed, 1, 1)
	t := make([]float64, len(d))
	for i := range d {
		v := BaseFS * math.Pow(10, -d[i])
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		t[i] = v
	}
	equalizeTransmittance(t, cleanT, w, h, sig)
	outD := make([]float64, len(t))
	for i := range t {
		outD[i] = -math.Log10(math.Max(t[i]/BaseFS, 1e-12))
	}
	return t, MeasureDensityGrain(outD, w, h)
}

// DensityFromLinearT converts linear scan transmittance (base 0.5) to density.
func DensityFromLinearT(t []float64) []float64 {
	d := make([]float64, len(t))
	for i, v := range t {
		d[i] = -math.Log10(math.Max(v/BaseFS, 1e-12))
	}
	return d
}
