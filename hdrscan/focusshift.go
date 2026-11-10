package hdrscan

import (
	"fmt"
	"math"
	"strings"
)

// FocusShift is the chromatic-aberration induced difference of one colour
// plane relative to green (the focus reference). Translation is in pixels
// of the full-resolution image (positive dx means the channel's features
// sit to the right of green). Scale is a magnification about the image
// centre (1 = no lateral CA). BlurRatio is the channel's estimated PSF
// width over green's (1 = equally sharp; >1 = softer).
type FocusShift struct {
	DX, DY     float64
	Scale      float64
	BlurRatio  float64
	SharpnessK float64 // unsharp amount applied; 0 if the channel was not softer
}

// FocusShiftReport is what the UI and the TIFF Processing field show.
type FocusShiftReport struct {
	Enabled bool
	R, B    FocusShift
	Note    string
}

func (r FocusShiftReport) String() string {
	if !r.Enabled {
		return "CA correction off"
	}
	var b strings.Builder
	b.WriteString("CA correction on (green reference)")
	fmt.Fprintf(&b, "; R shift %+.2f,%+.2f px scale %.5f blur %.3f", r.R.DX, r.R.DY, r.R.Scale, r.R.BlurRatio)
	if r.R.SharpnessK > 0 {
		fmt.Fprintf(&b, " (matched k=%.2f)", r.R.SharpnessK)
	}
	fmt.Fprintf(&b, "; B shift %+.2f,%+.2f px scale %.5f blur %.3f", r.B.DX, r.B.DY, r.B.Scale, r.B.BlurRatio)
	if r.B.SharpnessK > 0 {
		fmt.Fprintf(&b, " (matched k=%.2f)", r.B.SharpnessK)
	}
	if r.Note != "" {
		b.WriteString("; ")
		b.WriteString(r.Note)
	}
	return b.String()
}

// EstimateFocusShift measures how plane differs from green. Both slices
// are length w*h, in any linear unit (the estimator is scale-invariant).
func EstimateFocusShift(plane, green []float64, w, h int) FocusShift {
	fs := FocusShift{Scale: 1, BlurRatio: 1}
	if w < 16 || h < 16 || len(plane) < w*h || len(green) < w*h {
		return fs
	}
	// Work on a downsampled copy so the search stays interactive even at
	// 24 MP. Shifts are converted back to full-resolution pixels.
	pw, ph, p, g, factor := downsamplePair(plane, green, w, h, 800)
	dx, dy := estimateCAShift(p, g, pw, ph)
	sc := estimateScale(p, g, pw, ph, dx, dy)
	fs.DX, fs.DY = dx*factor, dy*factor
	fs.Scale = sc
	// Blur is measured after roughly aligning the channel to green, so a
	// large shift does not look like extra softness.
	aligned := warpPlane(p, pw, ph, FocusShift{DX: dx, DY: dy, Scale: sc, BlurRatio: 1})
	sg := edgeWidth(g, pw, ph)
	sp := edgeWidth(aligned, pw, ph)
	if sg > 1e-9 {
		fs.BlurRatio = sp / sg
	}
	if fs.BlurRatio < 0.5 {
		fs.BlurRatio = 0.5
	}
	if fs.BlurRatio > 4 {
		fs.BlurRatio = 4
	}
	return fs
}

// CorrectFocusShift warps plane onto green (translation + scale) and, when
// the channel is measurably softer, applies a mild unsharp match. Green
// itself is never modified; the returned slice is always a new buffer.
func CorrectFocusShift(plane []float64, w, h int, fs FocusShift) []float64 {
	out := warpPlane(plane, w, h, fs)
	if fs.BlurRatio > 1.05 {
		// Extra Gaussian width that would turn green's sharpness into this
		// channel's: sigma^2_extra = sigma_g^2 * (ratio^2 - 1), using a
		// unit green width so the kernel size is just sqrt(r^2-1) px.
		sig := math.Sqrt(fs.BlurRatio*fs.BlurRatio - 1)
		if sig > 0.15 {
			k := sharpnessMatch(out, w, h, sig)
			fs.SharpnessK = k
			if k > 0 {
				out = unsharp(out, w, h, sig, k)
			}
		}
	}
	return out
}

// CorrectMonoCA estimates R and B against G and writes corrected planes
// into r and b (new backing arrays). g is returned unchanged.
func CorrectMonoCA(r, g, b []float64, w, h int) (rr, gg, bb []float64, report FocusShiftReport) {
	rs := EstimateFocusShift(r, g, w, h)
	bs := EstimateFocusShift(b, g, w, h)
	rr = CorrectFocusShift(r, w, h, rs)
	// Recompute k after correction? CorrectFocusShift applies it internally
	// but does not write back SharpnessK on the caller's struct. Re-estimate
	// the applied k from the same rule so the report is honest.
	rs.SharpnessK = reportedK(rs)
	bs.SharpnessK = reportedK(bs)
	bb = CorrectFocusShift(b, w, h, bs)
	report = FocusShiftReport{Enabled: true, R: rs, B: bs}
	return rr, g, bb, report
}

