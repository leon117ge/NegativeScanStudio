package hdrscan

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"basetool/basestat"
	"basetool/camera"
	"basetool/rawconv"
)

// DialPrompt is called when the camera refuses USB exposure changes: the user
// must set e on the dials. Return false to cancel.
type DialPrompt func(e camera.Exposure, why string) bool

// Session holds the metering state for one connected camera.
type Session struct {
	Dev      camera.Device
	Log      *camera.Logger
	Target   float64 // 0.50..0.85 of full scale
	ROI      Rect    // empty = auto
	ProbeDir string
	Dials    DialPrompt
	Progress func(string)
	// Slowest is the slowest frame-0 shutter metering may use (0 = 1/60 s).
	Slowest uint32
	// Fixed (v0.81) is the Fixed frame-0 / leveling shutter, one of
	// FixedShutters (0 = 1/60 s).
	Fixed uint32

	Locked    *camera.Exposure
	Reading   Reading
	DialMode  bool
	LastProbe *rawconv.RawFrame
	// LastProbePath is the RAW of the last probe (kept until the next
	// probe): the locked probe has the frame-0 exposure, so decoding it on
	// the TIFF path gives the base values before a capture.
	LastProbePath string
	mu            sync.Mutex
}

func (s *Session) say(f string, a ...any) {
	msg := fmt.Sprintf(f, a...)
	if s.Log != nil {
		s.Log.Printf("%s", msg)
	}
	if s.Progress != nil {
		s.Progress(msg)
	}
}

// setExposure writes e, falling back to the user's dials.
func (s *Session) setExposure(e camera.Exposure, why string) error {
	if !s.DialMode {
		got, err := s.Dev.SetExposure(e)
		if err == nil && got == e {
			return nil
		}
		if err != nil && !errors.Is(err, camera.ErrNotWritable) {
			return err // includes camera.ErrNotManual: no hand-set popup
		}
		s.DialMode = true
		s.say("Camera did not accept the exposure over USB (%v) - switching to set-on-dials mode", err)
	}
	if err := s.checkManual(); err != nil {
		return err
	}
	for tries := 0; tries < 3; tries++ {
		if s.Dials == nil || !s.Dials(e, why) {
			return errors.New("cancelled")
		}
		got, err := s.Dev.Exposure()
		if err != nil {
			return err
		}
		if got == e {
			return nil
		}
		why = fmt.Sprintf("the camera still reports %s", got)
	}
	return fmt.Errorf("camera exposure does not match %s", e)
}

// checkManual returns camera.ErrNotManual when the body reports P/A/S.
func (s *Session) checkManual() error {
	mr, ok := s.Dev.(camera.ModeReader)
	if !ok {
		return nil
	}
	if m, name, err := mr.ExposureMode(); err == nil && !m {
		return fmt.Errorf("%w - the camera is in %s", camera.ErrNotManual, name)
	}
	return nil
}

// probe shoots one RAW, measures the base and removes the file.
func (s *Session) probe() (Reading, camera.Exposure, error) {
	e, err := s.Dev.Exposure()
	if err != nil {
		return Reading{}, e, err
	}
	os.MkdirAll(s.ProbeDir, 0o755)
	p, err := s.Dev.Capture(s.ProbeDir, "probe_")
	if err != nil {
		return Reading{}, e, err
	}
	if s.LastProbePath != "" && s.LastProbePath != p {
		os.Remove(s.LastProbePath)
	}
	s.LastProbePath = p
	f, err := rawconv.ReadRawFrame(p)
	if err != nil {
		return Reading{}, e, err
	}
	s.LastProbe = f
	r, err := MeasureBase(f, s.ROI)
	return r, e, err
}

func (s *Session) slowest() uint32 {
	if s.Slowest == 0 {
		return camera.Shutter1_60
	}
	return s.Slowest
}

// EnsureISO160 sets ISO 160 (keeping the shutter if it is a valid speed no
// slower than the limit, else 1/125 s). ISO is never changed afterwards.
func (s *Session) EnsureISO160() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureISO160()
}

func (s *Session) ensureISO160() error { return s.ensureISO(FixedISO, "ISO is fixed at 160") }

// EnsureConnectISO sets iso on connect (Fixed frame 0 uses the chosen ISO;
// iso == FixedISO behaves exactly like EnsureISO160).
func (s *Session) EnsureConnectISO(iso int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if iso <= 0 || iso == FixedISO {
		return s.ensureISO160()
	}
	return s.ensureISO(iso, fmt.Sprintf("Fixed frame 0 uses ISO %d", iso))
}

