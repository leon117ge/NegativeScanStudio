package hdrscan

// Guided hair / fibre removal (v0.77). The user selects a box on the merged
// HDR; only inside it an oriented-line detector finds the dark fibre and a
// texture inpaint heals it from a nearby clean patch. Ported from the
// real1031 prototype (dust/hair_fix.py + fibre_detect.py):
//
//  1. L = log2(R+G+B+3), half resolution (2x2 mean).
//  2. 24 oriented second-derivative kernels (sigma across 2.5, along 10
//     half-res px, zero mean) - max response and its angle.
//  3. Z = (r - median) / (1.4826 MAD) over the selection; non-maximum
//     suppression across the ridge; Z >= 2.
//  4. Ridge pixels are grouped with a 5x5 bridge; the longest group plus
//     groups of >= 40 px within 20 px of it are the fibre.
//  5. Full-res centreline (x2, dilate 2, Zhang-Suen thinning); band =
//     centreline dilated 3, grown up to 8 px into pixels still >= 0.03 EV
//     below the local (41 px median) background, plus a 2 px rim.
//  6. Healing in 24 px chunks along the centreline: the best of 24 offset
//     patches (14/22/32 px x 8 directions, never from the band) by log
//     match on the ring around the chunk, then a Shepard membrane of the
//     ring's log ratio so the patch meets its surroundings seamlessly.
//
// Nothing outside the band (which never leaves the selection) changes.
// Full-frame automatic detection is deliberately not offered: on real
// scans it also picks up genuine image lines.

import (
	"errors"
	"fmt"
	"image"
	"math"
	"runtime"
	"sort"
	"sync"
)

// HairOptions tune the guided hair removal.
type HairOptions struct {
	Z       float64 // ridge threshold in robust sigmas (2.0)
	Grow    int     // band growth iterations (8)
	GrowEV  float64 // growth: pixel must be this far below the background (0.03 EV)
	MaxSide int     // largest selection side in pixels (2400)
	MinSide int     // smallest selection side in pixels (24)
	MinLen  int     // shortest accepted fibre, half-res ridge pixels (15)
	// MinDepth: the line found must lie at least this far (median along its
	// centreline) below the local background, else nothing is changed (0.04 EV).
	MinDepth float64
}

// DefaultHairOptions are the prototype's validated settings.
func DefaultHairOptions() HairOptions {
	return HairOptions{Z: 2.0, Grow: 8, GrowEV: 0.03, MaxSide: 2400, MinSide: 24, MinLen: 15, MinDepth: 0.04}
}

// HairEdit is one healed selection. Apply/Undo switch the pixels.
type HairEdit struct {
	Rect      image.Rectangle // selection (clipped, even-aligned)
	Idx       []int32         // changed pixels (y*W+x)
	Old, New  []uint16        // 3 values per changed pixel
	Ridge     int             // half-res ridge pixels used
	Centre    []int32         // full-res centreline pixels (y*W+x)
	Band      int             // pixels in the healing band
	Chunks    int             // chunks healed
	Skipped   int             // chunks without a clean source patch (left as is)
	DepthPre  float64         // median centreline depth below the background, EV
	DepthPost float64
}

// Apply writes the healed pixels.
func (e *HairEdit) Apply(pix []uint16) {
	for k, i := range e.Idx {
		copy(pix[3*int(i):3*int(i)+3], e.New[3*k:3*k+3])
	}
}

// Undo restores the original pixels.
func (e *HairEdit) Undo(pix []uint16) {
	for k, i := range e.Idx {
		copy(pix[3*int(i):3*int(i)+3], e.Old[3*k:3*k+3])
	}
}

func (e *HairEdit) String() string {
	return fmt.Sprintf("hair removed in (%d,%d)-(%d,%d): %d px changed, centreline %d px, depth %.3f -> %.3f EV, %d chunks (%d without a clean source)",
		e.Rect.Min.X, e.Rect.Min.Y, e.Rect.Max.X, e.Rect.Max.Y, len(e.Idx), len(e.Centre), e.DepthPre, e.DepthPost, e.Chunks, e.Skipped)
}

