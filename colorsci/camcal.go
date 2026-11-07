package colorsci

// Camera (scanner input) calibration from a 24-patch transmission chart
// (v0.84, replaces the crosstalk chart fit in the UI). Port of
// prototype chartcal/fit.py: neutral-preserving 3x3 minimising mean
// CIEDE2000 against an IT8.7/1 (CGATS) reference in XYZ D50.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// CamPatchNames is the chart order: rows A..D, columns 1..6.
var CamPatchNames = func() [24]string {
	var n [24]string
	for i := range n {
		n[i] = fmt.Sprintf("%c%d", 'A'+i/6, i%6+1)
	}
	return n
}()

// CamRef is a reference chart: XYZ (0..100, D50) and Lab per patch.
type CamRef struct {
	XYZ   [24][3]float64
	Lab   [24][3]float64
	Illum string
	Desc  string
}

// ParseCGATS reads an IT8.7/1-style CGATS text with SAMPLE_ID and either
// XYZ_X/Y/Z or LAB_L/A/B (or both) for A1..D6.
func ParseCGATS(r io.Reader) (CamRef, error) {
	var ref CamRef
	var fields []string
	inFmt, inData := false, false
	have := map[string]bool{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		up := strings.ToUpper(ln)
		switch {
		case up == "BEGIN_DATA_FORMAT":
			inFmt, fields = true, nil
			continue
		case up == "END_DATA_FORMAT":
			inFmt = false
			continue
		case up == "BEGIN_DATA":
			inData = true
			continue
		case up == "END_DATA":
			inData = false
			continue
		}
		f := strings.Fields(ln)
		if inFmt {
			for _, x := range f {
				fields = append(fields, strings.ToUpper(x))
			}
			continue
		}
		if !inData {
			if len(f) >= 2 && strings.ToUpper(f[0]) == "ILLUMINATION_NAME" {
				ref.Illum = strings.Trim(f[1], "\"")
			}
			if len(f) >= 2 && strings.ToUpper(f[0]) == "DESCRIPTOR" {
				ref.Desc = strings.Trim(strings.Join(f[1:], " "), "\"")
			}
			continue
		}
		if len(f) != len(fields) {
			continue
		}
		var id string
		val := map[string]float64{}
		for i, k := range fields {
			if k == "SAMPLE_ID" || k == "SAMPLE_NAME" {
				id = strings.ToUpper(f[i])
				continue
			}
			if v, err := strconv.ParseFloat(f[i], 64); err == nil {
				val[k] = v
			}
		}
		idx := -1
		for i, n := range CamPatchNames {
			if n == id {
				idx = i
			}
		}
		if idx < 0 {
			continue
		}
		_, hx := val["XYZ_X"]
		_, hl := val["LAB_L"]
		switch {
		case hx:
			ref.XYZ[idx] = [3]float64{val["XYZ_X"], val["XYZ_Y"], val["XYZ_Z"]}
			ref.Lab[idx] = XYZToLabD50(ref.XYZ[idx])
			if hl {
				ref.Lab[idx] = [3]float64{val["LAB_L"], val["LAB_A"], val["LAB_B"]}
			}
		case hl:
			ref.Lab[idx] = [3]float64{val["LAB_L"], val["LAB_A"], val["LAB_B"]}
			ref.XYZ[idx] = LabToXYZD50(ref.Lab[idx])
		default:
			continue
		}
		have[id] = true
	}
	if len(have) != 24 {
		var miss []string
		for _, n := range CamPatchNames {
			if !have[n] {
				miss = append(miss, n)
			}
		}
		return ref, fmt.Errorf("reference file: %d of 24 patches (A1..D6) with XYZ or Lab found; missing %s", len(have), strings.Join(miss, " "))
	}
	return ref, nil
}

var camW50 = [3]float64{0.96422, 1, 0.82521}