func (s *Session) ensureISO(iso int, why string) error {
	e, err := s.Dev.Exposure()
	if err != nil {
		return err
	}
	if e.ISO == iso && e.Shutter > 0 && e.Shutter <= s.slowest() {
		return nil
	}
	want := camera.Exposure{ISO: iso, Shutter: 7812}
	if e.Shutter > 0 && e.Shutter != 0xffffffff && e.Shutter <= s.slowest() {
		want.Shutter = camera.Shutters[camera.ShutterIndex(e.Shutter)].Value
	}
	s.say("Camera reports %s - setting %s (%s)", e, want, why)
	if iso == FixedISO {
		return s.setExposure(want, "ISO 160 is fixed")
	}
	return s.setExposure(want, why)
}

// Meter probes and adjusts the shutter (ISO fixed at 160) until the base is at
// Target, then locks. If the base would need a shutter slower than the
// allowed limit it locks nothing and returns an error explaining what to do.
func (s *Session) Meter() (camera.Exposure, Reading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Locked = nil
	if s.Target < 0.50 || s.Target > 0.85 {
		s.Target = 0.80
	}
	slow := s.slowest()
	if err := s.checkManual(); err != nil {
		s.say("NOT METERED: %v", err)
		return camera.Exposure{}, Reading{}, err
	}
	if err := s.ensureISO160(); err != nil {
		if errors.Is(err, camera.ErrNotManual) {
			return camera.Exposure{}, Reading{}, err
		}
		return camera.Exposure{}, Reading{}, fmt.Errorf("set ISO 160: %w", err)
	}
	var tried = map[camera.Exposure]bool{}
	for it := 1; it <= 6; it++ {
		r, e, err := s.probe()
		if err != nil {
			return e, r, err
		}
		s.Reading = r
		s.say("Probe %d at %s: %s", it, e, r)
		if r.Warn != "" {
			s.say("  Note: %s", r.Warn)
		}
		if e.ISO != FixedISO {
			return e, r, fmt.Errorf("camera is at %s, not ISO 160 - set the ISO dial to A with ISO DIAL SETTING (A) = COMMAND, or set ISO 160 on the camera", e)
		}
		tried[e] = true
		if InTolerance(r, s.Target) && e.Shutter <= slow {
			s.Locked = &e
			s.say("Locked frame 0 at %s (base %.3f FS, target %.2f)", e, r.Max, s.Target)
			return e, r, nil
		}
		next, pred, note, tooDark := Solve(r, e, s.Target, slow)
		if tooDark && e.Shutter == next.Shutter {
			// Measured at the slowest allowed shutter and still too dark.
			s.say("NOT LOCKED: %s", note)
			return e, r, fmt.Errorf("%s", note)
		}
		if note != "" && !tooDark {
			s.say("%s", note)
		}
		if tooDark {
			s.say("Prediction says the base is too dark; probing at %s to confirm", camera.ShutterLabel(next.Shutter))
		}
		if next == e || tried[next] {
			if !r.Clipped && r.Peak < PeakMax && r.Max <= LockCap(s.Target) && e.Shutter <= slow {
				s.Locked = &e
				s.say("Locked at %s - closest possible (base %.3f FS)", e, r.Max)
				return e, r, nil
			}
			return e, r, fmt.Errorf("cannot reach the target: %s", note)
		}
		s.say("Next: %s (predicted base %.3f FS)", next, pred)
		if err := s.setExposure(next, "metering"); err != nil {
			return e, r, err
		}
	}
	return camera.Exposure{}, s.Reading, errors.New("metering did not converge in 6 probes")
}

// CapturePair shoots frame 0 at the locked exposure and +2 EV by shutter.
func (s *Session) CapturePair(dir string) (string, string, error) {
	r, err := s.CaptureBracket(dir, 2)
	r = append(r, "", "")
	return r[0], r[1], err
}

// CaptureBracket shoots n frames (0, +2, +4 EV ...) with the camera's own
// file names (tests); see CaptureBracketEV.
func (s *Session) CaptureBracket(dir string, n int) ([]string, error) {
	evs := []int{0}
	for k := 1; k < n; k++ {
		evs = append(evs, 2*k)
	}
	c, err := s.CaptureBracketEV(dir, evs, nil)
	if c == nil {
		return nil, err
	}
	return c.Raws, err
}

