package cropper

import (
	"fmt"
	"math"
	"sort"
)

// Side identifies one of the four frame edges.
const (
	SideLeft = iota
	SideRight
	SideTop
	SideBottom
)

var sideNames = [4]string{"left", "right", "top", "bottom"}

// Edge is a detected boundary between picture and film base / holder.
type Edge struct {
	Side     int
	P0, P1   [2]float64 // two points on the line, full-res source coordinates (pixel corners at integers)
	Tilt     float64    // degrees, y-down math sense (positive = clockwise on screen)
	Support  float64    // fraction of the edge length with edge pixels on the line
	Uniform  float64    // smoothness of the strip just outside the line
	Contrast float64    // median |outside - inside|
	Score    float64
}

// Detection is the auto-analysis result for one image.
type Detection struct {
	OK         bool
	Edges      []Edge
	Angle      float64 // correction to apply, degrees, positive = counter-clockwise
	Confidence float64 // 0..1
	Message    string
}

// Tuning (proxy-pixel units unless stated).
const (
	bandFrac      = 0.22 // search this fraction of the image from each side
	maxTiltDeg    = 5.0
	tiltStepDeg   = 0.1
	edgeThresh    = 0.02
	minSupport    = 0.25
	minUniform    = 0.70
	minContrast   = 0.05
	inlierTol     = 1.5
	sampleOffsetA = 3
	sampleOffsetB = 5
)

type pt struct{ t, u float32 }

type detector struct {
	p      *Proxy
	s      []float32 // smoothed RGB
	gx, gy []float32 // per-channel gradients
}

func gauss1(src []float32, w, h int) []float32 {
	k := [5]float32{0.0545, 0.2442, 0.4026, 0.2442, 0.0545}
	tmp := make([]float32, len(src))
	out := make([]float32, len(src))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				var a float32
				for i := -2; i <= 2; i++ {
					xx := min(max(x+i, 0), w-1)
					a += k[i+2] * src[(y*w+xx)*3+c]
				}
				tmp[(y*w+x)*3+c] = a
			}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				var a float32
				for i := -2; i <= 2; i++ {
					yy := min(max(y+i, 0), h-1)
					a += k[i+2] * tmp[(yy*w+x)*3+c]
				}
				out[(y*w+x)*3+c] = a
			}
		}
	}
	return out
}

func newDetector(p *Proxy) *detector {
	d := &detector{p: p, s: gauss1(p.Pix, p.W, p.H)}
	w, h := p.W, p.H
	d.gx = make([]float32, len(d.s))
	d.gy = make([]float32, len(d.s))
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			for c := 0; c < 3; c++ {
				i := (y*w+x)*3 + c
				d.gx[i] = 0.5 * (d.s[i+3] - d.s[i-3])
				d.gy[i] = 0.5 * (d.s[i+w*3] - d.s[i-w*3])
			}
		}
	}
	return d
}

// geometry of a side: scan index t along the edge, depth u from the border
func (d *detector) sideDims(side int) (lt, du int) {
	if side <= SideRight {
		return d.p.H, d.p.W
	}
	return d.p.W, d.p.H
}

func (d *detector) xy(side, t, u int) (int, int) {
	switch side {
	case SideLeft:
		return u, t
	case SideRight:
		return d.p.W - 1 - u, t
	case SideTop:
		return t, u
	}
	return t, d.p.H - 1 - u
}

// grad returns |gradient along u| (best channel) and the matching |along t|.
func (d *detector) grad(side, t, u int) (float32, float32) {
	x, y := d.xy(side, t, u)
	i := (y*d.p.W + x) * 3
	var best, bt float32
	for c := 0; c < 3; c++ {
		gu, gt := d.gx[i+c], d.gy[i+c]
		if side >= SideTop {
			gu, gt = gt, gu
		}
		if gu < 0 {
			gu = -gu
		}
		if gt < 0 {
			gt = -gt
		}
		if gu > best {
			best, bt = gu, gt
		}
	}
	return best, bt
}

func (d *detector) color(side int, t int, u float64) ([3]float32, bool) {
	lt, du := d.sideDims(side)
	ui := int(math.Round(u))
	if ui < 0 || ui >= du || t < 0 || t >= lt {
		return [3]float32{}, false
	}
	x, y := d.xy(side, t, ui)
	i := (y*d.p.W + x) * 3
	return [3]float32{d.s[i], d.s[i+1], d.s[i+2]}, true
}