const (
	hairNA      = 24
	hairSU      = 2.5
	hairSV      = 10.0
	hairKR      = 30 // kernel radius (3 sigma along), half-res px
	hairBgR     = 20 // 41x41 background median
	hairChunk   = 24
	hairNearL1  = 20
	hairSide    = 5   // symmetry test offset, half-res px
	hairSymFrac = 0.5 // share of a line's ridge pixels that must be darker than both sides
)

type hairTap struct {
	dx, dy int
	w      float32
}

var (
	hairKernOnce sync.Once
	hairKern     [hairNA][]hairTap
	hairLog      []float64 // log2(v+1)
)

func hairInit() {
	hairKernOnce.Do(func() {
		for a := 0; a < hairNA; a++ {
			th := math.Pi * float64(a) / hairNA
			c, s := math.Cos(th), math.Sin(th)
			gs := 0.0
			for y := -hairKR; y <= hairKR; y++ {
				for x := -hairKR; x <= hairKR; x++ {
					u, v := float64(x)*c+float64(y)*s, -float64(x)*s+float64(y)*c
					gs += math.Exp(-u*u/(2*hairSU*hairSU) - v*v/(2*hairSV*hairSV))
				}
			}
			var taps []hairTap
			var ws []float64
			sum := 0.0
			for y := -hairKR; y <= hairKR; y++ {
				for x := -hairKR; x <= hairKR; x++ {
					u, v := float64(x)*c+float64(y)*s, -float64(x)*s+float64(y)*c
					q := u*u/(hairSU*hairSU) + v*v/(hairSV*hairSV)
					if q >= 9 {
						continue
					}
					g := math.Exp(-q/2) / gs
					k := (u*u/math.Pow(hairSU, 4) - 1/(hairSU*hairSU)) * g * hairSU * hairSU
					taps = append(taps, hairTap{dx: x, dy: y})
					ws = append(ws, k)
					sum += k
				}
			}
			m := sum / float64(len(ws)) // zero mean over the support: no response to flat or linear L
			for i := range taps {
				taps[i].w = float32(ws[i] - m)
			}
			hairKern[a] = taps
		}
		hairLog = make([]float64, 65536)
		for i := range hairLog {
			hairLog[i] = math.Log2(float64(i) + 1)
		}
	})
}

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// RemoveHair finds and heals the fibre inside sel. pix (RGB, W x H) is not
// modified; call Apply on the result.
func RemoveHair(pix []uint16, W, H int, sel image.Rectangle, o HairOptions) (*HairEdit, error) {
	hairInit()
	if o.Z == 0 {
		o = DefaultHairOptions()
	}
	sel = sel.Canon().Intersect(image.Rect(0, 0, W, H))
	sel.Min.X, sel.Min.Y = sel.Min.X&^1, sel.Min.Y&^1
	sel.Max.X, sel.Max.Y = sel.Max.X&^1, sel.Max.Y&^1
	sw, sh := sel.Dx(), sel.Dy()
	if sw < o.MinSide || sh < o.MinSide {
		return nil, fmt.Errorf("selection too small (%dx%d px, at least %d px a side)", sw, sh, o.MinSide)
	}
	if o.MaxSide > 0 && (sw > o.MaxSide || sh > o.MaxSide) {
		return nil, fmt.Errorf("selection too large (%dx%d px, at most %d px a side) - select just the hair", sw, sh, o.MaxSide)
	}
	lum := func(x, y int) float32 {
		i := 3 * (y*W + x)
		return float32(math.Log2(float64(pix[i]) + float64(pix[i+1]) + float64(pix[i+2]) + 3))
	}

	// --- 1/2. half-res oriented filter over the selection + 1 px ---
	HW, HH := W/2, H/2
	hx0, hy0, hw, hh := sel.Min.X/2, sel.Min.Y/2, sw/2, sh/2
	m := hairKR + 2
	pw, ph := hw+2*m, hh+2*m
	lh := make([]float32, pw*ph)
	for y := 0; y < ph; y++ {
		sy := clampi(hy0-m+y, 0, HH-1)
		for x := 0; x < pw; x++ {
			sx := clampi(hx0-m+x, 0, HW-1)
			lh[y*pw+x] = (lum(2*sx, 2*sy) + lum(2*sx+1, 2*sy) + lum(2*sx, 2*sy+1) + lum(2*sx+1, 2*sy+1)) / 4
		}
	}
	// response on the selection grid with a 1 px border (index (y+1)*(hw+2)+x+1)
	rw, rh := hw+2, hh+2
	resp := make([]float32, rw*rh)
	ang := make([]uint8, rw*rh)
	var offs [hairNA][]int
	for a := 0; a < hairNA; a++ {
		offs[a] = make([]int, len(hairKern[a]))
		for i, t := range hairKern[a] {
			offs[a][i] = t.dy*pw + t.dx
		}
	}
	var wg sync.WaitGroup
	rows := make(chan int, rh)
	for y := 0; y < rh; y++ {
		rows <- y
	}
	close(rows)
	for g := 0; g < runtime.NumCPU(); g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for y := range rows {
				for x := 0; x < rw; x++ {
					c := (y-1+m)*pw + (x - 1 + m)
					best, ba := float32(-1e30), 0
					for a := 0; a < hairNA; a++ {
						var s float32
						for i, off := range offs[a] {
							s += hairKern[a][i].w * lh[c+off]
						}
						if s > best {
							best, ba = s, a
						}
					}
					resp[y*rw+x], ang[y*rw+x] = best, uint8(ba)
				}
			}
		}()
	}
	wg.Wait()

	// --- 3. robust Z + NMS ---
	vals := make([]float64, 0, hw*hh)
	for y := 0; y < hh; y++ {
		for x := 0; x < hw; x++ {
			vals = append(vals, float64(resp[(y+1)*rw+x+1]))
		}
	}
	med := median64(vals)
	for i := range vals {
		vals[i] = math.Abs(vals[i] - med)
	}
	sg := 1.4826 * median64(vals)
	if sg <= 1e-9 {
		return nil, errors.New("selection is flat - nothing to remove")
	}
	lo := make([]bool, hw*hh)
	zv := make([]float64, hw*hh)
	sym := make([]bool, hw*hh) // darker than both sides (see hairSymmetric)
	nlo := 0
	for y := 0; y < hh; y++ {
		for x := 0; x < hw; x++ {
			r := resp[(y+1)*rw+x+1]
			z := (float64(r) - med) / sg
			if z < o.Z {
				continue
			}
			zv[y*hw+x] = z
			th := math.Pi * float64(ang[(y+1)*rw+x+1]) / hairNA
			cx, cy := math.Cos(th), math.Sin(th)
			ax := clampi(int(math.RoundToEven(float64(x)+cx)), 0, hw-1)
			ay := clampi(int(math.RoundToEven(float64(y)+cy)), 0, hh-1)
			bx := clampi(int(math.RoundToEven(float64(x)-cx)), 0, hw-1)
			by := clampi(int(math.RoundToEven(float64(y)-cy)), 0, hh-1)
			if r >= resp[(ay+1)*rw+ax+1] && r >= resp[(by+1)*rw+bx+1] {
				lo[y*hw+x] = true
				sym[y*hw+x] = hairSymmetric(lh, pw, m+x, m+y, cx, cy)
				nlo++
			}
		}
	}
	if nlo == 0 {
		return nil, errors.New("no hair-like line found in the selection")
	}

	// --- 4. group ridge pixels (5x5 bridge, 4-connected labels) ---
	dil := make([]bool, hw*hh)
	for y := 0; y < hh; y++ {
		for x := 0; x < hw; x++ {
			if !lo[y*hw+x] {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					if xx, yy := x+dx, y+dy; xx >= 0 && yy >= 0 && xx < hw && yy < hh {
						dil[yy*hw+xx] = true
					}
				}
			}
		}
	}
	lab := label4(dil, hw, hh)
	// score = sum of Z above the threshold over the symmetric (fibre-like)
	// ridge pixels: a long strong fibre beats a grain network, and a shadow's
	// rim (one-sided) scores little
	cnt, nsym := map[int32]int{}, map[int32]int{}
	score := map[int32]float64{}
	for i, l := range lab {
		if lo[i] {
			cnt[l]++
			if sym[i] {
				nsym[l]++
				score[l] += zv[i] - o.Z
			}
		}
	}
	fibreLike := func(l int32) bool { return float64(nsym[l]) >= hairSymFrac*float64(cnt[l]) }
	var kbest int32
	for l, c := range score {
		if c > score[kbest] || (c == score[kbest] && l < kbest) {
			kbest = l
		}
	}
	if cnt[kbest] < o.MinLen {
		return nil, fmt.Errorf("no hair-like line found in the selection (longest ridge %d px)", 2*cnt[kbest])
	}
	if !fibreLike(kbest) {
		return nil, fmt.Errorf("no hair-like line found in the selection (strongest line is an edge, only %d of %d px darker than both sides)", nsym[kbest], cnt[kbest])
	}
	cl := make([]bool, hw*hh)
	for i := range lo {
		cl[i] = lo[i] && lab[i] == kbest
	}
	near := dilateCross(cl, hw, hh, hairNearL1, nil)
	add := map[int32]bool{}
	for i := range lo {
		if lo[i] && lab[i] != kbest && cnt[lab[i]] >= 40 && near[i] && fibreLike(lab[i]) {
			add[lab[i]] = true
		}
	}
	ridge := 0
	for i := range lo {
		if lo[i] && add[lab[i]] {
			cl[i] = true
		}
		if cl[i] {
			ridge++
		}
	}

	// --- 5. full-res centreline and band (selection frame) ---
	clf := make([]bool, sw*sh)
	for y := 0; y < hh; y++ {
		for x := 0; x < hw; x++ {
			if cl[y*hw+x] {
				clf[clampi(2*y, 0, sh-1)*sw+clampi(2*x, 0, sw-1)] = true
			}
		}
	}
	clf = thinZS(dilateCross(clf, sw, sh, 2, nil), sw, sh)
	// L with a background margin
	bm := hairBgR + 8
	lw, lhh := sw+2*bm, sh+2*bm
	lf := make([]float32, lw*lhh)
	fill := func(dst []float32) {
		for y := 0; y < lhh; y++ {
			sy := clampi(sel.Min.Y-bm+y, 0, H-1)
			for x := 0; x < lw; x++ {
				dst[y*lw+x] = lum(clampi(sel.Min.X-bm+x, 0, W-1), sy)
			}
		}
	}
	fill(lf)
	gk := gauss1(1.5, 6)
	smooth := func(L []float32, x, y int) float64 { // gaussian sigma 1.5 at selection (x,y)
		s := 0.0
		for dy := -6; dy <= 6; dy++ {
			r := (y + bm + dy) * lw
			for dx := -6; dx <= 6; dx++ {
				s += gk[dy+6] * gk[dx+6] * float64(L[r+x+bm+dx])
			}
		}
		return s
	}
	buf := make([]float32, 0, 41*41)
	bgAt := func(L []float32, x, y int) float64 {
		buf = buf[:0]
		for dy := -hairBgR; dy <= hairBgR; dy++ {
			r := (y + bm + dy) * lw
			buf = append(buf, L[r+x+bm-hairBgR:r+x+bm+hairBgR+1]...)
		}
		return float64(selectK(buf, len(buf)/2))
	}
	bgc := map[int]float64{}
	depth := func(i int) float64 {
		b, ok := bgc[i]
		if !ok {
			b = bgAt(lf, i%sw, i/sw)
			bgc[i] = b
		}
		return b - smooth(lf, i%sw, i/sw)
	}
	band := dilateCross(clf, sw, sh, 3, nil)
	for it := 0; it < o.Grow; it++ {
		ring := dilateCross(band, sw, sh, 1, nil)
		grew := false
		for i := range ring {
			if ring[i] && !band[i] && depth(i) >= o.GrowEV {
				band[i], grew = true, true
			}
		}
		if !grew {
			break
		}
	}
	band = dilateCross(band, sw, sh, 2, nil)

	e := &HairEdit{Rect: sel, Ridge: ridge}
	var cpre []float64
	for i, c := range clf {
		if c {
			e.Centre = append(e.Centre, int32((sel.Min.Y+i/sw)*W+sel.Min.X+i%sw))
			cpre = append(cpre, depth(i))
		}
	}
	e.DepthPre = median64(cpre)
	if e.DepthPre < o.MinDepth {
		return nil, fmt.Errorf("no hair-like line found in the selection (strongest line only %.3f EV deep)", e.DepthPre)
	}

	// --- 6. chunks: nearest centreline pixel (8-neighbour BFS) ---
	near8 := make([]int32, sw*sh)
	for i := range near8 {
		near8[i] = -1
	}
	q := make([]int32, 0, sw*sh)
	for i, c := range clf {
		if c {
			near8[i] = int32(i)
			q = append(q, int32(i))
		}
	}
	for h := 0; h < len(q); h++ {
		i := int(q[h])
		x, y := i%sw, i/sw
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				xx, yy := x+dx, y+dy
				if xx < 0 || yy < 0 || xx >= sw || yy >= sh {
					continue
				}
				j := yy*sw + xx
				if near8[j] < 0 {
					near8[j] = near8[i]
					q = append(q, int32(j))
				}
			}
		}
	}
	chunks := map[int][]int{}
	var keys []int
	for i, b := range band {
		if !b {
			continue
		}
		e.Band++
		c := int(near8[i])
		k := (c/sw/hairChunk)*1000 + (c%sw)/hairChunk
		if _, ok := chunks[k]; !ok {
			keys = append(keys, k)
		}
		chunks[k] = append(chunks[k], i)
	}
	sort.Ints(keys)
	inBand := func(gx, gy int) bool {
		x, y := gx-sel.Min.X, gy-sel.Min.Y
		return x >= 0 && y >= 0 && x < sw && y < sh && band[y*sw+x]
	}
	dirs := [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {-1, -1}, {1, -1}, {-1, 1}}
	lg := func(gx, gy, c int) float64 { return hairLog[pix[3*(gy*W+gx)+c]] }
	for _, k := range keys {
		P := chunks[k]
		e.Chunks++
		inP := map[int]bool{}
		for _, i := range P {
			inP[i] = true
		}
		var ring [][2]int // global coords
		seen := map[[2]int]bool{}
		for _, i := range P {
			x, y := sel.Min.X+i%sw, sel.Min.Y+i/sw
			for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				gx, gy := x+d[0], y+d[1]
				if gx < 0 || gy < 0 || gx >= W || gy >= H {
					continue
				}
				lx, ly := gx-sel.Min.X, gy-sel.Min.Y
				if lx >= 0 && ly >= 0 && lx < sw && ly < sh && inP[ly*sw+lx] {
					continue
				}
				if inBand(gx, gy) || seen[[2]int{gx, gy}] {
					continue
				}
				seen[[2]int{gx, gy}] = true
				ring = append(ring, [2]int{gx, gy})
			}
		}
		if len(ring) == 0 {
			e.Skipped++
			continue
		}
		bestE, bx, by := math.Inf(1), 0, 0
		for _, d := range []int{14, 22, 32} {
		dir:
			for _, dr := range dirs {
				ox, oy := dr[0]*d, dr[1]*d
				for _, i := range P {
					sx, sy := sel.Min.X+i%sw+ox, sel.Min.Y+i/sw+oy
					if sx < 0 || sy < 0 || sx >= W || sy >= H || inBand(sx, sy) {
						continue dir
					}
				}
				en := 0.0
				for _, r := range ring {
					sx, sy := r[0]+ox, r[1]+oy
					if sx < 0 || sy < 0 || sx >= W || sy >= H {
						continue dir
					}
					for c := 0; c < 3; c++ {
						dd := lg(r[0], r[1], c) - lg(sx, sy, c)
						en += dd * dd
					}
				}
				if en < bestE {
					bestE, bx, by = en, ox, oy
				}
			}
		}
		if math.IsInf(bestE, 1) {
			e.Skipped++
			continue
		}
		mem := make([][3]float64, len(ring))
		for j, r := range ring {
			for c := 0; c < 3; c++ {
				mem[j][c] = lg(r[0], r[1], c) - lg(r[0]+bx, r[1]+by, c)
			}
		}
		for _, i := range P {
			x, y := sel.Min.X+i%sw, sel.Min.Y+i/sw
			var num [3]float64
			den := 0.0
			for j, r := range ring {
				d2 := float64((r[0]-x)*(r[0]-x) + (r[1]-y)*(r[1]-y))
				w := 1 / (math.Max(d2, 1) * math.Max(d2, 1))
				den += w
				for c := 0; c < 3; c++ {
					num[c] += w * mem[j][c]
				}
			}
			gi := y*W + x
			changed := false
			var nv [3]uint16
			for c := 0; c < 3; c++ {
				v := math.Exp2(num[c]/den+lg(x+bx, y+by, c)) - 1
				v = math.RoundToEven(math.Min(math.Max(v, 0), 65535))
				nv[c] = uint16(v)
				if nv[c] != pix[3*gi+c] {
					changed = true
				}
			}
			if changed {
				e.Idx = append(e.Idx, int32(gi))
				e.Old = append(e.Old, pix[3*gi:3*gi+3]...)
				e.New = append(e.New, nv[:]...)
			}
		}
	}
	if len(e.Idx) == 0 {
		return nil, errors.New("a line was found but no clean source patch next to it - try a larger selection")
	}
	// depth after (on a copy of the luminance patch)
	lf2 := make([]float32, len(lf))
	copy(lf2, lf)
	for k, gi := range e.Idx {
		x, y := int(gi)%W-sel.Min.X+bm, int(gi)/W-sel.Min.Y+bm
		nv := e.New[3*k : 3*k+3]
		lf2[y*lw+x] = float32(math.Log2(float64(nv[0]) + float64(nv[1]) + float64(nv[2]) + 3))
	}
	var cpost []float64
	for i, c := range clf {
		if c {
			cpost = append(cpost, bgAt(lf2, i%sw, i/sw)-smooth(lf2, i%sw, i/sw))
		}
	}
	e.DepthPost = median64(cpost)
	return e, nil
}

