package rawconv

// ChannelPlane (v0.80) averages the photosites of one CFA colour (0 R,
// 1 G - both greens -, 2 B) over bin x bin cells, black-subtracted, as a
// fraction of full scale. clipped marks cells with a clipped photosite of
// that colour. Used to measure a frame lit by a single LED colour.
func (f *RawFrame) ChannelPlane(ch, bin int) (w, h int, v []float32, clipped []bool) {
	if bin < 1 {
		bin = 1
	}
	w, h = f.Width/bin, f.Height/bin
	v = make([]float32, w*h)
	clipped = make([]bool, w*h)
	if f.Maximum <= f.Black {
		return
	}
	scale := 1.0 / float64(f.Maximum-f.Black)
	clipAt := uint16(f.Maximum)
	if f.Maximum > 64 {
		clipAt = uint16(f.Maximum - 8)
	}
	for by := 0; by < h; by++ {
		for bx := 0; bx < w; bx++ {
			sum, n, clip := 0.0, 0, false
			for yy := 0; yy < bin; yy++ {
				y := by*bin + yy
				rrow := f.Raw[(y+f.Top)*f.RawWidth+f.Left:]
				crow := f.Color[y*f.Width:]
				blrow := f.BlackAt[y*f.Width:]
				for xx := 0; xx < bin; xx++ {
					x := bx*bin + xx
					c := int(crow[x])
					if c == 3 {
						c = 1
					}
					if c != ch {
						continue
					}
					if rrow[x] >= clipAt {
						clip = true
					}
					sum += float64(int(rrow[x]) - int(blrow[x]))
					n++
				}
			}
			i := by*w + bx
			if n > 0 {
				v[i] = float32(sum / float64(n) * scale)
			}
			clipped[i] = clip
		}
	}
	return
}
