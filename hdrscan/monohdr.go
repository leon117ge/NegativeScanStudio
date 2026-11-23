package hdrscan

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync/atomic"

	"basetool/lightlevel"
)

// MonoFrame is one raw monochrome exposure (or the mean of several
// same-exposure grabs) as it came from the camera: left-justified 16-bit
// values that still contain the BlackLevel pedestal.
type MonoFrame struct {
	W, H int
	Pix  []uint16
	// Black16 is the BlackLevel pedestal subtracted from every pixel
	// (16-bit units).
	Black16 float64
	// White16 is the raw clipping level (65520 for Mono12); 0 means 65535.
	White16    float64
	ExposureUS float64
	GainDB     float64
	EV         int
	Frames     int // grabs averaged into Pix; 0 or 1 = a single grab
}

// MonoNoise is the sensor noise model used for inverse-variance weights.
// FullWellE is the electron count at the raw white level at 0 dB, ReadE
// the temporal read noise in electrons. They only set the relative weights
// and the ghost threshold scale; the ghost test re-measures the actual
// scale from the frames.
type MonoNoise struct {
	FullWellE float64
	ReadE     float64
}

// Sensor model of the Hikrobot MV-CS240-90UM (v1.11). Published values
// only; nothing here is measured on the user's camera:
//
//   - Camera: Gpixel GMAX2424 sensor, 2.4 um pixels, 6016 x 4096, 12-bit
//     ADC (Mono12), dynamic range 72 dB, SNR 41 dB, gain 0-24 dB
//     (Hikrobot MV-CS240-90UM datasheet / 2025Q4 standard product catalog).
//   - Sensor: GMAX2424 full well 12.1 ke- (12-bit, 1x analog gain), read
//     noise 2.7 e- (12-bit), dynamic range 73 dB, max SNR 40.8 dB (Gpixel
//     product page and GMAX2424 press release).
//
// Cross-checks: 20*log10(12100/2.7) = 73.0 dB and 20*log10(sqrt(12100)) =
// 40.8 dB match the sensor sheet; the camera's 72 dB implies about 3.0 e-
// total floor, which is the 2.7 e- read noise plus Mono12 quantisation
// (about 3.0 e- per 12-bit step, 0.86 e- rms). Assumption: camera gain 0 dB
// is the sensor's 1x analog gain and full well maps to the 12-bit range.
const (
	CS240FullWellE  = 12100.0
	CS240ReadNoiseE = 2.7
)

// DefaultMonoNoise is the MV-CS240-90UM sensor model (see above).
func DefaultMonoNoise() MonoNoise {
	return MonoNoise{FullWellE: CS240FullWellE, ReadE: CS240ReadNoiseE}
}

func (n MonoNoise) norm() MonoNoise {
	if !(n.FullWellE > 100) {
		n.FullWellE = DefaultMonoNoise().FullWellE
	}
	if !(n.ReadE > 0) {
		n.ReadE = DefaultMonoNoise().ReadE
	}
	return n
}

// unitsPerE is 16-bit units per photo-electron for a frame.
func (n MonoNoise) unitsPerE(f *MonoFrame) float64 {
	white := f.white()
	return (white - f.Black16) / n.FullWellE * math.Pow(10, f.GainDB/20)
}

// variance of one frame's black-subtracted value s (16-bit units).
func (n MonoNoise) variance(s, k float64, frames int) float64 {
	if s < 0 {
		s = 0
	}
	v := s*k + n.ReadE*n.ReadE*k*k + 16*16/12.0
	if frames > 1 {
		v /= float64(frames)
	}
	return v
}

func (f *MonoFrame) white() float64 {
	if f.White16 > 0 {
		return f.White16
	}
	return 65535
}

func (f *MonoFrame) frames() int {
	if f.Frames < 1 {
		return 1
	}
	return f.Frames
}

func checkMonoFrame(f MonoFrame, name string) error {
	if f.W <= 0 || f.H <= 0 || len(f.Pix) < f.W*f.H {
		return fmt.Errorf("no %s frame", name)
	}
	return nil
}

