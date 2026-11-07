package hdrscan

import (
	"errors"
	"fmt"
	"image"
	"math"
	"sort"

	"basetool/basestat"
	"basetool/camera"
	"basetool/rawconv"
)

// Rect is a normalized (0..1) rectangle on the image; zero value = auto.
type Rect struct{ X0, Y0, X1, Y1 float64 }

func (r Rect) Empty() bool { return r.X1-r.X0 < 0.002 || r.Y1-r.Y0 < 0.002 }

// PixelRect maps the normalized rectangle onto a w×h image.
func (r Rect) PixelRect(w, h int) image.Rectangle {
	if r.Empty() || w <= 0 || h <= 0 {
		return image.Rectangle{}
	}
	x0 := int(r.X0 * float64(w))
	y0 := int(r.Y0 * float64(h))
	x1 := int(math.Ceil(r.X1 * float64(w)))
	y1 := int(math.Ceil(r.Y1 * float64(h)))
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > w {
		x1 = w
	}
	if y1 > h {
		y1 = h
	}
	if x1 <= x0 || y1 <= y0 {
		return image.Rectangle{}
	}
	return image.Rect(x0, y0, x1, y1)
}

// Reading is the measured clear-base level of one probe.
type Reading struct {
	RGB     [3]float64 // mean per channel, fraction of raw full scale
	Max     float64    // brightest channel mean
	Channel int        // index of the brightest channel
	Peak    float64    // brightest single raw pixel in the region (fraction FS)
	Clipped bool       // region touches the raw white level
	Auto    bool
	Where   string          // how the auto region was found
	Warn    string          // set when auto-detection had to fall back
	Area    image.Rectangle // sensor (visible) pixels
	Norm    Rect
	// Auto base over a cluster: RGB/Max are the cluster median; Lo/Hi are
	// the 2nd/98th percentile of the metered channel across it (light
	// falloff / vignetting), Coverage the cluster's share of the frame.
	Lo, Hi   float64
	Coverage float64
	Cells    int
	// Region is the metered base in sensor (= TIFF) pixels: the whole
	// cluster for an auto base, else the rectangle. Module 1's statistic is
	// applied to it when the base is sent to "1. Film base".
	Region basestat.Region
}

var chName = [3]string{"R", "G", "B"}

func (r Reading) String() string {
	src := "your rectangle"
	if r.Auto {
		src = fmt.Sprintf("auto base [%s] at x %d-%d, y %d-%d (%dx%d px)", r.Where, r.Area.Min.X, r.Area.Max.X, r.Area.Min.Y, r.Area.Max.Y, r.Area.Dx(), r.Area.Dy())
	}
	spread := ""
	if r.Cells > 0 && r.Lo > 0 {
		spread = fmt.Sprintf(", across the base %s %.3f-%.3f (%.2f EV falloff)", chName[r.Channel], r.Lo, r.Hi, math.Log2(r.Hi/r.Lo))
	}
	what := "brightest"
	if r.Cells > 0 {
		what = "median, brightest channel"
	}
	return fmt.Sprintf("%s: R %.3f G %.3f B %.3f FS (%s %s %.3f%s, peak pixel %.3f)", src, r.RGB[0], r.RGB[1], r.RGB[2], what, chName[r.Channel], r.Max, spread, r.Peak)
}

const bin = 8

