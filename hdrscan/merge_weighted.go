package hdrscan

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"
	"sync"
	"time"

	"basetool/rawconv"
)

// Merge methods (Options.Method).
const (
	MergeClassic  = 0 // hard clip masks + linear ramp on demosaiced frames (default up to v0.73)
	MergeWeighted = 1 // noise-optimal weights; RAW frames are merged on the sensor mosaic (default since v0.74)
)

// MethodName returns the UI name of a merge method.
func MethodName(m int) string {
	if m == MergeWeighted {
		return "Weighted"
	}
	return "Classic"
}

// Base protection band of the Weighted merge, as fractions of frame 0's
// base level: at or above 0.85·L the output is frame 0 exactly (so the
// clear base still equals frame 0), below 0.70·L it is the merge. Classic
// uses 0.35-0.50 because its masks cannot use the brighter frames there
// anyway; the sensor-mosaic merge can (e.g. blue photosites of a +2 frame
// in orange mid-density areas), so the band is narrower.
const weightedProtectLo, weightedProtectHi = 0.70, 0.85

// wStep is a brighter frame accepted by MergeN's ratio stage.
type wStep struct {
	k     int     // frame index
	ratio float64 // classic ratio to frame 0
	st    *Stats
}

// mergeWeighted is MergeN's Weighted path. It runs after the classic ratio
// stage (steps carry those ratios and Stats) and shares the frame-0 base
// protection: pixels at or above pHi keep frame 0 exactly.
//
// With RAW input it merges on the sensor mosaic (see mergeRaw) and
// demosaics once. Otherwise, or when Align=Correct finds a sub-pixel shift
// of 0.25 px or more, it blends the demosaiced frames (see mergeRGB).
func mergeWeighted(frames []*Plane, steps []wStep, o Options, T, L, pLo, pHi float64) ([]uint16, error) {
	f0 := frames[0]
	pLo, pHi = weightedProtectLo*L, weightedProtectHi*L
	// Sub-pixel shifts of each brighter frame against frame 0.
	shifts := make([][2]float64, len(steps))
	needResample := false
	if o.Align != AlignOff {
		for i, s := range steps {
			t0 := time.Now()
			dx, dy, gain, ok := subpixelShift(f0, frames[s.k], s.ratio, o.ClipT, 4)
			if !ok {
				s.st.addDetail("sub-pixel alignment: not measurable")
				continue
			}
			shifts[i] = [2]float64{dx, dy}
			s.st.Shift = image.Pt(int(math.Round(dx)), int(math.Round(dy)))
			s.st.ShiftGain = gain
			s.st.addDetail(fmt.Sprintf("sub-pixel shift (%.2f, %.2f) px", dx, dy))
			big := math.Hypot(dx, dy) >= 0.25
			if big && o.Align == AlignCorrect {
				needResample = true
				s.st.ShiftApplied = true
			} else if big {
				s.st.Warning = appendNote(s.st.Warning, fmt.Sprintf("frame moved by (%.2f, %.2f) px; set Align to Correct", dx, dy))
			}
			s.st.Elapsed += time.Since(t0)
		}
	}
	if !needResample {
		paths := make([]string, 0, len(steps)+1)
		paths = append(paths, f0.Source)
		for _, s := range steps {
			paths = append(paths, frames[s.k].Source)
		}
		allRaw := true
		for _, p := range paths {
			if p == "" || !rawconv.IsRaw(p) {
				allRaw = false
			}
		}
		if allRaw {
			out, err := mergeRaw(f0, paths, steps, L, pLo, pHi)
			if err == nil {
				return out, nil
			}
			for _, s := range steps {
				s.st.Warning = appendNote(s.st.Warning, "sensor-mosaic merge failed ("+err.Error()+"), used the RGB weighted merge")
			}
		}
	}
	return mergeRGB(frames, steps, shifts, o, T, L, pLo, pHi), nil
}

func appendNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

