package convert

// asShotScales returns green-normalized white-balance multipliers from
// LibRaw cam_mul (as-shot). ok is false when the multipliers are missing
// or outside a sane range, so a scan or TIFF without cam_mul is left alone.
func asShotScales(mul [4]float32) (rm, gm, bm float64, ok bool) {
	g := float64(mul[1])
	if g < 1e-6 {
		return 1, 1, 1, false
	}
	rm = float64(mul[0]) / g
	bm = float64(mul[2]) / g
	if rm < 0.05 || bm < 0.05 || rm > 16 || bm > 16 {
		return 1, 1, 1, false
	}
	return rm, 1, bm, true
}

func applyAsShot(v [3]float64, mul [4]float32) [3]float64 {
	rm, gm, bm, ok := asShotScales(mul)
	if !ok {
		return v
	}
	return [3]float64{v[0] * rm, v[1] * gm, v[2] * bm}
}

// camXYZValid reports whether LibRaw filled in a usable camera matrix.
func camXYZValid(m [4][3]float32) bool {
	var sum float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			sum += abs64(float64(m[i][j]))
		}
	}
	return sum > 0.1
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// camXYZToSRGB builds the row-normalised camera-RGB -> linear sRGB matrix
// (dcraw/LibRaw: rgb_cam = xyz_to_sRGB * cam_xyz, rows sum to 1 so a
// white-balanced camera grey stays a grey).
func camXYZToSRGB(cam [4][3]float32) (out [3][3]float64, ok bool) {
	if !camXYZValid(cam) {
		return out, false
	}
	// Inverse of the sRGB/Rec709 D65 RGB-to-XYZ matrix (IEC 61966-2-1).
	xyzToSRGB := [3][3]float64{
		{3.2404542, -1.5371385, -0.4985314},
		{-0.9692660, 1.8760108, 0.0415560},
		{0.0556434, -0.2040259, 1.0572252},
	}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			var s float64
			for k := 0; k < 3; k++ {
				s += xyzToSRGB[i][k] * float64(cam[j][k])
			}
			out[i][j] = s
		}
	}
	for i := 0; i < 3; i++ {
		row := out[i][0] + out[i][1] + out[i][2]
		if abs64(row) < 1e-8 {
			return out, false
		}
		out[i][0] /= row
		out[i][1] /= row
		out[i][2] /= row
	}
	return out, true
}

func applyCamXYZ(v [3]float64, cam [4][3]float32) [3]float64 {
	m, ok := camXYZToSRGB(cam)
	if !ok {
		return v
	}
	r := m[0][0]*v[0] + m[0][1]*v[1] + m[0][2]*v[2]
	g := m[1][0]*v[0] + m[1][1]*v[1] + m[1][2]*v[2]
	b := m[2][0]*v[0] + m[2][1]*v[1] + m[2][2]*v[2]
	if r < 0 {
		r = 0
	}
	if g < 0 {
		g = 0
	}
	if b < 0 {
		b = 0
	}
	return [3]float64{r, g, b}
}

// AutoEVScale returns the multiplier that places the interior median
// luminance at MidGrey. Clamped to +/- 4 stops so a wild histogram
// cannot blow out a frame.
func AutoEVScale(rgb []float64, w, h int) float64 {
	if w < 4 || h < 4 {
		return 1
	}
	x0, x1 := w/12, w-w/12
	y0, y1 := h/12, h-h/12
	hist := make([]int, 512)
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := (y*w + x) * 3
			Y := 0.2126*rgb[i] + 0.7152*rgb[i+1] + 0.0722*rgb[i+2]
			if Y < 0 {
				Y = 0
			}
			k := int(Y * 511)
			if k > 511 {
				k = 511
			}
			hist[k]++
			n++
		}
	}
	if n == 0 {
		return 1
	}
	want, acc, med := n/2, 0, 0.0
	for i, c := range hist {
		acc += c
		if acc >= want {
			med = (float64(i) + 0.5) / 511
			break
		}
	}
	if med < 1e-5 {
		return 1
	}
	s := MidGrey / med
	if s > 16 {
		s = 16
	}
	if s < 1.0/16 {
		s = 1.0 / 16
	}
	return s
}