func maxDiff(a, b [3]float32) float32 {
	m := float32(0)
	for c := 0; c < 3; c++ {
		v := a[c] - b[c]
		if v < 0 {
			v = -v
		}
		if v > m {
			m = v
		}
	}
	return m
}

type cand struct {
	a, b                     float64 // u = a + b*(t - tc)
	support, uni, con, uniIn float64
	smooth, zone             float64
	medOut, medIn            [3]float32
	inl                      int
}

func (d *detector) side(side int) []cand {
	lt, du := d.sideDims(side)
	ub := int(float64(du) * bandFrac)
	tc := float64(lt) / 2
	// edge points
	var pts []pt
	mags := make([]float32, ub+2)
	for t := 2; t < lt-2; t++ {
		for u := 1; u <= ub+1 && u < du-1; u++ {
			m, mt := d.grad(side, t, u)
			if m < edgeThresh || mt*1.5 > m {
				m = 0
			}
			mags[u] = m
		}
		for u := 2; u <= ub && u < du-2; u++ {
			m := mags[u]
			if m == 0 || m < mags[u-1] || m <= mags[u+1] {
				continue
			}
			// sub-pixel peak
			den := mags[u-1] - 2*m + mags[u+1]
			off := float32(0)
			if den < 0 {
				off = 0.5 * (mags[u-1] - mags[u+1]) / den
			}
			pts = append(pts, pt{float32(t), float32(u) + off})
		}
	}
	if len(pts) == 0 {
		return nil
	}
	// Hough over tilt x offset
	nth := int(2*maxTiltDeg/tiltStepDeg) + 1
	slack := int(math.Ceil(float64(lt)/2*math.Tan(maxTiltDeg*math.Pi/180))) + 2
	no := ub + 2*slack + 1
	acc := make([]int32, nth*no)
	tans := make([]float64, nth)
	for i := range tans {
		tans[i] = math.Tan((-maxTiltDeg + float64(i)*tiltStepDeg) * math.Pi / 180)
	}
	for _, p := range pts {
		dt := float64(p.t) - tc
		for i, tn := range tans {
			o := int(math.Round(float64(p.u)-dt*tn)) + slack
			if o >= 0 && o < no {
				acc[i*no+o]++
			}
		}
	}
	prof := make([]int32, no)
	pth := make([]int, no)
	for o := 0; o < no; o++ {
		for i := 0; i < nth; i++ {
			if v := acc[i*no+o]; v > prof[o] {
				prof[o], pth[o] = v, i
			}
		}
	}
	type peak struct {
		o int
		v int32
	}
	var peaks []peak
	minVotes := int32(float64(lt) * minSupport * 0.6)
	for o := 0; o < no; o++ {
		v := prof[o]
		if v < minVotes {
			continue
		}
		isMax := true
		for k := max(0, o-3); k <= min(no-1, o+3); k++ {
			if prof[k] > v || (prof[k] == v && k < o) {
				isMax = false
				break
			}
		}
		if isMax {
			peaks = append(peaks, peak{o, v})
		}
	}
	sort.Slice(peaks, func(i, j int) bool { return peaks[i].v > peaks[j].v })
	if len(peaks) > 10 {
		peaks = peaks[:10]
	}
	// bucket points by scanline for inlier search
	byT := make([][]float32, lt)
	for _, p := range pts {
		byT[int(p.t)] = append(byT[int(p.t)], p.u)
	}
	var out []cand
	for _, pk := range peaks {
		a := float64(pk.o - slack)
		b := tans[pth[pk.o]]
		var ts, us []float64
		for iter := 0; iter < 3; iter++ {
			tol := inlierTol
			if iter > 0 {
				tol = 1.0
			}
			ts, us = ts[:0], us[:0]
			for t := 0; t < lt; t++ {
				pred := a + b*(float64(t)-tc)
				bestD := tol
				bu := math.NaN()
				for _, u := range byT[t] {
					if dd := math.Abs(float64(u) - pred); dd <= bestD {
						bestD, bu = dd, float64(u)
					}
				}
				if !math.IsNaN(bu) {
					ts = append(ts, float64(t)-tc)
					us = append(us, bu)
				}
			}
			if len(ts) < 10 {
				break
			}
			a, b = lsq(ts, us)
		}
		if len(ts) < 10 || math.Abs(math.Atan(b))*180/math.Pi > maxTiltDeg+0.5 {
			continue
		}
		c := cand{a: a, b: b, inl: len(ts), support: float64(len(ts)) / float64(lt-4)}
		// strips outside / inside along the inliers
		step := max(1, len(ts)/300)
		var outs, ins [][3]float32
		var cons []float64
		for k := 0; k < len(ts); k += step {
			t := int(ts[k] + tc)
			u := a + b*ts[k]
			oc, ok1 := d.color(side, t, u-sampleOffsetA)
			oc2, ok2 := d.color(side, t, u-sampleOffsetB)
			ic, ok3 := d.color(side, t, u+sampleOffsetA)
			if !ok3 || (!ok1 && !ok2) {
				continue
			}
			if !ok1 {
				oc = oc2
			}
			outs = append(outs, oc)
			ins = append(ins, ic)
			cons = append(cons, float64(maxDiff(oc, ic)))
			_ = oc2
		}
		if len(outs) < 5 || a < 2 {
			continue
		}
		smooth := 0
		for k := 1; k < len(outs); k++ {
			if maxDiff(outs[k], outs[k-1]) < 0.05 {
				smooth++
			}
		}
		sort.Float64s(cons)
		c.smooth = float64(smooth) / float64(len(outs)-1)
		c.medOut, c.uni = globalUni(outs, 0.08)
		c.medIn, c.uniIn = globalUni(ins, 0.08)
		c.con = cons[len(cons)/2]
		// the whole strip between the image border and the line must be
		// made of flat zones (holder, rebate), not picture content
		depthEnd := a - sampleOffsetA
		nd := 0
		zsum := 0.0
		for k := 0; k < 6 && depthEnd > 1; k++ {
			du := 1 + (depthEnd-1)*float64(k)/5
			var zs [][3]float32
			for j := 0; j < len(ts); j += step {
				t := int(ts[j] + tc)
				if col, ok := d.color(side, t, du+b*ts[j]*float64(k)/5*0); ok {
					zs = append(zs, col)
				}
			}
			if len(zs) > 5 {
				_, u := globalUni(zs, 0.10)
				zsum += u
				nd++
			}
		}
		c.zone = c.uni
		if nd > 0 {
			c.zone = math.Min(c.uni, zsum/float64(nd))
		}
		out = append(out, c)
	}
	return out
}