func labF(t float64) float64 {
	if t > 216.0/24389 {
		return math.Cbrt(t)
	}
	return (24389.0/27*t + 16) / 116
}

// XYZToLabD50: XYZ 0..100 (or relative 0..1 when scale=1 handled by caller) -> Lab.
func XYZToLabD50(x [3]float64) [3]float64 { return camLab([3]float64{x[0] / 100, x[1] / 100, x[2] / 100}) }

func camLab(x [3]float64) [3]float64 {
	fx, fy, fz := labF(x[0]/camW50[0]), labF(x[1]/camW50[1]), labF(x[2]/camW50[2])
	return [3]float64{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

// LabToXYZD50 returns XYZ 0..100.
func LabToXYZD50(l [3]float64) [3]float64 {
	fy := (l[0] + 16) / 116
	fx, fz := fy+l[1]/500, fy-l[2]/200
	inv := func(f float64) float64 {
		if f*f*f > 216.0/24389 {
			return f * f * f
		}
		return (116*f - 16) / (24389.0 / 27)
	}
	return [3]float64{inv(fx) * camW50[0] * 100, inv(fy) * 100, inv(fz) * camW50[2] * 100}
}

// XYZD50ToRec709 (relative XYZ, Bradford D50->D65, linear Rec709).
var XYZD50ToRec709, Rec709ToXYZD50 = func() (Mat3, Mat3) {
	b := Mat3{{0.8951, 0.2664, -0.1614}, {-0.7502, 1.7135, 0.0367}, {0.0389, -0.0685, 1.0296}}
	bi, _ := b.Inverse()
	w65 := [3]float64{0.95047, 1, 1.08883}
	s, d := b.Apply(camW50), b.Apply(w65)
	cat := bi.Mul(Mat3{{d[0] / s[0], 0, 0}, {0, d[1] / s[1], 0}, {0, 0, d[2] / s[2]}}).Mul(b)
	x2r := Mat3{{3.2404542, -1.5371385, -0.4985314}, {-0.9692660, 1.8760108, 0.0415560}, {0.0556434, -0.2040259, 1.0572252}}
	f := x2r.Mul(cat)
	inv, _ := f.Inverse()
	return f, inv
}()

// DE2000 is CIEDE2000 between two Lab values.
func DE2000(l1, l2 [3]float64) float64 {
	rad, deg := math.Pi/180, 180/math.Pi
	C1, C2 := math.Hypot(l1[1], l1[2]), math.Hypot(l2[1], l2[2])
	Cm := (C1 + C2) / 2
	G := 0.5 * (1 - math.Sqrt(math.Pow(Cm, 7)/(math.Pow(Cm, 7)+math.Pow(25, 7))))
	a1p, a2p := (1+G)*l1[1], (1+G)*l2[1]
	C1p, C2p := math.Hypot(a1p, l1[2]), math.Hypot(a2p, l2[2])
	hp := func(b, a float64) float64 {
		if a == 0 && b == 0 {
			return 0
		}
		h := math.Atan2(b, a) * deg
		if h < 0 {
			h += 360
		}
		return h
	}
	h1, h2 := hp(l1[2], a1p), hp(l2[2], a2p)
	dL, dC := l2[0]-l1[0], C2p-C1p
	dh := 0.0
	if C1p*C2p != 0 {
		dh = h2 - h1
		if dh > 180 {
			dh -= 360
		} else if dh < -180 {
			dh += 360
		}
	}
	dH := 2 * math.Sqrt(C1p*C2p) * math.Sin(dh/2*rad)
	Lm, Cmp := (l1[0]+l2[0])/2, (C1p+C2p)/2
	hm := h1 + h2
	if C1p*C2p != 0 {
		if math.Abs(h1-h2) <= 180 {
			hm /= 2
		} else if hm < 360 {
			hm = (hm + 360) / 2
		} else {
			hm = (hm - 360) / 2
		}
	}
	T := 1 - 0.17*math.Cos((hm-30)*rad) + 0.24*math.Cos(2*hm*rad) + 0.32*math.Cos((3*hm+6)*rad) - 0.2*math.Cos((4*hm-63)*rad)
	dth := 30 * math.Exp(-math.Pow((hm-275)/25, 2))
	RC := 2 * math.Sqrt(math.Pow(Cmp, 7)/(math.Pow(Cmp, 7)+math.Pow(25, 7)))
	SL := 1 + 0.015*(Lm-50)*(Lm-50)/math.Sqrt(20+(Lm-50)*(Lm-50))
	SC, SH := 1+0.045*Cmp, 1+0.015*Cmp*T
	RT := -math.Sin(2*dth*rad) * RC
	return math.Sqrt(math.Pow(dL/SL, 2) + math.Pow(dC/SC, 2) + math.Pow(dH/SH, 2) + RT*(dC/SC)*(dH/SH))
}

// ---------- detection and sampling ----------

// CamClipLevel is the clip threshold (fraction of full scale) and
// CamClipFrac the pixel fraction above it that marks a patch clipped.
const (
	CamClipLevel = 0.98
	CamClipFrac  = 0.01
)

// CamPatch is one patch reading (linear 0..1).
type CamPatch struct {
	X, Y, Half float64
	Med        [3]float64
	ClipFrac   [3]float64
	Clipped    bool
}

// CamCorners are the centres of A1, A6, D6, D1 (same convention as
// ChartCorners: patch 1, 6, 24, 19 in row-major 6x4 order).
type CamCorners = ChartCorners

// DetectCamChart finds the 4x6 patch grid in a linear 16-bit RGB image and
// orients it so row A is the neutral ramp, A1 its darkest patch.
func DetectCamChart(pix []uint16, w, h int) (CamCorners, string, error) {
	const s = 4
	pw, ph := w/s, h/s
	mx := make([]float64, pw*ph)
	vals := make([]float64, 0, pw*ph/16)
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			j := ((y*s)*w + x*s) * 3
			m := math.Max(float64(pix[j]), math.Max(float64(pix[j+1]), float64(pix[j+2])))
			mx[y*pw+x] = m
			if (x+y)%4 == 0 {
				vals = append(vals, m)
			}
		}
	}
	sort.Float64s(vals)
	thr := math.Max(vals[len(vals)*60/100]*0.3, 300)
	// erode once (3x3) to cut thin bridges, then label 4-connected
	mask := make([]bool, pw*ph)
	for y := 1; y < ph-1; y++ {
		for x := 1; x < pw-1; x++ {
			ok := true
			for dy := -1; dy <= 1 && ok; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if mx[(y+dy)*pw+x+dx] <= thr {
						ok = false
						break
					}
				}
			}
			mask[y*pw+x] = ok
		}
	}
	lab := make([]int32, pw*ph)
	type blob struct{ x0, y0, x1, y1, n int }
	var blobs []blob
	stack := []int{}
	for i := range mask {
		if !mask[i] || lab[i] != 0 {
			continue
		}
		b := blob{x0: pw, y0: ph, x1: -1, y1: -1}
		id := int32(len(blobs) + 1)
		lab[i] = id
		stack = append(stack[:0], i)
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			px, py := p%pw, p/pw
			b.n++
			b.x0, b.x1, b.y0, b.y1 = min(b.x0, px), max(b.x1, px), min(b.y0, py), max(b.y1, py)
			for _, q := range [4]int{p - 1, p + 1, p - pw, p + pw} {
				if q >= 0 && q < len(mask) && mask[q] && lab[q] == 0 && !(q == p-1 && px == 0) && !(q == p+1 && px == pw-1) {
					lab[q] = id
					stack = append(stack, q)
				}
			}
		}
		blobs = append(blobs, b)
	}
	// square-ish blobs, filled, of the common size
	type cand struct{ x, y, size float64 }
	var cs []cand
	for _, b := range blobs {
		bw, bh := float64(b.x1-b.x0+1), float64(b.y1-b.y0+1)
		if bw < 20 || bh < 20 || bw/bh > 1.3 || bh/bw > 1.3 || float64(b.n) < 0.75*bw*bh {
			continue
		}
		cs = append(cs, cand{(float64(b.x0+b.x1)/2 + 0.5) * s, (float64(b.y0+b.y1)/2 + 0.5) * s, math.Sqrt(bw*bh) * s})
	}
	if len(cs) < 12 {
		return CamCorners{}, "", fmt.Errorf("found only %d patch-like areas", len(cs))
	}
	sz := make([]float64, len(cs))
	for i, c := range cs {
		sz[i] = c.size
	}
	sort.Float64s(sz)
	med := sz[len(sz)/2]
	var keep []cand
	for _, c := range cs {
		if c.size > 0.7*med && c.size < 1.3*med {
			keep = append(keep, c)
		}
	}
	// pitch: median nearest-neighbour distance
	var nn []float64
	for i, a := range keep {
		best := math.Inf(1)
		for j, b := range keep {
			if i != j {
				best = math.Min(best, math.Hypot(a.x-b.x, a.y-b.y))
			}
		}
		nn = append(nn, best)
	}
	sort.Float64s(nn)
	pitch := nn[len(nn)/2]
	minx, miny := math.Inf(1), math.Inf(1)
	for _, c := range keep {
		minx, miny = math.Min(minx, c.x), math.Min(miny, c.y)
	}
	// least squares x = x0 + i*ax + j*bx, y = y0 + i*ay + j*by
	var ata [3][3]float64
	var atx, aty [3]float64
	nC, nR := 0, 0
	for _, c := range keep {
		i, j := math.Round((c.x-minx)/pitch), math.Round((c.y-miny)/pitch)
		nC, nR = max(nC, int(i)+1), max(nR, int(j)+1)
		v := [3]float64{1, i, j}
		for a := 0; a < 3; a++ {
			for b := 0; b < 3; b++ {
				ata[a][b] += v[a] * v[b]
			}
			atx[a] += v[a] * c.x
			aty[a] += v[a] * c.y
		}
	}
	if !((nC == 6 && nR == 4) || (nC == 4 && nR == 6)) {
		return CamCorners{}, "", fmt.Errorf("patch grid is %dx%d, expected 6x4", nC, nR)
	}
	inv, err := Mat3(ata).Inverse()
	if err != nil {
		return CamCorners{}, "", err
	}
	px, py := inv.Apply(atx), inv.Apply(aty)
	at := func(i, j int) [2]float64 {
		return [2]float64{px[0] + float64(i)*px[1] + float64(j)*px[2], py[0] + float64(i)*py[1] + float64(j)*py[2]}
	}
	// candidate A rows: the two long edges, each in both directions
	type orient struct {
		c    CamCorners
		name string
	}
	var cands []orient
	if nC == 6 {
		cands = []orient{
			{CamCorners{at(0, 0), at(5, 0), at(5, 3), at(0, 3)}, "upright"},
			{CamCorners{at(5, 3), at(0, 3), at(0, 0), at(5, 0)}, "rotated 180"},
			{CamCorners{at(5, 0), at(0, 0), at(0, 3), at(5, 3)}, "mirrored"},
			{CamCorners{at(0, 3), at(5, 3), at(5, 0), at(0, 0)}, "mirrored + 180"},
		}
	} else {
		cands = []orient{
			{CamCorners{at(3, 0), at(3, 5), at(0, 5), at(0, 0)}, "rotated 90 cw"},
			{CamCorners{at(0, 5), at(0, 0), at(3, 0), at(3, 5)}, "rotated 90 ccw"},
			{CamCorners{at(0, 0), at(0, 5), at(3, 5), at(3, 0)}, "rotated 90 + mirrored"},
			{CamCorners{at(3, 5), at(3, 0), at(0, 0), at(0, 5)}, "rotated 270 + mirrored"},
		}
	}
	best, bs := 0, math.Inf(1)
	for k, o := range cands {
		ps := SampleCamChart(pix, w, h, o.c, 0.15)
		sc := rampScore(ps)
		if sc < bs {
			best, bs = k, sc
		}
	}
	return cands[best].c, cands[best].name, nil
}

