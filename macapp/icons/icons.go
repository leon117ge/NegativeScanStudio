// Package icons holds the vector icons of the Windows UI (pages.go and
// card.go, copied verbatim: 24-unit grid, x/image/vector), rendered to
// images for Gio. TestIconsMatchWindows fails if the copies drift.
package icons

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"
)

type iconFunc func(z *vector.Rasterizer, s float32)

// ByName maps the names used by appstate.Modules and the cards.
var ByName = map[string]iconFunc{
	"film": iconFilm, "wb": iconWB, "hdr": iconHDR, "crop": iconCrop, "convert": iconConvert,
	"develop": iconDevelop, "light": iconLight, "tri": iconTri, "camera": iconCamera, "meter": iconMeter, "preset": iconPreset,
}

// Render draws icon name at size px in fg on a transparent background.
func Render(name string, size int, fg color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	f := ByName[name]
	if f == nil {
		return img
	}
	z := vector.NewRasterizer(size, size)
	f(z, float32(size)/24)
	z.Draw(img, img.Bounds(), image.NewUniform(fg), image.Point{})
	return img
}

var _ = draw.Over
var _ = math.Pi

// copied from ../../pages.go (begin)
// path helpers on a 24-unit grid; ccw cuts a hole.
func vRect(z *vector.Rasterizer, s, x0, y0, x1, y1 float32, ccw bool) {
	if ccw {
		x0, x1 = x1, x0
	}
	z.MoveTo(x0*s, y0*s)
	z.LineTo(x1*s, y0*s)
	z.LineTo(x1*s, y1*s)
	z.LineTo(x0*s, y1*s)
	z.ClosePath()
}

func vArc(z *vector.Rasterizer, s, cx, cy, r, a0, a1 float32, ccw bool, closeToCentre bool) {
	n := 48
	for k := 0; k <= n; k++ {
		t := float64(k) / float64(n)
		if ccw {
			t = 1 - t
		}
		a := float64(a0) + (float64(a1)-float64(a0))*t
		x := (cx + r*float32(math.Cos(a))) * s
		y := (cy + r*float32(math.Sin(a))) * s
		if k == 0 {
			if closeToCentre {
				z.MoveTo(cx*s, cy*s)
				z.LineTo(x, y)
			} else {
				z.MoveTo(x, y)
			}
		} else {
			z.LineTo(x, y)
		}
	}
	z.ClosePath()
}

// film strip with sprocket holes
func iconFilm(z *vector.Rasterizer, s float32) {
	vRect(z, s, 2.5, 4.5, 21.5, 19.5, false)
	vRect(z, s, 5, 8.5, 19, 15.5, true)
	for _, x := range []float32{4.2, 8.1, 12, 15.9, 19.8} {
		vRect(z, s, x-1, 5.8, x+1, 7.3, true)
		vRect(z, s, x-1, 16.7, x+1, 18.2, true)
	}
}

// balance: ring with the left half filled
func iconWB(z *vector.Rasterizer, s float32) {
	vArc(z, s, 12, 12, 9.5, 0, 2*math.Pi, false, false)
	vArc(z, s, 12, 12, 7.8, 0, 2*math.Pi, true, false)
	vArc(z, s, 12, 12, 6.2, math.Pi/2, 3*math.Pi/2, false, true)
	// small drop above (eyedropper tip hint): a dot at the right
	vArc(z, s, 15.2, 12, 1.4, 0, 2*math.Pi, false, false)
}

// stacked bracketed frames
func iconHDR(z *vector.Rasterizer, s float32) {
	vRect(z, s, 2.5, 9.5, 15.5, 21.5, false)
	vRect(z, s, 4.3, 11.3, 13.7, 19.7, true)
	vRect(z, s, 6, 6, 19, 7.6, false)
	vRect(z, s, 17.4, 7.6, 19, 18, false)
	vRect(z, s, 9.5, 2.5, 21.5, 4.1, false)
	vRect(z, s, 19.9, 4.1, 21.5, 14.5, false)
	// sun inside the front frame
	vArc(z, s, 9, 15.5, 2.2, 0, 2*math.Pi, false, false)
}

// crop corners: two interlocking L brackets
func iconCrop(z *vector.Rasterizer, s float32) {
	vRect(z, s, 5.5, 2, 7.6, 16.4, false)
	vRect(z, s, 5.5, 16.4, 22, 18.5, false)
	vRect(z, s, 2, 5.5, 16.4, 7.6, false)
	vRect(z, s, 16.4, 7.6, 18.5, 22, false)
	// small level mark through the middle (rotate / straighten)
	vRect(z, s, 9.6, 11.2, 14.4, 12.8, false)
}

// develop: a negative frame with an arrow to a filled positive frame
func iconConvert(z *vector.Rasterizer, s float32) { iconDevelop(z, s) }

func iconDevelop(z *vector.Rasterizer, s float32) {
	vRect(z, s, 2, 6, 10, 18, false)
	vRect(z, s, 3.6, 7.6, 8.4, 16.4, true)
	vRect(z, s, 10.5, 11.2, 13.5, 12.8, false)
	vArc(z, s, 13.5, 12, 1.6, 0, 2*math.Pi, false, false)
	vRect(z, s, 14, 6, 22, 18, false)
}

// light: a lamp panel with three LEDs and rays (v0.80, RGB Light page)
func iconLight(z *vector.Rasterizer, s float32) {
	vRect(z, s, 3, 13, 21, 21, false)
	vRect(z, s, 4.6, 14.6, 19.4, 19.4, true)
	for _, x := range []float32{7.5, 12, 16.5} {
		vArc(z, s, x, 17, 1.5, 0, 2*math.Pi, false, false)
	}
	vRect(z, s, 11.2, 2.5, 12.8, 9.5, false)
	vRect(z, s, 4.4, 6.2, 6, 10.8, false)
	vRect(z, s, 18, 6.2, 19.6, 10.8, false)
}

// trichrome (v0.82): three overlapping rings (R, G, B separation)
func iconTri(z *vector.Rasterizer, s float32) {
	for _, c := range [][2]float32{{12, 7.5}, {7.8, 15}, {16.2, 15}} {
		vArc(z, s, c[0], c[1], 5.6, 0, 2*math.Pi, false, false)
		vArc(z, s, c[0], c[1], 4.0, 0, 2*math.Pi, true, false)
	}
}

// copied from ../../pages.go (end)

// copied from ../../card.go (begin)
// card icons (24-unit grid like the nav icons)
func iconCamera(z *vector.Rasterizer, s float32) {
	vRect(z, s, 3, 7, 21, 19, false)
	vRect(z, s, 4.5, 8.5, 19.5, 17.5, true)
	vArc(z, s, 12, 13, 3.4, 0, 6.2832, false, false)
	vRect(z, s, 8, 4.5, 14, 7, false)
}

func iconMeter(z *vector.Rasterizer, s float32) {
	vArc(z, s, 12, 15, 9, 3.1416, 6.2832, false, true)
	vArc(z, s, 12, 15, 7, 3.1416, 6.2832, true, true)
	vRect(z, s, 11, 8, 13, 15, false)
	vRect(z, s, 3, 16, 21, 17.5, false)
}

func iconPreset(z *vector.Rasterizer, s float32) {
	vRect(z, s, 4, 4, 20, 20, false)
	vRect(z, s, 5.5, 5.5, 18.5, 18.5, true)
	vRect(z, s, 8, 4, 16, 10, false)
	vRect(z, s, 8, 13, 16, 15, false)
}

// copied from ../../card.go (end)
