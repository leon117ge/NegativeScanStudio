// Package hdrscan meters the clear film base and merges a frame-0 / +2 EV pair.
package hdrscan

import (
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"basetool/rawconv"

	"golang.org/x/image/tiff"
)

// Plane is a linear 16-bit RGB image (RGBRGB...).
type Plane struct {
	W, H        int
	Pix         []uint16
	Make, Model string
	Source      string
	free        func()
}

// Free releases LibRaw memory (no-op for TIFF input).
func (p *Plane) Free() {
	if p != nil && p.free != nil {
		p.free()
		p.free = nil
		p.Pix = nil
	}
}

// Load reads a RAW (linear LibRaw path, same as the converter) or a 16-bit RGB TIFF.
func Load(path string) (*Plane, error) {
	if rawconv.IsRaw(path) {
		im, err := rawconv.Decode(path)
		if err != nil {
			return nil, err
		}
		return &Plane{W: im.Width, H: im.Height, Pix: im.Pix, Make: im.Make, Model: im.Model, Source: path, free: im.Free}, nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".tif" && ext != ".tiff" {
		return nil, fmt.Errorf("%s: not a RAW or TIFF file", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := tiff.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	b := img.Bounds()
	p := &Plane{W: b.Dx(), H: b.Dy(), Source: path}
	p.Pix = make([]uint16, p.W*p.H*3)
	switch m := img.(type) {
	case *image.RGBA64:
		for y := 0; y < p.H; y++ {
			row := m.Pix[y*m.Stride:]
			for x := 0; x < p.W; x++ {
				i, o := x*8, (y*p.W+x)*3
				p.Pix[o] = uint16(row[i])<<8 | uint16(row[i+1])
				p.Pix[o+1] = uint16(row[i+2])<<8 | uint16(row[i+3])
				p.Pix[o+2] = uint16(row[i+4])<<8 | uint16(row[i+5])
			}
		}
	case *image.NRGBA64:
		for y := 0; y < p.H; y++ {
			row := m.Pix[y*m.Stride:]
			for x := 0; x < p.W; x++ {
				i, o := x*8, (y*p.W+x)*3
				p.Pix[o] = uint16(row[i])<<8 | uint16(row[i+1])
				p.Pix[o+1] = uint16(row[i+2])<<8 | uint16(row[i+3])
				p.Pix[o+2] = uint16(row[i+4])<<8 | uint16(row[i+5])
			}
		}
	default:
		return nil, fmt.Errorf("%s: only 16-bit RGB TIFFs can be merged (got %T)", filepath.Base(path), img)
	}
	return p, nil
}

// Align modes.
const (
	AlignOff     = 0
	AlignCheck   = 1
	AlignCorrect = 2
)

// Options control the merge.
type Options struct {
	ClipT  float64 // clip threshold on the +2 frame, fraction of full scale (0.85)
	Ramp   float64 // blend ramp width as a fraction of T (0.25)
	Dilate int     // clip mask dilation in pixels (4)
	Align  int     // AlignOff / AlignCheck / AlignCorrect
	Ratio  float64 // 0 = measure (fixed +2 ratio, tests only)
	// Exposure is each frame's exposure (shutter seconds × ISO); nil or
	// zeros = unknown. Used as the fallback ratio and for the EV labels.
	Exposure []float64
	// BaseLevel is frame 0's clear-base level (brightest channel, 16-bit):
	// pixels within ~1 EV of it keep frame 0. 0 = detect, <0 = off.
	BaseLevel  float64
	MinSamples int // ratio samples needed (0 = 2000)
	// Method selects the blend: MergeClassic (0) or MergeWeighted (1).
	// DefaultOptions sets MergeWeighted (v0.74); a zero Options is Classic.
	Method int
}

// DefaultOptions are the spec values.
func DefaultOptions() Options {
	return Options{ClipT: 0.85, Ramp: 0.25, Dilate: 4, Align: AlignCheck, Method: MergeWeighted}
}

// Stats describe one merge step (one brighter frame).
type Stats struct {
	Label        string     // "+2", "+3", "+4" (empty = "+2")
	Ratio        float64    // used ratio (this frame / frame 0 scale)
	EV           float64    // exposure step from frame 0
	Nominal      float64    // exposure ratio to frame 0 from shutter/ISO (0 = unknown)
	Step         float64    // measured ratio to the next darker frame (0 = not measured)
	Method       string     // how Ratio was obtained
	Warning      string     // fallback / left-out note
	Dropped      bool       // frame not used
	ProtectPct   float64    // pixels kept (fully or partly) as frame 0 near the base level
	RatioRGB     [3]float64 // per-channel medians
	Samples      int
	ClippedPct   float64 // pixels with any channel >= T in +2
	MaskPct      float64 // after dilation
	BlendPct     float64 // pixels inside the ramp (0<w<1)
	FromPlus2Pct float64 // pixels taken fully from +2 (w=1)
	Shift        image.Point
	ShiftGain    float64 // relative SSD improvement of best shift over (0,0)
	ShiftApplied bool
	Elapsed      time.Duration
	Detail       string // Weighted merge notes (empty for Classic)
}

func (s Stats) String() string {
	al := "alignment not checked"
	if s.ShiftGain >= 0 {
		al = fmt.Sprintf("alignment shift (%d,%d)", s.Shift.X, s.Shift.Y)
		if s.Shift == (image.Point{}) {
			al = "alignment OK (0,0)"
		} else if s.ShiftApplied {
			al += " corrected"
		} else {
			al += " NOT corrected"
		}
	}
	l := s.Label
	if l == "" {
		l = "+2"
	}
	if s.Dropped {
		return fmt.Sprintf("%s: NOT USED - %s; clipped in %s: %.2f%%", l, s.Warning, l, s.ClippedPct)
	}
	nm := ""
	if s.Nominal > 0 {
		nm = fmt.Sprintf(", exposure ratio %.4f", s.Nominal)
	}
	ch := ""
	if s.Step > 0 {
		ch = fmt.Sprintf(" (per channel R %.4f G %.4f B %.4f)", s.RatioRGB[0], s.RatioRGB[1], s.RatioRGB[2])
	}
	wn := ""
	if s.Warning != "" {
		wn = "; WARNING: " + s.Warning
	}
	dt := ""
	if s.Detail != "" {
		dt = "; " + s.Detail
	}
	return fmt.Sprintf("%s: ratio %.4f [%s]%s%s; clipped in %s: %.2f%%, mask after dilation: %.2f%%, blended: %.2f%%, from %s: %.2f%%, kept as frame 0 near the base level: %.2f%%; %s%s%s",
		l, s.Ratio, s.Method, ch, nm, l, s.ClippedPct, s.MaskPct, s.BlendPct, l, s.FromPlus2Pct, s.ProtectPct, al, wn, dt)
}

func parallelRows(h int, fn func(y0, y1 int)) {
	n := runtime.NumCPU()
	if n > 16 {
		n = 16
	}
	var wg sync.WaitGroup
	step := (h + n - 1) / n
	for y0 := 0; y0 < h; y0 += step {
		y1 := y0 + step
		if y1 > h {
			y1 = h
		}
		wg.Add(1)
		go func(a, b int) { defer wg.Done(); fn(a, b) }(y0, y1)
	}
	wg.Wait()
}

func max3(a, b, c uint16) uint16 {
	if b > a {
		a = b
	}
	if c > a {
		a = c
	}
	return a
}

// dilate grows mask by r pixels (square structuring element, separable).
func dilate(m []uint8, w, h, r int) []uint8 {
	if r <= 0 {
		return m
	}
	tmp := make([]uint8, len(m))
	parallelRows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := m[y*w : (y+1)*w]
			out := tmp[y*w : (y+1)*w]
			last := -1 << 30
			for x := 0; x < w; x++ { // forward: distance to last set pixel
				if row[x] != 0 {
					last = x
				}
				if x-last <= r {
					out[x] = 1
				}
			}
			next := 1 << 30
			for x := w - 1; x >= 0; x-- {
				if row[x] != 0 {
					next = x
				}
				if next-x <= r {
					out[x] = 1
				}
			}
		}
	})
	res := make([]uint8, len(m))
	parallelRows(w, func(x0, x1 int) {
		for x := x0; x < x1; x++ {
			last := -1 << 30
			for y := 0; y < h; y++ {
				if tmp[y*w+x] != 0 {
					last = y
				}
				if y-last <= r {
					res[y*w+x] = 1
				}
			}
			next := 1 << 30
			for y := h - 1; y >= 0; y-- {
				if tmp[y*w+x] != 0 {
					next = y
				}
				if next-y <= r {
					res[y*w+x] = 1
				}
			}
		}
	})
	return res
}