// rampScore: low when A1..A6 rise in brightness with steady channel
// ratios (a neutral ramp under any fixed light balance).
func rampScore(ps [24]CamPatch) float64 {
	sc := 0.0
	var lr, lb []float64
	for i := 0; i < 6; i++ {
		m := ps[i].Med
		if i > 0 && m[0]+m[1]+m[2] <= ps[i-1].Med[0]+ps[i-1].Med[1]+ps[i-1].Med[2] {
			sc += 10
		}
		if i >= 1 && !ps[i].Clipped && m[0] > 1e-4 && m[1] > 1e-4 && m[2] > 1e-4 {
			lr = append(lr, math.Log(m[0]/m[1]))
			lb = append(lb, math.Log(m[2]/m[1]))
		}
	}
	sd := func(v []float64) float64 {
		if len(v) < 2 {
			return 1
		}
		mu := 0.0
		for _, x := range v {
			mu += x
		}
		mu /= float64(len(v))
		s := 0.0
		for _, x := range v {
			s += (x - mu) * (x - mu)
		}
		return math.Sqrt(s / float64(len(v)))
	}
	return sc + sd(lr) + sd(lb)
}

// SampleCamChart reads each patch: per-channel median of a square of half
// size frac*pitch (0.25 = central 50%), and the fraction of pixels at or
// above CamClipLevel.
func SampleCamChart(pix []uint16, w, h int, c CamCorners, frac float64) [24]CamPatch {
	var out [24]CamPatch
	r := c.PatchHalf(frac)
	lim := uint16(math.Ceil(CamClipLevel * 65535))
	for i := 0; i < 24; i++ {
		cx, cy := c.PatchCentre(i)
		x0, x1 := max(0, int(cx-r)), min(w, int(cx+r))
		y0, y1 := max(0, int(cy-r)), min(h, int(cy+r))
		p := CamPatch{X: cx, Y: cy, Half: r}
		n := (x1 - x0) * (y1 - y0)
		if n <= 0 {
			p.Clipped = true
			out[i] = p
			continue
		}
		var hist [3][]int32
		var clip [3]int
		for k := range hist {
			hist[k] = make([]int32, 65536)
		}
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				j := (y*w + x) * 3
				for k := 0; k < 3; k++ {
					v := pix[j+k]
					hist[k][v]++
					if v >= lim {
						clip[k]++
					}
				}
			}
		}
		for k := 0; k < 3; k++ {
			acc, half := 0, (n+1)/2
			for v, c := range hist[k] {
				acc += int(c)
				if acc >= half {
					p.Med[k] = float64(v) / 65535
					break
				}
			}
			p.ClipFrac[k] = float64(clip[k]) / float64(n)
			if p.ClipFrac[k] > CamClipFrac {
				p.Clipped = true
			}
		}
		out[i] = p
	}
	return out
}

