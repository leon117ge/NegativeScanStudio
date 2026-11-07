package hdrscan

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"basetool/basestat"
	"basetool/camera"
	"basetool/rawconv"
)

// Trichrome scan (v0.81): one exposure (or one HDR bracket) per light
// channel with only that channel lit, then the matching colour of each
// capture is combined into one RGB image: R from the red-lit frame, G from
// the green-lit frame, B from the blue-lit frame.

// TriTags name the channels in file names (<base>_R_0EV.RAF ...).
var TriTags = [3]string{"R", "G", "B"}

// ChannelLight switches the scan light for a trichrome capture. Only must
// return only after the light confirmed the state (channel ch on at its
// scan brightness, the other two off). Restore (v0.82) puts the light back
// exactly as it was before the scan (enables and brightness) - after a
// normal finish, a Stop and an error alike.
type ChannelLight interface {
	Only(ch int) error
	Restore() error
}

// TriShot is a captured trichrome set.
type TriShot struct {
	Base      string
	EVs       []int
	Raws      [3][]string // per channel, frame 0 first
	Exposures []float64   // shutter x ISO per EV step (same for every channel)
	Done      int         // channels completely captured
}

// CaptureTrichrome shoots, for R, G and B in turn, the locked frame 0
// (evs = [0]) or the bracket evs (0, 2[, 3]) with only that channel lit.
// Every exposure is checked before the light is touched; after the last
// shot, on any error and on ctx cancel the light is switched off (Off) and
// the camera returns to frame 0. name works as in CaptureBracketEV.
func (s *Session) CaptureTrichrome(ctx context.Context, dir string, evs []int, settle time.Duration, light ChannelLight, name func(time.Time) string) (sh *TriShot, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Locked == nil {
		return nil, errors.New("meter and lock the exposure first")
	}
	if light == nil {
		return nil, errors.New("connect the RGB light on Camera and Light > Light first")
	}
	if len(evs) == 0 {
		evs = []int{0}
	}
	es := []camera.Exposure{*s.Locked}
	if len(evs) > 1 {
		if es, err = BracketEV(*s.Locked, evs); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	sh = &TriShot{EVs: evs}
	for _, e := range es {
		sh.Exposures = append(sh.Exposures, ExposureValue(e))
	}
	defer func() {
		if oerr := light.Restore(); oerr != nil {
			s.say("WARNING: could not restore the light's pre-scan state after the trichrome scan: %v", oerr)
			if err == nil {
				err = fmt.Errorf("trichrome captured, but the light could not be restored to its pre-scan state: %w", oerr)
			}
		} else {
			s.say("Trichrome: light restored to its pre-scan state (confirmed)")
		}
		if serr := s.setExposure(es[0], "back to frame 0"); serr != nil {
			s.say("Warning: could not restore frame 0 exposure: %v", serr)
		}
	}()
	t0 := time.Time{}
	for ch := 0; ch < 3; ch++ {
		if err := ctx.Err(); err != nil {
			return sh, err
		}
		s.say("Trichrome %s: only %s on ...", TriTags[ch], TriTags[ch])
		if err := light.Only(ch); err != nil {
			return sh, fmt.Errorf("light %s only: %w", TriTags[ch], err)
		}
		if settle > 0 {
			select {
			case <-ctx.Done():
				return sh, ctx.Err()
			case <-time.After(settle):
			}
		}
		for k, e := range es {
			if err := ctx.Err(); err != nil {
				return sh, err
			}
			if err := s.setExposure(e, fmt.Sprintf("%s %s", TriTags[ch], StepTag(evs[k]))); err != nil {
				return sh, err
			}
			s.say("Trichrome %s %s at %s ...", TriTags[ch], StepTag(evs[k]), e)
			t := time.Now()
			if t0.IsZero() {
				t0 = t
			}
			r, err := s.Dev.Capture(dir, "")
			if err != nil {
				return sh, err
			}
			if name != nil {
				if sh.Base == "" {
					sh.Base = name(t0)
				}
				nr, rerr := renameNoClobber(r, filepath.Join(dir, sh.Base+"_"+TriTags[ch]+"_"+StepTag(evs[k])+filepath.Ext(r)))
				if rerr != nil {
					s.say("Warning: could not rename %s: %v", filepath.Base(r), rerr)
				} else {
					if s.Log != nil {
						s.Log.Printf("Saved %s as %s", filepath.Base(r), filepath.Base(nr))
					}
					r = nr
				}
			}
			sh.Raws[ch] = append(sh.Raws[ch], r)
		}
		sh.Done++
	}
	return sh, nil
}

// TriResult is a combined trichrome image.
type TriResult struct {
	Out      string
	Channel  [3]*Result // per-channel HDR merge (nil for single frames)
	Level    [3]float64 // mean of each channel's own colour in its frame (fraction of FS)
	Leak     [3][3]float64
	Note     string
	Dust     DustStats
	Elapsed  time.Duration
	Software string
	// v0.82: the clear film base measured in the written RGB TIFF (same
	// statistic as "Send base to Film base" of an HDR scan: module 1's
	// light-leak rejection, per-channel median over BaseRegion, linear
	// TIFF / 65535). These are FilmScan_Density_In's Film base R/G/B.
	Base       [3]float64
	BaseN      int
	BaseRegion basestat.Region
	BaseErr    string
}

// TriPath is <base>_RGB.tiff or <base>_RGB_HDR.tiff for the R frame 0.
func TriPath(dir, r0 string, hdr bool) string {
	b := filepath.Base(r0)
	b = strings.TrimSuffix(b, filepath.Ext(b))
	b = strings.TrimSuffix(b, "_0EV")
	b = strings.TrimSuffix(b, "_R")
	if hdr {
		return filepath.Join(dir, b+"_RGB_HDR.tiff")
	}
	return filepath.Join(dir, b+"_RGB.tiff")
}

// ProcessTrichrome merges each channel's bracket (if it has more than one
// frame) with ProcessN, then takes R from the red-lit image, G from the
// green-lit one and B from the blue-lit one and writes one linear 16-bit
// TIFF. The per-channel HDR TIFFs are deleted unless o.KeepTIFF.
func ProcessTrichrome(raws [3][]string, exps []float64, o ProcessOptions) (retRes *TriResult, retErr error) {
	start := time.Now()
	say := func(f string, a ...any) {
		if o.Progress != nil {
			o.Progress(fmt.Sprintf(f, a...))
		}
	}
	var wrote []string // v0.83: removed when stopped
	defer func() {
		if retErr != nil && stopped(o.Ctx) {
			removeAll(wrote)
			retRes = nil
		}
	}()
	for ch := 0; ch < 3; ch++ {
		if len(raws[ch]) == 0 {
			return nil, fmt.Errorf("no %s frame", TriTags[ch])
		}
		if len(raws[ch]) != len(raws[0]) {
			return nil, errors.New("the channels have different numbers of frames")
		}
	}
	hdr := len(raws[0]) > 1
	dir := o.OutDir
	if dir == "" {
		dir = filepath.Dir(raws[0][0])
	}
	res := &TriResult{}
	var planes [3]*Plane
	defer func() {
		for _, p := range planes {
			p.Free()
		}
	}()
	var tmp []string
	for ch := 0; ch < 3; ch++ {
		if err := ctxErr(o.Ctx); err != nil {
			return nil, err
		}
		src := raws[ch][0]
		if hdr {
			co := o
			co.Merge.Exposure = exps
			co.Dust, co.Color, co.KeepTIFF, co.DeleteRAW, co.OutDir = DustOff, ColorOptions{}, false, false, dir
			co.MeteredBase = [3]float64{}
			co.Progress = func(m string) { say("%s: %s", TriTags[ch], m) }
			r, err := ProcessN(raws[ch], co)
			if err != nil {
				if stopped(o.Ctx) {
					return nil, err
				}
				return nil, fmt.Errorf("%s HDR merge: %w", TriTags[ch], err)
			}
			res.Channel[ch] = r
			wrote = append(wrote, r.HDR)
			wrote = append(wrote, r.TIFFs...)
			src = r.HDR
			if !o.KeepTIFF {
				tmp = append(tmp, r.HDR)
			}
		} else {
			say("Converting %s ...", filepath.Base(src))
		}
		p, err := Load(src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", TriTags[ch], err)
		}
		planes[ch] = p
		if ch > 0 && (p.W != planes[0].W || p.H != planes[0].H) {
			return nil, fmt.Errorf("%s image is %dx%d, R is %dx%d", TriTags[ch], p.W, p.H, planes[0].W, planes[0].H)
		}
	}
	p0 := planes[0]
	n := p0.W * p0.H
	out := make([]uint16, 3*n)
	for ch := 0; ch < 3; ch++ {
		pix := planes[ch].Pix
		var sum [3]float64
		for i := 0; i < n; i++ {
			out[3*i+ch] = pix[3*i+ch]
			for c := 0; c < 3; c++ {
				sum[c] += float64(pix[3*i+c])
			}
		}
		for c := 0; c < 3; c++ {
			res.Leak[ch][c] = sum[c] / float64(n) / 65535
		}
		res.Level[ch] = res.Leak[ch][ch]
	}
	sw := o.Software + " trichrome R/G/B (one light channel per capture"
	if hdr {
		sw += fmt.Sprintf(", %d-frame HDR per channel", len(raws[0]))
	}
	sw += ")"
	if o.Dust != DustOff {
		say("Removing dust (%s) ...", DustName(o.Dust))
		comb := &Plane{W: p0.W, H: p0.H, Pix: out, Make: p0.Make, Model: p0.Model, Source: raws[0][0]}
		out, res.Dust = despeckleHDR(comb, out, o)
		sw += ", dust removal " + DustName(o.Dust)
	}
	measureTriBase(res, out, p0, o)
	if res.BaseErr == "" {
		say("Film base in the RGB TIFF: R %.4f G %.4f B %.4f (%d samples)", res.Base[0], res.Base[1], res.Base[2], res.BaseN)
	}
	if err := ctxErr(o.Ctx); err != nil {
		return nil, err
	}
	path := uniqueFile(TriPath(dir, raws[0][0], hdr))
	say("Writing %s ...", filepath.Base(path))
	wrote = append(wrote, path)
	if err := rawconv.WriteTIFF16(path, p0.W, p0.H, out, o.LZW, p0.Make, p0.Model, sw); err != nil {
		return nil, err
	}
	if err := ctxErr(o.Ctx); err != nil {
		return nil, err
	}
	res.Out, res.Software = path, sw
	for _, t := range tmp {
		os.Remove(t)
	}
	if o.DeleteRAW {
		for _, rs := range raws {
			for _, r := range rs {
				if rawconv.IsRaw(r) {
					os.Remove(r)
				}
			}
		}
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

// resolveTriBase picks the clear-base region: a user-drawn normalized
// rectangle (v0.87) wins, then a metered BaseRegion / BaseArea, else auto.
func resolveTriBase(bounds image.Rectangle, o ProcessOptions, auto func() (image.Rectangle, bool)) (basestat.Region, string) {
	if !o.BaseNorm.Empty() {
		r := o.BaseNorm.PixelRect(bounds.Dx(), bounds.Dy()).Intersect(bounds)
		if !r.Empty() {
			return basestat.Region{Rect: r}, "rectangle"
		}
	}
	reg := basestat.Region{Rect: o.BaseArea.Intersect(bounds)}
	if o.BaseRegion != nil && !o.BaseRegion.Rect.Empty() {
		reg = *o.BaseRegion
		reg.Rect = reg.Rect.Intersect(bounds)
	}
	if !reg.Rect.Empty() {
		return reg, "metered"
	}
	if auto != nil {
		if r, ok := auto(); ok && !r.Empty() {
			return basestat.Region{Rect: r.Intersect(bounds)}, "auto"
		}
	}
	return basestat.Region{}, ""
}

// measureTriBase measures the clear base on the combined RGB image: the
// user-drawn rectangle (v0.87) if set, else the metered base region (green
// box) if known, else the base area, else the base found in the image - as
// ProcessN does for a white-light HDR.
func measureTriBase(res *TriResult, out []uint16, p0 *Plane, o ProcessOptions) {
	bounds := image.Rect(0, 0, p0.W, p0.H)
	reg, _ := resolveTriBase(bounds, o, func() (image.Rectangle, bool) {
		comb := &Plane{W: p0.W, H: p0.H, Pix: out}
		return FindBasePlane(comb)
	})
	if reg.Rect.Empty() {
		res.BaseErr = "no clear base area (meter the base or keep film border in the frame)"
		return
	}
	leak := o.LeakPct
	if leak <= 0 {
		leak = 20
	}
	res.BaseRegion = reg
	var err error
	res.Base, _, res.BaseN, err = basestat.Measure(basestat.PlaneGetter(out, p0.W), p0.W, p0.H, reg, leak)
	if err != nil {
		res.BaseErr = err.Error()
	}
}
