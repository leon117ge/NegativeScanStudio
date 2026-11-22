package hdrscan

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Mono-trichrome lateral chromatic aberration correction (v1.09).
//
// Green is the focus and geometry reference and is never touched. For R and
// B the displacement against green is measured locally on a full-resolution
// tile grid (band-passed, integer NCC search plus Lucas-Kanade refinement).
// A radial lateral-CA model (r and r^3 about the image centre, separate
// horizontal and vertical coefficients, plus a translation kept only when it
// is clearly non-zero) is fitted robustly. A smooth local refinement (robust
// kernel average of the tile residuals, faded out where the radial model is
// small) absorbs film-flatness changes between the captures. The channel is
// resampled with a Kaiser-windowed sinc (8 taps, gain never above 1, so it
// cannot sharpen) only where the displacement is visible: below caBlendLo
// pixels are copied bit for bit, with a smooth blend up to caBlendHi. No
// sharpening of any kind is applied. A per-region guard (centre and four
// corners) checks that registration improved and sharpness did not drop; if
// not, that channel falls back to identity.

const (
	caSmoothSigma = 2.0 // px: grain and sensor-pattern suppression for the measurement only
	caMinNCC      = 0.5
	caInset       = 0.05 // fraction of the short edge kept free of tile centres (holder mask, rebate)
	caMinShift    = 0.20 // px: a fitted translation smaller than this is dropped
	caBlendLo     = 0.12 // px: below this the channel is copied unchanged
	caBlendHi     = 0.20 // px: at or above this the resampled value is used fully
	caTileFloor   = 0.15 // px: per-tile precision floor (content differences between dye layers)
	caLowSigma    = 8.0  // px: large-scale structure removed for the measurement
	caLowRadius   = 8    // three box passes of width 17 approximate a Gaussian of sigma 8.5
	caSearch      = 6    // px: integer search radius
	caGridX       = 19
	caGridY       = 13
	caSharpGuard  = 0.99 // minimum robust Tenengrad ratio per region
)

// RadialCA is the displacement of a colour plane relative to green:
// a feature at p in green sits at p + Disp(p) in the plane. With
// u = (p - (CX, CY)) / RN (RN = half-diagonal) and r = |u|:
//
//	dx = TX + ux * (AX1 + AX3 r^2)
//	dy = TY + uy * (AY1 + AY3 r^2)
//
// so the coefficients are pixels at r = 1 (the frame corner). AX = AY is
// the classic rotationally symmetric lateral CA; separate horizontal and
// vertical terms absorb the slight anisotropy seen on real scans (film not
// perfectly flat in the holder).
type RadialCA struct {
	TX, TY   float64
	AX1, AX3 float64
	AY1, AY3 float64
	CX, CY   float64
	RN       float64
}

const caParams = 6

const caBigTile = 5 // fallback patch size in half-tiles

func (m *RadialCA) setTheta(th [caParams]float64) {
	m.TX, m.TY, m.AX1, m.AX3, m.AY1, m.AY3 = th[0], th[1], th[2], th[3], th[4], th[5]
}

// Disp is the model displacement in pixels at (x, y).
func (m RadialCA) Disp(x, y float64) (dx, dy float64) {
	if m.RN <= 0 {
		return m.TX, m.TY
	}
	ux := (x - m.CX) / m.RN
	uy := (y - m.CY) / m.RN
	r2 := ux*ux + uy*uy
	return m.TX + ux*(m.AX1+m.AX3*r2), m.TY + uy*(m.AY1+m.AY3*r2)
}

// MaxCorner is the largest model displacement at the four frame corners.
func (m RadialCA) MaxCorner(w, h int) float64 {
	var mx float64
	for _, c := range [4][2]float64{{0, 0}, {float64(w - 1), 0}, {0, float64(h - 1)}, {float64(w - 1), float64(h - 1)}} {
		dx, dy := m.Disp(c[0], c[1])
		if v := math.Hypot(dx, dy); v > mx {
			mx = v
		}
	}
	return mx
}

// RegionCheck is one guard region: residual shift magnitude against green
// before and after correction (px) and robust Tenengrad after/before.
type RegionCheck struct {
	Name          string
	Measured      bool
	Before, After float64
	Sharp         float64
}

// ChannelCA describes what was fitted and applied for one colour plane.
type ChannelCA struct {
	Model      RadialCA
	Applied    bool
	Tiles      int // tiles measured
	Used       int // tiles that passed confidence and outlier rejection
	FitRMS     float64
	MaxCorner  float64 // largest applied displacement at the four frame corners
	LocalMax   float64 // largest local refinement on top of the radial model
	Regions    []RegionCheck
	Reason     string // why identity was used, if not applied
	Calculated bool
}

// FocusShiftReport is what the UI and the TIFF Processing field show.
type FocusShiftReport struct {
	Enabled bool
	R, B    ChannelCA
	Note    string
}