// ---------- fit ----------

// CamFit is the result of FitCamChart.
type CamFit struct {
	Use       [24]bool
	Full      Mat3       // camera -> linear Rec709 (incl. white balance and exposure)
	Mx        Mat3       // camera -> camera, keeps the camera grey (deliverable)
	Neutral   Mat3       // camera -> camera, grey -> equal RGB, Rec709 luma kept
	Grey      [3]float64 // camera grey direction (A2..A5), normalised to max 1
	DEBefore  [24]float64
	DEAfter   [24]float64
	MeanB     float64
	MaxB      float64
	MeanA     float64
	MaxA      float64
	BaseMul   [3]float64 // Mx row sums: an equal-RGB film base is multiplied by these
	Used      int
	Clipped   []string
	GainPatch string
	ClipEV    float64 // stops to reduce exposure so the clipped patches fit (0 if none)
}

func camDE(m Mat3, cam [3]float64, refLab [3]float64) float64 {
	o := m.Apply(cam)
	for k := range o {
		o[k] = math.Max(o[k], 1e-6)
	}
	return DE2000(refLab, camLab(Rec709ToXYZD50.Apply(o)))
}

// FitCamChart fits the neutral-preserving 3x3 (6 off-diagonal terms of Mx
// plus an exposure scale; the white balance is the fixed diag(w/g)).
func FitCamChart(ps [24]CamPatch, ref CamRef) (CamFit, error) {
	var f CamFit
	var tgt [24][3]float64
	for i := range ref.XYZ {
		tgt[i] = XYZD50ToRec709.Apply([3]float64{ref.XYZ[i][0] / 100, ref.XYZ[i][1] / 100, ref.XYZ[i][2] / 100})
	}
	for i, p := range ps {
		f.Use[i] = !p.Clipped && p.Med[0] > 0 && p.Med[1] > 0 && p.Med[2] > 0
		if f.Use[i] {
			f.Used++
		} else {
			f.Clipped = append(f.Clipped, CamPatchNames[i])
		}
	}
	// grey directions over the clean A2..A5 (LSQ through the origin vs target sum)
	var g, wv [3]float64
	den, nn := 0.0, 0
	for i := 1; i <= 4; i++ {
		if !f.Use[i] {
			continue
		}
		t := tgt[i][0] + tgt[i][1] + tgt[i][2]
		for k := 0; k < 3; k++ {
			g[k] += ps[i].Med[k] * t
			wv[k] += tgt[i][k] * t
		}
		den += t * t
		nn++
	}
	if f.Used < 12 || nn < 2 {
		return f, fmt.Errorf("only %d clean patches (%d clean greys A2..A5); need at least 12 incl. 2 greys - reduce exposure", f.Used, nn)
	}
	for k := 0; k < 3; k++ {
		g[k] /= den
		wv[k] /= den
	}
	// before: per-channel gain on the brightest clean grey (A5 normally)
	gi := -1
	for i := 5; i >= 1; i-- {
		if f.Use[i] {
			gi = i
			break
		}
	}
	f.GainPatch = CamPatchNames[gi]
	var m0 Mat3
	for k := 0; k < 3; k++ {
		m0[k][k] = tgt[gi][k] / ps[gi].Med[k]
	}
	wb := Mat3{{wv[0] / g[0], 0, 0}, {0, wv[1] / g[1], 0}, {0, 0, wv[2] / g[2]}}
	build := func(p []float64) (Mat3, Mat3) {
		mx := Mat3{{0, p[0], p[1]}, {p[2], 0, p[3]}, {p[4], p[5], 0}}
		for i := 0; i < 3; i++ {
			s := 0.0
			for j := 0; j < 3; j++ {
				if j != i {
					s += mx[i][j] * g[j]
				}
			}
			mx[i][i] = (g[i] - s) / g[i]
		}
		sc := math.Exp(p[6])
		full := wb.Mul(mx)
		for i := range full {
			for j := range full[i] {
				full[i][j] *= sc
			}
		}
		return full, mx
	}
	obj := func(p []float64) float64 {
		full, _ := build(p)
		s := 0.0
		for i := range ps {
			if f.Use[i] {
				s += camDE(full, ps[i].Med, ref.Lab[i])
			}
		}
		return s / float64(f.Used)
	}
	p := nelderMead(obj, make([]float64, 7), 0.05, 4000)
	for r := 0; r < 4; r++ { // restarts
		p = nelderMead(obj, p, 0.02, 4000)
	}
	f.Full, f.Mx = build(p)
	gm := f.Mx.Apply(g)
	_ = gm
	mxg := g // Mx keeps g
	y := 0.2126*g[0] + 0.7152*g[1] + 0.0722*g[2]
	f.Neutral = Mat3{{y / mxg[0], 0, 0}, {0, y / mxg[1], 0}, {0, 0, y / mxg[2]}}.Mul(f.Mx)
	mxv := math.Max(g[0], math.Max(g[1], g[2]))
	f.Grey = [3]float64{g[0] / mxv, g[1] / mxv, g[2] / mxv}
	for i := 0; i < 3; i++ {
		f.BaseMul[i] = f.Mx[i][0] + f.Mx[i][1] + f.Mx[i][2]
	}
	for i := range ps {
		f.DEBefore[i] = camDE(m0, ps[i].Med, ref.Lab[i])
		f.DEAfter[i] = camDE(f.Full, ps[i].Med, ref.Lab[i])
		if f.Use[i] {
			f.MeanB += f.DEBefore[i]
			f.MeanA += f.DEAfter[i]
			f.MaxB = math.Max(f.MaxB, f.DEBefore[i])
			f.MaxA = math.Max(f.MaxA, f.DEAfter[i])
		}
	}
	f.MeanB /= float64(f.Used)
	f.MeanA /= float64(f.Used)
	f.ClipEV = ClipStops(ps, g)
	return f, nil
}