func (s *Stats) addDetail(d string) { s.Detail = appendNote(s.Detail, d) }

// noiseC0 is the read-noise term of the variance model var(x) ∝ x + c0 in
// sensor DN (gain × read noise², ≈ 1.35 e-/DN × (2 DN)² for an X-T3 at base
// ISO). It only matters in the deepest shadows of a frame.
const noiseC0 = 5.5

// smoothHat is 1 below lo, 0 above hi and a smoothstep in between.
func smoothHat(v, lo, hi float64) float64 {
	if v <= lo {
		return 1
	}
	if v >= hi {
		return 0
	}
	t := (v - lo) / (hi - lo)
	return 1 - t*t*(3-2*t)
}

// mergeRaw merges RAW frames photosite by photosite and demosaics the
// merged mosaic once with frame 0's LibRaw settings:
//
//   - the ratio of each frame to the merged darker frames is measured per
//     CFA colour on 6×6 blocks well below clipping (ratio of sums);
//   - each photosite gets the inverse-variance (shot + read noise) weight
//     ratio²/(ratio·x̂+c0), times a saturation hat driven by the value
//     predicted from the darker frames (not by the frame's own noisy value),
//     and zero at the white level;
//   - a photosite that disagrees with the darker estimate by more than
//     5–7σ (moved dust, a shifted frame) is faded out (ghost rejection).
//
// No demosaic halo exists on the mosaic, so no dilation is needed and
// every unclipped photosite of a brighter frame is used.
func mergeRaw(f0 *Plane, paths []string, steps []wStep, L, pLo, pHi float64) ([]uint16, error) {
	t0 := time.Now()
	r0, err := rawconv.ReadRawFrame(paths[0])
	if err != nil {
		return nil, err
	}
	W, H := r0.Width, r0.Height
	if W != f0.W || H != f0.H {
		return nil, fmt.Errorf("sensor area %dx%d differs from the image %dx%d", W, H, f0.W, f0.H)
	}
	R := float64(r0.Maximum - r0.Black)
	if R < 64 {
		return nil, errors.New("bad white level")
	}
	white := r0.Maximum
	clipAt := float64(r0.Black) + 0.97*R
	np := W * H
	est := make([]float32, np) // merged signal, frame-0 DN above black
	vr := make([]float32, np)  // its variance (× gain)
	ch := make([]uint8, np)    // colour 0..2
	parallelRows(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := r0.Raw[(y+r0.Top)*r0.RawWidth+r0.Left:]
			for x := 0; x < W; x++ {
				i := y*W + x
				c := r0.Color[i]
				if c == 3 {
					c = 1
				}
				ch[i] = c
				est[i] = float32(int(row[x]) - int(r0.BlackAt[i]))
			}
		}
	})
	// Weights and the saturation hat use a same-colour local mean of the
	// estimate, not the photosite's own noisy value: weights that depend on
	// the noise of the values they weight bias the result.
	sm := sameColourMean(est, ch, W, H, 3)
	for i := range vr {
		vr[i] = float32(math.Max(float64(sm[i]), 0) + noiseC0)
	}
	blackAt := r0.BlackAt
	rawRef := r0.Raw
	r0.Raw = nil
	used := make([]float64, len(steps))
	for si, s := range steps {
		ts := time.Now()
		rk, err := rawconv.ReadRawFrame(paths[si+1])
		if err != nil {
			return nil, err
		}
		if rk.RawWidth != r0.RawWidth || rk.RawHeight != r0.RawHeight || rk.Width != W || rk.Height != H || rk.Top != r0.Top || rk.Left != r0.Left || rk.Maximum != r0.Maximum {
			return nil, errors.New("frames have different sensor layouts")
		}
		for i := 0; i < np; i += 997 {
			if rk.Color[i] != r0.Color[i] || rk.BlackAt[i] != blackAt[i] {
				return nil, errors.New("frames have different CFA or black levels")
			}
		}
		rk.Color, rk.BlackAt = nil, nil
		// Per-colour ratio against the current estimate.
		rho, nb := rawRatios(rk, est, ch, blackAt, s.ratio, R)
		s.st.addDetail(fmt.Sprintf("sensor ratio R %.4f G %.4f B %.4f (%d blocks)", rho[0], rho[1], rho[2], nb))
		// Accept the sensor ratios when all three were measured, lie within
		// 0.8-1.25x of the RGB-stage ratio and agree within 5 %.
		// A colour that is clipped everywhere (e.g. R and G in a +3 frame
		// of a negative) takes the mean of the measured colours: the
		// exposure ratio is the same for all of them.
		good := true
		lo, hi, sum, nm := math.Inf(1), 0.0, 0.0, 0
		for c := 0; c < 3; c++ {
			if rho[c] <= 0 {
				continue
			}
			if rho[c] < 0.8*s.ratio || rho[c] > 1.25*s.ratio {
				good = false
			}
			lo, hi = math.Min(lo, rho[c]), math.Max(hi, rho[c])
			sum += rho[c]
			nm++
		}
		if good && nm > 0 && nm < 3 {
			for c := 0; c < 3; c++ {
				if rho[c] <= 0 {
					rho[c] = sum / float64(nm)
				}
			}
			s.st.addDetail(fmt.Sprintf("%d of 3 colours measurable on the sensor, used their ratio %.4f for the others", nm, sum/float64(nm)))
		}
		if !good || nm == 0 || hi > 1.05*lo {
			s.st.addDetail(fmt.Sprintf("sensor ratios not usable, used the RGB-stage ratio %.4f", s.ratio))
			rho = [3]float64{s.ratio, s.ratio, s.ratio}
		}
		// Noise scale: robust spread of the normalised difference on
		// photosites well inside both frames' ranges.
		if si > 0 {
			sm = sameColourMean(est, ch, W, H, 3)
		}
		gainInv := noiseScale(rk, est, vr, ch, blackAt, rho, R)
		var mu sync.Mutex
		var nUsed, nRej, nFull, nPart int64
		var wSum float64
		parallelRows(H, func(y0, y1 int) {
			var u, rj, fu, pa int64
			var ws float64
			for y := y0; y < y1; y++ {
				row := rk.Raw[(y+rk.Top)*rk.RawWidth+rk.Left:]
				for x := 0; x < W; x++ {
					i := y*W + x
					rv := float64(row[x])
					if rv >= clipAt {
						continue
					}
					r := rho[ch[i]]
					e := float64(est[i])
					pred := r * math.Max(float64(sm[i]), 0)
					h := smoothHat(pred/R, 0.80, 0.92)
					if h <= 0 {
						continue
					}
					xk := (rv - float64(blackAt[i])) / r
					vk := (pred + noiseC0) / (r * r)
					v0 := float64(vr[i])
					d := xk - e
					z := math.Abs(d) / math.Sqrt((v0+vk)*gainInv)
					if z > 5 {
						rj++
						h *= smoothHat(z, 5, 7)
						if h <= 0 {
							continue
						}
					}
					wk := h / vk
					w0 := 1 / v0
					f := wk / (w0 + wk)
					est[i] = float32(e + f*d)
					vr[i] = float32(1 / (w0 + wk))
					u++
					ws += f
					if f >= 0.5 {
						fu++
					} else {
						pa++
					}
				}
			}
			mu.Lock()
			nUsed += u
			nRej += rj
			nFull += fu
			nPart += pa
			wSum += ws
			mu.Unlock()
		})
		rk.Raw = nil
		used[si] = 100 * float64(nUsed) / float64(np)
		s.st.FromPlus2Pct = 100 * float64(nFull) / float64(np)
		s.st.BlendPct = 100 * float64(nPart) / float64(np)
		s.st.addDetail(fmt.Sprintf("used on %.2f%% of photosites (mean weight %.3f), ghost-rejected %.3f%%, noise scale %.3f",
			used[si], wSum/math.Max(float64(nUsed), 1), 100*float64(nRej)/float64(np), math.Sqrt(gainInv)))
		s.st.Elapsed += time.Since(ts)
	}
	// Write the merged mosaic at 4× precision and demosaic it once.
	td := time.Now()
	const K = 4
	mos := rawRef
	parallelRows(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := mos[(y+r0.Top)*r0.RawWidth+r0.Left:]
			for x := 0; x < W; x++ {
				i := y*W + x
				v := float64(blackAt[i]) + K*float64(est[i])
				if v < 0 {
					v = 0
				} else if v > 65535 {
					v = 65535
				}
				row[x] = uint16(v + 0.5)
			}
		}
	})
	est, vr, sm = nil, nil, nil
	img, err := rawconv.DecodeMosaic(paths[0], mos, r0.RawWidth, r0.RawHeight, r0.Black+K*(white-r0.Black))
	if err != nil {
		return nil, err
	}
	out := make([]uint16, len(img.Pix))
	copy(out, img.Pix)
	img.Free()
	if len(out) != len(f0.Pix) {
		return nil, errors.New("demosaiced size differs from frame 0")
	}
	prot := protectBase(out, f0.Pix, W, H, L, pLo, pHi)
	last := steps[len(steps)-1].st
	for _, s := range steps {
		s.st.ProtectPct = prot
		s.st.Method += " + weighted sensor-mosaic merge"
	}
	last.addDetail(fmt.Sprintf("merged mosaic demosaiced in %.1fs (total weighted %.1fs)", time.Since(td).Seconds(), time.Since(t0).Seconds()))
	last.Elapsed += time.Since(td)
	return out, nil
}

