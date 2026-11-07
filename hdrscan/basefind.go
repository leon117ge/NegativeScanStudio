package hdrscan

import (
	"fmt"
	"math"
	"os"
	"sort"
)

// Film base auto-detection on a binned linear probe (bin x bin raw pixels per
// cell). The clear base is the least dense part of a colour negative: the
// brightest flat, unclipped, colour-consistent region. It can be anything from
// a thin rebate strip, a gap between frames or the land around sprocket holes
// up to the whole frame (a frame of pure clear base), so the size of a region
// does not decide - its level and colour do:
//
//  1. clipped cells and a 2-cell guard ring are excluded (sprocket holes and
//     unfiltered light together with their flare halo);
//  2. a cell is flat when its 3x3 neighbourhood is uniform either in the raw
//     cells (thin strips) or after a 3x3 box filter (24 px = 4 X-Trans
//     periods, which removes the CFA/PDAF phase pattern that makes single 8 px
//     cells noisy on X-Trans bodies); the threshold adapts to the frame's
//     measured cell noise, between 5% and 10%;
//  3. flat cells among the brighter ones are grown into clusters from the
//     brightest seed: neighbours must be similar, keep the seed's colour
//     (R/G, B/G within ~10%) and stay within 1 EV of the seed, so a vignetted
//     or unevenly lit full-frame base stays one cluster while image detail
//     does not join;
//  4. clusters near full scale in every channel, or much more neutral (higher
//     B/R) than the most orange bright cluster, are rejected as light leaks;
//  5. the brightest remaining cluster wins (median level); clusters within
//     0.25 EV of it are ranked by orange mask, then rebate/edge position, then
//     size.
//
// The reading uses the median of every cell of the winning cluster (borders
// eroded), not one small patch.

const (
	minBaseCells = 48 // 48 cells of 8x8 = 3072 raw pixels
	minShortSide = 3
	largeBase    = 0.20  // coverage that is reported as a large base area
	tinyBase     = 0.005 // interior clusters below this coverage are "tiny" (gaps between frames)
	// a cluster spans at most 0.4 EV below its brightest cell: enough for
	// normal light falloff/vignetting, while the brighter part of a strongly
	// vignetted base forms its own (brighter, so preferred) cluster
	minRel = 0.75
)

type baseFind struct {
	X0, Y0, X1, Y1 int // largest rectangle inside the cluster (exclusive max)
	BX0, BY0       int // cluster bounding box
	BX1, BY1       int
	Cells          []int // cells used for the reading (cluster, eroded)
	Coverage       float64
	Where          string
	Warn           string
	Clusters       int
	Orange         bool
}