// globalUni returns the per-channel median and the fraction of samples
// within tol of it.
func globalUni(cs [][3]float32, tol float32) ([3]float32, float64) {
	var med [3]float32
	if len(cs) == 0 {
		return med, 0
	}
	v := make([]float32, len(cs))
	for c := 0; c < 3; c++ {
		for i, x := range cs {
			v[i] = x[c]
		}
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		med[c] = v[len(v)/2]
	}
	n := 0
	for _, x := range cs {
		if maxDiff(x, med) <= tol {
			n++
		}
	}
	return med, float64(n) / float64(len(cs))
}

func (c cand) accepted() bool {
	if c.support < minSupport || c.con < minContrast {
		return false
	}
	if !(c.uni >= minUniform || (c.smooth >= 0.97 && c.uni >= 0.4)) {
		return false
	}
	return c.zone >= 0.6
}

// MinConfidence below which the detection is discarded.
const MinConfidence = 0.3

func weightedMedian(es []Edge, p *Proxy) float64 {
	type wv struct{ v, w float64 }
	var a []wv
	tot := 0.0
	for _, e := range es {
		lt := float64(p.H)
		if e.Side >= SideTop {
			lt = float64(p.W)
		}
		a = append(a, wv{e.Tilt, e.Score * lt})
		tot += e.Score * lt
	}
	sort.Slice(a, func(i, j int) bool { return a[i].v < a[j].v })
	acc := 0.0
	for i, x := range a {
		acc += x.w
		if acc >= tot/2 {
			if acc == tot/2 && i+1 < len(a) {
				return (x.v + a[i+1].v) / 2
			}
			return x.v
		}
	}
	return a[len(a)-1].v
}

func lsq(x, y []float64) (a, b float64) {
	var sx, sy, sxx, sxy float64
	n := float64(len(x))
	for i := range x {
		sx += x[i]
		sy += y[i]
		sxx += x[i] * x[i]
		sxy += x[i] * y[i]
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return sy / n, 0
	}
	b = (n*sxy - sx*sy) / den
	a = (sy - b*sx) / n
	return
}

