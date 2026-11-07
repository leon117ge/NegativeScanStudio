package cropper

import "math"

// Interpolation kernels.
const (
	InterpBicubic = 0 // Catmull-Rom
	InterpLanczos = 1 // Lanczos-3
)

// IntRect rounds a crop rectangle to whole pixels inside the canvas.
func IntRect(r Rect, W, H int) (x0, y0, w, h int) {
	x0 = int(math.Round(r.X0))
	y0 = int(math.Round(r.Y0))
	x1 := int(math.Round(r.X1))
	y1 := int(math.Round(r.Y1))
	x0, y0 = max(0, x0), max(0, y0)
	x1, y1 = min(W, x1), min(H, y1)
	return x0, y0, max(1, x1-x0), max(1, y1-y0)
}

func catmull(x float64) float64 {
	x = math.Abs(x)
	switch {
	case x < 1:
		return 1.5*x*x*x - 2.5*x*x + 1
	case x < 2:
		return -0.5*x*x*x + 2.5*x*x - 4*x + 2
	}
	return 0
}

func lanczos3(x float64) float64 {
	x = math.Abs(x)
	if x < 1e-9 {
		return 1
	}
	if x >= 3 {
		return 0
	}
	px := math.Pi * x
	return 3 * math.Sin(px) * math.Sin(px/3) / (px * px)
}

// Apply rotates im by angle (degrees, CCW positive) about its centre and
// returns the crop r (output-canvas coordinates) at full resolution. The
// result keeps format, bit depth and metadata of im. Angle 0 is a lossless
// pixel copy.
func Apply(im *Image, angle float64, r Rect, interp int) *Image {
	x0, y0, w, h := IntRect(r, im.W, im.H)
	out := &Image{W: w, H: h, Bits: im.Bits, Format: im.Format, meta: im.meta, Pix: make([]uint16, w*h*3)}
	if math.Abs(angle) < 1e-6 {
		for y := 0; y < h; y++ {
			copy(out.Pix[y*w*3:(y+1)*w*3], im.Pix[((y0+y)*im.W+x0)*3:])
		}
		return out
	}
	rad := 3
	kern := lanczos3
	if interp == InterpBicubic {
		rad, kern = 2, catmull
	}
	fw, fh := float64(im.W), float64(im.H)
	a := angle * math.Pi / 180 // inverse of rotFwd(angle) = rotation by +angle in y-down
	cs, sn := math.Cos(a), math.Sin(a)
	cx, cy := fw/2, fh/2
	parallelRows(h, func(ya, yb int) {
		n := 2 * rad
		wx := make([]float64, n)
		wy := make([]float64, n)
		for oy := ya; oy < yb; oy++ {
			qy := float64(y0+oy) + 0.5 - cy
			o := out.Pix[oy*w*3:]
			for ox := 0; ox < w; ox++ {
				qx := float64(x0+ox) + 0.5 - cx
				sx := cx + cs*qx - sn*qy - 0.5
				sy := cy + sn*qx + cs*qy - 0.5
				ix, iy := int(math.Floor(sx)), int(math.Floor(sy))
				fx, fy := sx-float64(ix), sy-float64(iy)
				var swx, swy float64
				for k := 0; k < n; k++ {
					wx[k] = kern(fx - float64(k-rad+1))
					wy[k] = kern(fy - float64(k-rad+1))
					swx += wx[k]
					swy += wy[k]
				}
				var r0, g0, b0 float64
				for j := 0; j < n; j++ {
					yy := min(max(iy+j-rad+1, 0), im.H-1)
					row := im.Pix[yy*im.W*3:]
					var rr, gg, bb float64
					for k := 0; k < n; k++ {
						xx := min(max(ix+k-rad+1, 0), im.W-1) * 3
						rr += wx[k] * float64(row[xx])
						gg += wx[k] * float64(row[xx+1])
						bb += wx[k] * float64(row[xx+2])
					}
					r0 += wy[j] * rr
					g0 += wy[j] * gg
					b0 += wy[j] * bb
				}
				norm := 1 / (swx * swy)
				o[ox*3] = clamp16(r0 * norm)
				o[ox*3+1] = clamp16(g0 * norm)
				o[ox*3+2] = clamp16(b0 * norm)
			}
		}
	})
	return out
}

func clamp16(v float64) uint16 {
	if v <= 0 {
		return 0
	}
	if v >= 65535 {
		return 65535
	}
	return uint16(v + 0.5)
}

// RenderProxy draws the rotated proxy (bilinear) as 8-bit RGBA, size pw x ph.
func RenderProxy(p *Proxy, angle float64) []byte {
	w, h := p.W, p.H
	out := make([]byte, w*h*4)
	a := angle * math.Pi / 180
	cs, sn := math.Cos(a), math.Sin(a)
	cx, cy := float64(w)/2, float64(h)/2
	parallelRows(h, func(ya, yb int) {
		for y := ya; y < yb; y++ {
			qy := float64(y) + 0.5 - cy
			for x := 0; x < w; x++ {
				qx := float64(x) + 0.5 - cx
				sx := cx + cs*qx - sn*qy - 0.5
				sy := cy + sn*qx + cs*qy - 0.5
				o := (y*w + x) * 4
				out[o+3] = 255
				if sx < -0.5 || sy < -0.5 || sx > float64(w)-0.5 || sy > float64(h)-0.5 {
					out[o], out[o+1], out[o+2] = 18, 18, 20
					continue
				}
				ix, iy := int(math.Floor(sx)), int(math.Floor(sy))
				fx, fy := float32(sx-float64(ix)), float32(sy-float64(iy))
				x0, x1 := min(max(ix, 0), w-1), min(max(ix+1, 0), w-1)
				y0, y1 := min(max(iy, 0), h-1), min(max(iy+1, 0), h-1)
				for c := 0; c < 3; c++ {
					v := (p.Pix[(y0*w+x0)*3+c]*(1-fx)+p.Pix[(y0*w+x1)*3+c]*fx)*(1-fy) +
						(p.Pix[(y1*w+x0)*3+c]*(1-fx)+p.Pix[(y1*w+x1)*3+c]*fx)*fy
					out[o+c] = uint8(min(max(v*255+0.5, 0), 255))
				}
			}
		}
	})
	return out
}
