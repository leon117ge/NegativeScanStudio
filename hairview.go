package main

import (
	"image"
	"image/color"
	"math"
	"sort"

	"basetool/hdrscan"
)

// hairLUT maps a 16-bit linear value to 8-bit display, log scale: the top
// 4.5 stops below the brightest 0.1% of the green channel (the clear film
// base) are spread over 0..255, shown as the negative is (no inversion).
func hairLUT(pix []uint16, w, h int) *[65536]uint8 {
	var s []float64
	step := max(1, int(math.Sqrt(float64(w*h)/200000)))
	for y := 0; y < h; y += step {
		for x := 0; x < w; x += step {
			s = append(s, math.Log2(float64(pix[3*(y*w+x)+1])+1))
		}
	}
	hi := 16.0
	if len(s) > 0 {
		sort.Float64s(s)
		hi = s[len(s)*999/1000]
	}
	lo := hi - 4.5
	var lut [65536]uint8
	for v := range lut {
		f := (math.Log2(float64(v)+1) - lo) / (hi - lo)
		lut[v] = uint8(math.Round(255 * math.Max(0, math.Min(1, f))))
	}
	return &lut
}

// hairPreview renders the image area vp at dw x dh (nearest sample) and
// marks the pixels changed by edits in red when mask is set.
func hairPreview(pix []uint16, w, h int, vp image.Rectangle, dw, dh int, lut *[65536]uint8, edits []*hdrscan.HairEdit, mask bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, dw, dh))
	if dw <= 0 || dh <= 0 || vp.Empty() {
		return img
	}
	sx := float64(vp.Dx()) / float64(dw)
	sy := float64(vp.Dy()) / float64(dh)
	for y := 0; y < dh; y++ {
		iy := min(h-1, vp.Min.Y+int(float64(y)*sy))
		o := y * img.Stride
		for x := 0; x < dw; x++ {
			ix := min(w-1, vp.Min.X+int(float64(x)*sx))
			i := 3 * (iy*w + ix)
			img.Pix[o+4*x] = lut[pix[i]]
			img.Pix[o+4*x+1] = lut[pix[i+1]]
			img.Pix[o+4*x+2] = lut[pix[i+2]]
			img.Pix[o+4*x+3] = 255
		}
	}
	if mask {
		red := color.RGBA{255, 40, 40, 255}
		for _, e := range edits {
			for _, gi := range e.Idx {
				x, y := int(gi)%w, int(gi)/w
				if !image.Pt(x, y).In(vp) {
					continue
				}
				img.SetRGBA(int(float64(x-vp.Min.X)/sx), int(float64(y-vp.Min.Y)/sy), red)
			}
		}
	}
	return img
}

// fitRect is the largest rectangle with the aspect of an aw x ah area that
// fits centred in a cw x ch box.
func fitRect(aw, ah, cw, ch int) image.Rectangle {
	if aw <= 0 || ah <= 0 || cw <= 0 || ch <= 0 {
		return image.Rectangle{}
	}
	sc := math.Min(float64(cw)/float64(aw), float64(ch)/float64(ah))
	w, h := int(float64(aw)*sc), int(float64(ah)*sc)
	x, y := (cw-w)/2, (ch-h)/2
	return image.Rect(x, y, x+w, y+h)
}