// sameColourMean returns, for every photosite, the mean of the photosites
// of its colour in the (2r+1)² window around it.
func sameColourMean(v []float32, ch []uint8, W, H, r int) []float32 {
	out := make([]float32, len(v))
	sum := make([]float32, len(v))
	cnt := make([]float32, len(v))
	for c := uint8(0); c < 3; c++ {
		// Horizontal pass.
		parallelRows(H, func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				o := y * W
				var s, n float32
				for x := 0; x < min(r, W); x++ {
					if ch[o+x] == c {
						s += v[o+x]
						n++
					}
				}
				for x := 0; x < W; x++ {
					if a := x + r; a < W && ch[o+a] == c {
						s += v[o+a]
						n++
					}
					if b := x - r - 1; b >= 0 && ch[o+b] == c {
						s -= v[o+b]
						n--
					}
					sum[o+x], cnt[o+x] = s, n
				}
			}
		})
		// Vertical pass, written only where the photosite has colour c.
		parallelRows(W, func(x0, x1 int) {
			for x := x0; x < x1; x++ {
				var s, n float32
				for y := 0; y < min(r, H); y++ {
					s += sum[y*W+x]
					n += cnt[y*W+x]
				}
				for y := 0; y < H; y++ {
					if a := y + r; a < H {
						s += sum[a*W+x]
						n += cnt[a*W+x]
					}
					if b := y - r - 1; b >= 0 {
						s -= sum[b*W+x]
						n -= cnt[b*W+x]
					}
					if i := y*W + x; ch[i] == c && n > 0 {
						out[i] = s / n
					}
				}
			}
		})
	}
	return out
}