// MeasureBase finds the clear film base in a RAW probe. With an empty roi it
// picks the brightest uniform region that is not clipped anywhere.
func MeasureBase(f *rawconv.RawFrame, roi Rect) (Reading, error) {
	bw, bh, rgb, clip := f.Binned(bin)
	if bw < 16 || bh < 16 {
		return Reading{}, errors.New("probe image too small")
	}
	var rd Reading
	var bx0, by0, bx1, by1 int
	var cells []int
	if roi.Empty() {
		rd.Auto = true
		// Overexposed base: clipped cells that are still clearly coloured
		// (one channel at full scale, blue well below) are base, not a
		// sprocket hole or white leak - force a darker probe.
		colClip := 0
		for i := range clip {
			r, g, b := rgb[3*i], rgb[3*i+1], rgb[3*i+2]
			mx := r
			if g > mx {
				mx = g
			}
			if mx >= 0.9 && b < 0.6*mx {
				colClip++
			}
		}
		fb, ok := findBase(bw, bh, rgb, clip)
		if colClip >= minBaseCells && (!ok || !fb.Orange) {
			// no orange base visible but coloured clipped areas are: the
			// base itself is probably clipped
			rd.Clipped = true
		}
		if ok {
			bx0, by0, bx1, by1 = fb.X0, fb.Y0, fb.X1, fb.Y1
			rd.Where, rd.Warn = fb.Where, fb.Warn
			cells, rd.Coverage = fb.Cells, fb.Coverage
		} else {
			var ok bool
			bx0, by0, bx1, by1, ok = autoWindow(bw, bh, rgb, clip)
			if !ok {
				return rd, errors.New("no unclipped flat area found (probe overexposed?) - drag a rectangle over the clear film base")
			}
			rd.Where = "thinnest flat image area (fallback)"
			rd.Warn = "no clear film base found (no rebate, gap or sprocket-area strip) - metered the thinnest image area instead; drag a rectangle over the base for an exact reading"
		}
	} else {
		bx0 = int(roi.X0 * float64(bw))
		by0 = int(roi.Y0 * float64(bh))
		bx1 = int(math.Ceil(roi.X1 * float64(bw)))
		by1 = int(math.Ceil(roi.Y1 * float64(bh)))
		if bx0 < 0 {
			bx0 = 0
		}
		if by0 < 0 {
			by0 = 0
		}
		if bx1 > bw {
			bx1 = bw
		}
		if by1 > bh {
			by1 = bh
		}
		if bx1 <= bx0 || by1 <= by0 {
			return rd, errors.New("selected rectangle is empty")
		}
	}
	if rd.Auto && rd.Clipped && rd.Warn == "" {
		rd.Warn = "part of the film base looks clipped (coloured cells at full scale) - the probe will be repeated darker"
	}
	if len(cells) > 0 {
		// median of the whole detected base (robust to dust and edges)
		vals := make([]float64, len(cells))
		for c := 0; c < 3; c++ {
			for k, i := range cells {
				vals[k] = float64(rgb[3*i+c])
			}
			sort.Float64s(vals)
			rd.RGB[c] = vals[len(vals)/2]
		}
		for c := 0; c < 3; c++ {
			if rd.RGB[c] > rd.Max {
				rd.Max, rd.Channel = rd.RGB[c], c
			}
		}
		for k, i := range cells {
			vals[k] = float64(rgb[3*i+rd.Channel])
		}
		sort.Float64s(vals)
		rd.Lo, rd.Hi = vals[len(vals)*2/100], vals[(len(vals)-1)*98/100]
		rd.Cells = len(cells)
	} else {
		n := 0
		for y := by0; y < by1; y++ {
			for x := bx0; x < bx1; x++ {
				i := y*bw + x
				for c := 0; c < 3; c++ {
					rd.RGB[c] += float64(rgb[3*i+c])
				}
				if clip[i] {
					rd.Clipped = true
				}
				n++
			}
		}
		for c := 0; c < 3; c++ {
			rd.RGB[c] /= float64(n)
			if rd.RGB[c] > rd.Max {
				rd.Max, rd.Channel = rd.RGB[c], c
			}
		}
	}
	rd.Area = image.Rect(bx0*bin, by0*bin, bx1*bin, by1*bin).Intersect(image.Rect(0, 0, f.Width, f.Height))
	rd.Region = basestat.Region{Rect: rd.Area}
	if len(cells) > 0 {
		mask := make([]bool, bw*bh)
		mnx, mny, mxx, mxy := bw, bh, 0, 0
		for _, i := range cells {
			mask[i] = true
			cx, cy := i%bw, i/bw
			mnx, mny = min(mnx, cx), min(mny, cy)
			mxx, mxy = max(mxx, cx+1), max(mxy, cy+1)
		}
		rd.Region = basestat.Region{Rect: image.Rect(mnx*bin, mny*bin, mxx*bin, mxy*bin).Intersect(image.Rect(0, 0, f.Width, f.Height)), Bin: bin, Cols: bw, Mask: mask}
	}
	rd.Norm = Rect{float64(bx0) / float64(bw), float64(by0) / float64(bh), float64(bx1) / float64(bw), float64(by1) / float64(bh)}
	// Peak raw pixel level in the region, ignoring the few hottest pixels
	// (hot/stuck pixels would otherwise block a large base area forever).
	scale := 1 / float64(f.Maximum-f.Black)
	const nb = 2048
	var hist [nb + 1]int
	npx, nclip := 0, 0
	add := func(r image.Rectangle) {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				b := int(f.BlackAt[y*f.Width+x])
				v := int(f.Raw[(y+f.Top)*f.RawWidth+x+f.Left])
				if v >= f.Maximum-8 {
					nclip++
				}
				k := int(float64(v-b) * scale * nb)
				if k < 0 {
					k = 0
				}
				if k > nb {
					k = nb
				}
				hist[k]++
				npx++
			}
		}
	}
	if len(cells) > 0 {
		// over the whole detected base: with light falloff the brightest
		// part of a large base is what could clip
		full := image.Rect(0, 0, f.Width, f.Height)
		for _, i := range cells {
			cx, cy := i%bw, i/bw
			add(image.Rect(cx*bin, cy*bin, cx*bin+bin, cy*bin+bin).Intersect(full))
		}
	} else {
		add(rd.Area)
	}
	skip := npx / 20000
	if skip < 3 {
		skip = 3
	}
	if nclip > skip {
		rd.Clipped = true
	}
	for k, acc := nb, 0; k >= 0; k-- {
		acc += hist[k]
		if acc > skip || k == 0 {
			rd.Peak = float64(k+1) / nb
			break
		}
	}
	return rd, nil
}