// Shot is a captured bracket.
type Shot struct {
	Raws      []string  // RAW paths, frame 0 first
	EVs       []int     // EV step of each frame
	Exposures []float64 // shutter seconds x ISO as set (frame 0 first)
	Base      string    // file base name ("" = camera names)
}

// CaptureBracketEV shoots one frame per EV step (0, 2, 3, 4 ...) by shutter
// only at the locked ISO and returns the downloaded RAWs in that order. All
// exposures are checked before the first shot; the camera is left at frame
// 0. If name is set it is called with frame 0's capture time and returns
// the base name; every RAW is then renamed to <base>_<0EV|+2EV|...>.<ext>
// (never overwriting).
func (s *Session) CaptureBracketEV(dir string, evs []int, name func(time.Time) string) (*Shot, error) {
	return s.CaptureBracketEVCtx(context.Background(), dir, evs, name)
}

// CaptureBracketEVCtx (v0.83) is CaptureBracketEV with a Stop: ctx is
// checked before every shot; on cancel the camera goes back to frame 0 and
// the shot so far is returned with ctx.Err() (the caller deletes its RAWs).
func (s *Session) CaptureBracketEVCtx(ctx context.Context, dir string, evs []int, name func(time.Time) string) (*Shot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Locked == nil {
		return nil, errors.New("meter and lock the exposure first")
	}
	if len(evs) < 2 {
		evs = []int{0, 2}
	}
	es, err := BracketEV(*s.Locked, evs)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	sh := &Shot{EVs: evs}
	for k, e := range es {
		if err := ctxErr(ctx); err != nil {
			if k > 0 {
				if serr := s.setExposure(es[0], "back to frame 0 (stopped)"); serr != nil {
					s.say("Warning: could not restore frame 0 exposure: %v", serr)
				}
			}
			return sh, err
		}
		label := "frame 0"
		if k > 0 {
			label = fmt.Sprintf("frame +%d EV", evs[k])
		}
		if err := s.setExposure(e, label); err != nil {
			return sh, err
		}
		x := ExposureValue(e) / ExposureValue(es[0])
		if k == 0 {
			s.say("Frame 0 at %s ...", e)
		} else {
			s.say("%s%s at %s (actual shutter %s = %.4f x frame 0's exposure time) ...", strings.ToUpper(label[:1]), label[1:], e, camera.ShutterLabel(e.Shutter), x)
		}
		t := time.Now()
		r, err := s.Dev.Capture(dir, "")
		if err != nil {
			return sh, err
		}
		if name != nil {
			if k == 0 {
				sh.Base = name(t)
			}
			ext := filepath.Ext(r)
			nr, err := renameNoClobber(r, filepath.Join(dir, sh.Base+"_"+StepTag(evs[k])+ext))
			if err != nil {
				s.say("Warning: could not rename %s: %v", filepath.Base(r), err)
			} else {
				if s.Log != nil {
					s.Log.Printf("Saved %s as %s", filepath.Base(r), filepath.Base(nr))
				}
				r = nr
			}
		}
		sh.Raws = append(sh.Raws, r)
		sh.Exposures = append(sh.Exposures, ExposureValue(e))
	}
	if err := s.setExposure(es[0], "back to frame 0"); err != nil {
		s.say("Warning: could not restore frame 0 exposure: %v", err)
	}
	return sh, nil
}

// ProcessOptions control conversion + merge.
type ProcessOptions struct {
	Merge Options
	// Ctx (v0.83) stops the processing (Stop button); on cancel every file
	// this call wrote is removed and ctx.Err() is returned.
	Ctx       context.Context
	OutDir    string // "" = folder of frame 0
	KeepTIFF  bool   // write frame TIFFs next to the HDR
	DeleteRAW bool   // delete the RAW frames after a successful merge
	LZW       bool
	Software  string
	BaseArea  image.Rectangle // known base area (pixels); empty = auto-detect
	// BaseNorm (v0.87) is a user-drawn clear-base rectangle in 0..1 image
	// coordinates. When set it is used instead of BaseArea / auto FindBase
	// (trichrome leveling's manual pick, the same as white-light Rectangle).
	BaseNorm Rect
	// BaseRegion (from the metering) is where module 1's statistic is
	// taken for "Send base to Film base"; nil = BaseArea.
	BaseRegion *basestat.Region
	LeakPct    float64 // module 1 light-leak setting (0 = 20)
	// MeteredBase is the base level metered on the probe at the frame-0
	// exposure (fraction of full scale per channel; zero = unknown). Frame
	// 0 is compared with it to catch a light change after metering.
	MeteredBase [3]float64
	// Dust is DustOff (default: the HDR is written exactly as before),
	// DustLight or DustStrong: the HDR is cleaned of dust specks.
	Dust int
	// Color (v0.66, zero = off): Cineon log / mask comp / crosstalk.
	// v0.73: every selected step is applied into the one <name>_HDR.tiff
	// (no _HDR_nodust / _HDR_cineon / _HDR_color companions); the TIFF
	// Software tag describes what was applied.
	Color    ColorOptions
	Progress func(string)
}

