package develop

// v0.71: per-frame orientation (rotate / flip) and the preview view
// transform (zoom / pan). Orientation is a lossless pixel remap applied
// outside the colour node chain, so colour parity is unaffected.

import (
	"fmt"
	"math"
)

// Orient is one of the 8 lossless orientations (dihedral group D4):
// o = r | f<<2 means "flip horizontally if f, then rotate r x 90 deg
// clockwise". 0 is the identity.
type Orient int

// Orientation operations (applied on top of the current orientation).
const (
	OpRotLeft = iota
	OpRotRight
	OpRot180
	OpFlipH
	OpFlipV
)

// m is the 2x2 integer matrix of o acting on centred image coordinates
// (x right, y down): p' = M p.
func (o Orient) m() [4]int {
	r, f := int(o)&3, int(o)>>2&1
	M := [4]int{1, 0, 0, 1}
	if f == 1 {
		M = [4]int{-1, 0, 0, 1}
	}
	for i := 0; i < r; i++ { // R = [0 -1; 1 0]: (x,y) -> (-y, x), clockwise on screen
		M = [4]int{-M[2], -M[3], M[0], M[1]}
	}
	return M
}

func mul(a, b [4]int) [4]int {
	return [4]int{a[0]*b[0] + a[1]*b[2], a[0]*b[1] + a[1]*b[3], a[2]*b[0] + a[3]*b[2], a[2]*b[1] + a[3]*b[3]}
}

func fromM(M [4]int) Orient {
	for o := Orient(0); o < 8; o++ {
		if o.m() == M {
			return o
		}
	}
	return 0
}

// Apply returns the orientation after performing op on an image that is
// already shown with orientation o.
func (o Orient) Apply(op int) Orient {
	var P [4]int
	switch op {
	case OpRotLeft:
		P = Orient(3).m()
	case OpRotRight:
		P = Orient(1).m()
	case OpRot180:
		P = Orient(2).m()
	case OpFlipH:
		P = Orient(4).m()
	case OpFlipV:
		P = Orient(6).m()
	default:
		return o
	}
	return fromM(mul(P, o.m()))
}

// Valid reports whether o is one of the 8 orientations.
func (o Orient) Valid() bool { return o >= 0 && o < 8 }

// Swaps reports whether o exchanges width and height.
func (o Orient) Swaps() bool { return int(o)&1 == 1 }

// Dims is the size of a w x h image after orientation o.
func (o Orient) Dims(w, h int) (int, int) {
	if o.Swaps() {
		return h, w
	}
	return w, h
}

// String names o for the UI / log.
func (o Orient) String() string {
	if !o.Valid() {
		return "invalid"
	}
	r, f := int(o)&3, int(o)>>2
	s := []string{"", "rotated 90 right", "rotated 180", "rotated 90 left"}[r]
	if f == 1 {
		if s == "" {
			return "flipped horizontally"
		}
		return "flipped horizontally + " + s
	}
	if s == "" {
		return "as scanned"
	}
	return s
}

// SrcAffine gives the source pixel of output pixel (X, Y) of an oriented
// w x h image: x = ax*X + bx*Y + cx, y = ay*X + by*Y + cy (exact integers).
func (o Orient) SrcAffine(w, h int) (ax, bx, cx, ay, by, cy int) {
	W, H := o.Dims(w, h)
	M := o.m()
	// Mt = inverse (orthogonal); centred doubled coords:
	// 2x+1-w = Mt00*(2X+1-W) + Mt01*(2Y+1-H) (Mt01 = M10)
	t00, t01, t10, t11 := M[0], M[2], M[1], M[3]
	ax, bx = t00, t01
	cx = (t00*(1-W) + t01*(1-H) + w - 1) / 2
	ay, by = t10, t11
	cy = (t10*(1-W) + t11*(1-H) + h - 1) / 2
	return
}

// Src maps output pixel (X, Y) of the oriented image to the source pixel.
func (o Orient) Src(w, h, X, Y int) (int, int) {
	ax, bx, cx, ay, by, cy := o.SrcAffine(w, h)
	return ax*X + bx*Y + cx, ay*X + by*Y + cy
}