// estimateShift searches ±r px on a central green crop (f2 scaled by 1/ratio vs f0).
func estimateShift(f0, f2 *Plane, ratio, T float64, r int) (image.Point, float64) {
	w, h := f0.W, f0.H
	cw, ch := 1024, 1024
	if cw > w-4*r {
		cw = w - 4*r
	}
	if ch > h-4*r {
		ch = h - 4*r
	}
	if cw < 64 || ch < 64 {
		return image.Point{}, -1
	}
	x0, y0 := (w-cw)/2, (h-ch)/2
	lim := uint16(T * 65535)
	ssd := func(dx, dy int) float64 {
		var s float64
		n := 0
		for y := y0; y < y0+ch; y += 2 {
			for x := x0; x < x0+cw; x += 2 {
				i := (y*w + x) * 3
				j := ((y+dy)*w + x + dx) * 3
				if max3(f2.Pix[j], f2.Pix[j+1], f2.Pix[j+2]) >= lim {
					continue
				}
				d := float64(f2.Pix[j+1])/ratio - float64(f0.Pix[i+1])
				s += d * d
				n++
			}
		}
		if n == 0 {
			return math.Inf(1)
		}
		return s / float64(n)
	}
	type res struct {
		p image.Point
		v float64
	}
	ch2 := make(chan res)
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			go func(dx, dy int) { ch2 <- res{image.Pt(dx, dy), ssd(dx, dy)} }(dx, dy)
		}
	}
	best := res{v: math.Inf(1)}
	var zero float64
	for k := 0; k < (2*r+1)*(2*r+1); k++ {
		x := <-ch2
		if x.p == (image.Point{}) {
			zero = x.v
		}
		if x.v < best.v || (x.v == best.v && x.p == image.Point{}) {
			best = x
		}
	}
	if zero <= 0 || math.IsInf(zero, 1) {
		return image.Point{}, 0
	}
	gain := (zero - best.v) / zero
	// Ignore insignificant "improvements" (noise).
	if gain < 0.02 {
		return image.Point{}, gain
	}
	return best.p, gain
}