func (r FocusShiftReport) String() string {
	if !r.Enabled {
		return "CA correction off"
	}
	var b strings.Builder
	b.WriteString("CA correction on (green reference, radial lateral model about image centre, windowed-sinc resampling, no sharpening)")
	writeChannelCA(&b, "R", r.R)
	writeChannelCA(&b, "B", r.B)
	if r.Note != "" {
		b.WriteString("; ")
		b.WriteString(r.Note)
	}
	return b.String()
}

func writeChannelCA(b *strings.Builder, name string, c ChannelCA) {
	fmt.Fprintf(b, "; %s ", name)
	if !c.Calculated {
		fmt.Fprintf(b, "identity (%s)", c.Reason)
		return
	}
	if c.Applied {
		b.WriteString("applied")
	} else {
		fmt.Fprintf(b, "identity (%s)", c.Reason)
	}
	m := c.Model
	fmt.Fprintf(b, ": shift %+.2f,%+.2f px, radial x a1 %+.2f a3 %+.2f, y a1 %+.2f a3 %+.2f px at corner radius, local refinement up to %.2f px, max corner %.2f px, tiles %d/%d",
		m.TX, m.TY, m.AX1, m.AX3, m.AY1, m.AY3, c.LocalMax, c.MaxCorner, c.Used, c.Tiles)
}

// CorrectMonoCA registers R and B to G. Green is returned unchanged. A
// channel that needs no correction (or fails the guard) is returned as the
// identical slice.
func CorrectMonoCA(r, g, b []float64, w, h int) (rr, gg, bb []float64, report FocusShiftReport) {
	report.Enabled = true
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); rr, report.R = correctChannelCA(r, g, w, h) }()
	go func() { defer wg.Done(); bb, report.B = correctChannelCA(b, g, w, h) }()
	wg.Wait()
	return rr, g, bb, report
}

func caTileHalf(w, h int) int {
	half := minInt(w, h) / 16
	if half > 112 {
		half = 112
	}
	return half
}

const caPad = caSearch + 4 + 5 // search margin + LK slack + smoothing kernel radius

func correctChannelCA(ch, g []float64, w, h int) ([]float64, ChannelCA) {
	var info ChannelCA
	half := caTileHalf(w, h)
	if half < 16 || len(ch) < w*h || len(g) < w*h {
		info.Reason = "image too small to measure"
		return ch, info
	}
	m, tiles, ok, why := fitChannelCA(ch, g, w, h, half, &info)
	info.Model = m
	info.MaxCorner = m.MaxCorner(w, h)
	if !ok {
		info.Reason = why
		return ch, info
	}
	info.Calculated = true
	f := newCAField(m, tiles, w, h)
	info.LocalMax = f.localMax
	info.MaxCorner = f.maxCorner(w, h)
	out, touched := applyCAField(ch, w, h, f)
	if !touched {
		info.Reason = fmt.Sprintf("displacement below %.2f px everywhere", caBlendLo)
		return ch, info
	}
	pass, why := guardChannelCA(ch, out, g, w, h, half, f, &info)
	if !pass {
		info.Reason = "guard: " + why
		return ch, info
	}
	info.Applied = true
	return out, info
}

// ---- measurement ----------------------------------------------------------

type tileShift struct {
	X, Y   float64
	DX, DY float64
	Wxx    float64 // information matrix of (DX, DY), px^-2
	Wxy    float64
	Wyy    float64
	NCC    float64
	OK     bool
}

func gridCentres(w, h, half int) [][2]int {
	m := half + caPad
	if in := int(caInset * float64(minInt(w, h))); in > m {
		m = in
	}
	var out [][2]int
	for j := 0; j < caGridY; j++ {
		y := m + int(math.Round(float64(j)*float64(h-1-2*m)/float64(caGridY-1)))
		for i := 0; i < caGridX; i++ {
			x := m + int(math.Round(float64(i)*float64(w-1-2*m)/float64(caGridX-1)))
			out = append(out, [2]int{x, y})
		}
	}
	return out
}

func fitChannelCA(ch, g []float64, w, h, half int, info *ChannelCA) (RadialCA, []tileShift, bool, string) {
	model := RadialCA{CX: float64(w-1) / 2, CY: float64(h-1) / 2, RN: math.Hypot(float64(w-1)/2, float64(h-1)/2)}
	centres := gridCentres(w, h, half)
	tiles := make([]tileShift, len(centres))
	parallelFor(len(centres), func(i int) {
		tiles[i] = estimateTile(ch, g, w, h, centres[i][0], centres[i][1], half)
		if !tiles[i].OK {
			// Low-texture area: a larger patch collects enough grain and
			// detail to register (the stronger smoothing keeps the sensor's
			// fixed pattern, identical in all three captures, from pulling
			// the estimate towards zero).
			tiles[i] = estimateTile(ch, g, w, h, centres[i][0], centres[i][1], caBigTile*half/2)
		}
	})
	info.Tiles = len(tiles)
	var good []tileShift
	for _, t := range tiles {
		if t.OK {
			good = append(good, t)
		}
	}
	if len(good) < 8 {
		return model, nil, false, fmt.Sprintf("only %d of %d tiles had enough texture", len(good), len(tiles))
	}
	theta, used, rms := robustRadialFit(good, model, true)
	if math.Hypot(theta[0], theta[1]) < caMinShift {
		// A tiny translation is more likely measurement bias than a real
		// offset between captures; refit with the radial terms only so the
		// optical centre stays untouched.
		theta, used, rms = robustRadialFit(good, model, false)
	}
	model.setTheta(theta)
	info.Used = used
	info.FitRMS = rms
	if used < 8 {
		return model, nil, false, fmt.Sprintf("only %d tiles agreed with a radial model", used)
	}
	return model, good, true, ""
}