func findBase(bw, bh int, rgb []float32, clip []bool) (baseFind, bool) {
	n := bw * bh
	at := func(i, c int) float64 { return float64(rgb[3*i+c]) }
	lev := func(i int) float64 { return math.Max(at(i, 0), math.Max(at(i, 1), at(i, 2))) }
	// 1. guard: dilate clipped cells by 2.
	bad := make([]bool, n)
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			i := y*bw + x
			if !clip[i] && lev(i) < 0.97 {
				continue
			}
			for yy := y - 2; yy <= y+2; yy++ {
				for xx := x - 2; xx <= x+2; xx++ {
					if yy >= 0 && xx >= 0 && yy < bh && xx < bw {
						bad[yy*bw+xx] = true
					}
				}
			}
		}
	}
	// box-filtered field (3x3, clamped at the border)
	sm := make([]float64, 3*n)
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			var s [3]float64
			k := 0
			for yy := y - 1; yy <= y+1; yy++ {
				for xx := x - 1; xx <= x+1; xx++ {
					if yy < 0 || xx < 0 || yy >= bh || xx >= bw {
						continue
					}
					for c := 0; c < 3; c++ {
						s[c] += at(yy*bw+xx, c)
					}
					k++
				}
			}
			for c := 0; c < 3; c++ {
				sm[3*(y*bw+x)+c] = s[c] / float64(k)
			}
		}
	}
	sat := func(i, c int) float64 { return sm[3*i+c] }
	slev := func(i int) float64 { return math.Max(sat(i, 0), math.Max(sat(i, 1), sat(i, 2))) }
	// frame cell noise: median relative cell-vs-smoothed residual per channel
	var thr [3]float64
	for c := 0; c < 3; c++ {
		var res []float64
		for i := 0; i < n; i += 3 {
			if bad[i] || sat(i, c) < 0.01 {
				continue
			}
			res = append(res, math.Abs(at(i, c)-sat(i, c))/sat(i, c))
		}
		thr[c] = 0.05
		if len(res) > 0 {
			sort.Float64s(res)
			thr[c] = math.Min(0.10, math.Max(0.05, 5*res[len(res)/2]))
		}
	}
	// 2. flat cells.
	rangeOK := func(x, y int, get func(int, int) float64, c int, rel float64) bool {
		lo, hi := math.Inf(1), math.Inf(-1)
		for yy := y - 1; yy <= y+1; yy++ {
			for xx := x - 1; xx <= x+1; xx++ {
				v := get(yy*bw+xx, c)
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
		}
		return hi-lo <= rel*get(y*bw+x, c)+0.002
	}
	flat := make([]bool, n)
	var levels []float64
	for y := 1; y < bh-1; y++ {
		for x := 1; x < bw-1; x++ {
			i := y*bw + x
			if bad[i] || lev(i) <= 0.002 {
				continue
			}
			rawOK, smOK := true, true
			for c := 0; c < 3; c++ {
				rawOK = rawOK && rangeOK(x, y, at, c, 0.06)
				smOK = smOK && rangeOK(x, y, sat, c, thr[c])
			}
			if rawOK || smOK {
				flat[i] = true
				levels = append(levels, slev(i))
			}
		}
	}
	if len(levels) == 0 {
		return baseFind{}, false
	}
	sort.Float64s(levels)
	top := levels[int(float64(len(levels)-1)*0.995)]
	var seeds []int
	cand := make([]bool, n)
	for i := range flat {
		if flat[i] && slev(i) >= 0.35*top {
			cand[i] = true
			seeds = append(seeds, i)
		}
	}
	sort.Slice(seeds, func(a, b int) bool { return slev(seeds[a]) > slev(seeds[b]) })
	// 3. clusters, grown from the brightest seed down.
	type cluster struct {
		cells          []int
		med            [3]float64
		lo, hi         float64 // p2/p98 of the metered channel over the cluster
		level, br      float64
		x0, y0, x1, y1 int // rectangle
		bx0, by0       int
		bx1, by1       int
		edge, strip    bool
		orange         bool
		reject         string
		used           []int
	}
	label := make([]int, n)
	for i := range label {
		label[i] = -1
	}
	chroma := func(get func(int, int) float64, i int) (float64, float64) {
		g := math.Max(get(i, 1), 1e-4)
		return math.Log(math.Max(get(i, 0), 1e-4) / g), math.Log(math.Max(get(i, 2), 1e-4) / g)
	}
	step := func(a, b int) bool {
		rawSim, smSim := true, true
		for c := 0; c < 3; c++ {
			va, vb := at(a, c), at(b, c)
			if math.Abs(va-vb) > 0.04*math.Max(va, vb)+0.002 {
				rawSim = false
			}
			sa, sb := sat(a, c), sat(b, c)
			if math.Abs(sa-sb) > 0.5*thr[c]*math.Max(sa, sb)+0.002 {
				smSim = false
			}
		}
		return rawSim || smSim
	}
	var cls []*cluster
	for _, s := range seeds {
		if label[s] >= 0 {
			continue
		}
		cl := &cluster{}
		id := len(cls)
		cls = append(cls, cl)
		sr, sb := chroma(sat, s)
		sl := slev(s)
		sameColour := func(j int) bool {
			for _, get := range []func(int, int) float64{sat, at} {
				r, b := chroma(get, j)
				if math.Abs(r-sr) <= 0.10 && math.Abs(b-sb) <= 0.10 {
					return true
				}
			}
			return false
		}
		stack := []int{s}
		label[s] = id
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			cl.cells = append(cl.cells, i)
			x, y := i%bw, i/bw
			for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				xx, yy := x+d[0], y+d[1]
				if xx < 0 || yy < 0 || xx >= bw || yy >= bh {
					continue
				}
				j := yy*bw + xx
				if cand[j] && label[j] < 0 && slev(j) >= minRel*sl && step(i, j) && sameColour(j) {
					label[j] = id
					stack = append(stack, j)
				}
			}
		}
	}
	// 4. measure clusters.
	var good []*cluster
	mx, my := bw/10, bh/10
	for id, cl := range cls {
		if len(cl.cells) < minBaseCells {
			continue
		}
		x0, y0, x1, y1, area := maxRect(bw, bh, label, id)
		if area < minBaseCells || x1-x0 < minShortSide || y1-y0 < minShortSide {
			continue
		}
		cl.x0, cl.y0, cl.x1, cl.y1 = x0, y0, x1, y1
		cl.bx0, cl.by0, cl.bx1, cl.by1 = bw, bh, 0, 0
		for _, i := range cl.cells {
			x, y := i%bw, i/bw
			cl.bx0, cl.by0 = min(cl.bx0, x), min(cl.by0, y)
			cl.bx1, cl.by1 = max(cl.bx1, x+1), max(cl.by1, y+1)
		}
		// erode by one cell (border cells mix with what is next to the base)
		// unless that leaves too little
		for _, i := range cl.cells {
			x, y := i%bw, i/bw
			inner := true
			for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				xx, yy := x+d[0], y+d[1]
				if xx < 0 || yy < 0 || xx >= bw || yy >= bh || label[yy*bw+xx] != id {
					inner = false
					break
				}
			}
			if inner {
				cl.used = append(cl.used, i)
			}
		}
		if len(cl.used) < minBaseCells/2 {
			cl.used = cl.cells
		}
		vals := make([]float64, len(cl.used))
		for c := 0; c < 3; c++ {
			for k, i := range cl.used {
				vals[k] = at(i, c)
			}
			sort.Float64s(vals)
			cl.med[c] = vals[len(vals)/2]
		}
		ch := 0
		for c := 1; c < 3; c++ {
			if cl.med[c] > cl.med[ch] {
				ch = c
			}
		}
		for k, i := range cl.used {
			vals[k] = at(i, ch)
		}
		sort.Float64s(vals)
		cl.lo, cl.hi = vals[len(vals)*2/100], vals[(len(vals)-1)*98/100]
		cl.level = cl.med[ch]
		cl.br = cl.med[2] / math.Max(cl.med[0], 1e-6)
		cl.orange = cl.med[2] < 0.85*cl.med[1] && cl.med[2] < 0.85*cl.med[0]
		w, h := x1-x0, y1-y0
		cl.strip = w >= 3*h || h >= 3*w
		cl.edge = cl.bx0 <= mx || cl.by0 <= my || cl.bx1 >= bw-mx || cl.by1 >= bh-my
		if math.Min(cl.med[0], math.Min(cl.med[1], cl.med[2])) >= 0.85 {
			cl.reject = "near full scale in every channel (light leak)"
		}
		good = append(good, cl)
	}
	// neutral-light rejection: much more neutral than the most orange bright cluster
	minBR := math.Inf(1)
	brightest := 0.0
	for _, cl := range good {
		if cl.reject == "" {
			brightest = math.Max(brightest, cl.level)
		}
	}
	for _, cl := range good {
		if cl.reject == "" && cl.level >= 0.25*brightest {
			minBR = math.Min(minBR, cl.br)
		}
	}
	for _, cl := range good {
		if cl.reject == "" && cl.br > 1.35*minBR && cl.br > 0.6 {
			cl.reject = fmt.Sprintf("too neutral (B/R %.2f vs %.2f for the film) - unfiltered light", cl.br, minBR)
		}
	}
	var ok []*cluster
	brightest = 0
	for _, cl := range good {
		if cl.reject == "" {
			ok = append(ok, cl)
		}
	}
	// The level gate below is relative to the brightest credible cluster. A
	// tiny patch inside the frame (a narrow gap between frames, flare) must
	// not set it, or it pushes every real rebate strip out of the running.
	tiny := func(cl *cluster) bool {
		return !cl.edge && float64(len(cl.cells)) < tinyBase*float64(n)
	}
	for _, cl := range ok {
		if !tiny(cl) {
			brightest = math.Max(brightest, cl.level)
		}
	}
	if brightest == 0 {
		for _, cl := range ok {
			brightest = math.Max(brightest, cl.level)
		}
	}
	res := baseFind{Clusters: len(ok)}
	if len(ok) == 0 {
		return res, false
	}
	// 5. choose: level first (0.25 EV = 1 point), then colour, position, size.
	score := func(cl *cluster) float64 {
		s := 4 * math.Log2(cl.level)
		if cl.orange {
			s += 1
		}
		switch {
		case float64(len(cl.cells)) >= largeBase*float64(n), cl.edge && cl.strip:
			s += 2 // a large clear base area or a rebate strip
		case cl.edge || cl.strip:
			s += 1
		}
		if tiny(cl) {
			s -= 1 // small interior patch: prefer a larger region at a similar level
		}
		return s + 0.05*math.Log2(float64(len(cl.cells)))
	}
	const near = 0.84 // 0.25 EV
	if os.Getenv("BASEDEBUG") != "" {
		for _, cl := range ok {
			fmt.Fprintf(os.Stderr, "cluster lev %.3f cells %d rect %d,%d-%d,%d edge %v strip %v orange %v score %.2f\n", cl.level, len(cl.cells), cl.x0, cl.y0, cl.x1, cl.y1, cl.edge, cl.strip, cl.orange, score(cl))
		}
	}
	var best *cluster
	k := 0
	for _, cl := range ok {
		if cl.level < brightest*near {
			continue
		}
		k++
		if best == nil || score(cl) > score(best) {
			best = cl
		}
	}
	res.X0, res.Y0, res.X1, res.Y1 = best.x0, best.y0, best.x1, best.y1
	res.BX0, res.BY0, res.BX1, res.BY1 = best.bx0, best.by0, best.bx1, best.by1
	res.Cells = best.used
	res.Coverage = float64(len(best.cells)) / float64(n)
	res.Orange = best.orange
	pct := 100 * res.Coverage
	var kind string
	switch {
	case res.Coverage >= largeBase:
		kind = fmt.Sprintf("large clear base area (%.0f%% of frame)", pct)
	case best.strip && best.edge:
		kind = fmt.Sprintf("rebate strip at the frame edge (%.1f%% of frame)", pct)
	case best.strip:
		kind = fmt.Sprintf("strip, gap between frames? (%.1f%% of frame)", pct)
	case best.edge:
		kind = fmt.Sprintf("base area near the frame edge (%.1f%% of frame)", pct)
	default:
		kind = fmt.Sprintf("flat base area (%.1f%% of frame)", pct)
	}
	res.Where = fmt.Sprintf("%s, median of %d cells, %d of %d base-like regions", kind, len(best.used), k, len(ok))
	if !best.orange {
		res.Warn = "the detected base is not orange-masked (B not below R and G) - fine for B&W/slide, otherwise check the region"
	}
	return res, true
}

// maxRect returns the largest axis-aligned rectangle of cells labelled id
// (histogram method, O(w*h)).
func maxRect(bw, bh int, label []int, id int) (x0, y0, x1, y1, area int) {
	hgt := make([]int, bw)
	type st struct{ x, h int }
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			if label[y*bw+x] == id {
				hgt[x]++
			} else {
				hgt[x] = 0
			}
		}
		var stack []st
		for x := 0; x <= bw; x++ {
			h := 0
			if x < bw {
				h = hgt[x]
			}
			start := x
			for len(stack) > 0 && stack[len(stack)-1].h >= h {
				t := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if a := t.h * (x - t.x); a > area {
					area = a
					x0, x1, y0, y1 = t.x, x, y-t.h+1, y+1
				}
				start = t.x
			}
			stack = append(stack, st{start, h})
		}
	}
	return
}