// hairSymmetric: a fibre is darker than BOTH sides. Lh is sampled at
// +-hairSide half-res px across the ridge (each the mean of 7 samples along
// it); the darker side must still be at least 40% as far above the centre
// as the brighter one. A shadow's rim (one side plateau, one side bright)
// and the run-out beyond a fibre's end (both sides flat) fail.
func hairSymmetric(lh []float32, pw, x, y int, cx, cy float64) bool {
	at := func(dx, dy float64) float64 {
		s := 0.0
		for t := -3.0; t <= 3; t++ {
			px := int(math.RoundToEven(float64(x) + dx - t*cy))
			py := int(math.RoundToEven(float64(y) + dy + t*cx))
			s += float64(lh[py*pw+px])
		}
		return s / 7
	}
	c := at(0, 0)
	a := at(hairSide*cx, hairSide*cy) - c
	b := at(-hairSide*cx, -hairSide*cy) - c
	lo, hi := math.Min(a, b), math.Max(a, b)
	return lo > 0 && lo >= 0.4*hi
}

func median64(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// selectK returns the k-th smallest value (reorders a).
func selectK(a []float32, k int) float32 {
	lo, hi := 0, len(a)-1
	for lo < hi {
		p := a[(lo+hi)/2]
		i, j := lo, hi
		for i <= j {
			for a[i] < p {
				i++
			}
			for a[j] > p {
				j--
			}
			if i <= j {
				a[i], a[j] = a[j], a[i]
				i++
				j--
			}
		}
		if k <= j {
			hi = j
		} else if k >= i {
			lo = i
		} else {
			return a[k]
		}
	}
	return a[k]
}

func gauss1(s float64, r int) []float64 {
	k := make([]float64, 2*r+1)
	t := 0.0
	for i := -r; i <= r; i++ {
		k[i+r] = math.Exp(-float64(i*i) / (2 * s * s))
		t += k[i+r]
	}
	for i := range k {
		k[i] /= t
	}
	return k
}

// dilateCross dilates m by n iterations of the 3x3 cross (L1 distance <= n).
func dilateCross(m []bool, w, h, n int, _ []bool) []bool {
	cur := append([]bool(nil), m...)
	for it := 0; it < n; it++ {
		nx := append([]bool(nil), cur...)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if cur[y*w+x] {
					continue
				}
				if (x > 0 && cur[y*w+x-1]) || (x < w-1 && cur[y*w+x+1]) || (y > 0 && cur[(y-1)*w+x]) || (y < h-1 && cur[(y+1)*w+x]) {
					nx[y*w+x] = true
				}
			}
		}
		cur = nx
	}
	return cur
}

