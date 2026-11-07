package cropper

import (
	"math"
	"sort"
)

// Proxy is a downscaled, normalised copy of an Image used for detection and
// for the on-screen preview. Pix is RGB float32 in 0..1.
type Proxy struct {
	W, H  int
	Scale float64 // full-res pixels per proxy pixel
	Pix   []float32
}

// MakeProxy box-averages im to a long side of at most maxSide pixels and
// normalises it (linear-looking 16-bit data gets a square-root lift).
func MakeProxy(im *Image, maxSide int) *Proxy {
	f := 1
	for max(im.W, im.H)/f > maxSide {
		f++
	}
	pw, ph := im.W/f, im.H/f
	p := &Proxy{W: pw, H: ph, Scale: float64(f), Pix: make([]float32, pw*ph*3)}
	inv := float32(1.0 / (65535.0 * float64(f*f)))
	parallelRows(ph, func(y0, y1 int) {
		acc := make([]uint64, pw*3)
		for py := y0; py < y1; py++ {
			for i := range acc {
				acc[i] = 0
			}
			for sy := py * f; sy < py*f+f; sy++ {
				row := im.Pix[sy*im.W*3:]
				for px := 0; px < pw; px++ {
					o := px * 3
					s := px * f * 3
					for k := 0; k < f; k++ {
						acc[o] += uint64(row[s])
						acc[o+1] += uint64(row[s+1])
						acc[o+2] += uint64(row[s+2])
						s += 3
					}
				}
			}
			out := p.Pix[py*pw*3:]
			for i := 0; i < pw*3; i++ {
				out[i] = float32(acc[i]) * inv
			}
		}
	})
	// normalisation
	n := pw * ph
	lum := make([]float32, 0, n/4+1)
	for i := 0; i < n; i += 4 {
		lum = append(lum, 0.25*p.Pix[i*3]+0.5*p.Pix[i*3+1]+0.25*p.Pix[i*3+2])
	}
	sort.Slice(lum, func(i, j int) bool { return lum[i] < lum[j] })
	hi := lum[int(float64(len(lum)-1)*0.995)]
	med := lum[len(lum)/2]
	if hi < 0.02 {
		hi = 0.02
	}
	lift := im.Bits == 16 && med/hi < 0.12
	k := 1 / hi
	for i, v := range p.Pix {
		v *= k
		if v > 1 {
			v = 1
		} else if v < 0 {
			v = 0
		}
		if lift {
			v = float32(math.Sqrt(float64(v)))
		}
		p.Pix[i] = v
	}
	return p
}