// Merge combines frame 0 (f0) and the +2 EV frame (f2) in frame-0 scale.
func Merge(f0, f2 *Plane, o Options) ([]uint16, Stats, error) {
	out, sts, err := MergeN([]*Plane{f0, f2}, o)
	if err != nil || len(sts) == 0 {
		return out, Stats{}, err
	}
	return out, sts[0], nil
}

// EVLabel formats an exposure step: "+2", "+3", "+2.6".
func EVLabel(ev float64) string {
	if r := math.Round(ev); math.Abs(ev-r) < 0.2 {
		return fmt.Sprintf("+%d", int(r))
	}
	return fmt.Sprintf("%+.1f", ev)
}

// nominalRatios turns per-frame exposures (seconds × ISO) into ratios to
// frame 0. nil when unknown or not strictly increasing.
func nominalRatios(e []float64, n int) []float64 {
	if len(e) != n || e[0] <= 0 {
		return nil
	}
	r := make([]float64, n)
	for k := range e {
		if e[k] <= 0 {
			return nil
		}
		r[k] = e[k] / e[0]
		if k > 0 && r[k] < r[k-1]*1.2 {
			return nil
		}
	}
	return r
}

// clipMask marks pixels with any channel >= T, dilated by o.Dilate.
func clipMask(f *Plane, lim uint16, dil int) (mask []uint8, clipped, masked int64) {
	w, h := f.W, f.H
	clip := make([]uint8, w*h)
	var mu sync.Mutex
	parallelRows(h, func(y0, y1 int) {
		c := 0
		for i := y0 * w; i < y1*w; i++ {
			if max3(f.Pix[3*i], f.Pix[3*i+1], f.Pix[3*i+2]) >= lim {
				clip[i] = 1
				c++
			}
		}
		mu.Lock()
		clipped += int64(c)
		mu.Unlock()
	})
	mask = dilate(clip, w, h, dil)
	for _, v := range mask {
		masked += int64(v)
	}
	return
}