// ToSrcF maps a continuous point of the oriented image (0..W, 0..H) to the
// source image (0..w, 0..h).
func (o Orient) ToSrcF(w, h int, X, Y float64) (float64, float64) {
	W, H := o.Dims(w, h)
	M := o.m()
	cx, cy := 2*X-float64(W), 2*Y-float64(H)
	sx := float64(M[0])*cx + float64(M[2])*cy
	sy := float64(M[1])*cx + float64(M[3])*cy
	return (sx + float64(w)) / 2, (sy + float64(h)) / 2
}

// FromSrcF maps a continuous source point to the oriented image.
func (o Orient) FromSrcF(w, h int, x, y float64) (float64, float64) {
	W, H := o.Dims(w, h)
	M := o.m()
	cx, cy := 2*x-float64(w), 2*y-float64(h)
	X := float64(M[0])*cx + float64(M[1])*cy
	Y := float64(M[2])*cx + float64(M[3])*cy
	return (X + float64(W)) / 2, (Y + float64(H)) / 2
}

// OrientU16 remaps 3-sample RGB pixels losslessly (o == 0 returns pix).
func OrientU16(pix []uint16, w, h int, o Orient) ([]uint16, int, int) {
	if o == 0 || !o.Valid() {
		return pix, w, h
	}
	W, H := o.Dims(w, h)
	out := make([]uint16, W*H*3)
	ax, bx, cx, ay, by, cy := o.SrcAffine(w, h)
	parallelRows(H, func(Y int) {
		for X := 0; X < W; X++ {
			s := ((ay*X+by*Y+cy)*w + ax*X + bx*Y + cx) * 3
			d := (Y*W + X) * 3
			out[d], out[d+1], out[d+2] = pix[s], pix[s+1], pix[s+2]
		}
	})
	return out, W, H
}

// OrientF32 is OrientU16 for float RGB.
func OrientF32(pix []float32, w, h int, o Orient) ([]float32, int, int) {
	if o == 0 || !o.Valid() {
		return pix, w, h
	}
	W, H := o.Dims(w, h)
	out := make([]float32, W*H*3)
	ax, bx, cx, ay, by, cy := o.SrcAffine(w, h)
	parallelRows(H, func(Y int) {
		for X := 0; X < W; X++ {
			s := ((ay*X+by*Y+cy)*w + ax*X + bx*Y + cx) * 3
			d := (Y*W + X) * 3
			out[d], out[d+1], out[d+2] = pix[s], pix[s+1], pix[s+2]
		}
	})
	return out, W, H
}

// RegionF32 reads the oriented image region [x0, x0+n*k) x [y0, y0+m*k)
// of the w x h source pix (u16 RGB, stride sw >= w) as n x m float RGB
// (0..1), averaging k x k blocks. The region must lie inside the oriented
// w x h frame.
func RegionF32(pix []uint16, sw, w, h int, o Orient, x0, y0, n, m, k int) []float32 {
	out := make([]float32, n*m*3)
	ax, bx, cx, ay, by, cy := o.SrcAffine(w, h)
	inv := float32(1 / (65535 * float64(k*k)))
	parallelRows(m, func(j int) {
		for i := 0; i < n; i++ {
			var s0, s1, s2 uint32
			for yy := 0; yy < k; yy++ {
				Y := y0 + j*k + yy
				for xx := 0; xx < k; xx++ {
					X := x0 + i*k + xx
					s := ((ay*X+by*Y+cy)*sw + ax*X + bx*Y + cx) * 3
					s0 += uint32(pix[s])
					s1 += uint32(pix[s+1])
					s2 += uint32(pix[s+2])
				}
			}
			d := (j*n + i) * 3
			out[d], out[d+1], out[d+2] = float32(s0)*inv, float32(s1)*inv, float32(s2)*inv
		}
	})
	return out
}

func parallelRows(h int, f func(y int)) {
	const parts = 8
	done := make(chan struct{}, parts)
	for p := 0; p < parts; p++ {
		a, b := p*h/parts, (p+1)*h/parts
		go func() {
			for y := a; y < b; y++ {
				f(y)
			}
			done <- struct{}{}
		}()
	}
	for p := 0; p < parts; p++ {
		<-done
	}
}

// ---------- preview view transform ----------

// View maps an image of IW x IH pixels (the oriented frame, full-resolution
// pixel units) into a viewport: Fit, or Zoom screen pixels per image pixel
// with the image point (CX, CY) at the viewport centre.
type View struct {
	Fit    bool
	Zoom   float64
	CX, CY float64
	IW, IH int
	VX, VY int // viewport origin (client pixels)
	VW, VH int // viewport size
	Margin int
}

