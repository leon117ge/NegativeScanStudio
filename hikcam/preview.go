package hikcam

import "image"

// LivePreviewMaxEdge is the longest side of a fit live-view bitmap.
// A 6016x4096 frame is reduced to 1504x1024 (4x) so GDI paints stay cheap.
const LivePreviewMaxEdge = 1600

// PreviewGray downsamples a 16-bit plane to an 8-bit gray image whose
// longer side is at most maxEdge. maxEdge <= 0 keeps the full size.
func PreviewGray(pix []uint16, w, h, maxEdge int) *image.Gray {
	if w <= 0 || h <= 0 {
		return image.NewGray(image.Rect(0, 0, 1, 1))
	}
	scale := previewScale(w, h, maxEdge)
	nw, nh := max(1, w/scale), max(1, h/scale)
	img := image.NewGray(image.Rect(0, 0, nw, nh))
	if len(pix) < w*h {
		return img
	}
	for y := 0; y < nh; y++ {
		src := (y * scale) * w
		dst := y * nw
		for x := 0; x < nw; x++ {
			img.Pix[dst+x] = byte(pix[src+x*scale] >> 8)
		}
	}
	return img
}

// PreviewGray8 downsamples a packed Mono8 buffer the same way.
func PreviewGray8(src []byte, w, h, maxEdge int) *image.Gray {
	if w <= 0 || h <= 0 {
		return image.NewGray(image.Rect(0, 0, 1, 1))
	}
	scale := previewScale(w, h, maxEdge)
	nw, nh := max(1, w/scale), max(1, h/scale)
	img := image.NewGray(image.Rect(0, 0, nw, nh))
	if len(src) < w*h {
		return img
	}
	for y := 0; y < nh; y++ {
		row := (y * scale) * w
		dst := y * nw
		for x := 0; x < nw; x++ {
			img.Pix[dst+x] = src[row+x*scale]
		}
	}
	return img
}

// CropGray8 copies a rectangle of a 16-bit plane into an 8-bit gray
// image at 1:1. Out-of-range rectangles fall back to the full frame.
func CropGray8(pix []uint16, w, h, x0, y0, x1, y1 int) *image.Gray {
	if w <= 0 || h <= 0 {
		return image.NewGray(image.Rect(0, 0, 1, 1))
	}
	if x1 <= x0 || y1 <= y0 {
		x0, y0, x1, y1 = 0, 0, w, h
	}
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > w {
		x1 = w
	}
	if y1 > h {
		y1 = h
	}
	cw, ch := x1-x0, y1-y0
	if cw <= 0 || ch <= 0 || len(pix) < w*h {
		return image.NewGray(image.Rect(0, 0, 1, 1))
	}
	img := image.NewGray(image.Rect(0, 0, cw, ch))
	for y := 0; y < ch; y++ {
		src := (y0+y)*w + x0
		dst := y * cw
		for x := 0; x < cw; x++ {
			img.Pix[dst+x] = byte(pix[src+x] >> 8)
		}
	}
	return img
}

func previewScale(w, h, maxEdge int) int {
	if maxEdge <= 0 {
		return 1
	}
	m := w
	if h > m {
		m = h
	}
	if m <= maxEdge {
		return 1
	}
	return (m + maxEdge - 1) / maxEdge
}

// fullGray16To8 is the previous live-view conversion: every photosite
// becomes one display pixel. Kept for the before/after timing test.
func fullGray16To8(pix []uint16, w, h int) *image.Gray {
	if w <= 0 || h <= 0 {
		return image.NewGray(image.Rect(0, 0, 1, 1))
	}
	img := image.NewGray(image.Rect(0, 0, w, h))
	n := w * h
	if len(pix) < n {
		n = len(pix)
	}
	for i := 0; i < n; i++ {
		img.Pix[i] = byte(pix[i] >> 8)
	}
	return img
}
