package cropper

import "math"

// Rect is an axis-aligned crop rectangle in output-canvas coordinates
// (the rotated canvas has the same size as the source).
type Rect struct{ X0, Y0, X1, Y1 float64 }

func (r Rect) W() float64  { return r.X1 - r.X0 }
func (r Rect) H() float64  { return r.Y1 - r.Y0 }
func (r Rect) Cx() float64 { return (r.X0 + r.X1) / 2 }
func (r Rect) Cy() float64 { return (r.Y0 + r.Y1) / 2 }

// Aspect options.
const (
	AspectAuto     = -1.0 // 3:2 for landscape frames, 2:3 for portrait
	AspectOriginal = -2.0
	AspectFree     = 0.0
)

// HalfPlane: N·p <= K
type HalfPlane struct{ Nx, Ny, K float64 }

type vec struct{ X, Y float64 }

// rotation applied to the picture: angle in degrees, positive = CCW on
// screen. In y-down coordinates this is a rotation by -angle.
func rotFwd(angle float64, W, H float64, x, y float64) (float64, float64) {
	r := -angle * math.Pi / 180
	cx, cy := W/2, H/2
	c, s := math.Cos(r), math.Sin(r)
	dx, dy := x-cx, y-cy
	return cx + c*dx - s*dy, cy + s*dx + c*dy
}

// RotInv maps an output-canvas point back to the source image.
func RotInv(angle float64, W, H float64, x, y float64) (float64, float64) {
	return rotFwd(-angle, W, H, x, y)
}

// RotFwd maps a source point into the rotated output canvas.
func RotFwd(angle float64, W, H float64, x, y float64) (float64, float64) {
	return rotFwd(angle, W, H, x, y)
}

func planeThrough(a, b vec, inside vec, shift float64) HalfPlane {
	nx, ny := b.Y-a.Y, -(b.X - a.X)
	l := math.Hypot(nx, ny)
	if l == 0 {
		return HalfPlane{0, 0, 1}
	}
	nx, ny = nx/l, ny/l
	k := nx*a.X + ny*a.Y
	if nx*inside.X+ny*inside.Y > k { // make the inside satisfy N·p <= K
		nx, ny, k = -nx, -ny, -k
	}
	return HalfPlane{nx, ny, k - shift}
}

// Constraints returns the half-planes bounding usable picture in the rotated
// canvas: the rotated source rectangle, the canvas itself and (optionally)
// the detected frame edges moved inwards by margin pixels.
func Constraints(W, H int, angle float64, edges []Edge, margin float64) []HalfPlane {
	fw, fh := float64(W), float64(H)
	ctr := vec{fw / 2, fh / 2}
	corners := [4]vec{{0, 0}, {fw, 0}, {fw, fh}, {0, fh}}
	var hp []HalfPlane
	var rc [4]vec
	for i, c := range corners {
		x, y := rotFwd(angle, fw, fh, c.X, c.Y)
		rc[i] = vec{x, y}
	}
	for i := 0; i < 4; i++ {
		hp = append(hp, planeThrough(rc[i], rc[(i+1)%4], ctr, 0))
		hp = append(hp, planeThrough(corners[i], corners[(i+1)%4], ctr, 0))
	}
	for _, e := range edges {
		ax, ay := rotFwd(angle, fw, fh, e.P0[0], e.P0[1])
		bx, by := rotFwd(angle, fw, fh, e.P1[0], e.P1[1])
		hp = append(hp, planeThrough(vec{ax, ay}, vec{bx, by}, ctr, margin))
	}
	return hp
}

// clip a convex polygon against N·p <= K
func clip(poly []vec, h HalfPlane) []vec {
	if len(poly) == 0 {
		return nil
	}
	var out []vec
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		da := h.Nx*a.X + h.Ny*a.Y - h.K
		db := h.Nx*b.X + h.Ny*b.Y - h.K
		if da <= 0 {
			out = append(out, a)
		}
		if (da < 0 && db > 0) || (da > 0 && db < 0) {
			t := da / (da - db)
			out = append(out, vec{a.X + t*(b.X-a.X), a.Y + t*(b.Y-a.Y)})
		}
	}
	return out
}

// Region returns the convex usable region polygon.
func Region(hp []HalfPlane, W, H int) []vec {
	poly := []vec{{-1, -1}, {float64(W) + 1, -1}, {float64(W) + 1, float64(H) + 1}, {-1, float64(H) + 1}}
	for _, h := range hp {
		poly = clip(poly, h)
	}
	return poly
}