// SubtractBlack returns frame minus its BlackLevel pedestal, as a
// 16-bit plane clamped at 0. This is the single-exposure path.
func SubtractBlack(f MonoFrame) MonoPlane {
	n := f.W * f.H
	out := make([]uint16, n)
	parallelRows(f.H, func(y0, y1 int) {
		for i := y0 * f.W; i < y1*f.W; i++ {
			out[i] = f64ToU16(float64(f.Pix[i]) - f.Black16)
		}
	})
	return MonoPlane{W: f.W, H: f.H, Pix: out}
}

// AverageFrames is the per-pixel mean of same-size raw grabs (rounded to
// the nearest 16-bit unit). Averaging N grabs cuts temporal noise by sqrt(N).
func AverageFrames(frames [][]uint16) []uint16 {
	if len(frames) == 0 {
		return nil
	}
	if len(frames) == 1 {
		return append([]uint16(nil), frames[0]...)
	}
	n := len(frames[0])
	out := make([]uint16, n)
	k := uint32(len(frames))
	parallelRows(n/4096+1, func(b0, b1 int) {
		lo, hi := b0*4096, min(n, b1*4096)
		for i := lo; i < hi; i++ {
			var s uint32
			for _, f := range frames {
				s += uint32(f[i])
			}
			out[i] = uint16((s + k/2) / k)
		}
	})
	return out
}

// MonoMergeStats describes a native mono HDR merge.
type MonoMergeStats struct {
	EV         []int
	Ratio      []float64 // brightness ratio of frame k to frame 0 (chained step ratios)
	Nominal    []float64
	RatioFrom  []string // "measured" or "nominal"
	NoiseScale []float64
	Used       []float64 // fraction of pixels where frame k got weight
	Ghost      []float64 // fraction of pixels faded out by the 5-7 sigma test
}

func (s MonoMergeStats) String() string {
	if len(s.EV) < 2 {
		return "single exposure (no HDR merge)"
	}
	var b strings.Builder
	b.WriteString("native mono HDR (black-subtracted, inverse-variance, 5-7 sigma ghost rejection)")
	for k := 1; k < len(s.EV); k++ {
		src := "nominal"
		if s.RatioFrom[k] == "measured" {
			src = fmt.Sprintf("measured; nominal %.3f", s.Nominal[k])
		}
		fmt.Fprintf(&b, "; %+d EV ratio %.3f (%s), used %.0f%%, ghost %.3f%%, noise scale %.2f",
			s.EV[k], s.Ratio[k], src, 100*s.Used[k], 100*s.Ghost[k], s.NoiseScale[k])
	}
	return b.String()
}

