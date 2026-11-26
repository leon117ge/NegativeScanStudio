package hdrctl

// Declarations copied verbatim from the Windows Focus check page
// (../../focuspage.go); TestCopiedFocusDeclsMatchWindows keeps them equal.

import (
	"image"
	"image/draw"
)

// ---------- focuspage.go ----------

var zoomItems = []string{"Fit", "100 %", "200 %", "400 %"}
var zoomVals = []int{0, 1, 2, 4}

func imageToGray16(src image.Image) *image.Gray16 {
	b := src.Bounds()
	dst := image.NewGray16(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	return dst
}

func gray16Pix(g *image.Gray16) []uint16 {
	n := g.Bounds().Dx() * g.Bounds().Dy()
	out := make([]uint16, n)
	i := 0
	for y := g.Bounds().Min.Y; y < g.Bounds().Max.Y; y++ {
		for x := g.Bounds().Min.X; x < g.Bounds().Max.X; x++ {
			r, _, _, _ := g.At(x, y).RGBA()
			out[i] = uint16(r)
			i++
		}
	}
	return out
}