// Lock window around the target: a base up to 1/3 EV below the target is
// accepted (one 1/3-stop shutter step), above it only 1/6 EV and never past
// SafeMax, so a lock never trades headroom for brightness.
const (
	TolBelow = 1.0 / 3
	TolAbove = 1.0 / 6
)

// Tolerance is kept for the closest-step search (half a 1/3 step).
const Tolerance = 1.0 / 6

// LockCap is the highest base level accepted for target.
func LockCap(target float64) float64 {
	return math.Min(SafeMax, target*math.Pow(2, TolAbove))
}

// inWindow reports whether level m is in the lock window for target.
func inWindow(m, target float64) bool {
	return m <= LockCap(target) && math.Log2(m/target) >= -TolBelow-1e-9
}

// SafeMax is the highest brightest-channel mean we ever lock: target range
// top (0.85) plus margin, so the base never clips.
const SafeMax = 0.87

// PeakMax is the highest single raw pixel allowed in the base region.
const PeakMax = 0.97

// InTolerance reports whether reading r meets target and is safe.
func InTolerance(r Reading, target float64) bool {
	return !r.Clipped && r.Peak < PeakMax && inWindow(r.Max, target)
}

// FixedISO is the only ISO used: X-T3 base ISO. Metering moves the shutter only.
const FixedISO = 160

