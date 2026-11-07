package hdrscan

// Fibre stage of the automatic dust removal (v0.79).
//
// The top-hat pass (dust.go) only sees DARK spots and judges a thin line by
// how far it stands out in a 129-px window (HairRules, Out >= 3.0 in
// Light). Two kinds of fibre slipped through on real frames:
//   - a fibre on the film during the scan: a dark, ~0.4 EV, 4-px wide
//     curved line over grain; its top-hat peak is only 1.6 to 2.3 x the
//     window's 99.5th percentile and the hysteresis split it in two, so
//     both pieces were rejected as "texture";
//   - a hair in the camera gate during exposure: it leaves an unexposed
//     line, which on the negative is LIGHTER than its surround (0.77 EV),
//     and the top-hat pass never looks for lighter lines at all.
//
// This stage looks for thin curved ridges of either polarity with a
// Hessian line filter (sigma 1.2 and 2.0 px, scaled with the width), and
// keeps a ridge only when its cross-section is that of a fibre lying on
// the film: sharp (FWHM <= 5 px; image lines the lens drew - face creases,
// edges, wires out of the focal plane - are wider), equal on both sides
// (not an edge or a shadow's rim), well above the grain, much stronger
// than every other ridge around it, not straight (frame lines, scanner
// streaks, wires), not a closed loop, and away from the frame edge, the
// rebate and the holder. A kept fibre is healed like the guided hair tool
// (hair.go): chunks along the centreline, each filled from an offset patch
// of real grain plus a smooth log-ratio membrane matched on its rim.

import (
	"math"
	"sort"
	"sync"
)

type fibreParams struct {
	Sig         [2]float64 // Hessian scales (px)
	Tile        int        // noise tile (px)
	ZLo, ZSeed  float32    // component and seed thresholds (robust sigmas)
	MinArea     int
	MaxArea     int
	MinLen      int     // skeleton px, at least
	MaxThick    float64 // area / skeleton length
	MaxFW       float64 // median full width at half depth across the line (px)
	MinCon      float64 // median depth (EV), at least
	MinCG       float64 // depth / local grain sigma
	MinIso      float64 // median ridge Z / 99.5th pct of ridge Z around it
	MinRatio    float64 // depth / |left side - right side|
	MaxStraight float64 // minor/major spread of the skeleton below this = straight
	Win         int     // context window (px)
	Border      int     // keep this far from the image edge (px)
	Rim         int     // band = component grown by this (px)
	Chunk       int     // fill chunk along the centreline (px)
}

func fibreParamsFor(level, w int) fibreParams {
	s := float64(w) / 6240
	if s < 0.25 {
		s = 0.25
	}
	ri := func(v float64) int { return int(math.Max(1, math.Round(v*s))) }
	p := fibreParams{Sig: [2]float64{1.2 * s, 2.0 * s}, Tile: ri(256), ZLo: 3.5, ZSeed: 7,
		MinArea: int(math.Max(8, 60*s*s)), MaxArea: int(8000 * s * s), MinLen: ri(40),
		MaxThick: math.Max(2, 6*s), MaxFW: math.Max(2.5, 5*s), MinCon: 0.25, MinCG: 4, MinIso: 2,
		MinRatio: 5, MaxStraight: 0.06, Win: ri(64), Border: ri(32), Rim: ri(2), Chunk: ri(16)}
	if p.Sig[0] < 0.8 {
		p.Sig = [2]float64{0.8, 1.4}
	}
	if level == DustStrong {
		p.MinCon, p.MinCG, p.MinIso, p.MinRatio = 0.18, 3.5, 1.8, 4
	}
	return p
}

// gblur is a separable gaussian blur with clamped edges.
func gblur(src []float32, w, h int, sig float64) []float32 {
	r := int(math.Ceil(3 * sig))
	k64 := gauss1(sig, r)
	k := make([]float32, len(k64))
	for i, v := range k64 {
		k[i] = float32(v)
	}
	tmp := make([]float32, len(src))
	out := make([]float32, len(src))
	parallelN(h, func(a, b int) {
		for y := a; y < b; y++ {
			row := src[y*w : (y+1)*w]
			for x := 0; x < w; x++ {
				var s float32
				if x >= r && x < w-r {
					for i, kv := range k {
						s += kv * row[x-r+i]
					}
				} else {
					for i, kv := range k {
						s += kv * row[clampi(x-r+i, 0, w-1)]
					}
				}
				tmp[y*w+x] = s
			}
		}
	})
	parallelN(h, func(a, b int) {
		for y := a; y < b; y++ {
			for x := 0; x < w; x++ {
				var s float32
				for i, kv := range k {
					s += kv * tmp[clampi(y-r+i, 0, h-1)*w+x]
				}
				out[y*w+x] = s
			}
		}
	})
	return out
}