func radialJ(t tileShift, m RadialCA) (jx, jy [caParams]float64) {
	ux := (t.X - m.CX) / m.RN
	uy := (t.Y - m.CY) / m.RN
	r2 := ux*ux + uy*uy
	jx = [caParams]float64{1, 0, ux, ux * r2, 0, 0}
	jy = [caParams]float64{0, 1, 0, 0, uy, uy * r2}
	return
}

// robustRadialFit is an information-weighted least-squares fit of
// the model parameters with Tukey re-weighting on the Mahalanobis residual.
func robustRadialFit(tiles []tileShift, m RadialCA, withShift bool) ([caParams]float64, int, float64) {
	rho := make([]float64, len(tiles))
	for i := range rho {
		rho[i] = 1
	}
	var theta [caParams]float64
	prior := [caParams]float64{1 / 9.0, 1 / 9.0, 1 / 25.0, 1 / 25.0, 1 / 25.0, 1 / 25.0}
	if !withShift {
		prior[0], prior[1] = 1e12, 1e12
	}
	for it := 0; it < 12; it++ {
		var N [caParams][caParams]float64
		var v [caParams]float64
		for k := 0; k < caParams; k++ {
			N[k][k] = prior[k]
		}
		for i, t := range tiles {
			if rho[i] == 0 {
				continue
			}
			jx, jy := radialJ(t, m)
			for a := 0; a < caParams; a++ {
				// row a of J^T W
				wa0 := rho[i] * (jx[a]*t.Wxx + jy[a]*t.Wxy)
				wa1 := rho[i] * (jx[a]*t.Wxy + jy[a]*t.Wyy)
				for c := 0; c < caParams; c++ {
					N[a][c] += wa0*jx[c] + wa1*jy[c]
				}
				v[a] += wa0*t.DX + wa1*t.DY
			}
		}
		sol, ok := solveN(N, v)
		if !ok {
			break
		}
		theta = sol
		// residuals
		md := make([]float64, len(tiles))
		for i, t := range tiles {
			rx, ry := tileResidual(t, m, theta)
			md[i] = math.Sqrt(math.Max(0, rx*rx*t.Wxx+2*rx*ry*t.Wxy+ry*ry*t.Wyy))
		}
		s := 1.4826 * median(md)
		if s < 0.5 {
			s = 0.5
		}
		c := 4.685 * s
		for i := range tiles {
			u := md[i] / c
			if u >= 1 {
				rho[i] = 0
			} else {
				rho[i] = (1 - u*u) * (1 - u*u)
			}
		}
	}
	used := 0
	var se, sw float64
	for i, t := range tiles {
		if rho[i] > 0 {
			used++
			rx, ry := tileResidual(t, m, theta)
			se += rho[i] * (rx*rx + ry*ry)
			sw += rho[i]
		}
	}
	rms := 0.0
	if sw > 0 {
		rms = math.Sqrt(se / sw)
	}
	return theta, used, rms
}

func tileResidual(t tileShift, m RadialCA, th [caParams]float64) (float64, float64) {
	jx, jy := radialJ(t, m)
	var px, py float64
	for k := 0; k < caParams; k++ {
		px += jx[k] * th[k]
		py += jy[k] * th[k]
	}
	return t.DX - px, t.DY - py
}

func solveN(A [caParams][caParams]float64, b [caParams]float64) ([caParams]float64, bool) {
	var x [caParams]float64
	const n = caParams
	for c := 0; c < n; c++ {
		p := c
		for r := c + 1; r < n; r++ {
			if math.Abs(A[r][c]) > math.Abs(A[p][c]) {
				p = r
			}
		}
		if math.Abs(A[p][c]) < 1e-15 {
			return x, false
		}
		A[c], A[p] = A[p], A[c]
		b[c], b[p] = b[p], b[c]
		for r := c + 1; r < n; r++ {
			f := A[r][c] / A[c][c]
			for k := c; k < n; k++ {
				A[r][k] -= f * A[c][k]
			}
			b[r] -= f * b[c]
		}
	}
	for c := n - 1; c >= 0; c-- {
		s := b[c]
		for k := c + 1; k < n; k++ {
			s -= A[c][k] * x[k]
		}
		x[c] = s / A[c][c]
	}
	return x, true
}