// measureStep measures the exposure ratio of next over prev (the next
// darker frame): per-channel medians of next/prev over pixels that are
// unclipped in next (outside its dilated clip mask) and above the noise
// floor (1 % of full scale) in prev. The
// result is the median of the three channel medians.
func measureStep(prev, next *Plane, mask []uint8, T float64) (ratio float64, rgb [3]float64, n int) {
	np := prev.W * prev.H
	stride := 1
	for np/stride > 4_000_000 {
		stride++
	}
	const floor = 0.01 * 65535
	var rs [3][]float32
	for i := 0; i < np; i += stride {
		if mask[i] != 0 { // clipped in next, or within Dilate px of a clipped pixel
			continue
		}
		for c := 0; c < 3; c++ {
			a, b := float64(prev.Pix[3*i+c]), float64(next.Pix[3*i+c])
			if a >= floor {
				rs[c] = append(rs[c], float32(b/a))
			}
		}
	}
	var meds []float64
	for c := 0; c < 3; c++ {
		n += len(rs[c])
		if len(rs[c]) >= 200 {
			sort.Slice(rs[c], func(i, j int) bool { return rs[c][i] < rs[c][j] })
			rgb[c] = float64(rs[c][len(rs[c])/2])
			meds = append(meds, rgb[c])
		}
	}
	switch len(meds) {
	case 0:
		return 0, rgb, n
	case 1:
		return meds[0], rgb, n
	case 2:
		return (meds[0] + meds[1]) / 2, rgb, n
	}
	sort.Float64s(meds)
	return meds[1], rgb, n
}

// baseLevel returns the brightest-channel median of frame 0's clear base
// (0 if none is found).
func baseLevel(f0 *Plane) float64 {
	r, ok := FindBasePlane(f0)
	if !ok || r.Empty() {
		return 0
	}
	step := 1
	for r.Dx()*r.Dy()/(step*step) > 400000 {
		step++
	}
	var v []float64
	for y := r.Min.Y; y < r.Max.Y; y += step {
		for x := r.Min.X; x < r.Max.X; x += step {
			i := (y*f0.W + x) * 3
			v = append(v, float64(max3(f0.Pix[i], f0.Pix[i+1], f0.Pix[i+2])))
		}
	}
	if len(v) < 50 {
		return 0
	}
	sort.Float64s(v)
	return v[len(v)/2]
}

