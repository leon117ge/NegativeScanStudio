// Software dust removal on the merged HDR negative (v0.65).
//
// There is no infrared channel, so dust is found from the image alone. On a
// backlit negative a speck of dust or a hair blocks part of the light: it
// is a small or thin spot darker than its surroundings. Real dust is often
// NOT opaque (v0.63 required that and so missed most of it): measured on a
// real scan, specks and hairs sit 0.5 to 2.7 EV below the background in R
// and G, which is 10 to 60 times the film grain. The steps:
//
//  1. L = log2(R+G+B): density-like, so thresholds mean the same in thin
//     and dense parts of the negative.
//  2. Black top-hat T = closing(L) - L with a square of radius R: keeps
//     dark spots and dark lines (hairs) narrower than 2R+1 px, in EV below
//     the local background.
//  3. Noise: per 128-px tile the median and robust sigma (MAD) of T, which
//     film grain sets. Z = (T - median) / sigma. Seeds Z >= Seed grow over
//     Z >= Low (hysteresis, 8-connected), so a hair is one spot.
//  4. Each spot is judged against its neighbourhood, not a global level:
//     - depth: peak T at least MinDepth EV;
//     - outlier: peak T / 99.5th percentile of T in the 129-px window
//     around it (the spot left out). Foliage, windows and lettering
//     have many dark details of similar depth (ratio < 2.3); dust stands
//     out (ratio 3 to 20);
//     - colour: the per-channel log drop. R and G must drop about equally
//     (dust is grey or brown) and B at least 0.8 x as much as G. The
//     colour distance cd between the spot's drop ratios and the colour
//     of the local texture (regression of the R and B high-pass on G in
//     the window) tells dust from real detail even in busy areas;
//     - shape: compact (box <= MaxSpot) or a thin line (hair).
//     A spot is dust if one of the (outlier >= o, cd >= c) rules holds
//     (hairs have their own, stricter rules); a compact spot deeper than
//     DeepEV needs only outlier >= 1. Spots with lighter pixels right next
//     to the dark strokes (lettering, grilles) are left alone.
//     Two scales: radius R finds specks and hairs; a closing of R cannot
//     see into a dense disc wider than 2R+1, so a second pass at R2 adds
//     deep compact discs only (absorbing the R spots they overlap).
//  5. Never changed: the film base region, the base level in the rebate
//     band along the frame edge, faint specks on the ragged frame line
//     next to the rebate, and spots next to the opaque holder.
//  6. Each spot (grown by a rim) is filled per channel from a nearby clean
//     patch of the same image (keeps the grain), corrected by a smooth
//     membrane so its edges match the surroundings (a healing brush).
//
// Off never calls this code, so the HDR is unchanged.
package hdrscan

import (
	"image"
	"math"
	"runtime"
	"slices"
	"sort"
	"sync"
	"time"
)

// Dust levels.
const (
	DustOff    = 0
	DustLight  = 1
	DustStrong = 2
)

// DustName is "off", "light" or "strong".
func DustName(l int) string {
	switch l {
	case DustLight:
		return "light"
	case DustStrong:
		return "strong"
	}
	return "off"
}

type dustRule struct{ Out, CD float64 }

type dustParams struct {
	R        int     // top-hat radius (px)
	R2       int     // large top-hat radius (px)
	Seed     float64 // seed threshold (robust sigmas of T)
	Low      float64 // grow threshold (robust sigmas of T)
	MinDepth float64 // peak EV below the background, at least
	MaxSpot  int     // largest bounding-box side of a compact spot (px)
	MaxLen   int     // longest hair (px)
	MaxThick float64 // thickest hair (px, area/length)
	RGLo     float64 // R drop / G drop range
	RGHi     float64
	BGMin    float64 // B drop / G drop, at least
	Rules    []dustRule
	// thin lines: edges of windows, railings and mouldings also give
	// line-shaped top-hat responses, so a hair must stand out more
	HairRules []dustRule
	// a compact spot this deep (EV) is accepted when it stands out at all
	// (out >= 1): neighbouring dust raises the window percentile, and no
	// real detail that deep was seen to be compact and neutral
	DeepEV float64
	Win    int // context window radius (px)
	Rim    int // px added around each spot before filling
	BaseEV float64
}

func dustParamsFor(level, w int) dustParams {
	s := float64(w) / 6240 // X-T3 = 1
	if s < 0.25 {
		s = 0.25
	}
	ri := func(v float64) int { return int(math.Max(1, math.Round(v*s))) }
	p := dustParams{R: ri(4), R2: ri(10), Seed: 10, Low: 4, MinDepth: 0.40, MaxSpot: ri(48), MaxLen: ri(300), MaxThick: math.Max(2, 6*s),
		RGLo: 0.70, RGHi: 1.45, BGMin: 0.80,
		Rules:     []dustRule{{3.2, 0}, {2.6, 0.2}, {1.1, 0.4}},
		HairRules: []dustRule{{3.0, 0}, {2.6, 0.7}},
		DeepEV:    2.0,
		Win:       ri(64), Rim: 1, BaseEV: 0.30}
	if level == DustStrong {
		p.Seed, p.MinDepth = 7, 0.30
		p.MaxSpot, p.MaxLen = ri(64), ri(400)
		p.Rules = []dustRule{{2.6, 0}, {2.0, 0.15}, {1.0, 0.3}}
		p.HairRules = []dustRule{{2.6, 0}, {2.2, 0.6}}
		p.DeepEV = 1.6
		p.Rim = ri(2)
	}
	return p
}

