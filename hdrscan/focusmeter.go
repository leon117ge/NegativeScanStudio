package hdrscan

// FocusScore is a unitless sharpness reading for the Focus Check page.
type FocusScore struct {
	Laplacian float64 // variance of the 3x3 Laplacian
	Tenengrad float64 // mean squared Sobel magnitude
	Grain     float64 // mid/high-frequency energy (film grain band)
	Combined  float64 // the number shown on the meter
}

// MeasureFocus scores the 0..1 samples of a (usually zoomed) region.
// pix is row-major, length w*h.
func MeasureFocus(pix []float64, w, h int) FocusScore {
	var s FocusScore
	if w < 5 || h < 5 || len(pix) < w*h {
		return s
	}
	s.Laplacian = laplacianVar(pix, w, h)
	s.Tenengrad = tenengrad(pix, w, h)
	s.Grain = grainEnergy(pix, w, h)
	// Combined: Laplacian variance dominates (classic autofocus), with a
	// grain-band term so a well-resolved film grain field scores higher
	// than a smooth but high-contrast edge at coarse zoom.
	s.Combined = s.Laplacian + 0.25*s.Grain
	return s
}

func laplacianVar(p []float64, w, h int) float64 {
	var sum, sum2 float64
	n := 0
	for y := 1; y < h-1; y++ {
		row := y * w
		for x := 1; x < w-1; x++ {
			// 4-neighbour Laplacian
			v := 4*p[row+x] - p[row+x-1] - p[row+x+1] - p[row-w+x] - p[row+w+x]
			sum += v
			sum2 += v * v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	nf := float64(n)
	mean := sum / nf
	v := sum2/nf - mean*mean
	if v < 0 {
		return 0
	}
	return v
}

// grainEnergy is the energy left after subtracting a local 5-tap mean,
// then ignoring the very highest frequency (single-pixel noise) by a
// 3-tap box. That band is where well-resolved film grain lives.
func grainEnergy(p []float64, w, h int) float64 {
	tmp := make([]float64, w*h)
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			var s float64
			n := 0
			for k := -2; k <= 2; k++ {
				xx := x + k
				if xx < 0 || xx >= w {
					continue
				}
				s += p[row+xx]
				n++
			}
			tmp[row+x] = p[row+x] - s/float64(n)
		}
	}
	var e float64
	n := 0
	for y := 1; y < h-1; y++ {
		row := y * w
		for x := 1; x < w-1; x++ {
			v := (tmp[row+x-1] + tmp[row+x] + tmp[row+x+1]) / 3
			e += v * v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return e / float64(n)
}

// PeakHold remembers the highest Combined score since the last reset.
type PeakHold struct {
	Peak float64
	Have bool
}

func (p *PeakHold) Update(v float64) {
	if !p.Have || v > p.Peak {
		p.Peak = v
		p.Have = true
	}
}

func (p *PeakHold) Reset() { *p = PeakHold{} }

// ToFloat64 copies 16-bit gray into 0..1 floats for the meter.
func Gray16ToFloat64(pix []uint16) []float64 {
	out := make([]float64, len(pix))
	for i, v := range pix {
		out[i] = float64(v) / 65535
	}
	return out
}

// CropGray copies a rectangle out of a gray plane. Empty / out of range
// rectangles fall back to the full frame.
func CropGray(pix []float64, w, h, x0, y0, x1, y1 int) (out []float64, cw, ch int) {
	if w <= 0 || h <= 0 || len(pix) < w*h {
		return nil, 0, 0
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
	cw, ch = x1-x0, y1-y0
	if cw <= 0 || ch <= 0 {
		return nil, 0, 0
	}
	out = make([]float64, cw*ch)
	for y := 0; y < ch; y++ {
		copy(out[y*cw:(y+1)*cw], pix[(y0+y)*w+x0:(y0+y)*w+x0+cw])
	}
	return out, cw, ch
}