// MergeN merges frame 0 and brighter frames (+2, +3, +4 EV ...) in frame-0
// scale. It never fails on a ratio:
//
//  1. Each frame's ratio to the next darker frame is measured on pixels
//     unclipped in the brighter frame and above the noise floor in the
//     darker one, and chained: ratio(+4/0) = ratio(+4/+2) × ratio(+2/0).
//  2. If that fails (mostly clipped frame, e.g. pure film base) the ratio
//     comes from the exposure times (o.Exposure), corrected by
//     measured/nominal of the +2 frame.
//  3. With no exposure information a +2 frame is assumed to be exactly 4;
//     a further frame is left out with a warning.
//
// The frames are blended from dark to bright: frame 0 supplies the
// highlights (the clear base), +2 the mid-dense areas and +4 the densest;
// each blend band is driven by the brighter frame's own (dilated) clip
// mask. Pixels within about 1 EV of frame 0's base level always keep frame
// 0's exact values, so the base in the HDR equals frame 0.
func MergeN(frames []*Plane, o Options) ([]uint16, []Stats, error) {
	if len(frames) < 2 {
		return nil, nil, errors.New("need frame 0 and at least one brighter frame")
	}
	f0 := frames[0]
	for k, f := range frames[1:] {
		if f.W != f0.W || f.H != f0.H {
			return nil, nil, fmt.Errorf("frame %d size differs: %dx%d vs %dx%d", k+1, f.W, f.H, f0.W, f0.H)
		}
	}
	if o.ClipT <= 0 || o.ClipT > 1 {
		o.ClipT = 0.85
	}
	if o.Ramp <= 0 || o.Ramp >= 1 {
		o.Ramp = 0.25
	}
	minS := o.MinSamples
	if minS <= 0 {
		minS = 2000
	}
	T := o.ClipT * 65535
	lim := uint16(T)
	n := len(frames)
	nom := nominalRatios(o.Exposure, n)

	type step struct {
		k     int
		ratio float64
		mask  []uint8
	}
	var steps []step
	sts := make([]Stats, 0, n-1)
	corr, corrFrom := 1.0, ""
	lastIdx, lastRatio, lastLabel := 0, 1.0, "0"
	for k := 1; k < n; k++ {
		t0 := time.Now()
		st := Stats{ShiftGain: -1}
		if nom != nil {
			st.Nominal = nom[k]
			st.EV = math.Log2(nom[k])
		}
		mask, clipped, masked := clipMask(frames[k], lim, o.Dilate)
		np := float64(f0.W * f0.H)
		st.ClippedPct, st.MaskPct = 100*float64(clipped)/np, 100*float64(masked)/np
		r, rgb, ns := measureStep(frames[lastIdx], frames[k], mask, T)
		st.RatioRGB, st.Samples = rgb, ns
		stepNom := 0.0
		if nom != nil {
			stepNom = nom[k] / nom[lastIdx]
		}
		guess := stepNom
		if guess == 0 {
			guess = math.Pow(4, float64(k-len(steps)))
		}
		ok, why := true, ""
		switch {
		case ns < minS || r <= 0:
			ok, why = false, fmt.Sprintf("only %d usable unclipped samples (need %d) - the frame is mostly clipped", ns, minS)
		case stepNom > 0 && (r < 0.6*stepNom || r > 1.6*stepNom):
			ok, why = false, fmt.Sprintf("measured %.3f is far from the exposure ratio %.3f", r, stepNom)
		case stepNom == 0 && (r < 1.5 || r > 40):
			ok, why = false, fmt.Sprintf("measured %.3f is implausible", r)
		}
		label, setCorr := "", false
		if st.EV == 0 && ok {
			st.EV = math.Log2(lastRatio * r)
		}
		var ratio float64
		switch {
		case o.Ratio > 0:
			ratio = o.Ratio * guess / 4 * lastRatio
			st.Method = fmt.Sprintf("fixed ratio %.4f", ratio)
		case ok:
			st.Step = r
			ratio = lastRatio * r
			if lastIdx == 0 {
				st.Method = fmt.Sprintf("measured against frame 0 (%d samples)", ns)
			} else {
				st.Method = fmt.Sprintf("chained: %.4f measured against %s (%d samples) x %.4f (%s/0)", r, lastLabel, ns, lastRatio, lastLabel)
			}
			if lastIdx == 0 && stepNom > 0 {
				corr, setCorr = r/stepNom, true
			}
		case nom != nil:
			ratio = nom[k] * corr
			if corrFrom != "" {
				st.Method = fmt.Sprintf("exposure-time fallback: nominal %.4f x %.4f (measured/nominal of %s)", nom[k], corr, corrFrom)
			} else {
				st.Method = fmt.Sprintf("exposure-time fallback: nominal %.4f", nom[k])
			}
			st.Warning = "ratio not measurable (" + why + "), used the exposure times"
		case len(steps) == 0:
			ratio = 4
			st.EV = 2
			st.Method = "assumed 4.0000 (no exposure information)"
			st.Warning = "ratio not measurable (" + why + ") and no exposure information - assumed exactly 4 (+2 EV)"
		default:
			if st.EV == 0 {
				st.EV = math.Log2(lastRatio * guess)
			}
			st.Label = EVLabel(st.EV)
			st.Dropped = true
			st.Method = "left out"
			st.Warning = fmt.Sprintf("frame %s left out: ratio not measurable (%s) and no exposure information", st.Label, why)
			st.Elapsed = time.Since(t0)
			sts = append(sts, st)
			continue
		}
		if st.EV == 0 {
			st.EV = math.Log2(ratio)
		}
		label = EVLabel(st.EV)
		st.Label = label
		if setCorr {
			corrFrom = label
		}
		st.Ratio = ratio
		st.Elapsed = time.Since(t0)
		sts = append(sts, st)
		steps = append(steps, step{k, ratio, mask})
		lastIdx, lastRatio, lastLabel = k, ratio, label
	}
	if len(steps) == 0 {
		return nil, sts, errors.New("no brighter frame could be used")
	}

	// Frame-0 base protection.
	L := o.BaseLevel
	if L == 0 {
		L = baseLevel(f0)
	}
	pLo, pHi := 0.35*L, 0.50*L

	if o.Method == MergeWeighted {
		ws := make([]wStep, 0, len(steps))
		si := 0
		for _, s := range steps {
			for sts[si].Dropped {
				si++
			}
			ws = append(ws, wStep{k: s.k, ratio: s.ratio, st: &sts[si]})
			si++
		}
		out, err := mergeWeighted(frames, ws, o, T, L, pLo, pHi)
		return out, sts, err
	}

	cur := f0.Pix
	si := 0
	for _, s := range steps {
		for sts[si].Dropped {
			si++
		}
		st := &sts[si]
		si++
		t0 := time.Now()
		fk := frames[s.k]
		var sh image.Point
		if o.Align != AlignOff {
			sh, st.ShiftGain = estimateShift(&Plane{W: f0.W, H: f0.H, Pix: cur}, fk, s.ratio, o.ClipT, 4)
			st.Shift = sh
			if o.Align == AlignCorrect && sh != (image.Point{}) {
				st.ShiftApplied = true
			} else {
				sh = image.Point{}
			}
		}
		var b, f, pr int64
		cur, b, f, pr = blendStep(cur, f0.Pix, fk, s.mask, s.ratio, T, o.Ramp, sh, pLo, pHi, L > 0)
		np := float64(f0.W * f0.H)
		st.BlendPct, st.FromPlus2Pct, st.ProtectPct = 100*float64(b)/np, 100*float64(f)/np, 100*float64(pr)/np
		st.Elapsed += time.Since(t0)
	}
	return cur, sts, nil
}