// ClipStops estimates how many stops less exposure keep every patch below
// 0.9 FS: clipped channels are projected from the patch's clean channels
// with the grey ratios (a coarse guess, at least 1/3 stop).
func ClipStops(ps [24]CamPatch, g [3]float64) float64 {
	worst := 0.0
	for _, p := range ps {
		if !p.Clipped {
			continue
		}
		est := 0.0
		for k := 0; k < 3; k++ {
			if p.ClipFrac[k] <= CamClipFrac {
				continue
			}
			v := 1.0
			for j := 0; j < 3; j++ { // project from a clean channel
				if p.ClipFrac[j] <= CamClipFrac && p.Med[j] > 0 {
					v = math.Max(v, p.Med[j]*g[k]/g[j])
				}
			}
			est = math.Max(est, v)
		}
		worst = math.Max(worst, math.Log2(est/0.9))
	}
	if worst == 0 {
		return 0
	}
	return math.Max(math.Ceil(worst*3)/3, 1.0/3)
}

// nelderMead minimises f from x0 (simple, adequate for 7 parameters).
func nelderMead(f func([]float64) float64, x0 []float64, step float64, iters int) []float64 {
	n := len(x0)
	pts := make([][]float64, n+1)
	val := make([]float64, n+1)
	for i := range pts {
		pts[i] = append([]float64(nil), x0...)
		if i > 0 {
			pts[i][i-1] += step
		}
		val[i] = f(pts[i])
	}
	for it := 0; it < iters; it++ {
		idx := make([]int, n+1)
		for i := range idx {
			idx[i] = i
		}
		sort.Slice(idx, func(a, b int) bool { return val[idx[a]] < val[idx[b]] })
		np, nv := make([][]float64, n+1), make([]float64, n+1)
		for i, j := range idx {
			np[i], nv[i] = pts[j], val[j]
		}
		pts, val = np, nv
		if math.Abs(val[n]-val[0]) < 1e-10 {
			break
		}
		c := make([]float64, n)
		for i := 0; i < n; i++ {
			for k := 0; k < n; k++ {
				c[k] += pts[i][k] / float64(n)
			}
		}
		at := func(t float64) []float64 {
			x := make([]float64, n)
			for k := range x {
				x[k] = c[k] + t*(pts[n][k]-c[k])
			}
			return x
		}
		xr := at(-1)
		fr := f(xr)
		switch {
		case fr < val[0]:
			xe := at(-2)
			if fe := f(xe); fe < fr {
				pts[n], val[n] = xe, fe
			} else {
				pts[n], val[n] = xr, fr
			}
		case fr < val[n-1]:
			pts[n], val[n] = xr, fr
		default:
			xc := at(0.5)
			if fr < val[n] {
				xc = at(-0.5)
			}
			if fc := f(xc); fc < math.Min(fr, val[n]) {
				pts[n], val[n] = xc, fc
			} else {
				for i := 1; i <= n; i++ {
					for k := range pts[i] {
						pts[i][k] = pts[0][k] + 0.5*(pts[i][k]-pts[0][k])
					}
					val[i] = f(pts[i])
				}
			}
		}
	}
	best := 0
	for i := range val {
		if val[i] < val[best] {
			best = i
		}
	}
	return pts[best]
}