func solve4(A [4][4]float64, b [4]float64) ([4]float64, bool) {
	var x [4]float64
	for c := 0; c < 4; c++ {
		p := c
		for r := c + 1; r < 4; r++ {
			if math.Abs(A[r][c]) > math.Abs(A[p][c]) {
				p = r
			}
		}
		if math.Abs(A[p][c]) < 1e-15 {
			return x, false
		}
		A[c], A[p] = A[p], A[c]
		b[c], b[p] = b[p], b[c]
		for r := c + 1; r < 4; r++ {
			f := A[r][c] / A[c][c]
			for k := c; k < 4; k++ {
				A[r][k] -= f * A[c][k]
			}
			b[r] -= f * b[c]
		}
	}
	for c := 3; c >= 0; c-- {
		s := b[c]
		for k := c + 1; k < 4; k++ {
			s -= A[c][k] * x[k]
		}
		x[c] = s / A[c][c]
	}
	return x, true
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

// extractRegion copies [x0, x0+n) x [y0, y0+n) with edge clamping.
func extractRegion(p []float64, w, h, x0, y0, n int) []float64 {
	out := make([]float64, n*n)
	for j := 0; j < n; j++ {
		yy := clampInt(y0+j, 0, h-1)
		for i := 0; i < n; i++ {
			out[j*n+i] = p[yy*w+clampInt(x0+i, 0, w-1)]
		}
	}
	return out
}

// estimateTile measures where the plane's features sit relative to green in
// the square of side 2*half centred at (cx, cy): integer NCC search, then a
// Lucas-Kanade refinement with gain and bias, on lightly smoothed data.
func estimateTile(ch, g []float64, w, h, cx, cy, half int) tileShift {
	res := tileShift{X: float64(cx), Y: float64(cy)}
	P := 2 * half
	E := caPad
	n := P + 2*E
	x0 := cx - half - E
	y0 := cy - half - E
	gs := bandPass(g, w, h, x0, y0, n)
	cs := bandPass(ch, w, h, x0, y0, n)
	// green template: interior P x P, zero mean / unit std
	gt := make([]float64, P*P)
	var gm, gv float64
	for j := 0; j < P; j++ {
		for i := 0; i < P; i++ {
			v := gs[(j+E)*n+i+E]
			gt[j*P+i] = v
			gm += v
		}
	}
	gm /= float64(P * P)
	for i := range gt {
		gt[i] -= gm
		gv += gt[i] * gt[i]
	}
	gstd := math.Sqrt(gv / float64(P*P))
	if gstd < 1e-6 {
		return res
	}
	for i := range gt {
		gt[i] /= gstd
	}
	var cm, cv float64
	for _, v := range cs {
		cm += v
	}
	cm /= float64(len(cs))
	for i := range cs {
		cs[i] -= cm
		cv += cs[i] * cs[i]
	}
	cstd := math.Sqrt(cv / float64(len(cs)))
	if cstd < 1e-6 {
		return res
	}
	for i := range cs {
		cs[i] /= cstd
	}
	// integer search
	best := -2.0
	bx, by := 0, 0
	for sy := -caSearch; sy <= caSearch; sy++ {
		for sx := -caSearch; sx <= caSearch; sx++ {
			var sc, scc, sgc float64
			cnt := 0
			for j := 0; j < P; j += 2 {
				crow := (j+E+sy)*n + E + sx
				grow := j * P
				for i := 0; i < P; i += 2 {
					c := cs[crow+i]
					sc += c
					scc += c * c
					sgc += gt[grow+i] * c
					cnt++
				}
			}
			mc := sc / float64(cnt)
			den := math.Sqrt(math.Max(1e-12, scc/float64(cnt)-mc*mc))
			v := sgc / float64(cnt) / den
			if v > best {
				best, bx, by = v, sx, sy
			}
		}
	}
	if best < caMinNCC*0.8 || absInt(bx) >= caSearch || absInt(by) >= caSearch {
		return res
	}
	// gradients of the plane region
	gxp := make([]float64, n*n)
	gyp := make([]float64, n*n)
	for j := 1; j < n-1; j++ {
		for i := 1; i < n-1; i++ {
			k := j*n + i
			gxp[k] = 0.5 * (cs[k+1] - cs[k-1])
			gyp[k] = 0.5 * (cs[k+n] - cs[k-n])
		}
	}
	dx, dy := float64(bx), float64(by)
	a, b := best, 0.0
	var H [4][4]float64
	var sse float64
	cnt := 0
	step := 1
	if P > 256 {
		step = 2
	}
	for it := 0; it < 20; it++ {
		H = [4][4]float64{}
		var gvec [4]float64
		sse = 0
		cnt = 0
		for j := 0; j < P; j += step {
			fy := float64(j+E) + dy
			for i := 0; i < P; i += step {
				fx := float64(i+E) + dx
				c, ok := sampleBilinear(cs, n, n, fx, fy)
				if !ok {
					continue
				}
				cgx, _ := sampleBilinear(gxp, n, n, fx, fy)
				cgy, _ := sampleBilinear(gyp, n, n, fx, fy)
				gg := gt[j*P+i]
				e := c - a*gg - b
				J := [4]float64{cgx, cgy, -gg, -1}
				for p := 0; p < 4; p++ {
					for q := p; q < 4; q++ {
						H[p][q] += J[p] * J[q]
					}
					gvec[p] += J[p] * e
				}
				sse += e * e
				cnt++
			}
		}
		for p := 0; p < 4; p++ {
			for q := 0; q < p; q++ {
				H[p][q] = H[q][p]
			}
		}
		step, ok := solve4(H, [4]float64{-gvec[0], -gvec[1], -gvec[2], -gvec[3]})
		if !ok {
			return res
		}
		dx += step[0]
		dy += step[1]
		a += step[2]
		b += step[3]
		if math.Abs(dx) > caSearch+2 || math.Abs(dy) > caSearch+2 {
			return res
		}
		if math.Abs(step[0]) < 1e-4 && math.Abs(step[1]) < 1e-4 {
			break
		}
	}
	if cnt < 16 {
		return res
	}
	// covariance of (dx, dy): sigma^2 * (H^-1)_dd, inflated for the
	// pixel-to-pixel correlation of smoothed grain, plus a precision floor.
	inv, ok := invert4(H)
	if !ok {
		return res
	}
	s2 := sse / float64(cnt-4)
	kappa := math.Max(1, 4*math.Pi*caSmoothSigma*caSmoothSigma/float64(step*step))
	cxx := s2*kappa*inv[0][0] + caTileFloor*caTileFloor
	cyy := s2*kappa*inv[1][1] + caTileFloor*caTileFloor
	cxy := s2 * kappa * inv[0][1]
	det := cxx*cyy - cxy*cxy
	if det <= 0 {
		return res
	}
	ncc := finalNCC(cs, gt, n, P, E, dx, dy)
	res.DX, res.DY = dx, dy
	res.Wxx = cyy / det
	res.Wyy = cxx / det
	res.Wxy = -cxy / det
	res.NCC = ncc
	res.OK = ncc >= caMinNCC
	return res
}

func finalNCC(cs, gt []float64, n, P, E int, dx, dy float64) float64 {
	var sc, scc, sgc, sg, sgg float64
	cnt := 0
	for j := 0; j < P; j++ {
		for i := 0; i < P; i++ {
			c, ok := sampleBilinear(cs, n, n, float64(i+E)+dx, float64(j+E)+dy)
			if !ok {
				continue
			}
			g := gt[j*P+i]
			sc += c
			scc += c * c
			sgc += g * c
			sg += g
			sgg += g * g
			cnt++
		}
	}
	if cnt == 0 {
		return 0
	}
	fn := float64(cnt)
	cov := sgc/fn - sc/fn*sg/fn
	vc := scc/fn - sc/fn*sc/fn
	vg := sgg/fn - sg/fn*sg/fn
	if vc <= 0 || vg <= 0 {
		return 0
	}
	return cov / math.Sqrt(vc*vg)
}

// bandPass returns the n x n region at (x0, y0) filtered with a difference
// of Gaussians (caSmoothSigma minus caLowSigma), so grain is suppressed and
// the large-scale density ramps that differ between dye layers do not
// dominate the match.
func bandPass(p []float64, w, h, x0, y0, n int) []float64 {
	b := int(math.Ceil(3 * caLowSigma))
	m := n + 2*b
	r := extractRegion(p, w, h, x0-b, y0-b, m)
	hi := blurSeparable(r, m, m, caSmoothSigma)
	lo := boxBlur3(r, m, m, caLowRadius)
	out := make([]float64, n*n)
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			k := (j+b)*m + i + b
			out[j*n+i] = hi[k] - lo[k]
		}
	}
	return out
}

