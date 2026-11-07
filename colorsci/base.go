package colorsci

import "sort"

// BrightestRegionBase estimates the film base when no clear rebate is
// available: the image is cut into blocks of size bs (inside a margin of
// 2%), each block's per-channel median is taken, and the median of the
// brightest 0.5% of blocks (by G) is returned. On a negative the brightest
// image areas are the thinnest (deep shadows), so this is a LOWER bound
// estimate of the true base reading - prefer the measured rebate.
func BrightestRegionBase(pix []uint16, w, h, bs int) [3]float64 {
	mx, my := w/50, h/50
	type blk struct{ m [3]float64 }
	var bl []blk
	buf := make([][]float64, 3)
	for y0 := my; y0+bs <= h-my; y0 += bs {
		for x0 := mx; x0+bs <= w-mx; x0 += bs {
			for c := range buf {
				buf[c] = buf[c][:0]
			}
			for y := y0; y < y0+bs; y += 2 {
				for x := x0; x < x0+bs; x += 2 {
					i := 3 * (y*w + x)
					for c := 0; c < 3; c++ {
						buf[c] = append(buf[c], float64(pix[i+c]))
					}
				}
			}
			var b blk
			for c := 0; c < 3; c++ {
				sort.Float64s(buf[c])
				b.m[c] = buf[c][len(buf[c])/2]
			}
			if b.m[0] < 65000 && b.m[1] < 65000 && b.m[2] < 65000 {
				bl = append(bl, b)
			}
		}
	}
	if len(bl) == 0 {
		return [3]float64{}
	}
	sort.Slice(bl, func(i, j int) bool { return bl[i].m[1] > bl[j].m[1] })
	n := max(len(bl)/200, 1)
	var out [3]float64
	for c := 0; c < 3; c++ {
		v := make([]float64, n)
		for i := 0; i < n; i++ {
			v[i] = bl[i].m[c]
		}
		sort.Float64s(v)
		out[c] = v[n/2]
	}
	return out
}

// RegionMean is the per-channel mean over a rectangle [x0,x1)×[y0,y1).
func RegionMean(pix []uint16, w, x0, y0, x1, y1 int) [3]float64 {
	var s [3]float64
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := 3 * (y*w + x)
			for c := 0; c < 3; c++ {
				s[c] += float64(pix[i+c])
			}
			n++
		}
	}
	for c := 0; c < 3; c++ {
		s[c] /= float64(max(n, 1))
	}
	return s
}
