package appstate

import (
	"image"

	"basetool/hdrscan"
)

// FitRect is the live view's imageRect: the image scaled to fit view,
// centred, aspect kept.
func FitRect(view image.Rectangle, img image.Point) image.Rectangle {
	if img.X <= 0 || img.Y <= 0 {
		return view
	}
	sc := float64(view.Dx()) / float64(img.X)
	if f := float64(view.Dy()) / float64(img.Y); f < sc {
		sc = f
	}
	w, h := int(float64(img.X)*sc), int(float64(img.Y)*sc)
	x := view.Min.X + (view.Dx()-w)/2
	y := view.Min.Y + (view.Dy()-h)/2
	return image.Rect(x, y, x+w, y+h)
}

// NormToPx is the paint helper toPx: a 0..1 rectangle on ir, at least 6 px.
func NormToPx(ir image.Rectangle, r hdrscan.Rect) image.Rectangle {
	x0 := ir.Min.X + int(r.X0*float64(ir.Dx()))
	y0 := ir.Min.Y + int(r.Y0*float64(ir.Dy()))
	x1 := ir.Min.X + int(r.X1*float64(ir.Dx()))
	y1 := ir.Min.Y + int(r.Y1*float64(ir.Dy()))
	if x1-x0 < 6 {
		cx := (x0 + x1) / 2
		x0, x1 = cx-3, cx+3
	}
	if y1-y0 < 6 {
		cy := (y0 + y1) / 2
		y0, y1 = cy-3, cy+3
	}
	return image.Rect(x0, y0, x1, y1)
}

// PxToNorm is norm: a view point as 0..1 image coordinates, clamped.
func PxToNorm(ir image.Rectangle, p image.Point) (float64, float64) {
	if ir.Dx() <= 0 || ir.Dy() <= 0 {
		return 0, 0
	}
	cl := func(v float64) float64 { return min(1, max(0, v)) }
	return cl(float64(p.X-ir.Min.X) / float64(ir.Dx())), cl(float64(p.Y-ir.Min.Y) / float64(ir.Dy()))
}

// DragRect is the rectangle between the press point a and the current point b.
func DragRect(ax, ay, bx, by float64) hdrscan.Rect {
	return hdrscan.Rect{X0: min(ax, bx), Y0: min(ay, by), X1: max(ax, bx), Y1: max(ay, by)}
}