// Solve picks the shutter (ISO fixed at 160) that puts the base at target,
// never slower than slowest (0 = 1/60 s). tooDark is set when even the
// slowest allowed shutter cannot reach the target.
func Solve(r Reading, at camera.Exposure, target float64, slowest uint32) (next camera.Exposure, pred float64, note string, tooDark bool) {
	if slowest == 0 {
		slowest = camera.Shutter1_60
	}
	lim := camera.ShutterIndex(slowest)
	if r.Clipped || r.Max >= 0.97 {
		// Unknown how far over: step down 2 EV by shutter and re-probe.
		i := camera.ShutterIndex(at.Shutter) - 6
		if i < 0 {
			i = 0
		}
		if i > lim {
			i = lim
		}
		return camera.Exposure{ISO: FixedISO, Shutter: camera.Shutters[i].Value}, 0, "base clipped in the probe - 2 EV darker (shutter) and probing again", false
	}
	m := r.Max
	if m < 0.005 {
		m = 0.005
	}
	atISO := at.ISO
	if atISO <= 0 {
		atISO = FixedISO
	}
	k := func(e camera.Exposure) float64 {
		return m * float64(e.ISO) / float64(atISO) * float64(e.Shutter) / float64(at.Shutter)
	}
	var best camera.Exposure
	bestD := math.Inf(1)
	for si := 0; si <= lim; si++ {
		e := camera.Exposure{ISO: FixedISO, Shutter: camera.Shutters[si].Value}
		p := k(e)
		if p > LockCap(target) {
			continue
		}
		if r.Peak > 0 && r.Peak*p/m >= PeakMax {
			continue // brightest part of the base (light falloff) would clip
		}
		d := math.Abs(math.Log2(p / target))
		if d < bestD-1e-9 {
			best, bestD = e, d
		}
	}
	if best.ISO == 0 {
		best = camera.Exposure{ISO: FixedISO, Shutter: camera.Shutters[0].Value}
		return best, k(best), "too bright even at 1/8000 s - stop the lens down or dim the light", false
	}
	p := k(best)
	if !inWindow(p, target) {
		if p < target && best.Shutter == camera.Shutters[lim].Value {
			need := float64(at.Shutter) * float64(atISO) / FixedISO * target / m
			ev := math.Log2(target / p)
			return best, p, fmt.Sprintf("base too dark at ISO 160 and %s (the slowest allowed shutter): it would need about %s (%.1f EV more). ISO stays at 160 - open the aperture or brighten the light (or allow slower shutters in the meter options)",
				camera.ShutterLabel(best.Shutter), camera.ShutterLabel(camera.Shutters[camera.ShutterIndex(uint32(need))].Value), ev), true
		}
		note = fmt.Sprintf("1/3-stop steps and the %.2f safety limit: closest safe setting gives %.3f", LockCap(target), p)
	}
	return best, p, note, false
}

// Plus2 is the +2 EV exposure for frame 0 (shutter only).
func Plus2(e camera.Exposure) (camera.Exposure, error) { return PlusEV(e, 2) }

// Bracket returns frame 0 and n-1 frames at +2, +4 ... EV (shutter only).
func Bracket(e0 camera.Exposure, n int) ([]camera.Exposure, error) {
	evs := []int{0}
	for k := 1; k < n; k++ {
		evs = append(evs, 2*k)
	}
	return BracketEV(e0, evs)
}

// BracketEV returns one exposure per EV step (0, 2, 3, 4 ...): frame 0's
// exposure with the shutter nearest to frame 0's time x 2^ev. All are
// checked before anything is shot.
func BracketEV(e0 camera.Exposure, evs []int) ([]camera.Exposure, error) {
	var out []camera.Exposure
	for _, ev := range evs {
		if ev == 0 {
			out = append(out, e0)
			continue
		}
		e, err := PlusEV(e0, ev)
		if err != nil {
			slow := camera.Shutters[len(camera.Shutters)-1]
			return nil, fmt.Errorf("the +%d EV frame would need %s x %d, slower than the camera's slowest timed shutter (%s) - nothing was shot. Use the 0 / +2 EV bracket, or brighten the light / open the aperture and meter again",
				ev, camera.ShutterLabel(e0.Shutter), 1<<ev, slow.Label)
		}
		out = append(out, e)
	}
	return out, nil
}

// PlusEV returns e with the camera shutter speed nearest to e's exposure
// time x 2^ev (the X-T3 table is in 1/3 stops, so this is exact for whole
// stops: +3 EV = 9 thirds = x8).
func PlusEV(e camera.Exposure, ev int) (camera.Exposure, error) {
	want := float64(e.Shutter) * math.Pow(2, float64(ev))
	best, bd := -1, math.Inf(1)
	for i, r := range camera.Shutters {
		if d := math.Abs(math.Log2(float64(r.Value) / want)); d < bd {
			best, bd = i, d
		}
	}
	if best < 0 || bd > 0.2 {
		return e, fmt.Errorf("no shutter speed near %s x %d", camera.ShutterLabel(e.Shutter), 1<<ev)
	}
	return camera.Exposure{ISO: e.ISO, Shutter: camera.Shutters[best].Value}, nil
}