// rawRatios measures rk/est per colour on 6×6 blocks that are far from
// clipping in rk (selected by the darker estimate, not by rk's own noise).
func rawRatios(rk *rawconv.RawFrame, est []float32, ch []uint8, blackAt []uint16, guess, R float64) (rho [3]float64, blocks int) {
	W, H := rk.Width, rk.Height
	const B = 6
	type blk struct{ a, b float64 }
	var all [3][]blk
	var mu sync.Mutex
	parallelRows(H/B, func(by0, by1 int) {
		var loc [3][]blk
		for by := by0; by < by1; by++ {
			for bx := 0; bx < W/B; bx++ {
				var a, b [3]float64
				var n [3]int
				var bad [3]bool
				for yy := 0; yy < B; yy++ {
					y := by*B + yy
					row := rk.Raw[(y+rk.Top)*rk.RawWidth+rk.Left:]
					for xx := 0; xx < B; xx++ {
						x := bx*B + xx
						i := y*W + x
						c := ch[i]
						e := float64(est[i])
						if guess*e > 0.70*R {
							bad[c] = true
						}
						a[c] += e
						b[c] += float64(int(row[x]) - int(blackAt[i]))
						n[c]++
					}
				}
				for c := 0; c < 3; c++ {
					if n[c] == 0 || bad[c] {
						continue
					}
					m := a[c] / float64(n[c])
					if m*guess < 0.03*R || m < 20 {
						continue
					}
					loc[c] = append(loc[c], blk{a[c], b[c]})
				}
			}
		}
		mu.Lock()
		for c := 0; c < 3; c++ {
			all[c] = append(all[c], loc[c]...)
		}
		mu.Unlock()
	})
	for c := 0; c < 3; c++ {
		bl := all[c]
		if len(bl) < 300 {
			continue
		}
		rs := make([]float64, len(bl))
		for i, x := range bl {
			rs[i] = x.b / x.a
		}
		sort.Float64s(rs)
		med := rs[len(rs)/2]
		var sa, sb float64
		for _, x := range bl {
			if r := x.b / x.a; r > 0.92*med && r < 1.08*med {
				sa += x.a
				sb += x.b
			}
		}
		if sa > 0 {
			rho[c] = sb / sa
			blocks += len(bl)
		}
	}
	return
}