// boxBlur3 is three passes of a (2*rad+1) box filter in x and y with edge
// clamping: a cheap Gaussian approximation (sigma ~ sqrt(rad*(rad+1))).
func boxBlur3(src []float64, w, h, rad int) []float64 {
	a := append([]float64(nil), src...)
	b := make([]float64, len(src))
	for pass := 0; pass < 3; pass++ {
		boxPass(a, b, w, h, rad, 1, w)
		boxPass(b, a, h, w, rad, w, 1)
	}
	return a
}

// boxPass filters lines of length n (stride step between samples, lines
// lineStride apart) from src into dst.
func boxPass(src, dst []float64, n, lines, rad, step, lineStride int) {
	inv := 1 / float64(2*rad+1)
	for l := 0; l < lines; l++ {
		base := l * lineStride
		at := func(i int) float64 { return src[base+clampInt(i, 0, n-1)*step] }
		var s float64
		for i := -rad; i <= rad; i++ {
			s += at(i)
		}
		for i := 0; i < n; i++ {
			dst[base+i*step] = s * inv
			s += at(i+rad+1) - at(i-rad)
		}
	}
}

func invert4(A [4][4]float64) ([4][4]float64, bool) {
	var inv [4][4]float64
	for c := 0; c < 4; c++ {
		var e [4]float64
		e[c] = 1
		col, ok := solve4(A, e)
		if !ok {
			return inv, false
		}
		for r := 0; r < 4; r++ {
			inv[r][c] = col[r]
		}
	}
	return inv, true
}