// RegionPoints exposes the region polygon as flat x,y pairs.
func RegionPoints(hp []HalfPlane, W, H int) [][2]float64 {
	var out [][2]float64
	for _, v := range Region(hp, W, H) {
		out = append(out, [2]float64{v.X, v.Y})
	}
	return out
}

func centroid(p []vec) vec {
	var c vec
	for _, v := range p {
		c.X += v.X
		c.Y += v.Y
	}
	if len(p) > 0 {
		c.X /= float64(len(p))
		c.Y /= float64(len(p))
	}
	return c
}

// areaCentroid is the area-weighted centre of a convex polygon (vertex
// average when it is degenerate).
func areaCentroid(p []vec) vec {
	var A, cx, cy float64
	for i := range p {
		a, b := p[i], p[(i+1)%len(p)]
		cr := a.X*b.Y - b.X*a.Y
		A += cr
		cx += (a.X + b.X) * cr
		cy += (a.Y + b.Y) * cr
	}
	if math.Abs(A) < 1e-9 {
		if len(p) == 0 {
			return vec{}
		}
		return centroid(p)
	}
	return vec{cx / (3 * A), cy / (3 * A)}
}

// centres where a w x h rectangle fits
func feasible(hp []HalfPlane, W, H int, w, h float64) []vec {
	poly := []vec{{-1, -1}, {float64(W) + 1, -1}, {float64(W) + 1, float64(H) + 1}, {-1, float64(H) + 1}}
	for _, p := range hp {
		k := p.K - (math.Abs(p.Nx)*w+math.Abs(p.Ny)*h)/2
		poly = clip(poly, HalfPlane{p.Nx, p.Ny, k})
		if len(poly) == 0 {
			return nil
		}
	}
	return poly
}

// fitAspect finds the largest rect with w/h = a.
// At the largest size the feasible centres collapse to a thin sliver (a
// segment when the aspect differs from the region's), whose vertex average
// is biased towards one end; the centre nearest the usable region's own
// centre keeps the crop centred along the free direction.
func fitAspect(hp []HalfPlane, W, H int, a float64) (Rect, bool) {
	lo, hi := 0.0, float64(H)+float64(W)/a
	var best Rect
	ok := false
	mid := areaCentroid(feasible(hp, W, H, 0, 0))
	for it := 0; it < 50; it++ {
		h := (lo + hi) / 2
		if f := feasible(hp, W, H, a*h, h); len(f) > 0 {
			lo = h
			c := closestIn(f, mid)
			best = Rect{c.X - a*h/2, c.Y - h/2, c.X + a*h/2, c.Y + h/2}
			ok = true
		} else {
			hi = h
		}
	}
	// Edges a fraction of a degree off square make the largest fit a thin
	// wedge whose tip sits at one end (QA B11: 1:1 hugging the left, 16:9
	// the top). Giving up 0.2% of the size widens the wedge to the whole
	// free range, so the crop can sit in the middle of it.
	if ok {
		h := lo - math.Max(2, lo*0.002)
		if f := feasible(hp, W, H, a*h, h); h > 0 && len(f) > 0 {
			c := closestIn(f, mid)
			best = Rect{c.X - a*h/2, c.Y - h/2, c.X + a*h/2, c.Y + h/2}
		}
	}
	return best, ok
}

// RegionAspect returns width/height of the usable region's bounding box.
func RegionAspect(hp []HalfPlane, W, H int) float64 {
	poly := Region(hp, W, H)
	if len(poly) == 0 {
		return float64(W) / float64(H)
	}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, v := range poly {
		x0, x1 = math.Min(x0, v.X), math.Max(x1, v.X)
		y0, y1 = math.Min(y0, v.Y), math.Max(y1, v.Y)
	}
	return (x1 - x0) / math.Max(1, y1-y0)
}

// ResolveAspect turns the aspect option into a concrete w/h ratio (0 = free).
func ResolveAspect(aspect float64, hp []HalfPlane, W, H int) float64 {
	switch aspect {
	case AspectAuto:
		if RegionAspect(hp, W, H) >= 1 {
			return 1.5
		}
		return 2.0 / 3.0
	case AspectOriginal:
		return float64(W) / float64(H)
	}
	return aspect
}