// noiseScale returns the variance multiplier (1/gain) that makes the
// normalised differences between rk and the estimate unit-variance, from
// their median absolute deviation. Falls back to 1/1.0 when too few
// photosites overlap.
func noiseScale(rk *rawconv.RawFrame, est, vr []float32, ch []uint8, blackAt []uint16, rho [3]float64, R float64) float64 {
	W, H := rk.Width, rk.Height
	var zs []float64
	for y := 0; y < H; y += 7 {
		row := rk.Raw[(y+rk.Top)*rk.RawWidth+rk.Left:]
		for x := y % 5; x < W; x += 11 {
			i := y*W + x
			r := rho[ch[i]]
			e := float64(est[i])
			pred := r * e
			if pred < 0.02*R || pred > 0.75*R {
				continue
			}
			xk := (float64(row[x]) - float64(blackAt[i])) / r
			vk := (pred + noiseC0) / (r * r)
			zs = append(zs, math.Abs(xk-e)/math.Sqrt(float64(vr[i])+vk))
		}
	}
	if len(zs) < 2000 {
		return 1
	}
	sort.Float64s(zs)
	s := 1.4826 * zs[len(zs)/2]
	if s < 0.2 || s > 5 {
		return 1
	}
	return s * s
}

// protectBase makes pixels near frame 0's base level equal frame 0, with
// the same ramp as the classic merge: weight of the merged data is
// (pHi-m0)/(pHi-pLo) between pLo and pHi of frame 0's brightest channel.
// Returns the percentage of pixels affected.
func protectBase(out, f0 []uint16, W, H int, L, pLo, pHi float64) float64 {
	if L <= 0 {
		return 0
	}
	var mu sync.Mutex
	var cnt int64
	parallelRows(H, func(y0, y1 int) {
		var p int64
		for i := y0 * W; i < y1*W; i++ {
			m0 := float64(max3(f0[3*i], f0[3*i+1], f0[3*i+2]))
			if m0 <= pLo {
				continue
			}
			p++
			if m0 >= pHi {
				copy(out[3*i:3*i+3], f0[3*i:3*i+3])
				continue
			}
			wt := (pHi - m0) / (pHi - pLo)
			for c := 0; c < 3; c++ {
				a := float64(f0[3*i+c])
				v := a + wt*(float64(out[3*i+c])-a)
				out[3*i+c] = uint16(math.Min(math.Max(v+0.5, 0), 65535))
			}
		}
		mu.Lock()
		cnt += p
		mu.Unlock()
	})
	return 100 * float64(cnt) / float64(W*H)
}