// ---- resampling -----------------------------------------------------------

// Resampling kernel: sinc windowed by a Kaiser window (radius 4, beta 7).
// Its frequency response stays at or below 1.0003 for every sub-pixel phase
// (Lanczos-3 overshoots by up to 2.4 % around 0.2 cycles/px, which reads as
// sharpening), while keeping 1.00 at 0.2 and 0.96 at 0.3 cycles/px.
const (
	ksPhases = 1024
	ksRadius = 4
	ksBeta   = 7.0
	ksTaps   = 2 * ksRadius
)

var (
	ksOnce  sync.Once
	ksTable [ksPhases + 1][ksTaps]float64
)

func besselI0(x float64) float64 {
	s, t := 1.0, 1.0
	for k := 1; k < 50; k++ {
		t *= (x / (2 * float64(k))) * (x / (2 * float64(k)))
		s += t
		if t < 1e-17*s {
			break
		}
	}
	return s
}

func kaiserSinc(x float64) float64 {
	if x <= -ksRadius || x >= ksRadius {
		return 0
	}
	sinc := 1.0
	if x != 0 {
		px := math.Pi * x
		sinc = math.Sin(px) / px
	}
	u := x / ksRadius
	return sinc * besselI0(ksBeta*math.Sqrt(1-u*u)) / besselI0(ksBeta)
}

func ksInit() {
	for p := 0; p <= ksPhases; p++ {
		f := float64(p) / ksPhases
		var s float64
		for k := 0; k < ksTaps; k++ {
			v := kaiserSinc(float64(k-ksRadius+1) - f)
			ksTable[p][k] = v
			s += v
		}
		for k := 0; k < ksTaps; k++ {
			ksTable[p][k] /= s
		}
	}
}

// sampleSinc interpolates src at (x, y); taps run from floor-3 to floor+4.
func sampleSinc(src []float64, w, h int, x, y float64) float64 {
	ix := int(math.Floor(x))
	iy := int(math.Floor(y))
	wx := &ksTable[int((x-float64(ix))*ksPhases+0.5)]
	wy := &ksTable[int((y-float64(iy))*ksPhases+0.5)]
	x0 := ix - ksRadius + 1
	y0 := iy - ksRadius + 1
	if x0 >= 0 && y0 >= 0 && x0+ksTaps <= w && y0+ksTaps <= h {
		var s float64
		for j := 0; j < ksTaps; j++ {
			r := src[(y0+j)*w+x0 : (y0+j)*w+x0+ksTaps]
			s += wy[j] * (wx[0]*r[0] + wx[1]*r[1] + wx[2]*r[2] + wx[3]*r[3] + wx[4]*r[4] + wx[5]*r[5] + wx[6]*r[6] + wx[7]*r[7])
		}
		return s
	}
	var s float64
	for j := 0; j < ksTaps; j++ {
		yy := clampInt(y0+j, 0, h-1)
		var rs float64
		for i := 0; i < ksTaps; i++ {
			rs += wx[i] * src[yy*w+clampInt(x0+i, 0, w-1)]
		}
		s += wy[j] * rs
	}
	return s
}

func caBlend(mag float64) float64 {
	if mag <= caBlendLo {
		return 0
	}
	if mag >= caBlendHi {
		return 1
	}
	t := (mag - caBlendLo) / (caBlendHi - caBlendLo)
	return t * t * (3 - 2*t)
}

// applyRadialCA returns corrected(p) = src(p + d(p)) blended back to src
// where |d| is small. touched is false when no pixel needed resampling.
func applyCAField(src []float64, w, h int, m *caField) ([]float64, bool) {
	ksOnce.Do(ksInit)
	out := make([]float64, w*h)
	var mu sync.Mutex
	touched := false
	parallelRows(h, func(y0, y1 int) {
		t := false
		for y := y0; y < y1; y++ {
			row := y * w
			fy := float64(y)
			for x := 0; x < w; x++ {
				fx := float64(x)
				dx, dy := m.disp(fx, fy)
				wt := caBlend(math.Hypot(dx, dy))
				v := src[row+x]
				if wt > 0 {
					s := sampleSinc(src, w, h, fx+dx, fy+dy)
					v += wt * (s - v)
					if v < 0 {
						v = 0
					}
					t = true
				}
				out[row+x] = v
			}
		}
		if t {
			mu.Lock()
			touched = true
			mu.Unlock()
		}
	})
	return out, touched
}

// ---- local refinement ----------------------------------------------------