// ExposureValue is shutter seconds x ISO (the camera's shutter code is in
// microseconds of the actual exposure time).
func ExposureValue(e camera.Exposure) float64 { return float64(e.Shutter) / 1e6 * float64(e.ISO) }

// autoWindow finds the brightest uniform 6x6-bin window with no clipped bin in
// or around it.
func autoWindow(bw, bh int, rgb []float32, clip []bool) (bx0, by0, bx1, by1 int, ok bool) {
	win := 6
	best := -1.0
	maxc := func(i int) float64 {
		v := float64(rgb[3*i])
		if g := float64(rgb[3*i+1]); g > v {
			v = g
		}
		if b := float64(rgb[3*i+2]); b > v {
			v = b
		}
		return v
	}
	for y := 1; y+win < bh-1; y += 2 {
		for x := 1; x+win < bw-1; x += 2 {
			var s, s2 float64
			good := true
			for yy := y - 1; yy <= y+win && good; yy++ { // one-bin guard ring
				for xx := x - 1; xx <= x+win; xx++ {
					i := yy*bw + xx
					if clip[i] || maxc(i) >= 0.97 {
						good = false
						break
					}
				}
			}
			if !good {
				continue
			}
			for yy := y; yy < y+win; yy++ {
				for xx := x; xx < x+win; xx++ {
					v := maxc(yy*bw + xx)
					s += v
					s2 += v * v
				}
			}
			nn := float64(win * win)
			mean := s / nn
			sd := math.Sqrt(math.Max(0, s2/nn-mean*mean))
			if mean <= 0 || sd/mean > 0.03 { // reject edges / texture
				continue
			}
			if mean > best {
				best = mean
				bx0, by0, bx1, by1 = x, y, x+win, y+win
			}
		}
	}
	return bx0, by0, bx1, by1, best >= 0
}

// FindBasePlane locates the clear base on a decoded 16-bit image (for the
// equality check in manual merges). Returns a pixel rectangle.
func FindBasePlane(p *Plane) (image.Rectangle, bool) {
	bw, bh := p.W/bin, p.H/bin
	rgb := make([]float32, bw*bh*3)
	clip := make([]bool, bw*bh)
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			var s [3]float64
			for y := by * bin; y < by*bin+bin; y++ {
				for x := bx * bin; x < bx*bin+bin; x++ {
					i := (y*p.W + x) * 3
					for c := 0; c < 3; c++ {
						v := p.Pix[i+c]
						s[c] += float64(v)
						if v >= 65000 {
							clip[by*bw+bx] = true
						}
					}
				}
			}
			for c := 0; c < 3; c++ {
				rgb[(by*bw+bx)*3+c] = float32(s[c] / (bin * bin * 65535))
			}
		}
	}
	if fb, ok := findBase(bw, bh, rgb, clip); ok {
		return image.Rect(fb.X0*bin, fb.Y0*bin, fb.X1*bin, fb.Y1*bin), true
	}
	x0, y0, x1, y1, ok := autoWindow(bw, bh, rgb, clip)
	return image.Rect(x0*bin, y0*bin, x1*bin, y1*bin), ok
}

// Preview renders a probe as a small gamma-2.2 image (raw camera colours).
func Preview(f *rawconv.RawFrame) image.Image {
	w, h, rgb, _ := f.Binned(bin)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	lut := make([]uint8, 4097)
	for i := range lut {
		lut[i] = uint8(255*math.Pow(float64(i)/4096, 1/2.2) + 0.5)
	}
	for i := 0; i < w*h; i++ {
		for c := 0; c < 3; c++ {
			v := rgb[3*i+c]
			if v < 0 {
				v = 0
			} else if v > 1 {
				v = 1
			}
			img.Pix[4*i+c] = lut[int(v*4096)]
		}
		img.Pix[4*i+3] = 255
	}
	return img
}