// label4 labels 4-connected true regions 1..n (0 = background).
func label4(m []bool, w, h int) []int32 {
	lab := make([]int32, w*h)
	var n int32
	var st []int
	for s := range m {
		if !m[s] || lab[s] != 0 {
			continue
		}
		n++
		lab[s] = n
		st = append(st[:0], s)
		for len(st) > 0 {
			i := st[len(st)-1]
			st = st[:len(st)-1]
			x, y := i%w, i/w
			for _, j := range [4]int{i - 1, i + 1, i - w, i + w} {
				if (j == i-1 && x == 0) || (j == i+1 && x == w-1) || (j == i-w && y == 0) || (j == i+w && y == h-1) {
					continue
				}
				if m[j] && lab[j] == 0 {
					lab[j] = n
					st = append(st, j)
				}
			}
		}
	}
	return lab
}

// thinZS is Zhang-Suen thinning.
func thinZS(m []bool, w, h int) []bool {
	img := append([]bool(nil), m...)
	at := func(x, y int) bool { return x >= 0 && y >= 0 && x < w && y < h && img[y*w+x] }
	for {
		changed := false
		for step := 0; step < 2; step++ {
			var del []int
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if !img[y*w+x] {
						continue
					}
					p := [8]bool{at(x, y-1), at(x+1, y-1), at(x+1, y), at(x+1, y+1), at(x, y+1), at(x-1, y+1), at(x-1, y), at(x-1, y-1)}
					b, a := 0, 0
					for k := 0; k < 8; k++ {
						if p[k] {
							b++
						}
						if !p[k] && p[(k+1)%8] {
							a++
						}
					}
					if b < 2 || b > 6 || a != 1 {
						continue
					}
					if step == 0 && (p[0] && p[2] && p[4] || p[2] && p[4] && p[6]) {
						continue
					}
					if step == 1 && (p[0] && p[2] && p[6] || p[0] && p[4] && p[6]) {
						continue
					}
					del = append(del, y*w+x)
				}
			}
			for _, i := range del {
				img[i] = false
			}
			if len(del) > 0 {
				changed = true
			}
		}
		if !changed {
			return img
		}
	}
}