// Result of Process.
type Result struct {
	HDR         string
	TIFFs       []string
	Stats       Stats   // first brighter frame (+2)
	StatsN      []Stats // one per brighter frame (+2, +4 ...)
	Frames      int     // number of merged frames
	BaseArea    image.Rectangle
	BaseF0      [3]float64
	BaseHDR     [3]float64
	BaseDiff    int // pixels in the base patch that differ from frame 0 by more than 1 (rounding)
	BaseExact   int // pixels not bit-identical (includes ±1 rounding)
	BaseMax     int // largest difference in any channel
	BaseTotal   int
	BaseWhere   string // what the base patch is
	ExposureSrc string // where the exposure ratios came from
	OrderNote   string // set when hand-picked frames were reordered dark-to-bright
	LightNote   string // frame 0 vs metered base
	// Module 1 ("1. Film base") values of the base: TIFF/65535, leak
	// rejection, per-channel median over Base1Region - computed on the HDR
	// (Base1) and frame 0 (Base1F0), so they survive deleting the TIFFs.
	Base1, Base1F0 [3]float64
	Base1N         int
	Base1Region    basestat.Region
	Base1Err       string
	// Dust removal (DustStats.Level != DustOff) was applied to HDR.
	DustStats DustStats
	// Colour processing (Color enabled): ColorNote = what was applied to
	// HDR (also in its Software tag); ColorErr = why it was not applied
	// (HDR is then the linear merge).
	ColorNote, ColorErr string
	Elapsed             time.Duration
}

func (r Result) BaseCheck() string {
	if r.BaseTotal == 0 {
		return "base check: no clear-base area found"
	}
	verdict := fmt.Sprintf("HDR = frame 0 in all %d base pixels", r.BaseTotal)
	if r.BaseExact > 0 {
		verdict += fmt.Sprintf(" (%d differ by 1 = rounding)", r.BaseExact)
	}
	if r.BaseDiff > 0 {
		verdict = fmt.Sprintf("%d of %d base pixels (%.2f%%) differ from frame 0 by more than 1 (max %d)",
			r.BaseDiff, r.BaseTotal, 100*float64(r.BaseDiff)/float64(r.BaseTotal), r.BaseMax)
	}
	return fmt.Sprintf("base check over %s: frame 0 RGB %.1f/%.1f/%.1f, HDR RGB %.1f/%.1f/%.1f - %s",
		r.BaseWhere, r.BaseF0[0], r.BaseF0[1], r.BaseF0[2], r.BaseHDR[0], r.BaseHDR[1], r.BaseHDR[2], verdict)
}

// baseCompare compares a and b over the region: mean RGB, pixels that
// differ at all, by more than 1, and the largest difference.
func baseCompare(a, b []uint16, w int, reg basestat.Region) (ma, mb [3]float64, exact, diff, maxd, total int) {
	rc := reg.Rect
	for y := rc.Min.Y; y < rc.Max.Y; y++ {
		for x := rc.Min.X; x < rc.Max.X; x++ {
			if !reg.Contains(x, y) {
				continue
			}
			i := (y*w + x) * 3
			total++
			big, any := false, false
			for c := 0; c < 3; c++ {
				ma[c] += float64(a[i+c])
				mb[c] += float64(b[i+c])
				d := int(a[i+c]) - int(b[i+c])
				if d < 0 {
					d = -d
				}
				if d > 0 {
					any = true
				}
				if d > 1 {
					big = true
				}
				if d > maxd {
					maxd = d
				}
			}
			if big {
				diff++
			} else if any {
				exact++
			}
		}
	}
	if total > 0 {
		for c := 0; c < 3; c++ {
			ma[c] /= float64(total)
			mb[c] /= float64(total)
		}
	}
	return
}