// ApplyCam applies m blended from identity by strength (0..1).
func ApplyCam(m Mat3, strength float64, v [3]float64) [3]float64 {
	o := m.Apply(v)
	for k := range o {
		o[k] = v[k] + strength*(o[k]-v[k])
	}
	return o
}

// CamDCTL renders the standalone DCTL (same behaviour as
// prototype chartcal/CameraCal_Trichrome.dctl).
func CamDCTL(name, info string, mx, mn Mat3) string {
	var b strings.Builder
	b.WriteString("// CameraCal_Trichrome.dctl - camera + light input profile \"" + name + "\"\n")
	b.WriteString("// Written by NegativeScan Studio (Chart calibration). " + info + "\n")
	b.WriteString("// Apply on LINEAR scan data, BEFORE FilmScan_Density_In. Re-measure the film base after inserting it.\n")
	b.WriteString("// Default matrix keeps the camera grey unchanged (M * grey = grey): it only fixes cross-channel response.\n")
	b.WriteString("// \"Neutralise grey\" instead maps the chart grey to equal RGB (Rec709 luma kept) - a baked white balance.\n\n")
	b.WriteString("DEFINE_UI_PARAMS(cal_on, Enable camera calibration, DCTLUI_CHECK_BOX, 1)\n")
	b.WriteString("DEFINE_UI_PARAMS(strength, Strength, DCTLUI_SLIDER_FLOAT, 1.0, 0.0, 1.0, 0.01)\n")
	b.WriteString("DEFINE_UI_PARAMS(neutralise, Neutralise grey - bake white balance, DCTLUI_CHECK_BOX, 0)\n\n")
	b.WriteString("__DEVICE__ float3 transform(int p_Width, int p_Height, int p_X, int p_Y, float p_R, float p_G, float p_B)\n{\n")
	b.WriteString("    if (cal_on == 0)\n    {\n        return make_float3(p_R, p_G, p_B);\n    }\n")
	for _, s := range []struct {
		p string
		m Mat3
	}{{"a", mx}, {"n", mn}} {
		for i := 0; i < 3; i++ {
			for j := 0; j < 3; j++ {
				fmt.Fprintf(&b, "    const float %s%d%d = %.6ff;\n", s.p, i, j, s.m[i][j])
			}
		}
	}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			fmt.Fprintf(&b, "    float m%d%d = (neutralise != 0) ? n%d%d : a%d%d;\n", i, j, i, j, i, j)
		}
	}
	b.WriteString("    float r = m00 * p_R + m01 * p_G + m02 * p_B;\n    float g = m10 * p_R + m11 * p_G + m12 * p_B;\n    float b = m20 * p_R + m21 * p_G + m22 * p_B;\n")
	b.WriteString("    float k = _fminf(_fmaxf(strength, 0.0f), 1.0f);\n    r = p_R + k * (r - p_R);\n    g = p_G + k * (g - p_G);\n    b = p_B + k * (b - p_B);\n")
	b.WriteString("    return make_float3(r, g, b);\n}\n")
	return b.String()
}

// ErrNoCamChart is returned when no grid could be found.
var ErrNoCamChart = errors.New("colorsci: chart grid not found")