// MergeMonoNative merges an exposure bracket of raw mono frames on the
// single mono plane (no demosaic, no RGB round trip, no temporary files):
//
//   - every frame has its BlackLevel pedestal removed first;
//   - the brightness ratio of each brighter frame to the previous one is
//     measured on 16x16 blocks well inside the linear range of both
//     (fallback: exposure ratio) and chained back to frame 0, so wide
//     brackets such as 0/+3/+5 still have plenty of overlap per step;
//   - each pixel gets the inverse-variance weight of the shot + read +
//     quantisation noise model, times a saturation hat driven by the value
//     predicted from frame 0 (not by the frame's own noisy value), and zero
//     at the raw clipping level (no dilation: a mono sensor has no demosaic
//     halo);
//   - a pixel that disagrees with the darker estimate by more than 5-7
//     sigma (dust that moved, film that shifted) is faded out.
//
// The result is linear, black-subtracted and in frame-0 units, so the film
// base keeps the level the light leveling measured.
func MergeMonoNative(frames []MonoFrame, nz MonoNoise) (MonoPlane, MonoMergeStats, error) {
	var st MonoMergeStats
	if len(frames) == 0 {
		return MonoPlane{}, st, fmt.Errorf("no bracket frames")
	}
	for i, f := range frames {
		if err := checkMonoFrame(f, fmt.Sprintf("bracket %d", i)); err != nil {
			return MonoPlane{}, st, err
		}
		if f.W != frames[0].W || f.H != frames[0].H {
			return MonoPlane{}, st, fmt.Errorf("bracket sizes differ: 0 is %dx%d, %d is %dx%d", frames[0].W, frames[0].H, i, f.W, f.H)
		}
		if i > 0 && f.EV <= frames[i-1].EV {
			return MonoPlane{}, st, fmt.Errorf("bracket EVs must increase (%d after %d)", f.EV, frames[i-1].EV)
		}
	}
	st.EV = make([]int, len(frames))
	for i, f := range frames {
		st.EV[i] = f.EV
	}
	if len(frames) == 1 {
		return SubtractBlack(frames[0]), st, nil
	}
	nz = nz.norm()
	W, H := frames[0].W, frames[0].H
	n := W * H
	f0 := &frames[0]
	k0 := nz.unitsPerE(f0)
	e0 := make([]float32, n)
	parallelRows(H, func(y0, y1 int) {
		for i := y0 * W; i < y1*W; i++ {
			e0[i] = float32(float64(f0.Pix[i]) - f0.Black16)
		}
	})
	m0 := boxMean32(e0, W, H, 2)
	num := make([]float32, n)
	den := make([]float32, n)
	parallelRows(H, func(y0, y1 int) {
		for i := y0 * W; i < y1*W; i++ {
			w := 1 / nz.variance(float64(m0[i]), k0, f0.frames())
			num[i] = float32(w) * e0[i]
			den[i] = float32(w)
		}
	})
	st.Ratio = make([]float64, len(frames))
	st.Nominal = make([]float64, len(frames))
	st.RatioFrom = make([]string, len(frames))
	st.NoiseScale = make([]float64, len(frames))
	st.Used = make([]float64, len(frames))
	st.Ghost = make([]float64, len(frames))
	st.Ratio[0], st.Nominal[0], st.RatioFrom[0], st.NoiseScale[0], st.Used[0] = 1, 1, "reference", 1, 1
	prevE := e0
	for k := 1; k < len(frames); k++ {
		fk, fp := &frames[k], &frames[k-1]
		nom := math.Pow(2, float64(fk.EV-f0.EV))
		link := math.Pow(2, float64(fk.EV-fp.EV))
		if f0.ExposureUS > 0 && fk.ExposureUS > 0 && fp.ExposureUS > 0 {
			nom = fk.ExposureUS / f0.ExposureUS * math.Pow(10, (fk.GainDB-f0.GainDB)/20)
			link = fk.ExposureUS / fp.ExposureUS * math.Pow(10, (fk.GainDB-fp.GainDB)/20)
		}
		st.Nominal[k] = nom
		lr, ok := monoRatio(fp, fk, prevE, link)
		var r float64
		if ok {
			r = st.Ratio[k-1] * lr
			st.RatioFrom[k] = "measured"
		} else {
			r = st.Ratio[k-1] * link
			st.RatioFrom[k] = "nominal"
		}
		st.Ratio[k] = r
		if k+1 < len(frames) {
			ek := make([]float32, n)
			parallelRows(H, func(y0, y1 int) {
				for i := y0 * W; i < y1*W; i++ {
					ek[i] = float32(float64(fk.Pix[i]) - fk.Black16)
				}
			})
			prevE = ek
		}
		kk := nz.unitsPerE(fk)
		white := fk.white()
		clipAt := white - 0.5*16 // any code at the top 12-bit step is clipped
		headroom := white - fk.Black16
		s, ok := monoNoiseScale(fk, m0, num, den, r, nz, kk, headroom)
		if !ok {
			// Too few unclipped samples (a +5 EV frame on a thin
			// negative): keep the previous step's scale.
			s = st.NoiseScale[k-1]
		}
		st.NoiseScale[k] = s
		var used, ghost atomic.Int64
		parallelRows(H, func(y0, y1 int) {
			var u, g int64
			for i := y0 * W; i < y1*W; i++ {
				raw := float64(fk.Pix[i])
				if raw >= clipAt {
					continue
				}
				pred := r * float64(m0[i])
				hat := smoothHat(pred/headroom, 0.80, 0.92)
				if hat <= 0 {
					continue
				}
				x := (raw - fk.Black16) / r
				vk := nz.variance(pred, kk, fk.frames()) / (r * r)
				est := float64(num[i]) / float64(den[i])
				ve := 1 / float64(den[i])
				z := math.Abs(x-est) / math.Sqrt(vk+ve) / s
				gw := smoothHat(z, 5, 7)
				if gw < 1 {
					g++
				}
				if gw <= 0 {
					continue
				}
				w := hat * gw / vk
				num[i] += float32(w * x)
				den[i] += float32(w)
				u++
			}
			used.Add(u)
			ghost.Add(g)
		})
		st.Used[k] = float64(used.Load()) / float64(n)
		st.Ghost[k] = float64(ghost.Load()) / float64(n)
	}
	out := make([]uint16, n)
	parallelRows(H, func(y0, y1 int) {
		for i := y0 * W; i < y1*W; i++ {
			out[i] = f64ToU16(float64(num[i]) / float64(den[i]))
		}
	})
	return MonoPlane{W: W, H: H, Pix: out}, st, nil
}