// DustStats reports what Despeckle did.
type DustStats struct {
	Level     int
	Spots     int        // spots filled
	Hairs     int        // of which thin and long
	Fibres    int        // thin curved fibres healed by the fibre stage (v0.79)
	Pixels    int64      // pixels changed
	Rejected  int        // candidate spots left alone
	Protected int        // of which on or next to the film base / holder
	Opaque    int        // filled spots at least 1.5 EV deep
	Floor     [3]float64 // genuine density floor (16-bit, per channel)
	Elapsed   time.Duration
}

// DustSpot describes one candidate (for tests and tuning).
type DustSpot struct {
	X0, Y0, X1, Y1 int
	Px             int
	Z, Peak        float64 // peak in sigmas and in EV
	Drop           [3]float64
	KR, KB         float64 // local texture colour slopes (R/G, B/G)
	CD, Out        float64
	Len, Thick     float64
	Accepted, Hair bool
	Large          bool // found by the large-scale pass
	Why            string
	Bright         float64 // G: lighter-than-surround excursion next to the spot (EV)
	// fibre stage (v0.79): Fibre marks its candidates; Light = lighter than
	// the surround; Peak = depth (EV), FW = width at half depth (px),
	// Ratio = depth / side asymmetry, Out = isolation, CD = depth in grain
	// sigmas, Straight = skeleton minor/major spread
	Fibre, Light bool
	FW, Ratio    float64
	Straight     float64
}

func parallelN(n int, fn func(a, b int)) {
	k := runtime.NumCPU()
	if k > 16 {
		k = 16
	}
	var wg sync.WaitGroup
	step := (n + k - 1) / k
	for a := 0; a < n; a += step {
		b := a + step
		if b > n {
			b = n
		}
		wg.Add(1)
		go func(a, b int) { defer wg.Done(); fn(a, b) }(a, b)
	}
	wg.Wait()
}

// runMinMax writes the max (or min) of src over a window of radius r along
// a line of n samples with stride st, into dst (van Herk / Gil-Werman).
func runMinMax(src, dst []float32, off, n, st, r int, isMax bool, g, h []float32) {
	k := 2*r + 1
	better := func(a, b float32) float32 {
		if isMax == (a > b) {
			return a
		}
		return b
	}
	pad := math.Inf(-1)
	if !isMax {
		pad = math.Inf(1)
	}
	m := n + 2*r
	m = (m + k - 1) / k * k
	at := func(i int) float32 {
		j := i - r
		if j < 0 || j >= n {
			return float32(pad)
		}
		return src[off+j*st]
	}
	for b := 0; b < m; b += k {
		g[b] = at(b)
		for i := b + 1; i < b+k; i++ {
			g[i] = better(g[i-1], at(i))
		}
		h[b+k-1] = at(b + k - 1)
		for i := b + k - 2; i >= b; i-- {
			h[i] = better(h[i+1], at(i))
		}
	}
	for x := 0; x < n; x++ {
		// window [x, x+2r] in padded coordinates
		dst[off+x*st] = better(h[x], g[x+2*r])
	}
}

// morph applies a square max (dilate) or min (erode) of radius r.
func morph(src []float32, w, h, r int, isMax bool) []float32 {
	tmp := make([]float32, len(src))
	out := make([]float32, len(src))
	k := 2*r + 1
	parallelN(h, func(a, b int) {
		m := (w + 2*r + k - 1) / k * k
		g, hh := make([]float32, m+k), make([]float32, m+k)
		for y := a; y < b; y++ {
			runMinMax(src, tmp, y*w, w, 1, r, isMax, g, hh)
		}
	})
	parallelN(w, func(a, b int) {
		m := (h + 2*r + k - 1) / k * k
		g, hh := make([]float32, m+k), make([]float32, m+k)
		// process columns via a small transposed buffer for cache use
		col := make([]float32, h)
		res := make([]float32, h)
		for x := a; x < b; x++ {
			for y := 0; y < h; y++ {
				col[y] = tmp[y*w+x]
			}
			runMinMax(col, res, 0, h, 1, r, isMax, g, hh)
			for y := 0; y < h; y++ {
				out[y*w+x] = res[y]
			}
		}
	})
	return out
}