// hess returns the scale-normalised Hessian of g at (x,y) (3x3 Sobel, as
// OpenCV's ksize 3 second derivatives), x and y clamped.
func hess(g []float32, w, h, x, y int, s2 float64) (xx, yy, xy float64) {
	at := func(dx, dy int) float64 { return float64(g[clampi(y+dy, 0, h-1)*w+clampi(x+dx, 0, w-1)]) }
	sm := [3]float64{1, 2, 1}
	for i := -1; i <= 1; i++ {
		xx += sm[i+1] * (at(-1, i) - 2*at(0, i) + at(1, i))
		yy += sm[i+1] * (at(i, -1) - 2*at(i, 0) + at(i, 1))
	}
	xy = at(-1, -1) - at(1, -1) - at(-1, 1) + at(1, 1)
	return xx * s2, yy * s2, xy * s2
}

// removeFibres finds fibres in out (already despeckled) and heals them in
// place. touched marks the changed pixels. It returns the candidates
// judged (debug) and the number of fibres healed.
func removeFibres(out []uint16, w, h, level int, protect func(x, y int) bool, baseL, holderL float32, touched []bool, debug bool) ([]DustSpot, int) {
	fp := fibreParamsFor(level, w)
	n := w * h
	if w < 4*fp.Win || h < 4*fp.Win {
		return nil, 0
	}
	src := make([]uint16, len(out))
	copy(src, out)
	L := make([]float32, n)
	parallelN(h, func(a, b int) {
		for i := a * w; i < b*w; i++ {
			L[i] = float32(math.Log2(float64(src[3*i]) + float64(src[3*i+1]) + float64(src[3*i+2]) + 3))
		}
	})
	// ridge strength per polarity (0 = darker lines, 1 = lighter lines),
	// the larger over the two scales
	var R [2][]float32
	R[0], R[1] = make([]float32, n), make([]float32, n)
	var g12 []float32
	for si, sg := range fp.Sig {
		g := gblur(L, w, h, sg)
		s2 := sg * sg
		parallelN(h, func(a, b int) {
			for y := a; y < b; y++ {
				for x := 0; x < w; x++ {
					xx, yy, xy := hess(g, w, h, x, y, s2)
					t := math.Sqrt((xx-yy)*(xx-yy) + 4*xy*xy)
					l1, l2 := (xx+yy+t)/2, (xx+yy-t)/2
					i := y*w + x
					if l1 > 0 {
						if r := float32(l1 - math.Abs(l2)); r > R[0][i] {
							R[0][i] = r
						}
					}
					if -l2 > 0 {
						if r := float32(-l2 - math.Abs(l1)); r > R[1][i] {
							R[1][i] = r
						}
					}
				}
			}
		})
		if si == 0 {
			g12 = g
		}
	}
	// robust Z per tile (in place)
	tl := fp.Tile
	tw, th := (w+tl-1)/tl, (h+tl-1)/tl
	for pol := 0; pol < 2; pol++ {
		Z := R[pol]
		parallelN(tw*th, func(a, b int) {
			buf := make([]float32, 0, tl*tl/4)
			for t := a; t < b; t++ {
				tx, ty := t%tw, t/tw
				x0, y0, x1, y1 := tx*tl, ty*tl, min((tx+1)*tl, w), min((ty+1)*tl, h)
				buf = buf[:0]
				for y := y0; y < y1; y += 2 {
					for x := x0; x < x1; x += 2 {
						buf = append(buf, Z[y*w+x])
					}
				}
				med := selectK(buf, len(buf)/2)
				for i := range buf {
					buf[i] = float32(math.Abs(float64(buf[i] - med)))
				}
				sig := float32(math.Max(1e-5, 1.4826*float64(selectK(buf, len(buf)/2))))
				for y := y0; y < y1; y++ {
					for x := x0; x < x1; x++ {
						Z[y*w+x] = (Z[y*w+x] - med) / sig
					}
				}
			}
		})
	}

	type comp struct {
		pol            int
		px             []int32
		x0, y0, x1, y1 int
	}
	var infos []DustSpot
	type kept struct {
		c     comp
		skel  []int32
		thick float64
	}
	var keep []kept
	lab := make([]int32, n)
	for pol := 0; pol < 2; pol++ {
		Z := R[pol]
		for i := range lab {
			lab[i] = 0
		}
		var comps []comp
		stack := make([]int32, 0, 4096)
		for i := 0; i < n; i++ {
			if lab[i] != 0 || Z[i] < fp.ZSeed {
				continue
			}
			id := int32(len(comps) + 1)
			x, y := i%w, i/w
			c := comp{pol: pol, x0: x, y0: y, x1: x, y1: y}
			over := false
			lab[i] = id
			stack = append(stack[:0], int32(i))
			for len(stack) > 0 {
				j := int(stack[len(stack)-1])
				stack = stack[:len(stack)-1]
				if !over {
					c.px = append(c.px, int32(j))
					if len(c.px) > fp.MaxArea {
						over, c.px = true, nil
					}
				}
				jx, jy := j%w, j/w
				c.x0, c.x1, c.y0, c.y1 = min(c.x0, jx), max(c.x1, jx), min(c.y0, jy), max(c.y1, jy)
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						qx, qy := jx+dx, jy+dy
						if qx < 0 || qy < 0 || qx >= w || qy >= h {
							continue
						}
						if q := qy*w + qx; lab[q] == 0 && Z[q] >= fp.ZLo {
							lab[q] = id
							stack = append(stack, int32(q))
						}
					}
				}
			}
			if !over && len(c.px) >= fp.MinArea {
				comps = append(comps, c)
			} else {
				comps = append(comps, comp{pol: -1})
			}
		}
		res := make([]DustSpot, len(comps))
		ok := make([]bool, len(comps))
		skels := make([][]int32, len(comps))
		thicks := make([]float64, len(comps))
		parallelN(len(comps), func(a, b int) {
			for k := a; k < b; k++ {
				c := &comps[k]
				if c.pol < 0 {
					continue
				}
				d := &res[k]
				d.X0, d.Y0, d.X1, d.Y1, d.Px = c.x0, c.y0, c.x1, c.y1, len(c.px)
				d.Fibre, d.Light = true, pol == 1
				id := int32(k + 1)
				// local mask with a 1 px pad: skeleton, loops
				bw, bh := c.x1-c.x0+3, c.y1-c.y0+3
				m := make([]bool, bw*bh)
				for _, j := range c.px {
					m[(int(j)/w-c.y0+1)*bw+int(j)%w-c.x0+1] = true
				}
				sk := thinZS(m, bw, bh)
				var skel []int32
				var mx, my float64
				for li, v := range sk {
					if v {
						gx, gy := li%bw+c.x0-1, li/bw+c.y0-1
						skel = append(skel, int32(gy*w+gx))
						mx += float64(gx)
						my += float64(gy)
					}
				}
				ns := len(skel)
				d.Len = float64(ns)
				if ns == 0 {
					d.Why = "shape"
					continue
				}
				d.Thick = float64(len(c.px)) / float64(ns)
				if ns < fp.MinLen {
					d.Why = "short"
					continue
				}
				if d.Thick > fp.MaxThick {
					d.Why = "thick"
					continue
				}
				// straightness: spread of the skeleton across its main axis
				mx, my = mx/float64(ns), my/float64(ns)
				var cxx, cyy, cxy float64
				for _, j := range skel {
					dx, dy := float64(int(j)%w)-mx, float64(int(j)/w)-my
					cxx += dx * dx
					cyy += dy * dy
					cxy += dx * dy
				}
				tr, det := cxx+cyy, cxx*cyy-cxy*cxy
				disc := math.Sqrt(math.Max(0, tr*tr/4-det))
				d.Straight = math.Sqrt(math.Max(0, tr/2-disc) / math.Max(tr/2+disc, 1e-9))
				if d.Straight < fp.MaxStraight {
					d.Why = "straight"
					continue
				}
				// closed loop: background not reachable from the pad
				{
					seen := make([]bool, bw*bh)
					q := []int{0}
					seen[0] = true
					for qi := 0; qi < len(q); qi++ {
						li := q[qi]
						lx, ly := li%bw, li/bw
						for _, o := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
							nx, ny := lx+o[0], ly+o[1]
							if nx < 0 || ny < 0 || nx >= bw || ny >= bh {
								continue
							}
							if ni := ny*bw + nx; !seen[ni] && !m[ni] {
								seen[ni] = true
								q = append(q, ni)
							}
						}
					}
					holes := 0
					for li := range m {
						if !m[li] && !seen[li] {
							holes++
						}
					}
					if holes >= 4 {
						d.Why = "loop"
						continue
					}
				}
				// cross-section along the normal (sigma-1.2 Hessian)
				sgn := float32(1)
				if pol == 1 {
					sgn = -1
				}
				bil := func(x, y float64) float64 {
					x = math.Min(math.Max(x, 0), float64(w-1))
					y = math.Min(math.Max(y, 0), float64(h-1))
					x0, y0 := int(x), int(y)
					x1, y1 := min(x0+1, w-1), min(y0+1, h-1)
					fx, fy := x-float64(x0), y-float64(y0)
					v := (1-fx)*(1-fy)*float64(L[y0*w+x0]) + fx*(1-fy)*float64(L[y0*w+x1]) + (1-fx)*fy*float64(L[y1*w+x0]) + fx*fy*float64(L[y1*w+x1])
					return v * float64(sgn)
				}
				var cons, asyms, fws, zs []float64
				s2 := fp.Sig[0] * fp.Sig[0]
				var prof [17]float64
				for _, j := range skel {
					x, y := int(j)%w, int(j)/w
					xx, yy, xy := hess(g12, w, h, x, y, s2)
					a := 0.5 * math.Atan2(2*xy, xx-yy)
					if pol == 1 {
						a += math.Pi / 2
					}
					nx, ny := math.Cos(a), math.Sin(a)
					for o := -8; o <= 8; o++ {
						prof[o+8] = bil(float64(x)+float64(o)*nx, float64(y)+float64(o)*ny)
					}
					c := math.Min(prof[7], math.Min(prof[8], prof[9]))
					sl := medianOf(append([]float64(nil), prof[0:5]...))
					sr := medianOf(append([]float64(nil), prof[12:17]...))
					side := (sl + sr) / 2
					cons = append(cons, side-c)
					asyms = append(asyms, math.Abs(sl-sr))
					pk := 0.0
					for o := 6; o <= 10; o++ {
						pk = math.Max(pk, side-prof[o])
					}
					f := 0
					for o := 0; o < 17; o++ {
						if side-prof[o] > pk/2 {
							f++
						}
					}
					fws = append(fws, float64(f))
					zs = append(zs, float64(Z[j]))
				}
				con, asym := medianOf(cons), medianOf(asyms)
				d.Peak = con
				d.FW = medianOf(fws)
				d.Ratio = con / math.Max(asym, 1e-3)
				zmed := medianOf(zs)
				d.Z = zmed
				switch {
				case con < fp.MinCon:
					d.Why = "shallow"
				case d.FW > fp.MaxFW:
					d.Why = "soft"
				case d.Ratio < fp.MinRatio:
					d.Why = "edge"
				}
				if d.Why != "" {
					continue
				}
				// isolation and grain in the window, the line (grown by 4) left out
				wx0, wy0 := max(0, c.x0-fp.Win), max(0, c.y0-fp.Win)
				wx1, wy1 := min(w-1, c.x1+fp.Win), min(h-1, c.y1+fp.Win)
				near := func(x, y int) bool {
					for dy := -4; dy <= 4; dy++ {
						for dx := -4; dx <= 4; dx++ {
							qx, qy := x+dx, y+dy
							if qx >= 0 && qy >= 0 && qx < w && qy < h && lab[qy*w+qx] == id {
								return true
							}
						}
					}
					return false
				}
				var zo []float32
				var hp []float64
				for y := wy0; y <= wy1; y++ {
					for x := wx0; x <= wx1; x++ {
						if x >= c.x0-4 && x <= c.x1+4 && y >= c.y0-4 && y <= c.y1+4 && near(x, y) {
							continue
						}
						i := y*w + x
						zo = append(zo, Z[i])
						// grain: L minus its 3x3-ring mean (cheap high-pass)
						if x > 0 && y > 0 && x < w-1 && y < h-1 && (x+y)&1 == 0 {
							s := L[i-w-1] + L[i-w] + L[i-w+1] + L[i-1] + L[i+1] + L[i+w-1] + L[i+w] + L[i+w+1]
							hp = append(hp, float64(L[i]-s/8))
						}
					}
				}
				if len(zo) < 100 || len(hp) < 100 {
					d.Why = "edge of image"
					continue
				}
				p995 := float64(selectK(zo, int(0.995*float64(len(zo)-1))))
				d.Out = zmed / math.Max(p995, 1)
				hm := medianOf(hp)
				for i := range hp {
					hp[i] = math.Abs(hp[i] - hm)
				}
				gs := 1.4826 * medianOf(hp)
				d.CD = con / math.Max(gs, 1e-4) // depth in grain sigmas
				switch {
				case d.CD < fp.MinCG:
					d.Why = "grain"
				case d.Out < fp.MinIso:
					d.Why = "texture"
				}
				if d.Why != "" {
					continue
				}
				// frame edge, rebate (base level), holder, protected region
				if c.x0 < fp.Border || c.y0 < fp.Border || c.x1 >= w-fp.Border || c.y1 >= h-fp.Border {
					d.Why = "frame edge"
					continue
				}
				gb := fp.Win / 4
				eb := 3 * fp.Win
				if baseL > 0 {
					nearEdge := c.x0 < eb || c.y0 < eb || c.x1 >= w-eb || c.y1 >= h-eb
				base:
					for y := max(0, c.y0-gb); y <= min(h-1, c.y1+gb); y++ {
						for x := max(0, c.x0-gb); x <= min(w-1, c.x1+gb); x++ {
							if L[y*w+x] >= baseL && (nearEdge || lab[y*w+x] == id) {
								d.Why = "base level"
								break base
							}
						}
					}
				}
				if d.Why == "" && !math.IsInf(float64(holderL), -1) {
					g := fp.Win / 2
					cnt, tot := 0, 0
					for y := max(0, c.y0-g); y <= min(h-1, c.y1+g); y++ {
						for x := max(0, c.x0-g); x <= min(w-1, c.x1+g); x++ {
							tot++
							if L[y*w+x] < holderL {
								cnt++
							}
						}
					}
					if cnt*8 >= tot {
						d.Why = "holder"
					}
				}
				if d.Why == "" && protect != nil {
					for _, j := range c.px {
						if protect(int(j)%w, int(j)/w) {
							d.Why = "base region"
							break
						}
					}
				}
				if d.Why == "" {
					d.Accepted, d.Hair = true, true
					ok[k] = true
					skels[k] = skel
					thicks[k] = d.Thick
				}
			}
		})
		for k := range comps {
			if comps[k].pol < 0 {
				continue
			}
			if ok[k] {
				keep = append(keep, kept{comps[k], skels[k], thicks[k]})
			}
			if debug || ok[k] {
				infos = append(infos, res[k])
			}
		}
	}
	R[0], R[1], g12 = nil, nil, nil
	if len(keep) == 0 {
		return infos, 0
	}

	// --- heal: band = line grown by Rim, filled chunk by chunk ---
	band := make([]bool, n) // every kept fibre's band: never a source
	for _, k := range keep {
		for _, j := range k.c.px {
			jx, jy := int(j)%w, int(j)/w
			for dy := -fp.Rim; dy <= fp.Rim; dy++ {
				for dx := -fp.Rim; dx <= fp.Rim; dx++ {
					qx, qy := jx+dx, jy+dy
					if qx >= 0 && qy >= 0 && qx < w && qy < h {
						band[qy*w+qx] = true
					}
				}
			}
		}
	}
	type fillPx struct {
		i int
		v [3]uint16
	}
	fills := make([][]fillPx, len(keep))
	var mu sync.Mutex
	healed := 0
	lgv := func(i, c int) float64 { return math.Log2(float64(src[3*i+c]) + 1) }
	parallelN(len(keep), func(a, b int) {
		for ki := a; ki < b; ki++ {
			k := keep[ki]
			c := k.c
			// this fibre's band (sorted) and nearest skeleton pixel (BFS)
			bx0, by0 := max(0, c.x0-fp.Rim-1), max(0, c.y0-fp.Rim-1)
			bx1, by1 := min(w-1, c.x1+fp.Rim+1), min(h-1, c.y1+fp.Rim+1)
			bw, bh := bx1-bx0+1, by1-by0+1
			own := make([]bool, bw*bh)
			for _, j := range c.px {
				jx, jy := int(j)%w, int(j)/w
				for dy := -fp.Rim; dy <= fp.Rim; dy++ {
					for dx := -fp.Rim; dx <= fp.Rim; dx++ {
						qx, qy := jx+dx, jy+dy
						if qx >= bx0 && qy >= by0 && qx <= bx1 && qy <= by1 {
							own[(qy-by0)*bw+qx-bx0] = true
						}
					}
				}
			}
			nearest := make([]int32, bw*bh)
			for i := range nearest {
				nearest[i] = -1
			}
			var q []int
			for _, j := range k.skel {
				li := (int(j)/w-by0)*bw + int(j)%w - bx0
				nearest[li] = j
				q = append(q, li)
			}
			for qi := 0; qi < len(q); qi++ {
				li := q[qi]
				lx, ly := li%bw, li/bw
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						nx, ny := lx+dx, ly+dy
						if nx < 0 || ny < 0 || nx >= bw || ny >= bh {
							continue
						}
						if ni := ny*bw + nx; own[ni] && nearest[ni] < 0 {
							nearest[ni] = nearest[li]
							q = append(q, ni)
						}
					}
				}
			}
			chunks := map[int][]int{}
			var keys []int
			for li, v := range own {
				if !v || nearest[li] < 0 {
					continue
				}
				gi := (li/bw+by0)*w + li%bw + bx0
				if (baseL > 0 && L[gi] >= baseL) || (protect != nil && protect(gi%w, gi/w)) {
					continue
				}
				s := int(nearest[li])
				key := (s/w/fp.Chunk)*100000 + (s%w)/fp.Chunk
				if _, ok := chunks[key]; !ok {
					keys = append(keys, key)
				}
				chunks[key] = append(chunks[key], gi)
			}
			sort.Ints(keys)
			t := int(math.Ceil(k.thick)) + 2*fp.Rim
			dists := []int{t + 3, 2*t + 4, 3*t + 6}
			var fl []fillPx
			for _, key := range keys {
				P := chunks[key]
				inP := map[int]bool{}
				for _, i := range P {
					inP[i] = true
				}
				var ring []int
				seen := map[int]bool{}
				for _, i := range P {
					x, y := i%w, i/w
					for _, o := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
						gx, gy := x+o[0], y+o[1]
						if gx < 0 || gy < 0 || gx >= w || gy >= h {
							continue
						}
						g := gy*w + gx
						if inP[g] || band[g] || seen[g] {
							continue
						}
						seen[g] = true
						ring = append(ring, g)
					}
				}
				if len(ring) < 4 {
					continue
				}
				sort.Ints(ring)
				bestE, bx, by := math.Inf(1), 0, 0
				for _, dd := range dists {
				dir:
					for _, o := range [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {-1, -1}, {1, -1}, {-1, 1}} {
						ox, oy := o[0]*dd, o[1]*dd
						for _, i := range P {
							sx, sy := i%w+ox, i/w+oy
							if sx < 0 || sy < 0 || sx >= w || sy >= h || band[sy*w+sx] {
								continue dir
							}
						}
						e := 0.0
						for _, r := range ring {
							sx, sy := r%w+ox, r/w+oy
							if sx < 0 || sy < 0 || sx >= w || sy >= h {
								continue dir
							}
							for ch := 0; ch < 3; ch++ {
								dv := lgv(r, ch) - lgv(sy*w+sx, ch)
								e += dv * dv
							}
						}
						if e < bestE {
							bestE, bx, by = e, ox, oy
						}
					}
				}
				if math.IsInf(bestE, 1) {
					continue
				}
				off := by*w + bx
				mem := make([][3]float64, len(ring))
				for j, r := range ring {
					for ch := 0; ch < 3; ch++ {
						mem[j][ch] = lgv(r, ch) - lgv(r+off, ch)
					}
				}
				sort.Ints(P)
				for _, i := range P {
					x, y := i%w, i/w
					var num [3]float64
					den := 0.0
					for j, r := range ring {
						d2 := float64((r%w-x)*(r%w-x) + (r/w-y)*(r/w-y))
						wt := 1 / (math.Max(d2, 1) * math.Max(d2, 1))
						den += wt
						for ch := 0; ch < 3; ch++ {
							num[ch] += wt * mem[j][ch]
						}
					}
					var nv [3]uint16
					for ch := 0; ch < 3; ch++ {
						v := math.Exp2(num[ch]/den+lgv(i+off, ch)) - 1
						nv[ch] = uint16(math.RoundToEven(math.Min(math.Max(v, 0), 65535)))
					}
					fl = append(fl, fillPx{i, nv})
				}
			}
			fills[ki] = fl
			if len(fl) > 0 {
				mu.Lock()
				healed++
				mu.Unlock()
			}
		}
	})
	for _, fl := range fills {
		for _, f := range fl {
			out[3*f.i], out[3*f.i+1], out[3*f.i+2] = f.v[0], f.v[1], f.v[2]
			touched[f.i] = true
		}
	}
	return infos, healed
}