// mergeRGB is the Weighted merge on demosaiced frames (TIFF input or
// shifted frames). Demosaic spreads clipping into neighbouring pixels, so
// like Classic it only uses pixels whose brightest channel is below T, but
// the mask edge is feathered (no seams), weights are inverse-variance, and
// shifts are applied with Catmull-Rom sub-pixel resampling.
func mergeRGB(frames []*Plane, steps []wStep, shifts [][2]float64, o Options, T, L, pLo, pHi float64) []uint16 {
	f0 := frames[0]
	W, H := f0.W, f0.H
	np := W * H
	const R = 3
	// 16-bit units: c0 scaled by 65535/(white-black) ≈ 4.27 for a 14-bit raw.
	c0 := noiseC0 * 4.27
	est := make([]float32, np*3)
	vr := make([]float32, np*3)
	for i := 0; i < np*3; i++ {
		v := float64(f0.Pix[i])
		est[i] = float32(v)
		vr[i] = float32(v + c0)
	}
	rampLo := T - o.Ramp*T
	for si, s := range steps {
		ts := time.Now()
		fk := frames[s.k]
		sh := shifts[si]
		if !s.st.ShiftApplied {
			sh = [2]float64{}
		}
		lim := uint16(T)
		clip := make([]uint8, np)
		for i := 0; i < np; i++ {
			if max3(fk.Pix[3*i], fk.Pix[3*i+1], fk.Pix[3*i+2]) >= lim {
				clip[i] = 1
			}
		}
		m := dilate(clip, W, H, o.Dilate+R)
		feather := boxBlurInv(m, W, H, R)
		m, clip = nil, nil
		ratio := s.ratio
		var mu sync.Mutex
		var nFull, nPart int64
		parallelRows(H, func(y0, y1 int) {
			var fu, pa int64
			var px [3]float64
			for y := y0; y < y1; y++ {
				for x := 0; x < W; x++ {
					i := y*W + x
					sx, sy := float64(x)+sh[0], float64(y)+sh[1]
					ix, iy := int(math.Round(sx)), int(math.Round(sy))
					if ix < 0 || iy < 0 || ix >= W || iy >= H {
						continue
					}
					fw := float64(feather[iy*W+ix])
					if fw <= 0 {
						continue
					}
					if sh == [2]float64{} {
						for c := 0; c < 3; c++ {
							px[c] = float64(fk.Pix[3*i+c])
						}
					} else {
						catmullRom(fk, sx, sy, &px)
					}
					m2 := math.Max(px[0], math.Max(px[1], px[2]))
					h := fw
					if m2 >= T {
						continue
					} else if m2 > rampLo {
						h *= (T - m2) / (T - rampLo)
					}
					full := true
					for c := 0; c < 3; c++ {
						e := float64(est[3*i+c])
						pred := ratio * math.Max(e, 0)
						vk := (pred + c0) / (ratio * ratio)
						v0 := float64(vr[3*i+c])
						wk := h / vk
						w0 := 1 / v0
						f := wk / (w0 + wk)
						est[3*i+c] = float32(e + f*(px[c]/ratio-e))
						vr[3*i+c] = float32(1 / (w0 + wk))
						if h < 0.999 {
							full = false
						}
					}
					if full {
						fu++
					} else {
						pa++
					}
				}
			}
			mu.Lock()
			nFull += fu
			nPart += pa
			mu.Unlock()
		})
		s.st.FromPlus2Pct = 100 * float64(nFull) / float64(np)
		s.st.BlendPct = 100 * float64(nPart) / float64(np)
		s.st.Method += " + weighted RGB merge"
		s.st.Elapsed += time.Since(ts)
	}
	out := make([]uint16, np*3)
	for i := range out {
		out[i] = uint16(math.Min(math.Max(float64(est[i])+0.5, 0), 65535))
	}
	prot := protectBase(out, f0.Pix, W, H, L, pLo, pHi)
	for _, s := range steps {
		s.st.ProtectPct = prot
	}
	return out
}