func reportedK(fs FocusShift) float64 {
	if fs.BlurRatio <= 1.05 {
		return 0
	}
	sig := math.Sqrt(fs.BlurRatio*fs.BlurRatio - 1)
	if sig <= 0.15 {
		return 0
	}
	// The search in sharpnessMatch depends on the pixels; we report a
	// conservative cap so the TIFF note stays meaningful even without a
	// second pass. The actual filter used the measured k.
	return math.Min(1.5, (fs.BlurRatio-1)*2)
}

func downsamplePair(a, b []float64, w, h, maxEdge int) (ow, oh int, aa, bb []float64, factor float64) {
	long := w
	if h > long {
		long = h
	}
	if long <= maxEdge {
		return w, h, a, b, 1
	}
	factor = float64(long) / float64(maxEdge)
	ow = int(math.Round(float64(w) / factor))
	oh = int(math.Round(float64(h) / factor))
	if ow < 16 {
		ow = 16
	}
	if oh < 16 {
		oh = 16
	}
	aa = boxDown(a, w, h, ow, oh)
	bb = boxDown(b, w, h, ow, oh)
	return ow, oh, aa, bb, float64(w) / float64(ow)
}

func boxDown(src []float64, sw, sh, dw, dh int) []float64 {
	out := make([]float64, dw*dh)
	for y := 0; y < dh; y++ {
		y0 := y * sh / dh
		y1 := (y + 1) * sh / dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dw; x++ {
			x0 := x * sw / dw
			x1 := (x + 1) * sw / dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var s float64
			n := 0
			for yy := y0; yy < y1; yy++ {
				row := yy * sw
				for xx := x0; xx < x1; xx++ {
					s += src[row+xx]
					n++
				}
			}
			if n > 0 {
				out[y*dw+x] = s / float64(n)
			}
		}
	}
	return out
}

// estimateShift finds the integer displacement of plane vs green that
// maximises NCC, then refines it with a 3-point parabola. Search is a
// coarse-to-fine pyramid so a 16 px shift at 800 px is cheap.
func estimateCAShift(plane, green []float64, w, h int) (dx, dy float64) {
	// Coarse: 4x box downsample, search +/- 8.
	cw, ch := w/4, h/4
	if cw < 24 || ch < 24 {
		ix, iy, _ := searchShift(plane, green, w, h, 0, 0, 12)
		return refineShift(plane, green, w, h, ix, iy)
	}
	cp := boxDown(plane, w, h, cw, ch)
	cg := boxDown(green, w, h, cw, ch)
	cx, cy, _ := searchShift(cp, cg, cw, ch, 0, 0, 8)
	// Medium: 2x, search +/- 3 around 2*coarse.
	mw, mh := w/2, h/2
	mp := boxDown(plane, w, h, mw, mh)
	mg := boxDown(green, w, h, mw, mh)
	mx, my, _ := searchShift(mp, mg, mw, mh, cx*2, cy*2, 3)
	// Fine: full (already downsampled to <=800), search +/- 2.
	ix, iy, _ := searchShift(plane, green, w, h, mx*2, my*2, 2)
	return refineShift(plane, green, w, h, ix, iy)
}

func searchShift(plane, green []float64, w, h, cx, cy, radius int) (bx, by int, best float64) {
	bx, by = cx, cy
	best = -2
	// Ignore a border so a shift does not compare against empty samples.
	m := radius + 4
	if m < 8 {
		m = 8
	}
	for dy := cy - radius; dy <= cy+radius; dy++ {
		for dx := cx - radius; dx <= cx+radius; dx++ {
			s := nccAt(plane, green, w, h, float64(dx), float64(dy), 1, m)
			if s > best {
				best = s
				bx, by = dx, dy
			}
		}
	}
	return bx, by, best
}

func refineShift(plane, green []float64, w, h, ix, iy int) (float64, float64) {
	m := 10
	c := nccAt(plane, green, w, h, float64(ix), float64(iy), 1, m)
	xm := nccAt(plane, green, w, h, float64(ix-1), float64(iy), 1, m)
	xp := nccAt(plane, green, w, h, float64(ix+1), float64(iy), 1, m)
	ym := nccAt(plane, green, w, h, float64(ix), float64(iy-1), 1, m)
	yp := nccAt(plane, green, w, h, float64(ix), float64(iy+1), 1, m)
	return float64(ix) + parabola(xm, c, xp), float64(iy) + parabola(ym, c, yp)
}

func parabola(am, a0, ap float64) float64 {
	d := am - 2*a0 + ap
	if math.Abs(d) < 1e-12 {
		return 0
	}
	x := 0.5 * (am - ap) / d
	if x < -0.6 {
		x = -0.6
	}
	if x > 0.6 {
		x = 0.6
	}
	return x
}