// The radial model captures the lens; the film is not perfectly flat and can
// move slightly between the three captures, which adds a smooth, local
// displacement on top of it (measured on 0281: up to about 0.4 px). The
// refinement is a robust, kernel-weighted average of the tile residuals,
// shrunk towards zero where few tiles support it and faded out where the
// radial model itself is small, so the frame centre stays untouched.
const (
	caLocalSigma = 0.09 // kernel sigma as a fraction of the frame diagonal
	caLocalW0    = 2.0  // shrinkage: effective tile count giving half weight
	caLocalRamp  = 1.0  // px: refinement fully enabled where |model| >= this
	caLocalCap   = 1.0  // px: largest refinement allowed
	caLocalNodes = 48   // nodes along the long edge
)

type caField struct {
	m        RadialCA
	gw, gh   int
	sx, sy   float64
	rx, ry   []float64
	localMax float64
}

func newCAField(m RadialCA, tiles []tileShift, w, h int) *caField {
	f := &caField{m: m}
	if len(tiles) == 0 {
		return f
	}
	long := maxInt(w, h)
	f.gw = int(math.Ceil(float64(w)/float64(long)*caLocalNodes)) + 1
	f.gh = int(math.Ceil(float64(h)/float64(long)*caLocalNodes)) + 1
	f.sx = float64(w-1) / float64(f.gw-1)
	f.sy = float64(h-1) / float64(f.gh-1)
	f.rx = make([]float64, f.gw*f.gh)
	f.ry = make([]float64, f.gw*f.gh)
	resx := make([]float64, len(tiles))
	resy := make([]float64, len(tiles))
	for i, t := range tiles {
		dx, dy := m.Disp(t.X, t.Y)
		resx[i] = t.DX - dx
		resy[i] = t.DY - dy
	}
	sig := caLocalSigma * math.Hypot(float64(w), float64(h))
	inv2s2 := 1 / (2 * sig * sig)
	parallelFor(f.gh, func(j int) {
		kw := make([]float64, len(tiles))
		ny := float64(j) * f.sy
		for i := 0; i < f.gw; i++ {
			nx := float64(i) * f.sx
			for k, t := range tiles {
				dx, dy := t.X-nx, t.Y-ny
				kw[k] = math.Exp(-(dx*dx + dy*dy) * inv2s2)
			}
			// robust local mean: Tukey re-weighting around the current estimate
			var mx, my float64
			var wsum float64
			for it := 0; it < 5; it++ {
				var sx, sy, sw float64
				for k := range tiles {
					wk := kw[k]
					if it > 0 {
						u := math.Hypot(resx[k]-mx, resy[k]-my) / 0.6
						if u >= 1 {
							continue
						}
						wk *= (1 - u*u) * (1 - u*u)
					}
					sx += wk * resx[k]
					sy += wk * resy[k]
					sw += wk
				}
				if sw <= 1e-12 {
					mx, my, wsum = 0, 0, 0
					break
				}
				mx, my, wsum = sx/sw, sy/sw, sw
			}
			shrink := wsum / (wsum + caLocalW0)
			mdx, mdy := m.Disp(nx, ny)
			ramp := math.Min(1, math.Hypot(mdx, mdy)/caLocalRamp)
			mx *= shrink * ramp
			my *= shrink * ramp
			if l := math.Hypot(mx, my); l > caLocalCap {
				mx *= caLocalCap / l
				my *= caLocalCap / l
			}
			f.rx[j*f.gw+i] = mx
			f.ry[j*f.gw+i] = my
		}
	})
	for i := range f.rx {
		if l := math.Hypot(f.rx[i], f.ry[i]); l > f.localMax {
			f.localMax = l
		}
	}
	return f
}

func (f *caField) disp(x, y float64) (float64, float64) {
	dx, dy := f.m.Disp(x, y)
	if f.rx == nil {
		return dx, dy
	}
	gx := x / f.sx
	gy := y / f.sy
	i := clampInt(int(gx), 0, f.gw-2)
	j := clampInt(int(gy), 0, f.gh-2)
	tx := gx - float64(i)
	ty := gy - float64(j)
	k := j*f.gw + i
	bl := func(a []float64) float64 {
		return (1-ty)*((1-tx)*a[k]+tx*a[k+1]) + ty*((1-tx)*a[k+f.gw]+tx*a[k+f.gw+1])
	}
	return dx + bl(f.rx), dy + bl(f.ry)
}