// Zoom limits (screen px per image px).
const (
	ZoomMax = 16.0
)

// FitScale is the zoom that fits the whole image in the viewport.
func (v View) FitScale() float64 {
	if v.IW <= 0 || v.IH <= 0 {
		return 1
	}
	return math.Max(1e-6, math.Min(float64(v.VW-2*v.Margin)/float64(v.IW), float64(v.VH-2*v.Margin)/float64(v.IH)))
}

// Scale is the effective zoom.
func (v View) Scale() float64 {
	if v.Fit {
		return v.FitScale()
	}
	return v.Zoom
}

// Center is the effective image point at the viewport centre.
func (v View) Center() (float64, float64) {
	if v.Fit {
		return float64(v.IW) / 2, float64(v.IH) / 2
	}
	return v.CX, v.CY
}

// ToScreen maps an image point to client coordinates.
func (v View) ToScreen(x, y float64) (float64, float64) {
	s := v.Scale()
	cx, cy := v.Center()
	return float64(v.VX) + float64(v.VW)/2 + (x-cx)*s, float64(v.VY) + float64(v.VH)/2 + (y-cy)*s
}

// ToImage maps client coordinates to an image point.
func (v View) ToImage(sx, sy float64) (float64, float64) {
	s := v.Scale()
	cx, cy := v.Center()
	return cx + (sx-float64(v.VX)-float64(v.VW)/2)/s, cy + (sy-float64(v.VY)-float64(v.VH)/2)/s
}

// MinZoom is the smallest zoom (a quarter of fit, at most 1).
func (v View) MinZoom() float64 { return math.Min(1, v.FitScale()/4) }

// clamp keeps the centre inside the image.
func (v *View) clamp() {
	v.CX = math.Max(0, math.Min(float64(v.IW), v.CX))
	v.CY = math.Max(0, math.Min(float64(v.IH), v.CY))
}

// ZoomAt sets the zoom to z keeping the image point under (sx, sy) fixed.
func (v *View) ZoomAt(z, sx, sy float64) {
	z = math.Max(v.MinZoom(), math.Min(ZoomMax, z))
	px, py := v.ToImage(sx, sy)
	v.Fit, v.Zoom = false, z
	v.CX = px - (sx-float64(v.VX)-float64(v.VW)/2)/z
	v.CY = py - (sy-float64(v.VY)-float64(v.VH)/2)/z
	v.clamp()
}

// SetZoom sets the zoom about the current centre (100% button).
func (v *View) SetZoom(z float64) {
	v.CX, v.CY = v.Center()
	v.Fit, v.Zoom = false, math.Max(v.MinZoom(), math.Min(ZoomMax, z))
	v.clamp()
}

// PanTo moves the view so that image point (ix, iy) is under (sx, sy).
func (v *View) PanTo(ix, iy, sx, sy float64) {
	if v.Fit {
		v.CX, v.CY = v.Center()
		v.Zoom, v.Fit = v.FitScale(), false
	}
	v.CX = ix - (sx-float64(v.VX)-float64(v.VW)/2)/v.Zoom
	v.CY = iy - (sy-float64(v.VY)-float64(v.VH)/2)/v.Zoom
	v.clamp()
}

// Label is the zoom readout, e.g. "Fit 23%" or "100%".
func (v View) Label() string {
	p := v.Scale() * 100
	f := "%.0f%%"
	if p < 10 {
		f = "%.1f%%"
	}
	if v.Fit {
		return "Fit " + fmt.Sprintf(f, p)
	}
	return fmt.Sprintf(f, p)
}

// Visible is the part of the image inside the viewport, in integer image
// pixels (expanded to whole pixels, clipped to the image).
func (v View) Visible() (x0, y0, x1, y1 int) {
	ax, ay := v.ToImage(float64(v.VX), float64(v.VY))
	bx, by := v.ToImage(float64(v.VX+v.VW), float64(v.VY+v.VH))
	x0, y0 = max(0, int(math.Floor(ax))), max(0, int(math.Floor(ay)))
	x1, y1 = min(v.IW, int(math.Ceil(bx))), min(v.IH, int(math.Ceil(by)))
	return
}