// Despeckle returns a cleaned copy of pix (RGB 16-bit, w x h). protect
// marks pixels that must not change (the film base region); baseSum is
// the clear base's R+G+B (16-bit means; 0 = unknown): pixels at the base
// level are left alone.
func Despeckle(pix []uint16, w, h, level int, protect func(x, y int) bool, baseSum float64) ([]uint16, DustStats) {
	out, st, _ := despeckle(pix, w, h, level, protect, baseSum, false)
	return out, st
}

// DespeckleDebug is Despeckle that also returns every candidate spot.
func DespeckleDebug(pix []uint16, w, h, level int, protect func(x, y int) bool, baseSum float64) ([]uint16, DustStats, []DustSpot) {
	return despeckle(pix, w, h, level, protect, baseSum, true)
}

func medianOf(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 0 {
		return (s[m-1] + s[m]) / 2
	}
	return s[m]
}

func despeckle(pix []uint16, w, h, level int, protect func(x, y int) bool, baseSum float64, debug bool) ([]uint16, DustStats, []DustSpot) {
	t0 := time.Now()
	st := DustStats{Level: level}
	out := make([]uint16, len(pix))
	copy(out, pix)
	if level == DustOff || w < 32 || h < 32 {
		st.Elapsed = time.Since(t0)
		return out, st, nil
	}
	p := dustParamsFor(level, w)
	n := w * h
	L := make([]float32, n)
	parallelN(h, func(a, b int) {
		for i := a * w; i < b*w; i++ {
			L[i] = float32(math.Log2(float64(pix[3*i]) + float64(pix[3*i+1]) + float64(pix[3*i+2]) + 3))
		}
	})
	floor, opq, holder := densityLevels(pix, w, h)
	st.Floor = floor
	holderL := float32(math.Inf(-1))
	if holder {
		holderL = float32(math.Log2(opq[0]+opq[1]+opq[2]+3) + 2)
	}
	type spot struct {
		px             []int32
		x0, y0, x1, y1 int
		over           bool
		peak, zmax     float32
	}
	baseL := float32(0)
	if baseSum > 0 {
		baseL = float32(math.Log2(baseSum+3) - p.BaseEV)
	}
	lg := func(i, c int) float64 { return math.Log2(float64(pix[3*i+c]) + 16) }
	type passRes struct {
		T      []float32
		lab    []int32
		spots  []spot
		info   []DustSpot
		isHair []bool
		accept []bool
	}
	// one detection pass at closing radius R: labelling plus judgement
	pass := func(R int, compactOnly bool) passRes {
		var spots []spot
		T := morph(morph(L, w, h, R, true), w, h, R, false) // closing
		parallelN(h, func(a, b int) {
			for i := a * w; i < b*w; i++ {
				d := T[i] - L[i]
				if d < 0 {
					d = 0
				}
				T[i] = d
			}
		})

		// grain noise per tile: median and robust sigma of T
		const tile = 128
		tw, th := (w+tile-1)/tile, (h+tile-1)/tile
		tmed := make([]float32, tw*th)
		tsig := make([]float32, tw*th)
		parallelN(tw*th, func(a, b int) {
			buf := make([]float32, 0, tile*tile/4)
			for t := a; t < b; t++ {
				tx, ty := t%tw, t/tw
				buf = buf[:0]
				for y := ty * tile; y < min((ty+1)*tile, h); y += 2 {
					for x := tx * tile; x < min((tx+1)*tile, w); x += 2 {
						buf = append(buf, T[y*w+x])
					}
				}
				sort.Slice(buf, func(i, j int) bool { return buf[i] < buf[j] })
				med := buf[len(buf)/2]
				for i := range buf {
					buf[i] = float32(math.Abs(float64(buf[i] - med)))
				}
				sort.Slice(buf, func(i, j int) bool { return buf[i] < buf[j] })
				tmed[t] = med
				tsig[t] = float32(math.Max(0.005, 1.4826*float64(buf[len(buf)/2])))
			}
		})
		zAt := func(i int) float32 {
			t := (i/w/tile)*tw + (i%w)/tile
			return (T[i] - tmed[t]) / tsig[t]
		}

		// seeds and hysteresis labelling (8-connected)
		lab := make([]int32, n)
		maxPx := max(4*p.MaxSpot*p.MaxSpot, int(float64(p.MaxLen)*p.MaxThick*2))
		seed, low := float32(p.Seed), float32(p.Low)
		stack := make([]int32, 0, 4096)
		for i := 0; i < n; i++ {
			if lab[i] != 0 || zAt(i) < seed {
				continue
			}
			id := int32(len(spots) + 1)
			x, y := i%w, i/w
			s := spot{x0: x, y0: y, x1: x, y1: y}
			lab[i] = id
			stack = append(stack[:0], int32(i))
			for len(stack) > 0 {
				j := int(stack[len(stack)-1])
				stack = stack[:len(stack)-1]
				if !s.over {
					s.px = append(s.px, int32(j))
					if len(s.px) > maxPx {
						s.over, s.px = true, nil
					}
				}
				jx, jy := j%w, j/w
				s.x0, s.x1, s.y0, s.y1 = min(s.x0, jx), max(s.x1, jx), min(s.y0, jy), max(s.y1, jy)
				if T[j] > s.peak {
					s.peak = T[j]
				}
				if z := zAt(j); z > s.zmax {
					s.zmax = z
				}
				for dy := -1; dy <= 1; dy++ {
					qy := jy + dy
					if qy < 0 || qy >= h {
						continue
					}
					for dx := -1; dx <= 1; dx++ {
						qx := jx + dx
						if qx < 0 || qx >= w {
							continue
						}
						q := qy*w + qx
						if lab[q] == 0 && zAt(q) >= low {
							lab[q] = id
							stack = append(stack, int32(q))
						}
					}
				}
			}
			spots = append(spots, s)
		}

		info := make([]DustSpot, len(spots))
		isHair := make([]bool, len(spots))
		accept := make([]bool, len(spots))
		parallelN(len(spots), func(a, b int) {
			for k := a; k < b; k++ {
				s := &spots[k]
				d := &info[k]
				d.X0, d.Y0, d.X1, d.Y1, d.Px = s.x0, s.y0, s.x1, s.y1, len(s.px)
				d.Z, d.Peak = float64(s.zmax), float64(s.peak)
				if s.over {
					d.Why = "too big"
					continue
				}
				// context window and city-block distance to the spot (<= 11)
				wx0, wy0 := max(0, s.x0-p.Win), max(0, s.y0-p.Win)
				wx1, wy1 := min(w-1, s.x1+p.Win), min(h-1, s.y1+p.Win)
				ww, wh := wx1-wx0+1, wy1-wy0+1
				dist := make([]uint8, ww*wh)
				for i := range dist {
					dist[i] = 255
				}
				q := make([]int32, 0, len(s.px)*4)
				for _, j := range s.px {
					li := (int(j)/w-wy0)*ww + int(j)%w - wx0
					dist[li] = 0
					q = append(q, int32(li))
				}
				for qi := 0; qi < len(q); qi++ {
					li := int(q[qi])
					dv := dist[li]
					if dv >= 11 {
						continue
					}
					lx, ly := li%ww, li/ww
					for _, o := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
						nx, ny := lx+o[0], ly+o[1]
						if nx < 0 || ny < 0 || nx >= ww || ny >= wh {
							continue
						}
						ni := ny*ww + nx
						if dist[ni] == 255 {
							dist[ni] = dv + 1
							q = append(q, int32(ni))
						}
					}
				}
				gi := func(lx, ly int) int { return (ly+wy0)*w + lx + wx0 }
				// per-channel drop: core (top 30% of Z in the spot) vs ring 3..4 px out
				zs := make([]float64, len(s.px))
				for i, j := range s.px {
					zs[i] = float64(zAt(int(j)))
				}
				zc := append([]float64(nil), zs...)
				sort.Float64s(zc)
				zcut := zc[int(0.7*float64(len(zc)-1))]
				var core, ring [3][]float64
				for i, j := range s.px {
					if zs[i] >= zcut {
						for c := 0; c < 3; c++ {
							core[c] = append(core[c], lg(int(j), c))
						}
					}
				}
				for li, dv := range dist {
					if dv == 3 || dv == 4 {
						for c := 0; c < 3; c++ {
							ring[c] = append(ring[c], lg(gi(li%ww, li/ww), c))
						}
					}
				}
				for c := 0; c < 3; c++ {
					d.Drop[c] = medianOf(ring[c]) - medianOf(core[c])
				}
				// bright excursion: dust only darkens, while lettering, grilles
				// and specular detail put lighter pixels right next to the
				// dark strokes (holes and edges within 2 px of the spot)
				{
					var nb []float64
					for li, dv := range dist {
						if dv == 1 || dv == 2 {
							nb = append(nb, lg(gi(li%ww, li/ww), 1))
						}
					}
					if len(nb) > 0 {
						sort.Float64s(nb)
						d.Bright = nb[int(0.9*float64(len(nb)-1))] - medianOf(ring[1])
					}
				}
				// local texture colour: regress R and B high-pass on G (9x9 box)
				// over the window's dark details (Z > 3) within 48 px
				kx0, ky0 := max(wx0, s.x0-p.Win*3/4), max(wy0, s.y0-p.Win*3/4)
				kx1, ky1 := min(wx1, s.x1+p.Win*3/4), min(wy1, s.y1+p.Win*3/4)
				const br = 4
				ex0, ey0, ex1, ey1 := max(0, kx0-br), max(0, ky0-br), min(w-1, kx1+br), min(h-1, ky1+br)
				ew, eh := ex1-ex0+1, ey1-ey0+1
				var integ [3][]float64
				for c := 0; c < 3; c++ {
					ig := make([]float64, (ew+1)*(eh+1))
					for y := 0; y < eh; y++ {
						row := 0.0
						for x := 0; x < ew; x++ {
							row += lg((y+ey0)*w+x+ex0, c)
							ig[(y+1)*(ew+1)+x+1] = ig[y*(ew+1)+x+1] + row
						}
					}
					integ[c] = ig
				}
				hp := func(x, y, c int) float64 {
					ax0, ay0 := max(ex0, x-br)-ex0, max(ey0, y-br)-ey0
					ax1, ay1 := min(ex1, x+br)-ex0+1, min(ey1, y+br)-ey0+1
					ig := integ[c]
					sum := ig[ay1*(ew+1)+ax1] - ig[ay0*(ew+1)+ax1] - ig[ay1*(ew+1)+ax0] + ig[ay0*(ew+1)+ax0]
					return lg(y*w+x, c) - sum/float64((ax1-ax0)*(ay1-ay0))
				}
				kfit := func(all bool) (float64, float64, int) {
					var sRG, sBG, sGG float64
					cnt := 0
					for y := ky0; y <= ky1; y++ {
						for x := kx0; x <= kx1; x++ {
							li := (y-wy0)*ww + x - wx0
							if dist[li] <= 3 {
								continue
							}
							if !all && zAt(y*w+x) <= 3 {
								continue
							}
							g := hp(x, y, 1)
							sRG += hp(x, y, 0) * g
							sBG += hp(x, y, 2) * g
							sGG += g * g
							cnt++
						}
					}
					sGG = math.Max(sGG, 1e-9)
					return sRG / sGG, sBG / sGG, cnt
				}
				kR, kB, cnt := kfit(false)
				if cnt < 30 {
					kR, kB, _ = kfit(true)
				}
				d.KR, d.KB = kR, kB
				// outlier: peak vs the 99.5th percentile of T around it
				tv := make([]float32, 0, ww*wh)
				for li, dv := range dist {
					if dv > 4 {
						tv = append(tv, T[gi(li%ww, li/ww)])
					}
				}
				if len(tv) > 0 {
					sort.Slice(tv, func(i, j int) bool { return tv[i] < tv[j] })
					pr := float64(tv[int(0.995*float64(len(tv)-1))])
					d.Out = float64(s.peak) / math.Max(pr, 1e-3)
				}
				// shape
				var mx, my float64
				for _, j := range s.px {
					mx += float64(int(j) % w)
					my += float64(int(j) / w)
				}
				np_ := float64(len(s.px))
				mx, my = mx/np_, my/np_
				var cxx, cyy, cxy float64
				for _, j := range s.px {
					dx, dy := float64(int(j)%w)-mx, float64(int(j)/w)-my
					cxx += dx * dx
					cyy += dy * dy
					cxy += dx * dy
				}
				if np_ > 1 {
					cxx, cyy, cxy = cxx/(np_-1), cyy/(np_-1), cxy/(np_-1)
				}
				tr, det := cxx+cyy, cxx*cyy-cxy*cxy
				lmax := tr/2 + math.Sqrt(math.Max(0, tr*tr/4-det))
				d.Len = 4 * math.Sqrt(math.Max(lmax, 0.1))
				d.Thick = np_ / math.Max(d.Len, 1)
				rg := d.Drop[0] / math.Max(d.Drop[1], 1e-3)
				bgr := math.Min(d.Drop[2], 4*d.Drop[1]) / math.Max(d.Drop[1], 1e-3)
				d.CD = math.Abs(rg-kR) + 0.5*math.Abs(bgr-kB)
				compact := max(s.x1-s.x0+1, s.y1-s.y0+1) <= p.MaxSpot
				hairShape := d.Thick <= p.MaxThick && d.Len <= float64(p.MaxLen)
				rule := false
				rules := p.Rules
				if d.Len >= 12 && d.Len >= 4*d.Thick {
					rules = p.HairRules
				}
				if compact && d.Peak >= p.DeepEV && d.Out >= 1 {
					rule = true
				}
				for _, r := range rules {
					if d.Out >= r.Out && d.CD >= r.CD {
						rule = true
					}
				}
				switch {
				case d.Peak < p.MinDepth:
					d.Why = "shallow"
				case d.Drop[1] <= 0.15:
					d.Why = "no drop"
				case rg < p.RGLo || rg > p.RGHi:
					d.Why = "R/G colour"
				case bgr < p.BGMin:
					d.Why = "B/G colour"
				case !compact && !hairShape, compactOnly && (!compact || d.Len >= 4*d.Thick):
					d.Why = "shape"
				case compactOnly && (d.Peak < p.DeepEV || d.Out < 1.5):
					// the large scale only adds deep (near opaque) discs
					d.Why = "shallow"
				case !rule:
					d.Why = "texture"
				case d.Bright >= 0.3 && d.Bright >= 0.35*d.Peak:
					d.Why = "bright detail"
				}
				if d.Why == "" {
					// the holder: its edge is soft and the gradient makes dark
					// spot-like ripples, so keep clear by half a window; it is
					// a large dark area, unlike other (opaque) dust nearby
					g := p.Win / 2
					n := 0
					for yy := max(0, s.y0-g); yy <= min(h-1, s.y1+g); yy++ {
						for xx := max(0, s.x0-g); xx <= min(w-1, s.x1+g); xx++ {
							if xx >= s.x0-3 && xx <= s.x1+3 && yy >= s.y0-3 && yy <= s.y1+3 {
								continue // the spot's own (possibly opaque) core
							}
							if i := yy*w + xx; L[i]+T[i] < holderL {
								n++
							}
						}
					}
					if n >= g*g/2 {
						d.Why = "holder"
					}
				}
				// base-level background is only protected in the rebate band
				// along the frame edge (edge print, DX code); inside the
				// picture a spot on base-level film is dust in a deep shadow
				eb := 3 * p.Win
				nearEdge := s.x0 < eb || s.y0 < eb || s.x1 >= w-eb || s.y1 >= h-eb
				if d.Why == "" && baseL > 0 && nearEdge {
					var bgv []float64
					for _, j := range s.px {
						bgv = append(bgv, float64(L[j]+T[j]))
					}
					if float32(medianOf(bgv)) >= baseL {
						d.Why = "base level"
					}
					// the frame line next to the rebate is ragged and
					// carries faint dark fringe specks that are not dust
					// (real dust there is deeper than 1 EV and still goes)
					g := p.Win / 4
					if d.Peak >= 1 {
						g = -1
					}
				rebate:
					for yy := max(0, s.y0-g); g >= 0 && d.Why == "" && yy <= min(h-1, s.y1+g); yy++ {
						for xx := max(0, s.x0-g); xx <= min(w-1, s.x1+g); xx++ {
							if L[yy*w+xx] >= baseL {
								d.Why = "base level"
								break rebate
							}
						}
					}
				}
				if d.Why == "" && protect != nil {
					for _, j := range s.px {
						if protect(int(j)%w, int(j)/w) {
							d.Why = "base region"
							break
						}
					}
				}
				if d.Why == "" {
					accept[k] = true
					d.Accepted = true
					isHair[k] = d.Len >= 12 && d.Len >= 4*d.Thick
					d.Hair = isHair[k]
				}
			}
		})
		return passRes{T, lab, spots, info, isHair, accept}
	}
	// two scales: R finds specks, hairs and dust edges; a closing of R
	// cannot see into a dense disc wider than 2R+1 (its centre is left
	// unchanged), so a second pass at R2 looks for compact spots only
	r1 := pass(p.R, false)
	r2 := pass(p.R2, true)
	spots, info, isHair, accept := r1.spots, r1.info, r1.isHair, r1.accept
	lab := r1.lab
	{
		// owner (pass-1 index) of every accepted pass-1 pixel
		own := map[int32]int{}
		for k := range spots {
			if accept[k] {
				for _, j := range spots[k].px {
					own[j] = k
				}
			}
		}
		for k := range r2.spots {
			if !r2.accept[k] {
				continue
			}
			s := r2.spots[k]
			// merged pass-1 owners in ascending order (map order is random
			// and decided the pixel order, hence the fill: B12)
			var merged []int
			for _, j := range s.px {
				if o, ok := own[j]; ok {
					merged = append(merged, o)
				}
			}
			sort.Ints(merged)
			merged = slices.Compact(merged)
			// a large-scale spot that is wholly a pass-1 spot adds nothing
			px := append([]int32(nil), s.px...)
			in := map[int32]bool{}
			for _, j := range px {
				in[j] = true
			}
			for _, o := range merged {
				accept[o] = false
				info[o].Accepted = false
				info[o].Why = "merged"
				for _, j := range spots[o].px {
					if !in[j] {
						in[j] = true
						px = append(px, j)
						jx, jy := int(j)%w, int(j)/w
						s.x0, s.x1, s.y0, s.y1 = min(s.x0, jx), max(s.x1, jx), min(s.y0, jy), max(s.y1, jy)
					}
				}
			}
			s.px = px
			id := int32(len(spots) + 1)
			for _, j := range px {
				if lab[j] == 0 {
					lab[j] = id
				}
			}
			d := r2.info[k]
			d.Px = len(px)
			d.Large = true
			spots = append(spots, s)
			info = append(info, d)
			isHair = append(isHair, false)
			accept = append(accept, true)
		}
		// rejected pass-2 spots must not serve as fill sources either
		for i, v := range r2.lab {
			if v != 0 && lab[i] == 0 {
				lab[i] = -1
			}
		}
	}
	var keep []int
	for k := range spots {
		if accept[k] {
			keep = append(keep, k)
			continue
		}
		if info[k].Why == "merged" {
			continue
		}
		st.Rejected++
		switch info[k].Why {
		case "holder", "base level", "base region":
			st.Protected++
		}
	}

	// fill each spot (parallel over spots; spots do not overlap but their
	// rims and source patches may, so read from pix, write to out)
	mask := make([]bool, n) // all accepted spots grown by the rim
	grow := func(s *spot, fn func(i int)) {
		r := p.Rim
		for _, j := range s.px {
			jx, jy := int(j)%w, int(j)/w
			for dy := -r; dy <= r; dy++ {
				for dx := -r; dx <= r; dx++ {
					qx, qy := jx+dx, jy+dy
					if qx >= 0 && qy >= 0 && qx < w && qy < h {
						fn(qy*w + qx)
					}
				}
			}
		}
	}
	// any spot pixel (also rejected ones) must not be used as a source
	bad := func(i int) bool { return lab[i] != 0 || mask[i] }
	for _, k := range keep {
		grow(&spots[k], func(i int) { mask[i] = true })
	}
	// Each spot's fill is computed in parallel into its own list and the
	// lists are applied afterwards in spot order: regions (spot + rim) of
	// neighbouring spots may overlap, and writing out[] from the workers
	// made the result depend on goroutine timing (B12).
	type fillPx struct {
		i int
		v [3]uint16
	}
	fills := make([][]fillPx, len(keep))
	var mu sync.Mutex
	parallelN(len(keep), func(a, b int) {
		for ki, k := range keep[a:b] {
			ki += a
			s := &spots[k]
			// region = spot + rim (unique, ascending), ring = 1 px outside it
			regSet := map[int]bool{}
			var regList []int
			grow(s, func(i int) {
				if (protect == nil || !protect(i%w, i/w)) && !regSet[i] {
					regSet[i] = true
					regList = append(regList, i)
				}
			})
			sort.Ints(regList)
			reg := regSet
			var ring []int
			seen := map[int]bool{}
			for _, i := range regList {
				ix, iy := i%w, i/w
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						qx, qy := ix+dx, iy+dy
						if qx < 0 || qy < 0 || qx >= w || qy >= h {
							continue
						}
						q := qy*w + qx
						if !reg[q] && !seen[q] {
							seen[q] = true
							ring = append(ring, q)
						}
					}
				}
			}
			if len(ring) < 4 {
				continue
			}
			// source patch: try offsets in 8 directions, pick the best ring match
			bw, bh := s.x1-s.x0+1+2*p.Rim, s.y1-s.y0+1+2*p.Rim
			var dists []int
			if isHair[k] {
				thick := int(math.Ceil(float64(len(s.px))/math.Hypot(float64(bw), float64(bh)))) + 2*p.Rim
				dists = []int{thick + 3, 2*thick + 4}
			} else {
				dists = []int{max(bw, bh) + 2, max(bw, bh)*2 + 3}
			}
			best, bestE := image.Point{}, math.Inf(1)
			for _, d := range dists {
				for _, o := range [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {-1, -1}, {1, -1}, {-1, 1}} {
					ox, oy := o[0]*d, o[1]*d
					okk := true
					for _, i := range regList {
						qx, qy := i%w+ox, i/w+oy
						if qx < 0 || qy < 0 || qx >= w || qy >= h || bad(qy*w+qx) {
							okk = false
							break
						}
					}
					if !okk {
						continue
					}
					e := 0.0
					for _, q := range ring {
						qx, qy := q%w+ox, q/w+oy
						if qx < 0 || qy < 0 || qx >= w || qy >= h {
							e = math.Inf(1)
							break
						}
						for c := 0; c < 3; c++ {
							dd := math.Log2((float64(pix[3*q+c]) + 1) / (float64(pix[3*(qy*w+qx)+c]) + 1))
							e += dd * dd
						}
					}
					if e < bestE {
						best, bestE = image.Point{ox, oy}, e
					}
				}
			}
			haveSrc := !math.IsInf(bestE, 1)
			// membrane: per ring pixel the log ratio target/source (or the
			// target itself without a source), Shepard-interpolated inside
			rs := ring
			if len(rs) > 192 {
				step := (len(rs) + 191) / 192
				var sub []int
				for i := 0; i < len(rs); i += step {
					sub = append(sub, rs[i])
				}
				rs = sub
			}
			type rv struct {
				x, y float64
				v    [3]float64
			}
			rvs := make([]rv, len(rs))
			for i, q := range rs {
				qx, qy := q%w, q/w
				rvs[i].x, rvs[i].y = float64(qx), float64(qy)
				for c := 0; c < 3; c++ {
					t := math.Log2(float64(pix[3*q+c]) + 1)
					if haveSrc {
						sq := (qy+best.Y)*w + qx + best.X
						t -= math.Log2(float64(pix[3*sq+c]) + 1)
					}
					rvs[i].v[c] = t
				}
			}
			var fl []fillPx
			for _, i := range regList {
				// pixels at (or near) the film-base level are never touched
				if (baseL > 0 && L[i] >= baseL) || (protect != nil && protect(i%w, i/w)) {
					continue
				}
				ix, iy := float64(i%w), float64(i/w)
				var acc [3]float64
				var nvs [3]uint16
				ws := 0.0
				for _, r := range rvs {
					d2 := (r.x-ix)*(r.x-ix) + (r.y-iy)*(r.y-iy)
					wt := 1 / (d2 * d2)
					ws += wt
					for c := 0; c < 3; c++ {
						acc[c] += wt * r.v[c]
					}
				}
				for c := 0; c < 3; c++ {
					m := acc[c] / ws
					if haveSrc {
						sq := (i/w+best.Y)*w + i%w + best.X
						m += math.Log2(float64(pix[3*sq+c]) + 1)
					}
					v := math.Exp2(m) - 1
					if v < 0 {
						v = 0
					}
					if v > 65535 {
						v = 65535
					}
					nvs[c] = uint16(math.Round(v))
				}
				fl = append(fl, fillPx{i, nvs})
			}
			fills[ki] = fl
			mu.Lock()
			st.Spots++
			if s.peak >= 1.5 {
				st.Opaque++
			}
			if isHair[k] {
				st.Hairs++
			}
			mu.Unlock()
		}
	})
	touched := make([]bool, n)
	for _, fl := range fills {
		for _, f := range fl {
			i := f.i
			out[3*i], out[3*i+1], out[3*i+2] = f.v[0], f.v[1], f.v[2]
			touched[i] = true
		}
	}
	// v0.79: thin curved fibres of either polarity (fibre.go)
	{
		fi, nf := removeFibres(out, w, h, level, protect, baseL, holderL, touched, debug)
		st.Fibres = nf
		info = append(info, fi...)
	}
	for i, t := range touched {
		if t && (out[3*i] != pix[3*i] || out[3*i+1] != pix[3*i+1] || out[3*i+2] != pix[3*i+2]) {
			st.Pixels++
		}
	}
	st.Elapsed = time.Since(t0)
	if !debug {
		info = nil
	}
	return out, st, info
}