func (f *caField) maxCorner(w, h int) float64 {
	var mx float64
	for _, c := range [4][2]float64{{0, 0}, {float64(w - 1), 0}, {0, float64(h - 1)}, {float64(w - 1), float64(h - 1)}} {
		dx, dy := f.disp(c[0], c[1])
		if v := math.Hypot(dx, dy); v > mx {
			mx = v
		}
	}
	return mx
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---- guard ----------------------------------------------------------------

func guardRegions(w, h, half int) ([]string, [][2]int) {
	off := half + caPad + minInt(w, h)/32
	return []string{"centre", "top-left", "top-right", "bottom-left", "bottom-right"},
		[][2]int{{w / 2, h / 2}, {off, off}, {w - 1 - off, off}, {off, h - 1 - off}, {w - 1 - off, h - 1 - off}}
}

// robustTenengrad is the Tenengrad of the region after a sigma 0.85 blur, so
// single-pixel grain noise does not dominate the sharpness reading.
func robustTenengrad(p []float64, w, h, cx, cy, half int) float64 {
	n := 2*half + 8
	r := extractRegion(p, w, h, cx-half-4, cy-half-4, n)
	bl := blurSeparable(r, n, n, 0.85)
	in := make([]float64, 0, (n-8)*(n-8))
	for j := 4; j < n-4; j++ {
		in = append(in, bl[j*n+4:j*n+n-4]...)
	}
	return tenengrad(in, n-8, n-8)
}

func guardChannelCA(orig, corr, g []float64, w, h, half int, f *caField, info *ChannelCA) (bool, string) {
	names, cs := guardRegions(w, h, 2*half)
	rh := 2 * half
	checks := make([]RegionCheck, len(cs))
	parallelFor(len(cs), func(i int) {
		c := cs[i]
		rc := RegionCheck{Name: names[i], Sharp: 1}
		// Compare the same scene content: the corrected region at c shows
		// what the uncorrected plane had at c + d(c).
		dx, dy := f.disp(float64(c[0]), float64(c[1]))
		b := robustTenengrad(orig, w, h, c[0]+int(math.Round(dx)), c[1]+int(math.Round(dy)), rh)
		a := robustTenengrad(corr, w, h, c[0], c[1], rh)
		if b > 0 {
			rc.Sharp = a / b
		}
		tb := estimateTile(orig, g, w, h, c[0], c[1], rh)
		ta := estimateTile(corr, g, w, h, c[0], c[1], rh)
		if tb.OK && ta.OK {
			rc.Measured = true
			rc.Before = math.Hypot(tb.DX, tb.DY)
			rc.After = math.Hypot(ta.DX, ta.DY)
		}
		checks[i] = rc
	})
	info.Regions = checks
	for _, rc := range checks {
		if rc.Sharp < caSharpGuard {
			return false, fmt.Sprintf("%s sharpness ratio %.3f", rc.Name, rc.Sharp)
		}
		if rc.Measured && rc.After > rc.Before+0.05 && rc.After > 0.2 {
			return false, fmt.Sprintf("%s residual %.2f px > %.2f px uncorrected", rc.Name, rc.After, rc.Before)
		}
	}
	return true, ""
}

// ---- small helpers --------------------------------------------------------

func parallelFor(n int, f func(i int)) {
	workers := runtime.NumCPU()
	if workers > n {
		workers = n
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for k := 0; k < workers; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				f(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sampleBilinear(p []float64, w, h int, x, y float64) (float64, bool) {
	if x < 0 || y < 0 || x > float64(w-1) || y > float64(h-1) {
		return 0, false
	}
	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	x1 := x0 + 1
	y1 := y0 + 1
	if x1 >= w {
		x1 = w - 1
	}
	if y1 >= h {
		y1 = h - 1
	}
	tx := x - float64(x0)
	ty := y - float64(y0)
	a := p[y0*w+x0]
	b := p[y0*w+x1]
	c := p[y1*w+x0]
	d := p[y1*w+x1]
	return (1-ty)*((1-tx)*a+tx*b) + ty*((1-tx)*c+tx*d), true
}

func tenengrad(p []float64, w, h int) float64 {
	var s float64
	n := 0
	for y := 1; y < h-1; y++ {
		row := y * w
		for x := 1; x < w-1; x++ {
			gx := p[row+x+1] - p[row+x-1]
			gy := p[row+w+x] - p[row-w+x]
			s += gx*gx + gy*gy
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return s / float64(n)
}

func gaussianKernel(sig float64) []float64 {
	if sig < 0.15 {
		sig = 0.15
	}
	r := int(math.Ceil(sig * 3))
	if r < 1 {
		r = 1
	}
	k := make([]float64, 2*r+1)
	var sum float64
	for i := -r; i <= r; i++ {
		v := math.Exp(-float64(i*i) / (2 * sig * sig))
		k[i+r] = v
		sum += v
	}
	for i := range k {
		k[i] /= sum
	}
	return k
}

func blurSeparable(src []float64, w, h int, sig float64) []float64 {
	k := gaussianKernel(sig)
	r := len(k) / 2
	tmp := make([]float64, w*h)
	out := make([]float64, w*h)
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			var s float64
			for i := -r; i <= r; i++ {
				xx := x + i
				if xx < 0 {
					xx = 0
				}
				if xx >= w {
					xx = w - 1
				}
				s += src[row+xx] * k[i+r]
			}
			tmp[row+x] = s
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var s float64
			for i := -r; i <= r; i++ {
				yy := y + i
				if yy < 0 {
					yy = 0
				}
				if yy >= h {
					yy = h - 1
				}
				s += tmp[yy*w+x] * k[i+r]
			}
			out[y*w+x] = s
		}
	}
	return out
}