// boxBlurInv returns the (2r+1)² box mean of 1-m.
func boxBlurInv(m []uint8, W, H, r int) []float32 {
	tmp := make([]float32, W*H)
	parallelRows(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				s := 0
				for d := -r; d <= r; d++ {
					xx := min(max(x+d, 0), W-1)
					s += 1 - int(m[y*W+xx])
				}
				tmp[y*W+x] = float32(s)
			}
		}
	})
	out := make([]float32, W*H)
	n := float32((2*r + 1) * (2*r + 1))
	parallelRows(H, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < W; x++ {
				var s float32
				for d := -r; d <= r; d++ {
					yy := min(max(y+d, 0), H-1)
					s += tmp[yy*W+x]
				}
				out[y*W+x] = s / n
			}
		}
	})
	return out
}

func crWeights(t float64) [4]float64 {
	t2, t3 := t*t, t*t*t
	return [4]float64{
		-0.5*t3 + t2 - 0.5*t,
		1.5*t3 - 2.5*t2 + 1,
		-1.5*t3 + 2*t2 + 0.5*t,
		0.5*t3 - 0.5*t2,
	}
}

// catmullRom samples p at (x, y) with a Catmull-Rom kernel (edges clamped).
func catmullRom(p *Plane, x, y float64, out *[3]float64) {
	x0, y0 := math.Floor(x), math.Floor(y)
	wx, wy := crWeights(x-x0), crWeights(y-y0)
	ix, iy := int(x0), int(y0)
	*out = [3]float64{}
	for j := 0; j < 4; j++ {
		yy := min(max(iy-1+j, 0), p.H-1)
		for i := 0; i < 4; i++ {
			xx := min(max(ix-1+i, 0), p.W-1)
			w := wx[i] * wy[j]
			o := (yy*p.W + xx) * 3
			out[0] += w * float64(p.Pix[o])
			out[1] += w * float64(p.Pix[o+1])
			out[2] += w * float64(p.Pix[o+2])
		}
	}
}