// monoRatio measures frame k / frame p (the previous bracket step, its
// black-subtracted values in e0) on 16x16 blocks where frame p is well
// above the noise floor and frame k is well below clipping.
func monoRatio(f0, fk *MonoFrame, e0 []float32, nom float64) (float64, bool) {
	W, H := f0.W, f0.H
	const B = 16
	white := fk.white()
	headroom := white - fk.Black16
	var rs []float64
	for by := 0; by+B <= H; by += B {
		for bx := 0; bx+B <= W; bx += B {
			var s0, sk float64
			mx := 0.0
			for y := by; y < by+B; y++ {
				for x := bx; x < bx+B; x++ {
					i := y*W + x
					s0 += float64(e0[i])
					v := float64(fk.Pix[i])
					sk += v - fk.Black16
					if v > mx {
						mx = v
					}
				}
			}
			s0 /= B * B
			sk /= B * B
			if s0 < 0.01*headroom || mx > fk.Black16+0.75*headroom {
				continue
			}
			rs = append(rs, sk/s0)
		}
	}
	if len(rs) < 16 {
		return nom, false
	}
	sort.Float64s(rs)
	r := rs[len(rs)/2]
	if r < 0.75*nom || r > 1.33*nom {
		return nom, false
	}
	return r, true
}

// monoNoiseScale is the robust ratio of the observed frame-k vs merged
// difference to the noise model (1 = the model is right); false when there
// are too few usable samples to tell.
func monoNoiseScale(fk *MonoFrame, m0, num, den []float32, r float64, nz MonoNoise, kk, headroom float64) (float64, bool) {
	W, H := fk.W, fk.H
	var zs []float64
	for y := 0; y < H; y += 7 {
		for x := y % 5; x < W; x += 11 {
			i := y*W + x
			pred := r * float64(m0[i])
			if pred < 0.02*headroom || pred > 0.75*headroom {
				continue
			}
			xk := (float64(fk.Pix[i]) - fk.Black16) / r
			vk := nz.variance(pred, kk, fk.frames()) / (r * r)
			est := float64(num[i]) / float64(den[i])
			zs = append(zs, math.Abs(xk-est)/math.Sqrt(vk+1/float64(den[i])))
		}
	}
	if len(zs) < 500 {
		return 1, false
	}
	sort.Float64s(zs)
	s := 1.4826 * zs[len(zs)/2]
	if s > 50 {
		// Far off the model: cap rather than fall back to 1, which would
		// reject ordinary noise as ghosts and throw the bright frames away.
		return 50, true
	}
	if s < 1 {
		// Never tighten the ghost test below the model: smooth content
		// with an over-estimated noise model would otherwise reject nothing
		// anyway, and texture would be rejected spuriously.
		return 1, true
	}
	return s, true
}

// boxMean32 is a (2r+1)^2 box mean with edge clamping.
func boxMean32(src []float32, w, h, r int) []float32 {
	tmp := make([]float32, len(src))
	out := make([]float32, len(src))
	parallelRows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := src[y*w : (y+1)*w]
			for x := 0; x < w; x++ {
				var s float32
				for d := -r; d <= r; d++ {
					xx := x + d
					if xx < 0 {
						xx = 0
					} else if xx >= w {
						xx = w - 1
					}
					s += row[xx]
				}
				tmp[y*w+x] = s / float32(2*r+1)
			}
		}
	})
	parallelRows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				var s float32
				for d := -r; d <= r; d++ {
					yy := y + d
					if yy < 0 {
						yy = 0
					} else if yy >= h {
						yy = h - 1
					}
					s += tmp[yy*w+x]
				}
				out[y*w+x] = s / float32(2*r+1)
			}
		}
	})
	return out
}

// ---- leveling -------------------------------------------------------------