// densityFloor estimates, per channel, the transmission of the densest
// genuine film (16-bit): the 1st percentile of 64-px block medians, leaving
// out blocks as dark as the opaque level (holder, unexposed surround): the
// 0.5th percentile of the block medians. If there are hardly any such
// blocks (no holder in the frame) the opaque level is the densest film.
// DensityFloor is exported for tests.
func DensityFloor(pix []uint16, w, h int) [3]float64 { return densityFloor(pix, w, h) }

func densityFloor(pix []uint16, w, h int) [3]float64 {
	f, _, _ := densityLevels(pix, w, h)
	return f
}

// densityLevels returns the genuine density floor, the opaque level (the
// 0.5th percentile of the block medians) and whether an opaque surround
// (holder) is in the frame.
func densityLevels(pix []uint16, w, h int) (out, o [3]float64, holder bool) {
	const B = 64
	var meds [3][]float64
	for by := 0; by+B <= h; by += B {
		for bx := 0; bx+B <= w; bx += B {
			for c := 0; c < 3; c++ {
				v := make([]float64, 0, 256)
				for y := by; y < by+B; y += 4 {
					for x := bx; x < bx+B; x += 4 {
						v = append(v, float64(pix[3*(y*w+x)+c]))
					}
				}
				sort.Float64s(v)
				meds[c] = append(meds[c], v[len(v)/2])
			}
		}
	}
	nb := len(meds[0])
	if nb == 0 {
		return
	}
	for c := 0; c < 3; c++ {
		s := append([]float64(nil), meds[c]...)
		sort.Float64s(s)
		o[c] = s[nb/200]
	}
	// film blocks: brighter than 2x the opaque level in every channel
	var film [3][]float64
	dark := 0
	for i := 0; i < nb; i++ {
		ok := true
		for c := 0; c < 3; c++ {
			if meds[c][i] <= 2*o[c]+16 {
				ok = false
			}
		}
		if !ok {
			dark++
			continue
		}
		for c := 0; c < 3; c++ {
			film[c] = append(film[c], meds[c][i])
		}
	}
	// a real holder is far darker than any film: its level must lie at
	// least 4 EV below the median block (dense film rarely is 2.5 EV)
	if dark >= nb/50 && len(film[0]) >= 20 {
		var oS, mS float64
		for c := 0; c < 3; c++ {
			s := append([]float64(nil), meds[c]...)
			sort.Float64s(s)
			oS += o[c]
			mS += s[nb/2]
		}
		holder = oS*16 <= mS
	}
	for c := 0; c < 3; c++ {
		if dark < nb/50 || len(film[c]) < 20 {
			out[c] = o[c] // no opaque surround: the densest film itself
			continue
		}
		sort.Float64s(film[c])
		out[c] = film[c][len(film[c])/100]
	}
	return
}
