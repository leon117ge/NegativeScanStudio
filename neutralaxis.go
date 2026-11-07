package main

// v0.76: "Measure from scan..." of DCTL > Mask & Crosstalk.

import (
	"fmt"
	"math"

	"basetool/colorsci"
)

// neutralAxisFromImage reads a scan (positive values 0..1 of the negative)
// and estimates its grey axis in density above base (fractions of full
// scale, as the Film base result). Same sampling as analyzeWB: 4% margins,
// at most 700000 pixels.
func neutralAxisFromImage(path string, base [3]float64) ([2]float64, int, error) {
	var axis [2]float64
	if !(base[0] > 0 && base[1] > 0 && base[2] > 0) {
		return axis, 0, fmt.Errorf("no film base")
	}
	img, err := decodeImage(path)
	if err != nil {
		return axis, 0, err
	}
	get, _ := getter(img)
	b := img.Bounds()
	W, H := b.Dx(), b.Dy()
	mx, my := int(float64(W)*0.04), int(float64(H)*0.04)
	cw, ch := W-2*mx, H-2*my
	if cw < 16 || ch < 16 {
		return axis, 0, fmt.Errorf("image too small")
	}
	step := 1
	for (cw/step)*(ch/step) > 700000 {
		step++
	}
	d := make([][3]float64, 0, (cw/step+1)*(ch/step+1))
	for y := my; y < H-my; y += step {
		for x := mx; x < W-mx; x += step {
			r, g, bl := get(b.Min.X+x, b.Min.Y+y)
			d = append(d, [3]float64{
				math.Log10(base[0] / math.Max(r, 1e-6)),
				math.Log10(base[1] / math.Max(g, 1e-6)),
				math.Log10(base[2] / math.Max(bl, 1e-6)),
			})
		}
	}
	return colorsci.NeutralAxis(d)
}