// FitRect finds the largest crop of the requested aspect inside hp and
// shrinks it by inset (fraction of each dimension, per side).
func FitRect(hp []HalfPlane, W, H int, aspect, inset float64) (Rect, bool) {
	a := ResolveAspect(aspect, hp, W, H)
	var r Rect
	ok := false
	if a > 0 {
		r, ok = fitAspect(hp, W, H, a)
	} else {
		// free: maximise area over aspect
		bestA := -1.0
		for i := 0; i <= 80; i++ {
			ar := math.Exp(math.Log(0.2) + float64(i)/80*math.Log(25))
			if rr, k := fitAspect(hp, W, H, ar); k && rr.W()*rr.H() > bestA {
				bestA, r, ok = rr.W()*rr.H(), rr, true
			}
		}
		if ok {
			lo, hi := math.Log(r.W()/r.H())-0.05, math.Log(r.W()/r.H())+0.05
			for it := 0; it < 30; it++ {
				m1, m2 := lo+(hi-lo)/3, hi-(hi-lo)/3
				r1, _ := fitAspect(hp, W, H, math.Exp(m1))
				r2, _ := fitAspect(hp, W, H, math.Exp(m2))
				if r1.W()*r1.H() < r2.W()*r2.H() {
					lo = m1
				} else {
					hi = m2
				}
			}
			if rr, k := fitAspect(hp, W, H, math.Exp((lo+hi)/2)); k && rr.W()*rr.H() >= bestA {
				r = rr
			}
		}
	}
	if !ok {
		return Rect{0, 0, float64(W), float64(H)}, false
	}
	return Inset(r, inset), true
}

// Inset shrinks r about its centre.
func Inset(r Rect, inset float64) Rect {
	if inset <= 0 {
		return r
	}
	dx, dy := r.W()*inset, r.H()*inset
	return Rect{r.X0 + dx, r.Y0 + dy, r.X1 - dx, r.Y1 - dy}
}

// Fits reports whether r lies inside all half-planes (with a small tolerance).
func Fits(hp []HalfPlane, r Rect) bool {
	for _, p := range hp {
		for _, c := range [4]vec{{r.X0, r.Y0}, {r.X1, r.Y0}, {r.X1, r.Y1}, {r.X0, r.Y1}} {
			if p.Nx*c.X+p.Ny*c.Y > p.K+0.5 {
				return false
			}
		}
	}
	return true
}

// FitInside keeps r's centre and aspect and shrinks/moves it until it fits.
func FitInside(hp []HalfPlane, W, H int, r Rect) Rect {
	if Fits(hp, r) {
		return r
	}
	w, h := r.W(), r.H()
	// try moving first: closest feasible centre
	if f := feasible(hp, W, H, w, h); len(f) > 0 {
		c := closestIn(f, vec{r.Cx(), r.Cy()})
		return Rect{c.X - w/2, c.Y - h/2, c.X + w/2, c.Y + h/2}
	}
	lo, hi := 0.0, 1.0
	for it := 0; it < 40; it++ {
		s := (lo + hi) / 2
		if len(feasible(hp, W, H, w*s, h*s)) > 0 {
			lo = s
		} else {
			hi = s
		}
	}
	f := feasible(hp, W, H, w*lo, h*lo)
	c := closestIn(f, vec{r.Cx(), r.Cy()})
	return Rect{c.X - w*lo/2, c.Y - h*lo/2, c.X + w*lo/2, c.Y + h*lo/2}
}

func closestIn(poly []vec, p vec) vec {
	if len(poly) == 0 {
		return p
	}
	inside := true
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		if (b.X-a.X)*(p.Y-a.Y)-(b.Y-a.Y)*(p.X-a.X) < 0 {
			inside = false
		}
	}
	// polygon orientation may be either; test both senses
	if !inside {
		inside = true
		for i := range poly {
			a, b := poly[i], poly[(i+1)%len(poly)]
			if (b.X-a.X)*(p.Y-a.Y)-(b.Y-a.Y)*(p.X-a.X) > 0 {
				inside = false
			}
		}
	}
	if inside {
		return p
	}
	best, bd := poly[0], math.Inf(1)
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		dx, dy := b.X-a.X, b.Y-a.Y
		l2 := dx*dx + dy*dy
		t := 0.0
		if l2 > 0 {
			t = math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/l2))
		}
		q := vec{a.X + t*dx, a.Y + t*dy}
		if d := math.Hypot(q.X-p.X, q.Y-p.Y); d < bd {
			best, bd = q, d
		}
	}
	return best
}
