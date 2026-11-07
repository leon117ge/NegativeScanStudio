// Package basestat is module 1's ("1. Film base") film-base statistic, shared
// so that a base sent from the HDR scan module is on exactly the same scale
// and computed exactly the same way as module 1's own measurement:
// linear 16-bit TIFF value / 65535 per channel, light-leak rejection on
// R+G+B around the clean base, then the per-channel median.
package basestat

import (
	"errors"
	"image"
	"sort"
)

// Median sorts v in place and returns its median (0 for empty input).
func Median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

// CleanMedian drops light leaks (pixels more than leakPct brighter than the
// clean base, R+G+B) and dark specks (below 80 %), then returns the median
// per channel. ok is false when less than 40 % (or fewer than 50 samples)
// survive. rejected is the dropped fraction.
func CleanMedian(vr, vg, vb []float64, leakPct float64) (rgb [3]float64, rejected float64, ok bool) {
	if len(vr) == 0 {
		return rgb, 1, false
	}
	// Reference = 30th percentile of brightness, assuming most of the
	// sample is clean base.
	lum := make([]float64, len(vr))
	for i := range vr {
		lum[i] = vr[i] + vg[i] + vb[i]
	}
	sl := append([]float64{}, lum...)
	sort.Float64s(sl)
	ref := sl[len(sl)*3/10]
	hi := ref * (1 + leakPct/100.0)
	lo := ref * 0.80
	for it := 0; it < 4; it++ {
		var acc []float64
		for _, l := range lum {
			if l <= hi && l >= lo {
				acc = append(acc, l)
			}
		}
		if len(acc) < 50 {
			break
		}
		ref = Median(acc)
		hi = ref * (1 + leakPct/100.0)
		lo = ref * 0.80
	}
	var kr, kg, kb []float64
	for i := range vr {
		if lum[i] <= hi && lum[i] >= lo {
			kr = append(kr, vr[i])
			kg = append(kg, vg[i])
			kb = append(kb, vb[i])
		}
	}
	rejected = 1 - float64(len(kr))/float64(len(vr))
	if len(kr) < len(vr)*2/5 || len(kr) < 50 {
		return rgb, rejected, false
	}
	return [3]float64{Median(kr), Median(kg), Median(kb)}, rejected, true
}

// Steps returns module 1's sampling steps for a w×h area (at most about
// 400000 samples; the longer side is thinned).
func Steps(w, h int) (sx, sy int) {
	sx, sy = 1, 1
	if n := w * h; n > 400000 {
		s := n / 400000
		if w > h {
			sx = s + 1
		} else {
			sy = s + 1
		}
	}
	return
}

// Region is an image area in pixels: Rect, optionally restricted to the
// cells of a Bin×Bin grid (cell (cx,cy) covers x cx*Bin.., y cy*Bin..)
// where Mask[cy*Cols+cx] is set. A nil Mask means the whole rectangle.
type Region struct {
	Rect image.Rectangle
	Bin  int
	Cols int
	Mask []bool
}

// Contains reports whether pixel (x,y) belongs to the region.
func (r *Region) Contains(x, y int) bool {
	if !(image.Point{x, y}).In(r.Rect) {
		return false
	}
	if r.Mask == nil || r.Bin <= 0 {
		return true
	}
	i := (y/r.Bin)*r.Cols + x/r.Bin
	return i >= 0 && i < len(r.Mask) && r.Mask[i]
}

// Pixels returns how many pixels the region covers.
func (r *Region) Pixels() int {
	if r.Mask == nil || r.Bin <= 0 {
		return r.Rect.Dx() * r.Rect.Dy()
	}
	n := 0
	for y := r.Rect.Min.Y; y < r.Rect.Max.Y; y++ {
		for x := r.Rect.Min.X; x < r.Rect.Max.X; x++ {
			if r.Contains(x, y) {
				n++
			}
		}
	}
	return n
}

// Getter returns linear 0..1 RGB at (x,y) (image coordinates from 0,0).
type Getter func(x, y int) (float64, float64, float64)

// Measure applies module 1's statistic to the region of an image of size
// w×h: sample on module 1's grid (over the region's bounding box), clean
// and take the per-channel median.
func Measure(get Getter, w, h int, reg Region, leakPct float64) (rgb [3]float64, rejected float64, n int, err error) {
	rc := reg.Rect.Intersect(image.Rect(0, 0, w, h))
	if rc.Empty() {
		return rgb, 0, 0, errors.New("base region is outside the image")
	}
	reg.Rect = rc
	sx, sy := Steps(rc.Dx(), rc.Dy())
	var vr, vg, vb []float64
	for y := rc.Min.Y; y < rc.Max.Y; y += sy {
		for x := rc.Min.X; x < rc.Max.X; x += sx {
			if !reg.Contains(x, y) {
				continue
			}
			r, g, b := get(x, y)
			vr = append(vr, r)
			vg = append(vg, g)
			vb = append(vb, b)
		}
	}
	if len(vr) < 50 {
		return rgb, 0, len(vr), errors.New("base region too small")
	}
	rgb, rejected, ok := CleanMedian(vr, vg, vb, leakPct)
	if !ok {
		return rgb, rejected, len(vr), errors.New("too little clean base in the region (light leak?)")
	}
	return rgb, rejected, len(vr), nil
}

// PlaneGetter reads an interleaved 16-bit RGB buffer the way module 1
// reads a 16-bit TIFF (value / 65535).
func PlaneGetter(pix []uint16, w int) Getter {
	return func(x, y int) (float64, float64, float64) {
		i := (y*w + x) * 3
		return float64(pix[i]) / 65535, float64(pix[i+1]) / 65535, float64(pix[i+2]) / 65535
	}
}