// toEdge converts a side candidate to full-res source coordinates.
func (d *detector) toEdge(side int, c cand) Edge {
	lt, _ := d.sideDims(side)
	tc := float64(lt) / 2
	conv := func(t float64) [2]float64 {
		u := c.a + c.b*(t-tc)
		var x, y float64
		switch side {
		case SideLeft:
			x, y = u, t
		case SideRight:
			x, y = float64(d.p.W-1)-u, t
		case SideTop:
			x, y = t, u
		default:
			x, y = t, float64(d.p.H-1)-u
		}
		// pixel centres are at i+0.5 in continuous coordinates
		return [2]float64{(x + 0.5) * d.p.Scale, (y + 0.5) * d.p.Scale}
	}
	e := Edge{Side: side, P0: conv(0), P1: conv(float64(lt - 1)), Support: c.support, Uniform: c.uni, Contrast: c.con}
	dx, dy := e.P1[0]-e.P0[0], e.P1[1]-e.P0[1]
	if side <= SideRight {
		e.Tilt = math.Atan2(-dx, dy) * 180 / math.Pi
	} else {
		e.Tilt = math.Atan2(dy, dx) * 180 / math.Pi
	}
	e.Score = math.Min(1, c.support/0.6) * c.uni
	return e
}

// Detect finds the picture edges in a proxy and estimates the skew.
func Detect(p *Proxy) Detection {
	d := newDetector(p)
	var det Detection
	for side := 0; side < 4; side++ {
		cs := d.side(side)
		_, du := d.sideDims(side)
		var acc []cand
		for _, c := range cs {
			if c.accepted() {
				acc = append(acc, c)
			}
		}
		if len(acc) == 0 {
			continue
		}
		// holder -> rebate -> picture: step inwards while the next line's
		// outside matches the current line's (flat) inside
		sort.Slice(acc, func(i, j int) bool { return acc[i].a < acc[j].a })
		pick := acc[0]
		for _, c := range acc[1:] {
			if c.a-pick.a > 0.12*float64(du) || c.a-pick.a < 2 {
				continue
			}
			if pick.uniIn >= 0.6 && maxDiff(pick.medIn, c.medOut) <= 0.12 {
				pick = c
			}
		}
		det.Edges = append(det.Edges, d.toEdge(side, pick))
	}
	if len(det.Edges) == 0 {
		det.Message = "No film border found - manual mode"
		return det
	}
	// weighted angle, rejecting an edge that disagrees strongly
	angle := func(ex int) (float64, float64) {
		var sw, sa float64
		for i, e := range det.Edges {
			if i == ex {
				continue
			}
			lt := p.H
			if e.Side >= SideTop {
				lt = p.W
			}
			w := e.Score * float64(lt)
			sw += w
			sa += w * e.Tilt
		}
		if sw == 0 {
			return 0, 0
		}
		return sa / sw, sw
	}
	tilt, _ := angle(-1)
	spread := 0.0
	for _, e := range det.Edges {
		spread = math.Max(spread, math.Abs(e.Tilt-tilt))
	}
	if spread > 0.5 && len(det.Edges) >= 3 {
		// drop the worst outlier
		worst, wd := -1, 0.0
		for i, e := range det.Edges {
			if dd := math.Abs(e.Tilt - tilt); dd > wd {
				worst, wd = i, dd
			}
		}
		det.Edges = append(det.Edges[:worst], det.Edges[worst+1:]...)
		tilt, _ = angle(-1)
		spread = 0
		for _, e := range det.Edges {
			spread = math.Max(spread, math.Abs(e.Tilt-tilt))
		}
	}
	if len(det.Edges) >= 3 {
		tilt = weightedMedian(det.Edges, p)
	}
	det.Angle = tilt
	conf := 0.0
	for _, e := range det.Edges {
		conf += e.Score
	}
	conf /= 4
	if len(det.Edges) >= 2 {
		conf = math.Min(1, conf*1.15)
	}
	if spread > 0.3 {
		conf *= math.Max(0.3, 1-(spread-0.3))
	}
	det.Confidence = conf
	if conf < MinConfidence {
		det.Message = fmt.Sprintf("Low confidence (%.0f%%) - manual mode", conf*100)
		det.Edges = nil
		det.Angle = 0
		return det
	}
	if math.Abs(tilt) > 3 && (len(det.Edges) < 3 || conf < 0.6) {
		det.Message = fmt.Sprintf("Implausible angle %.1f° - manual mode", tilt)
		det.Edges = nil
		det.Angle = 0
		return det
	}
	det.OK = true
	names := ""
	for _, e := range det.Edges {
		if names != "" {
			names += ", "
		}
		names += sideNames[e.Side]
	}
	det.Message = fmt.Sprintf("Edges: %s", names)
	return det
}