// subpixelShift estimates where frame fk's content lies relative to f0
// (fk(x+d) ≈ ratio·f0(x)): integer SSD search ±r, then Lucas-Kanade
// refinement, on a central 1024² crop of the channel with the most
// unclipped pixels in fk. ok is false when too few pixels overlap.
func subpixelShift(f0, fk *Plane, ratio, clipT float64, r int) (dx, dy, gain float64, ok bool) {
	W, H := f0.W, f0.H
	cw, chh := min(1024, W-8*r), min(1024, H-8*r)
	if cw < 64 || chh < 64 {
		return 0, 0, -1, false
	}
	x0, y0 := (W-cw)/2, (H-chh)/2
	lim := clipT * 65535
	floor := 0.01 * 65535
	// Pick the channel.
	best, bestN := 0, -1
	for c := 0; c < 3; c++ {
		n := 0
		for y := y0; y < y0+chh; y += 4 {
			for x := x0; x < x0+cw; x += 4 {
				i := (y*W + x) * 3
				if float64(fk.Pix[i+c]) < lim && float64(f0.Pix[i+c]) > floor {
					n++
				}
			}
		}
		if n > bestN {
			best, bestN = c, n
		}
	}
	c := best
	if bestN < 2000 {
		return 0, 0, -1, false
	}
	inv := 1 / ratio
	valid := func(x, y int) bool {
		for yy := y - 2; yy <= y+2; yy++ {
			for xx := x - 2; xx <= x+2; xx += 2 {
				if float64(fk.Pix[(yy*W+xx)*3+c]) >= lim {
					return false
				}
			}
		}
		return true
	}
	ssd := func(sx, sy int) float64 {
		var s float64
		n := 0
		for y := y0; y < y0+chh; y += 2 {
			for x := x0; x < x0+cw; x += 2 {
				a := float64(f0.Pix[(y*W+x)*3+c])
				if a < floor || !valid(x+sx, y+sy) {
					continue
				}
				d := float64(fk.Pix[((y+sy)*W+x+sx)*3+c])*inv - a
				s += d * d
				n++
			}
		}
		if n < 1000 {
			return math.Inf(1)
		}
		return s / float64(n)
	}
	type res struct {
		dx, dy int
		v      float64
	}
	rc := make(chan res)
	for sy := -r; sy <= r; sy++ {
		for sx := -r; sx <= r; sx++ {
			go func(sx, sy int) { rc <- res{sx, sy, ssd(sx, sy)} }(sx, sy)
		}
	}
	bi := res{v: math.Inf(1)}
	var zero float64
	for k := 0; k < (2*r+1)*(2*r+1); k++ {
		x := <-rc
		if x.dx == 0 && x.dy == 0 {
			zero = x.v
		}
		if x.v < bi.v {
			bi = x
		}
	}
	if math.IsInf(bi.v, 1) {
		return 0, 0, -1, false
	}
	dx, dy = float64(bi.dx), float64(bi.dy)
	// Lucas-Kanade (inverse compositional on f0's gradient).
	sample := func(x, y float64) float64 {
		xf, yf := math.Floor(x), math.Floor(y)
		ix, iy := int(xf), int(yf)
		tx, ty := x-xf, y-yf
		g := func(xx, yy int) float64 { return float64(fk.Pix[(yy*W+xx)*3+c]) }
		return (1-ty)*((1-tx)*g(ix, iy)+tx*g(ix+1, iy)) + ty*((1-tx)*g(ix, iy+1)+tx*g(ix+1, iy+1))
	}
	var lastE float64
	for it := 0; it < 8; it++ {
		var a11, a12, a22, b1, b2, se float64
		n := 0
		idx, idy := int(math.Floor(dx)), int(math.Floor(dy))
		for y := y0; y < y0+chh; y++ {
			for x := x0; x < x0+cw; x++ {
				i := (y*W + x) * 3
				a := float64(f0.Pix[i+c])
				if a < floor || !valid(x+idx, y+idy) || !valid(x+idx+1, y+idy+1) {
					continue
				}
				gx := (float64(f0.Pix[i+3+c]) - float64(f0.Pix[i-3+c])) / 2
				gy := (float64(f0.Pix[i+3*W+c]) - float64(f0.Pix[i-3*W+c])) / 2
				e := sample(float64(x)+dx, float64(y)+dy)*inv - a
				a11 += gx * gx
				a12 += gx * gy
				a22 += gy * gy
				b1 += gx * e
				b2 += gy * e
				se += e * e
				n++
			}
		}
		det := a11*a22 - a12*a12
		if n < 1000 || det <= 0 {
			return 0, 0, -1, false
		}
		ux := -(a22*b1 - a12*b2) / det
		uy := -(-a12*b1 + a11*b2) / det
		ux = math.Max(-1, math.Min(1, ux))
		uy = math.Max(-1, math.Min(1, uy))
		dx += ux
		dy += uy
		lastE = se / float64(n)
		if math.Hypot(ux, uy) < 0.005 {
			break
		}
	}
	if math.Abs(dx) > float64(r) || math.Abs(dy) > float64(r) {
		return 0, 0, -1, false
	}
	gain = 0
	if zero > 0 && !math.IsInf(zero, 1) {
		gain = (zero - lastE) / zero
	}
	return dx, dy, gain, true
}