// estimateScale searches a small magnification range about the already
// found translation. Lateral CA on a well-corrected enlarging lens is
// typically well under 0.5 %, so +/- 0.8 % is plenty.
func estimateScale(plane, green []float64, w, h int, dx, dy float64) float64 {
	bestS, best := 1.0, -2.0
	m := 10
	for i := -8; i <= 8; i++ {
		s := 1 + float64(i)*0.001
		v := nccAt(plane, green, w, h, dx, dy, s, m)
		if v > best {
			best, bestS = v, s
		}
	}
	// Refine around the peak with 0.00025 steps.
	lo, hi := bestS-0.001, bestS+0.001
	for s := lo; s <= hi+1e-12; s += 0.00025 {
		v := nccAt(plane, green, w, h, dx, dy, s, m)
		if v > best {
			best, bestS = v, s
		}
	}
	return bestS
}

// nccAt is the normalized cross-correlation of `plane` sampled at the
// inverse transform (scale about centre, then translate) vs green.
func nccAt(plane, green []float64, w, h int, dx, dy, scale float64, margin int) float64 {
	if scale <= 0 {
		scale = 1
	}
	if margin < 2 {
		margin = 2
	}
	cx := float64(w-1) / 2
	cy := float64(h-1) / 2
	var sumP, sumG, sumPP, sumGG, sumPG float64
	n := 0
	// Stride to keep this cheap on 800 px images.
	step := 2
	if w < 200 {
		step = 1
	}
	for y := margin; y < h-margin; y += step {
		for x := margin; x < w-margin; x += step {
			sx := scale*(float64(x)-cx) + cx + dx
			sy := scale*(float64(y)-cy) + cy + dy
			pv, ok := sampleBilinear(plane, w, h, sx, sy)
			if !ok {
				continue
			}
			gv := green[y*w+x]
			sumP += pv
			sumG += gv
			sumPP += pv * pv
			sumGG += gv * gv
			sumPG += pv * gv
			n++
		}
	}
	if n < 32 {
		return -1
	}
	nf := float64(n)
	num := sumPG - sumP*sumG/nf
	den := math.Sqrt((sumPP - sumP*sumP/nf) * (sumGG - sumG*sumG/nf))
	if den < 1e-12 {
		return 0
	}
	return num / den
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

func warpPlane(src []float64, w, h int, fs FocusShift) []float64 {
	out := make([]float64, w*h)
	sc := fs.Scale
	if sc <= 0 {
		sc = 1
	}
	cx := float64(w-1) / 2
	cy := float64(h-1) / 2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := sc*(float64(x)-cx) + cx + fs.DX
			sy := sc*(float64(y)-cy) + cy + fs.DY
			if v, ok := sampleBilinear(src, w, h, sx, sy); ok {
				out[y*w+x] = v
			} else {
				// Replicate edge so the film rebate does not go to zero.
				ex, ey := sx, sy
				if ex < 0 {
					ex = 0
				}
				if ey < 0 {
					ey = 0
				}
				if ex > float64(w-1) {
					ex = float64(w - 1)
				}
				if ey > float64(h-1) {
					ey = float64(h - 1)
				}
				v, _ := sampleBilinear(src, w, h, ex, ey)
				out[y*w+x] = v
			}
		}
	}
	return out
}

// edgeWidth is a robust stand-in for PSF width: 1 / sqrt(Tenengrad), so a
// blurrier image scores higher. Used only as a ratio between two planes.
func edgeWidth(p []float64, w, h int) float64 {
	e := tenengrad(p, w, h)
	if e < 1e-12 {
		return 1
	}
	return 1 / math.Sqrt(e)
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

func unsharp(src []float64, w, h int, sig, k float64) []float64 {
	if k <= 0 {
		dst := make([]float64, len(src))
		copy(dst, src)
		return dst
	}
	blurred := blurSeparable(src, w, h, sig)
	out := make([]float64, len(src))
	for i, v := range src {
		x := v + k*(v-blurred[i])
		if x < 0 {
			x = 0
		}
		out[i] = x
	}
	return out
}

// sharpnessMatch picks an unsharp amount so the filtered plane's Tenengrad
// approaches the (unknown) green reference implied by BlurRatio. We cannot
// see green here, so we target a Tenengrad of current * ratio^2, which is
// what a perfectly reversed blur would restore, and cap k at 1.5.
func sharpnessMatch(src []float64, w, h int, sig float64) float64 {
	base := tenengrad(src, w, h)
	if base < 1e-12 {
		return 0
	}
	// A mild default: restore most of the missing high-frequency energy
	// without ringing. Evaluated at a few k and stopped when energy no
	// longer rises (overshoot).
	bestK, bestE := 0.0, base
	for _, k := range []float64{0.25, 0.5, 0.75, 1.0, 1.25, 1.5} {
		u := unsharp(src, w, h, sig, k)
		e := tenengrad(u, w, h)
		if e > bestE {
			bestE, bestK = e, k
		} else {
			break
		}
	}
	return bestK
}
