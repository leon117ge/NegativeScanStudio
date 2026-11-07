package colorsci

// v0.76: neutral (grey) axis of a scanned colour negative, in density
// above the film base. Used by "Keep neutral axis" of the crosstalk matrix.

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// NeutralAxis estimates the grey axis g = (R/G, 1, B/G) from densities
// above the base (log10(base / value) per channel): candidates with every
// density > 0.02 and 0.05 < D_G < 1.5; start from the first principal axis
// (uncentred, through the base), then 6 times take the median R/G and B/G
// of the pixels within 2 degrees of the axis. The reference is
// neutral_est.py of the v0.76 QA.
func NeutralAxis(d [][3]float64) (axis [2]float64, n int, err error) {
	var x [][3]float64
	for _, p := range d {
		if math.Min(p[0], math.Min(p[1], p[2])) > 0.02 && p[1] > 0.05 && p[1] < 1.5 {
			x = append(x, p)
		}
	}
	if len(x) < 2000 {
		return axis, len(x), fmt.Errorf("too few usable pixels (%d)", len(x))
	}
	var a [3][3]float64
	for _, p := range x {
		for i := 0; i < 3; i++ {
			for j := 0; j < 3; j++ {
				a[i][j] += p[i] * p[j]
			}
		}
	}
	g := [3]float64{1, 1, 1}
	for it := 0; it < 50; it++ {
		var h [3]float64
		for i := 0; i < 3; i++ {
			h[i] = a[i][0]*g[0] + a[i][1]*g[1] + a[i][2]*g[2]
		}
		l := math.Sqrt(h[0]*h[0] + h[1]*h[1] + h[2]*h[2])
		g = [3]float64{h[0] / l, h[1] / l, h[2] / l}
	}
	if g[1] <= 0 {
		return axis, 0, errors.New("no grey axis found")
	}
	g = [3]float64{g[0] / g[1], 1, g[2] / g[1]}
	cone := math.Cos(2 * math.Pi / 180)
	rr := make([]float64, 0, len(x))
	bb := make([]float64, 0, len(x))
	for it := 0; it < 6; it++ {
		gl := math.Sqrt(g[0]*g[0] + g[1]*g[1] + g[2]*g[2])
		rr, bb = rr[:0], bb[:0]
		for _, p := range x {
			pl := math.Sqrt(p[0]*p[0] + p[1]*p[1] + p[2]*p[2])
			if (p[0]*g[0]+p[1]*g[1]+p[2]*g[2])/(pl*gl) > cone {
				rr = append(rr, p[0]/p[1])
				bb = append(bb, p[2]/p[1])
			}
		}
		n = len(rr)
		if n < 2000 {
			return axis, n, fmt.Errorf("too few near-neutral pixels (%d)", n)
		}
		g = [3]float64{medianOf(rr), 1, medianOf(bb)}
	}
	return [2]float64{g[0], g[2]}, n, nil
}

// medianOf sorts v (numpy median: mean of the two middle values).
func medianOf(v []float64) float64 {
	sort.Float64s(v)
	k := len(v)
	if k%2 == 1 {
		return v[k/2]
	}
	return (v[k/2-1] + v[k/2]) / 2
}

// KeepNeutral rescales the rows of m so that m g = g, g = (r, 1, b).
func KeepNeutral(m Mat3, r, b float64) Mat3 {
	g := [3]float64{r, 1, b}
	for i := 0; i < 3; i++ {
		t := m[i][0]*g[0] + m[i][1]*g[1] + m[i][2]*g[2]
		if math.Abs(t) < 1e-4 {
			continue
		}
		k := g[i] / t
		for j := 0; j < 3; j++ {
			m[i][j] *= k
		}
	}
	return m
}