// baseSpread explains a failed module-1 statistic: brightness (R+G+B)
// percentiles over the region and the share within the accepted window
// (0.8 to 1+leak of the median).
func baseSpread(pix []uint16, w, h int, reg basestat.Region, leak float64) string {
	rc := reg.Rect.Intersect(image.Rect(0, 0, w, h))
	sx, sy := basestat.Steps(rc.Dx(), rc.Dy())
	var l []float64
	for y := rc.Min.Y; y < rc.Max.Y; y += sy {
		for x := rc.Min.X; x < rc.Max.X; x += sx {
			if reg.Contains(x, y) {
				i := (y*w + x) * 3
				l = append(l, float64(int(pix[i])+int(pix[i+1])+int(pix[i+2]))/65535)
			}
		}
	}
	if len(l) < 50 {
		return fmt.Sprintf("only %d samples in the region", len(l))
	}
	sort.Float64s(l)
	p := func(q float64) float64 { return l[int(q*float64(len(l)-1))] }
	med := p(0.5)
	in := 0
	for _, v := range l {
		if v >= 0.8*med && v <= (1+leak/100)*med {
			in++
		}
	}
	ev := 0.0
	if p(0.05) > 0 {
		ev = math.Log2(p(0.95) / p(0.05))
	}
	return fmt.Sprintf("%d samples, R+G+B 5/50/95%% = %.3f/%.3f/%.3f (%.2f EV spread); %.0f%% lie within 0.8-%.2f x the median, 40%% are needed",
		len(l), p(0.05), med, p(0.95), ev, 100*float64(in)/float64(len(l)), 1+leak/100)
}

func uniqueFile(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	b := p[:len(p)-len(ext)]
	for i := 2; ; i++ {
		q := fmt.Sprintf("%s_%d%s", b, i, ext)
		if _, err := os.Stat(q); err != nil {
			return q
		}
	}
}

// Process converts both frames on the linear path, merges them and writes
// <frame0>_HDR.tiff.
func Process(src0, src2 string, o ProcessOptions) (*Result, error) {
	return ProcessN([]string{src0, src2}, o)
}

