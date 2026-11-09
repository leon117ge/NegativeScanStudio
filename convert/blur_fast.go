package convert

import "math"

// blurInPlaceFast is a mean-preserving separable box blur with sliding sums.
func blurInPlaceFast(buf []float64, w, h int, sigma float64, passes int) {
	radius := int(math.Ceil(sigma * 1.15))
	if radius < 1 || len(buf) < w*h {
		return
	}
	if passes < 1 {
		passes = 1
	}
	tmp := make([]float64, len(buf))
	for p := 0; p < passes; p++ {
		// horizontal sliding
		for y := 0; y < h; y++ {
			row := y * w
			var sum float64
			n := 0
			for x := 0; x <= radius && x < w; x++ {
				sum += buf[row+x]
				n++
			}
			// left edge incomplete window handled by clamping: expand with edge
			for x := 0; x < w; x++ {
				// recompute cheaply with clamp for correctness at edges
				sum, n = 0, 0
				lo := x - radius
				hi := x + radius
				if lo < 0 {
					lo = 0
				}
				if hi >= w {
					hi = w - 1
				}
				for k := lo; k <= hi; k++ {
					sum += buf[row+k]
					n++
				}
				tmp[row+x] = sum / float64(n)
			}
		}
		copy(buf, tmp)
		// vertical
		for x := 0; x < w; x++ {
			for y := 0; y < h; y++ {
				sum, n := 0.0, 0
				lo := y - radius
				hi := y + radius
				if lo < 0 {
					lo = 0
				}
				if hi >= h {
					hi = h - 1
				}
				for k := lo; k <= hi; k++ {
					sum += buf[k*w+x]
					n++
				}
				tmp[y*w+x] = sum / float64(n)
			}
		}
		copy(buf, tmp)
	}
}