// LevelingImageMonoBlack is LevelingImageMono for a raw frame that still
// contains a BlackLevel pedestal: values are pedestal-subtracted (so the
// leveled film base matches the black-subtracted scan) while clipping is
// still judged on the raw code.
func LevelingImageMonoBlack(pix []uint16, w, h int, black16 float64) *lightlevel.Image {
	return levelingImageMono(pix, w, h, black16)
}

// ---- R/G/B film movement --------------------------------------------------

// MovementLimitPx is the global R or B vs G translation above which the
// scan is flagged: the film or holder probably moved between captures.
// The lens alone puts a constant offset between channels (decentring:
// about 1.0 px B vs G on the EX50 at f/4 in scan 0281), so the limit sits
// above that.
const MovementLimitPx = 1.5

// MovementReport is the global translation of R and B against G.
type MovementReport struct {
	Valid  bool
	R, B   [2]float64 // px, full resolution
	Tiles  [2]int
	Warn   bool
	Reason string
}

func (m MovementReport) String() string {
	if !m.Valid {
		r := m.Reason
		if r == "" {
			r = "not enough texture"
		}
		return "R/G/B movement check: not measured (" + r + ")"
	}
	s := fmt.Sprintf("R/G/B movement check: R vs G %+.2f, %+.2f px; B vs G %+.2f, %+.2f px",
		m.R[0], m.R[1], m.B[0], m.B[1])
	if m.Warn {
		return "WARNING: " + s + fmt.Sprintf(" (over %.1f px: the film or holder probably moved between the R, G and B captures; this is not corrected - check the holder and capture again)", MovementLimitPx)
	}
	return s + " (OK)"
}

// MeasureMovement measures the global R vs G and B vs G translation at
// quarter resolution on a 3x3 grid of large tiles in the centre 60 % of
// the frame (where lateral CA is smallest) and takes the median of the
// confident tiles. It never changes the planes.
func MeasureMovement(r, g, b []float64, w, h int) MovementReport {
	const ds = 4
	dw, dh := w/ds, h/ds
	if dw < 96 || dh < 96 {
		return MovementReport{Reason: "frame too small"}
	}
	down := func(p []float64) []float64 {
		o := make([]float64, dw*dh)
		parallelRows(dh, func(y0, y1 int) {
			for y := y0; y < y1; y++ {
				for x := 0; x < dw; x++ {
					var s float64
					for j := 0; j < ds; j++ {
						row := (y*ds + j) * w
						for i := 0; i < ds; i++ {
							s += p[row+x*ds+i]
						}
					}
					o[y*dw+x] = s / (ds * ds)
				}
			}
		})
		return o
	}
	gd := down(g)
	half := minInt(dw, dh) / 10
	if half > 64 {
		half = 64
	}
	if half < 16 {
		half = 16
	}
	var rep MovementReport
	one := func(p []float64) ([2]float64, int) {
		pd := down(p)
		var xs, ys []float64
		for j := -1; j <= 1; j++ {
			for i := -1; i <= 1; i++ {
				cx := dw/2 + i*int(0.3*float64(dw)/2)
				cy := dh/2 + j*int(0.3*float64(dh)/2)
				if cx-half-caPad < 0 || cy-half-caPad < 0 || cx+half+caPad >= dw || cy+half+caPad >= dh {
					continue
				}
				t := estimateTile(pd, gd, dw, dh, cx, cy, half)
				if !t.OK {
					continue
				}
				xs = append(xs, t.DX*ds)
				ys = append(ys, t.DY*ds)
			}
		}
		if len(xs) < 3 {
			return [2]float64{}, len(xs)
		}
		return [2]float64{median(xs), median(ys)}, len(xs)
	}
	rep.R, rep.Tiles[0] = one(r)
	rep.B, rep.Tiles[1] = one(b)
	if rep.Tiles[0] < 3 || rep.Tiles[1] < 3 {
		rep.Reason = fmt.Sprintf("confident tiles R %d, B %d of 9", rep.Tiles[0], rep.Tiles[1])
		return rep
	}
	rep.Valid = true
	rep.Warn = math.Hypot(rep.R[0], rep.R[1]) > MovementLimitPx || math.Hypot(rep.B[0], rep.B[1]) > MovementLimitPx
	return rep
}