// ProcessN converts frame 0 and the brighter frames (+2, +4 EV ...) on the
// linear path, merges them in frame-0 scale and writes <frame0>_HDR.tiff.
func ProcessN(srcs []string, o ProcessOptions) (retRes *Result, retErr error) {
	start := time.Now()
	say := func(f string, a ...any) {
		if o.Progress != nil {
			o.Progress(fmt.Sprintf(f, a...))
		}
	}
	var wrote []string
	defer func() {
		if retErr != nil && stopped(o.Ctx) {
			removeAll(wrote)
			retRes = nil
		}
	}()
	if len(srcs) < 2 {
		return nil, errors.New("need frame 0 and at least one brighter frame")
	}
	src0 := srcs[0]
	dir := o.OutDir
	if dir == "" {
		dir = filepath.Dir(src0)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var names []string
	for _, s := range srcs {
		names = append(names, filepath.Base(s))
	}
	say("Converting %s ...", strings.Join(names, ", "))
	planes := make([]*Plane, len(srcs))
	errs := make([]error, len(srcs))
	var wg sync.WaitGroup
	for k := range srcs {
		wg.Add(1)
		go func(k int) { defer wg.Done(); planes[k], errs[k] = Load(srcs[k]) }(k)
	}
	wg.Wait()
	defer func() {
		for _, p := range planes {
			p.Free()
		}
	}()
	for k, e := range errs {
		if e != nil {
			if k == 0 {
				return nil, fmt.Errorf("frame 0: %w", e)
			}
			return nil, fmt.Errorf("frame %d (%s): %w", k, filepath.Base(srcs[k]), e)
		}
	}
	res := &Result{}
	mo := o.Merge
	if !(len(mo.Exposure) == len(srcs) && nominalRatios(mo.Exposure, len(srcs)) != nil) {
		// Hand-picked files (Merge files, CLI): put them in dark-to-bright
		// order. Frame 0 must be the darkest: it sets the HDR scale, the film
		// base and the output name. Exposure x ISO from the RAW EXIF decides
		// when every file has it, otherwise the frames' measured brightness.
		if ord, how := frameOrder(srcs, planes); ord != nil {
			ns, np := make([]string, len(srcs)), make([]*Plane, len(planes))
			for i, k := range ord {
				ns[i], np[i] = srcs[k], planes[k]
			}
			var was []string
			for _, s := range srcs {
				was = append(was, filepath.Base(s))
			}
			srcs, planes = ns, np
			src0 = srcs[0]
			if o.OutDir == "" {
				dir = filepath.Dir(src0)
			}
			res.OrderNote = fmt.Sprintf("the files were not in dark-to-bright order (%s); merged as %s, ordered by %s",
				strings.Join(was, ", "), strings.Join(baseNames(srcs), " < "), how)
			say("Reordered frames: %s", res.OrderNote)
		}
	}
	p0 := planes[0]
	sw := o.Software
	if len(mo.Exposure) == len(srcs) && nominalRatios(mo.Exposure, len(srcs)) != nil {
		res.ExposureSrc = "shutter speeds set by the app"
	} else {
		mo.Exposure = nil
		res.ExposureSrc = "unknown (not RAW files)"
		var ex []float64
		for _, s := range srcs {
			if !rawconv.IsRaw(s) {
				ex = nil
				break
			}
			iso, sh, err := rawconv.ReadExposure(s)
			if err != nil || iso <= 0 || sh <= 0 {
				ex = nil
				res.ExposureSrc = "unknown (no exposure time in the RAW EXIF)"
				break
			}
			ex = append(ex, iso*sh)
		}
		if ex != nil {
			if nominalRatios(ex, len(ex)) != nil {
				mo.Exposure = ex
				res.ExposureSrc = "RAW EXIF exposure time x ISO"
			} else {
				res.ExposureSrc = "unknown (RAW EXIF exposures do not increase)"
			}
		}
	}
	if err := ctxErr(o.Ctx); err != nil {
		return nil, err
	}
	if o.KeepTIFF {
		for _, p := range planes {
			if !rawconv.IsRaw(p.Source) {
				continue
			}
			if err := ctxErr(o.Ctx); err != nil {
				return nil, err
			}
			b := filepath.Base(p.Source)
			t := uniqueFile(filepath.Join(dir, strings.TrimSuffix(b, filepath.Ext(b))+".tiff"))
			wrote = append(wrote, t)
			if err := rawconv.WriteTIFF16(t, p.W, p.H, p.Pix, o.LZW, p.Make, p.Model, sw); err != nil {
				return nil, err
			}
			res.TIFFs = append(res.TIFFs, t)
		}
	}
	if err := ctxErr(o.Ctx); err != nil {
		return nil, err
	}
	say("Merging ...")
	out, sts, err := MergeN(planes, mo)
	if err != nil {
		return nil, err
	}
	if err := ctxErr(o.Ctx); err != nil {
		return nil, err
	}
	res.Stats, res.StatsN, res.Frames = sts[0], sts, 1
	for _, st := range sts {
		if !st.Dropped {
			res.Frames++
		}
	}
	// v0.73: one output file per frame. Dust removal and the colour
	// options are applied in memory and written into <name>_HDR.tiff;
	// the base statistics below are taken on the linear (cleaned) merge.
	mk, md := p0.Make, p0.Model
	if o.Dust != DustOff {
		say("Removing dust (%s) ...", DustName(o.Dust))
		clean, ds := despeckleHDR(p0, out, o)
		res.DustStats = ds
		out = clean
	}
	sws := sw + " HDR"
	if o.Dust != DustOff {
		sws += ", dust removal " + DustName(o.Dust)
	}
	area := o.BaseArea.Intersect(image.Rect(0, 0, p0.W, p0.H))
	if area.Empty() {
		area, _ = FindBasePlane(p0)
	}
	reg := basestat.Region{Rect: area}
	if o.BaseRegion != nil && !o.BaseRegion.Rect.Empty() {
		reg = *o.BaseRegion
		reg.Rect = reg.Rect.Intersect(image.Rect(0, 0, p0.W, p0.H))
	}
	if !reg.Rect.Empty() {
		res.BaseArea = reg.Rect
		var maxd int
		res.BaseF0, res.BaseHDR, res.BaseExact, res.BaseDiff, maxd, res.BaseTotal = baseCompare(p0.Pix, out, p0.W, reg)
		res.BaseMax = maxd
		if reg.Mask != nil {
			res.BaseWhere = fmt.Sprintf("the metered clear base (%d px in %d-px cells within (%d,%d)-(%d,%d))",
				res.BaseTotal, reg.Bin, reg.Rect.Min.X, reg.Rect.Min.Y, reg.Rect.Max.X, reg.Rect.Max.Y)
		} else {
			res.BaseWhere = fmt.Sprintf("clear base (%d,%d)-(%d,%d)", reg.Rect.Min.X, reg.Rect.Min.Y, reg.Rect.Max.X, reg.Rect.Max.Y)
		}
		if m := o.MeteredBase; m[0] > 0 && res.BaseTotal > 0 {
			c := 0
			for i := 1; i < 3; i++ {
				if m[i] > m[c] {
					c = i
				}
			}
			f := res.BaseF0[c] / 65535 / m[c]
			if ev := math.Log2(f); math.Abs(ev) > 0.25 {
				res.LightNote = fmt.Sprintf("frame 0's base (%s %.3f FS) is %+.2f EV off the metered base (%s %.3f FS) at the same exposure - did the light or the film change after metering?",
					chName[c], res.BaseF0[c]/65535, ev, chName[c], m[c])
			}
		}
	}
	if !reg.Rect.Empty() {
		res.Base1Region = reg
		leak := o.LeakPct
		if leak <= 0 {
			leak = 20
		}
		var err1 error
		res.Base1, _, res.Base1N, err1 = basestat.Measure(basestat.PlaneGetter(out, p0.W), p0.W, p0.H, reg, leak)
		if err1 == nil {
			res.Base1F0, _, _, err1 = basestat.Measure(basestat.PlaneGetter(p0.Pix, p0.W), p0.W, p0.H, reg, leak)
		}
		if err1 != nil {
			res.Base1Err = err1.Error() + " - frame 0 over the metered base region: " + baseSpread(p0.Pix, p0.W, p0.H, reg, leak)
		}
	}
	final := out
	if o.Color.Enabled() {
		if cp, desc, err := applyColor(o.Color, res, out, say); err != nil {
			res.ColorErr = err.Error()
			sws += ", encoding linear (colour processing not applied: " + err.Error() + ")"
		} else {
			final, res.ColorNote = cp, desc
			sws += ", " + desc
		}
	}
	if err := ctxErr(o.Ctx); err != nil {
		return nil, err
	}
	hdr := uniqueFile(HDRPath(dir, src0))
	say("Writing %s ...", filepath.Base(hdr))
	wrote = append(wrote, hdr)
	if err := rawconv.WriteTIFF16(hdr, p0.W, p0.H, final, o.LZW, mk, md, sws); err != nil {
		return nil, err
	}
	if err := ctxErr(o.Ctx); err != nil {
		return nil, err // stopped while writing: the HDR is removed too
	}
	res.HDR = hdr
	if o.DeleteRAW {
		for _, s := range srcs {
			if rawconv.IsRaw(s) {
				os.Remove(s)
			}
		}
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

// despeckleHDR cleans the merged HDR with the film base protected: the
// base region used for the base values (metered, or found on frame 0) and
// every spot whose background is near the base level stay unchanged.
func despeckleHDR(p0 *Plane, out []uint16, o ProcessOptions) ([]uint16, DustStats) {
	pix, st, _ := despeckleHDRDebug(p0, out, o, false)
	return pix, st
}

func despeckleHDRDebug(p0 *Plane, out []uint16, o ProcessOptions, debug bool) ([]uint16, DustStats, []DustSpot) {
	full := image.Rect(0, 0, p0.W, p0.H)
	area := o.BaseArea.Intersect(full)
	if area.Empty() {
		area, _ = FindBasePlane(p0)
	}
	reg := basestat.Region{Rect: area}
	if o.BaseRegion != nil && !o.BaseRegion.Rect.Empty() {
		reg = *o.BaseRegion
		reg.Rect = reg.Rect.Intersect(full)
	}
	var protect func(x, y int) bool
	baseSum := 0.0
	if !reg.Rect.Empty() {
		// grow the protected region by 8 px so no fill reaches into it
		g := reg
		g.Rect = reg.Rect.Inset(-8).Intersect(full)
		protect = func(x, y int) bool {
			if !(image.Point{x, y}).In(g.Rect) {
				return false
			}
			for dy := -8; dy <= 8; dy += 8 {
				for dx := -8; dx <= 8; dx += 8 {
					if reg.Contains(x+dx, y+dy) {
						return true
					}
				}
			}
			return false
		}
		var s [3]float64
		n := 0
		step := 1
		for reg.Rect.Dx()*reg.Rect.Dy()/(step*step) > 400000 {
			step++
		}
		for y := reg.Rect.Min.Y; y < reg.Rect.Max.Y; y += step {
			for x := reg.Rect.Min.X; x < reg.Rect.Max.X; x += step {
				if reg.Contains(x, y) {
					i := (y*p0.W + x) * 3
					for c := 0; c < 3; c++ {
						s[c] += float64(out[i+c])
					}
					n++
				}
			}
		}
		if n > 50 {
			baseSum = (s[0] + s[1] + s[2]) / float64(n)
		}
	}
	return despeckle(out, p0.W, p0.H, o.Dust, protect, baseSum, debug)
}

// ProbeBase decodes the locked probe RAW on the same linear LibRaw path as
// the frame TIFFs and applies module 1's statistic over the metered base.
// The probe was shot at the locked frame-0 exposure.
func (s *Session) ProbeBase(leakPct float64) ([3]float64, int, error) {
	s.mu.Lock()
	p, reg := s.LastProbePath, s.Reading.Region
	s.mu.Unlock()
	if p == "" || reg.Rect.Empty() {
		return [3]float64{}, 0, errors.New("no metered probe")
	}
	pl, err := Load(p)
	if err != nil {
		return [3]float64{}, 0, err
	}
	defer pl.Free()
	if leakPct <= 0 {
		leakPct = 20
	}
	rgb, _, n, err := basestat.Measure(basestat.PlaneGetter(pl.Pix, pl.W), pl.W, pl.H, reg, leakPct)
	return rgb, n, err
}

func baseNames(p []string) []string {
	var b []string
	for _, s := range p {
		b = append(b, filepath.Base(s))
	}
	return b
}

// frameOrder returns the dark-to-bright permutation of the frames, or nil
// when they already are in that order (or the order cannot be told). RAW
// EXIF exposure x ISO decides when every file has it; otherwise the mean
// level of a pixel subsample (clipping only compresses it, it keeps the
// order). Frames whose levels are within 15% are treated as equal so noise
// never swaps them.
func frameOrder(srcs []string, planes []*Plane) ([]int, string) {
	n := len(srcs)
	key := make([]float64, n)
	how := "RAW EXIF exposure time x ISO"
	for k, s := range srcs {
		if !rawconv.IsRaw(s) {
			key = nil
			break
		}
		iso, sh, err := rawconv.ReadExposure(s)
		if err != nil || iso <= 0 || sh <= 0 {
			key = nil
			break
		}
		key[k] = iso * sh
	}
	tol := 1.0
	for i := 0; key != nil && i < n; i++ {
		for j := i + 1; j < n; j++ {
			if key[i] == key[j] {
				key = nil // EXIF cannot tell these frames apart
				break
			}
		}
	}
	if key == nil {
		key, how, tol = make([]float64, n), "measured frame brightness", 1.15
		for k, p := range planes {
			key[k] = meanLevel(p)
		}
	}
	ord := make([]int, n)
	for i := range ord {
		ord[i] = i
	}
	less := func(a, b int) bool { return key[a]*tol < key[b] }
	sort.SliceStable(ord, func(i, j int) bool { return less(ord[i], ord[j]) })
	same := true
	for i, k := range ord {
		if k != i {
			same = false
		}
	}
	if same {
		return nil, ""
	}
	for i := 1; i < n; i++ {
		// only reorder when the result is strictly dark-to-bright
		if !less(ord[i-1], ord[i]) {
			return nil, ""
		}
	}
	return ord, how
}

func meanLevel(p *Plane) float64 {
	if p == nil || len(p.Pix) == 0 {
		return 0
	}
	var sum float64
	var cnt int
	for i := 0; i < len(p.Pix); i += 3 * 61 {
		sum += float64(p.Pix[i]) + float64(p.Pix[i+1]) + float64(p.Pix[i+2])
		cnt++
	}
	return sum / float64(cnt)
}

// ctxErr is ctx.Err() for an optional ctx.
func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func stopped(ctx context.Context) bool { return ctxErr(ctx) != nil }

// removeAll deletes files (missing ones are fine).
func removeAll(fs []string) {
	for _, f := range fs {
		if f != "" {
			os.Remove(f)
		}
	}
}