// blendStep blends the brighter frame fk (ratio to frame 0) into cur.
func blendStep(cur, f0 []uint16, fk *Plane, mask []uint8, ratio, T, ramp float64, sh image.Point, pLo, pHi float64, protect bool) (out []uint16, blend, full, prot int64) {
	w, h := fk.W, fk.H
	out = make([]uint16, w*h*3)
	inv := 1 / ratio
	rampLo := T - ramp*T
	var mu sync.Mutex
	parallelRows(h, func(y0, y1 int) {
		var b, f, p int64
		for y := y0; y < y1; y++ {
			sy := y + sh.Y
			if sy < 0 {
				sy = 0
			} else if sy >= h {
				sy = h - 1
			}
			for x := 0; x < w; x++ {
				i := y*w + x
				sx := x + sh.X
				if sx < 0 {
					sx = 0
				} else if sx >= w {
					sx = w - 1
				}
				j := sy*w + sx
				if mask[j] != 0 { // clipped (dilated) in fk: keep the darker data
					copy(out[3*i:3*i+3], cur[3*i:3*i+3])
					continue
				}
				m2 := float64(max3(fk.Pix[3*j], fk.Pix[3*j+1], fk.Pix[3*j+2]))
				wt := 1.0
				if m2 >= T {
					wt = 0
				} else if m2 > rampLo {
					wt = (T - m2) / (T - rampLo)
				}
				if protect && wt > 0 {
					m0 := float64(max3(f0[3*i], f0[3*i+1], f0[3*i+2]))
					if m0 >= pHi {
						wt = 0
						p++
					} else if m0 > pLo {
						wt *= (pHi - m0) / (pHi - pLo)
						p++
					}
				}
				if wt <= 0 {
					copy(out[3*i:3*i+3], cur[3*i:3*i+3])
					continue
				}
				if wt < 1 {
					b++
				} else {
					f++
				}
				for c := 0; c < 3; c++ {
					a := float64(cur[3*i+c])
					v := a + wt*(float64(fk.Pix[3*j+c])*inv-a)
					if v > 65535 {
						v = 65535
					}
					out[3*i+c] = uint16(v + 0.5)
				}
			}
		}
		mu.Lock()
		blend += b
		full += f
		prot += p
		mu.Unlock()
	})
	return
}

// HDRPath returns <dir>/<frame0 name without extension>_HDR.tiff.
func HDRPath(dir, frame0 string) string {
	b := filepath.Base(frame0)
	b = strings.TrimSuffix(b, filepath.Ext(b))
	b = strings.TrimSuffix(b, filepath.Ext(b)) // DSCF0001.RAF.tiff -> DSCF0001
	b = strings.TrimSuffix(b, "_0EV")          // Roll_20261030_172436_0001_0EV -> ..._0001
	return filepath.Join(dir, b+"_HDR.tiff")
}

// RegionMean returns the mean RGB of a pixel rectangle.
func RegionMean(pix []uint16, w int, r image.Rectangle) [3]float64 {
	var s [3]float64
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := (y*w + x) * 3
			for c := 0; c < 3; c++ {
				s[c] += float64(pix[i+c])
			}
			n++
		}
	}
	if n > 0 {
		for c := range s {
			s[c] /= float64(n)
		}
	}
	return s
}

// SameRegion reports how many pixels in r differ between a and b.
func SameRegion(a, b []uint16, w int, r image.Rectangle) (diff, total int) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := (y*w + x) * 3
			total++
			if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] {
				diff++
			}
		}
	}
	return
}
