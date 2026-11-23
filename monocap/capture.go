package monocap

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"basetool/hdrscan"
	"basetool/hikcam"
	"basetool/scanmeta"
)

// Light switches the trichrome light. rgblight.ChannelSwitch satisfies it.
type Light interface {
	Only(ch int) error
	Off() error
}

// SimLight drives the simulator's light state.
type SimLight struct{ S *hikcam.Sim }

func (l SimLight) Only(ch int) error {
	l.S.LightChannel = func() int { return ch }
	l.S.SetDark(false)
	return nil
}

func (l SimLight) Off() error { l.S.SetDark(true); return nil }

var tags = [3]string{"R", "G", "B"}

// Settle check defaults.
const (
	DefaultSettleTol     = 0.003 // relative change of the frame mean between grabs
	DefaultSettleTimeout = 3 * time.Second
)

// Options control one R/G/B capture.
type Options struct {
	Cam   hikcam.Device
	Light Light // nil: the light is not switched (manual)
	// Settle is the minimum wait after switching the light (the existing
	// setting). The settle check then grabs until two consecutive frame
	// means agree within SettleTol, or SettleTimeout passes.
	Settle        time.Duration
	SettleTol     float64
	SettleTimeout time.Duration
	NoSettleCheck bool
	BaseUS        float64 // shared exposure
	MeterEach     bool    // per-channel auto-meter (optional, default off)
	MeterBox      hdrscan.Rect
	EVs           []int // bracket; must include 0
	// FramesPerExposure same-exposure grabs averaged per bracket step (>=1).
	FramesPerExposure int
	Noise             hdrscan.MonoNoise
	Calib             Store
	UseDark, UseFlat  bool
	Log               func(string)
	Status            func(string)
}

// Result is three black-subtracted linear planes plus the capture record.
type Result struct {
	Planes  [3]hdrscan.MonoPlane
	Mono    scanmeta.MonoCapture
	HDR     [3]string
	Settle  [3]string
	Notes   []string // English warnings for the page
	Elapsed time.Duration
}

func (o *Options) log(f string, a ...any) {
	if o.Log != nil {
		o.Log(fmt.Sprintf(f, a...))
	}
}

func (o *Options) status(f string, a ...any) {
	if o.Status != nil {
		o.Status(fmt.Sprintf(f, a...))
	}
}

func (o *Options) norm() {
	if o.SettleTol <= 0 {
		o.SettleTol = DefaultSettleTol
	}
	if o.SettleTimeout <= 0 {
		o.SettleTimeout = DefaultSettleTimeout
	}
	if o.FramesPerExposure < 1 {
		o.FramesPerExposure = 1
	}
	if o.FramesPerExposure > 16 {
		o.FramesPerExposure = 16
	}
	if len(o.EVs) == 0 {
		o.EVs = []int{0}
	}
}

// FrameMean is the pedestal-subtracted mean of a sparse grid of the frame.
func FrameMean(fr *hikcam.Frame) float64 {
	if fr == nil || fr.W <= 0 || fr.H <= 0 {
		return 0
	}
	step := max(1, min(fr.W, fr.H)/64)
	var s float64
	var n int
	for y := step / 2; y < fr.H; y += step {
		row := fr.Pix[y*fr.W:]
		for x := step / 2; x < fr.W; x += step {
			s += float64(row[x])
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return math.Max(0, s/float64(n)-fr.Black16)
}

// SettleState decides when the light output is stable.
type SettleState struct {
	Tol   float64
	prev  float64
	n     int
	Delta float64 // last relative change
}

// Add a frame mean; true when it agrees with the previous one within Tol.
func (s *SettleState) Add(mean float64) bool {
	s.n++
	if s.n == 1 {
		s.prev = mean
		s.Delta = math.Inf(1)
		return false
	}
	ref := math.Max(math.Max(s.prev, mean), 65535*0.002)
	s.Delta = math.Abs(mean-s.prev) / ref
	s.prev = mean
	return s.Delta <= s.Tol
}

// waitSettled grabs at the current settings until the light is stable.
func (o *Options) waitSettled(ch int) (string, error) {
	if o.NoSettleCheck {
		return fmt.Sprintf("fixed wait %v", o.Settle), nil
	}
	st := SettleState{Tol: o.SettleTol}
	deadline := time.Now().Add(o.SettleTimeout)
	for i := 0; ; i++ {
		fr, err := o.Cam.Grab(grabTimeout(o.Cam.Settings().ExposureUS))
		if err != nil {
			return "", fmt.Errorf("%s settle grab: %w", tags[ch], err)
		}
		if st.Add(FrameMean(fr)) {
			return fmt.Sprintf("stable after %v + %d check grabs (change %.2f%%)", o.Settle, i+1, 100*st.Delta), nil
		}
		if time.Now().After(deadline) || i >= 40 {
			return fmt.Sprintf("NOT stable after %v + %v (last change %.2f%%, limit %.2f%%) - captured anyway", o.Settle, o.SettleTimeout, 100*st.Delta, 100*o.SettleTol), nil
		}
	}
}

func grabTimeout(us float64) time.Duration {
	t := hikcam.GrabTimeout(us)
	if t < 5*time.Second {
		t = 5 * time.Second
	}
	return t
}

// grabAveraged grabs n frames at the current settings and averages them.
func (o *Options) grabAveraged(n int) (*hikcam.Frame, error) {
	var first *hikcam.Frame
	stack := make([][]uint16, 0, n)
	for i := 0; i < n; i++ {
		fr, err := o.Cam.Grab(grabTimeout(o.Cam.Settings().ExposureUS))
		if err != nil {
			return nil, err
		}
		if first == nil {
			first = fr
		} else if fr.W != first.W || fr.H != first.H {
			return nil, errors.New("frame size changed during averaging")
		}
		stack = append(stack, fr.Pix)
	}
	out := *first
	out.Pix = hdrscan.AverageFrames(stack)
	return &out, nil
}

// Capture shoots R, then G, then B and returns black-subtracted planes.
func Capture(o Options) (*Result, error) {
	o.norm()
	start := time.Now()
	cam := o.Cam
	res := &Result{}
	res.Mono.FramesPerExposure = o.FramesPerExposure
	res.Mono.Dark, res.Mono.Flat = "off", "off"
	if o.UseDark {
		res.Mono.Dark = "none found"
	}
	if o.UseFlat {
		res.Mono.Flat = "none found"
	}
	var darkIDs, flatIDs []string
	addID := func(list *[]string, id string) {
		for _, v := range *list {
			if v == id {
				return
			}
		}
		*list = append(*list, id)
	}
	var hdrNotes []string
	for ch := 0; ch < 3; ch++ {
		if o.Light != nil {
			if err := o.Light.Only(ch); err != nil {
				return nil, fmt.Errorf("light %s only: %w", tags[ch], err)
			}
		}
		time.Sleep(o.Settle)
		st := cam.Settings()
		if !o.MeterEach {
			st.ExposureUS = o.BaseUS
			st.GainDB = 0
			if err := cam.Apply(st); err != nil {
				return nil, fmt.Errorf("%s apply shared exposure: %w", tags[ch], err)
			}
		}
		note, err := o.waitSettled(ch)
		if err != nil {
			return nil, err
		}
		res.Settle[ch] = note
		o.log("%s light settle: %s", tags[ch], note)
		if strings.HasPrefix(note, "NOT") {
			res.Notes = append(res.Notes, fmt.Sprintf("WARNING: %s light did not settle: %s", tags[ch], note))
		}
		if o.MeterEach {
			cfg := hdrscan.ScanMeterConfig()
			for attempt := 0; attempt < 4; attempt++ {
				fr, err := cam.Grab(grabTimeout(st.ExposureUS))
				if err != nil {
					return nil, fmt.Errorf("%s meter grab: %w", tags[ch], err)
				}
				full := hdrscan.FrameSamples(fr.Pix, fr.W, fr.H, hdrscan.Rect{})
				base := hdrscan.FrameSamples(fr.Pix, fr.W, fr.H, o.MeterBox)
				cur := hdrscan.ExposureState{ExposureUS: st.ExposureUS, GainDB: st.GainDB}
				next, why := hdrscan.MeterScanExposure(full, base, cur, cfg)
				o.log("%s meter %d: %s (%.0f us, %.2f dB)", tags[ch], attempt+1, why, next.ExposureUS, next.GainDB)
				if why == "on target" || (math.Abs(next.ExposureUS-st.ExposureUS) < 1 && math.Abs(next.GainDB-st.GainDB) < 0.05) {
					break
				}
				st.ExposureUS, st.GainDB = next.ExposureUS, next.GainDB
				if err := cam.Apply(st); err != nil {
					return nil, fmt.Errorf("%s apply metered exposure: %w", tags[ch], err)
				}
			}
		}
		var frames []hdrscan.MonoFrame
		evs := append([]int{0}, nonZero(o.EVs)...)
		for _, ev := range evs {
			evSt := st
			if ev != 0 {
				evSt.ExposureUS = math.Min(hikcam.CS240ExpMaxUS, st.ExposureUS*hdrscan.EVScale(ev))
				if err := cam.Apply(evSt); err != nil {
					return nil, fmt.Errorf("%s %+d EV apply: %w", tags[ch], ev, err)
				}
			}
			fr, err := o.grabAveraged(o.FramesPerExposure)
			if err != nil {
				return nil, fmt.Errorf("%s %+d EV grab: %w", tags[ch], ev, err)
			}
			mf := hdrscan.MonoFrame{W: fr.W, H: fr.H, Pix: fr.Pix, Black16: fr.Black16, White16: hikcam.White16(fr.PixelFormat),
				ExposureUS: fr.ExposureUS, GainDB: fr.GainDB, EV: ev, Frames: o.FramesPerExposure}
			if res.Mono.PixelFormat == "" {
				res.Mono.PixelFormat = fr.PixelFormat
				res.Mono.BlackSubtracted16 = fr.Black16
				res.Mono.BlackLevelDN = fr.Black16 / hikcam.Scale16(fr.PixelFormat)
			}
			if o.UseDark {
				if d := o.Calib.FindDark(fr.ExposureUS, fr.GainDB, fr.PixelFormat, fr.W, fr.H); d != nil {
					mf.Dark = d.Pix
					addID(&darkIDs, d.ID)
				} else {
					msg := fmt.Sprintf("no dark frame for %.0f us %s %dx%d: BlackLevel pedestal subtracted instead", fr.ExposureUS, fr.PixelFormat, fr.W, fr.H)
					addID(&res.Notes, msg)
				}
			}
			frames = append(frames, mf)
			o.log("Captured %s %+d EV: %dx%d %s (%.0f us, %d frame(s) averaged)", tags[ch], ev, fr.W, fr.H, fr.PixelFormat, fr.ExposureUS, o.FramesPerExposure)
			o.status("Captured %s %+d EV (%.0f us) ...", tags[ch], ev, fr.ExposureUS)
		}
		_ = cam.Apply(st)
		if len(frames) > 1 {
			o.status("Merging %s HDR ...", tags[ch])
		}
		plane, ms, err := hdrscan.MergeMonoNative(frames, o.Noise)
		if err != nil {
			return nil, fmt.Errorf("%s HDR merge: %w", tags[ch], err)
		}
		res.HDR[ch] = ms.String()
		if len(frames) > 1 {
			o.log("%s %s", tags[ch], ms.String())
			hdrNotes = append(hdrNotes, tags[ch]+": "+ms.String())
		}
		if o.UseFlat {
			if f := o.Calib.FindFlat(ch, res.Mono.PixelFormat, plane.W, plane.H); f != nil {
				p, err := hdrscan.ApplyFlat(plane, f.Pix)
				if err != nil {
					return nil, fmt.Errorf("%s flat field: %w", tags[ch], err)
				}
				plane = p
				addID(&flatIDs, f.ID)
			} else {
				addID(&res.Notes, fmt.Sprintf("no %s flat field for %s %dx%d: not flat-field corrected", tags[ch], res.Mono.PixelFormat, plane.W, plane.H))
			}
		}
		res.Planes[ch] = plane
	}
	if len(darkIDs) > 0 {
		res.Mono.Dark = strings.Join(darkIDs, "; ")
	}
	if len(flatIDs) > 0 {
		res.Mono.Flat = strings.Join(flatIDs, "; ")
		if len(flatIDs) < 3 {
			res.Mono.Flat += " (incomplete: some channels not corrected)"
		}
	}
	if len(hdrNotes) > 0 {
		res.Mono.HDR = strings.Join(hdrNotes, " | ")
	} else {
		res.Mono.HDR = "single exposure per channel"
	}
	res.Mono.Settle = fmt.Sprintf("R %s; G %s; B %s", res.Settle[0], res.Settle[1], res.Settle[2])
	res.Elapsed = time.Since(start)
	return res, nil
}

func nonZero(evs []int) []int {
	var out []int
	for _, e := range evs {
		if e != 0 {
			out = append(out, e)
		}
	}
	return out
}

// ---- calibration capture ----------------------------------------------------

// CalOptions control dark / flat capture.
type CalOptions struct {
	Cam      hikcam.Device
	Light    Light
	Settle   time.Duration
	Frames   int // grabs averaged per calibration frame (default 16)
	Store    Store
	Software string
	LightRGB [3]int
	Log      func(string)
	Status   func(string)
}

func (o *CalOptions) log(f string, a ...any) {
	if o.Log != nil {
		o.Log(fmt.Sprintf(f, a...))
	}
}

func (o *CalOptions) frames() int {
	if o.Frames < 2 {
		return 16
	}
	return o.Frames
}

func (o *CalOptions) grab(n int) (*hikcam.Frame, float64, error) {
	var first *hikcam.Frame
	var stack [][]uint16
	for i := 0; i < n; i++ {
		fr, err := o.Cam.Grab(grabTimeout(o.Cam.Settings().ExposureUS))
		if err != nil {
			return nil, 0, err
		}
		if first == nil {
			first = fr
		}
		stack = append(stack, fr.Pix)
	}
	// temporal noise of one grab from the first two frames
	noise := 0.0
	if len(stack) >= 2 {
		var s, s2, k float64
		a, b := stack[0], stack[1]
		for i := 0; i < len(a); i += 7 {
			d := float64(a[i]) - float64(b[i])
			s += d
			s2 += d * d
			k++
		}
		m := s / k
		noise = math.Sqrt(math.Max(0, s2/k-m*m) / 2)
	}
	out := *first
	out.Pix = hdrscan.AverageFrames(stack)
	return &out, noise, nil
}

// CaptureDarks turns the light off and stores an averaged dark frame for
// each exposure (gain 0 dB, current pixel format).
func CaptureDarks(o CalOptions, exposures []float64) ([]CalHeader, error) {
	if o.Light != nil {
		if err := o.Light.Off(); err != nil {
			return nil, fmt.Errorf("light off: %w", err)
		}
	}
	time.Sleep(o.Settle)
	st0 := o.Cam.Settings()
	defer func() { _ = o.Cam.Apply(st0) }()
	var out []CalHeader
	for _, us := range exposures {
		st := st0
		st.ExposureUS = us
		st.GainDB = 0
		if err := o.Cam.Apply(st); err != nil {
			return out, err
		}
		if o.Status != nil {
			o.Status(fmt.Sprintf("Dark frames at %.0f us ...", us))
		}
		fr, noise, err := o.grab(o.frames())
		if err != nil {
			return out, err
		}
		var sum float64
		for _, v := range fr.Pix {
			sum += float64(v)
		}
		mean := sum / float64(len(fr.Pix))
		now := tagTime()
		h := CalHeader{Kind: "dark", Created: shanghai(now), Software: o.Software, Camera: o.Cam.Info().Model,
			W: fr.W, H: fr.H, PixelFormat: fr.PixelFormat, ExposureUS: fr.ExposureUS, GainDB: fr.GainDB,
			BlackLevelDN: st.BlackLevel, Black16: fr.Black16, Frames: o.frames(), Channel: -1, Mean16: mean, ReadNoise16: noise}
		h.ID = fmt.Sprintf("dark %.0fus %s %dx%d x%d %s", h.ExposureUS, h.PixelFormat, h.W, h.H, h.Frames, h.Created)
		if mean > fr.Black16+0.05*65535 {
			return out, fmt.Errorf("dark frame at %.0f us is not dark (mean %.0f, pedestal %.0f): cover the lens or switch the light off", us, mean, fr.Black16)
		}
		path, err := o.Store.Save(CalFrame{CalHeader: h, Pix: fr.Pix})
		if err != nil {
			return out, err
		}
		sc := hikcam.Scale16(fr.PixelFormat)
		o.log("Saved %s: mean %.1f DN (pedestal %.1f DN), temporal noise %.2f DN per grab (native DN, %s)",
			path, mean/sc, fr.Black16/sc, noise/sc, fr.PixelFormat)
		out = append(out, h)
	}
	return out, nil
}

// CaptureFlats shoots the empty gate under R, G and B, adjusting the
// exposure so the centre sits near 45 % of full scale, and stores a
// normalised gain map per channel (1.0 at the centre median).
func CaptureFlats(o CalOptions, startUS float64) ([]CalHeader, error) {
	st0 := o.Cam.Settings()
	defer func() { _ = o.Cam.Apply(st0) }()
	if sim, ok := o.Cam.(*hikcam.Sim); ok {
		sim.SetFlatField(true)
		defer sim.SetFlatField(false)
	}
	var out []CalHeader
	for ch := 0; ch < 3; ch++ {
		if o.Light != nil {
			if err := o.Light.Only(ch); err != nil {
				return out, fmt.Errorf("light %s only: %w", tags[ch], err)
			}
		}
		time.Sleep(o.Settle)
		st := st0
		st.GainDB = 0
		st.ExposureUS = startUS
		var fr *hikcam.Frame
		for it := 0; it < 6; it++ {
			if err := o.Cam.Apply(st); err != nil {
				return out, err
			}
			f, err := o.Cam.Grab(grabTimeout(st.ExposureUS))
			if err != nil {
				return out, err
			}
			fr = f
			head := hikcam.White16(f.PixelFormat) - f.Black16
			m := centreMedian(f) / head
			if m > 0.35 && m < 0.55 {
				break
			}
			ratio := 0.45 / math.Max(m, 0.005)
			if m >= 0.97 {
				ratio = 0.25
			}
			ratio = math.Max(0.1, math.Min(8, ratio))
			st.ExposureUS = math.Max(hikcam.CS240ExpMinUS, math.Min(hikcam.CS240ExpMaxUS, st.ExposureUS*ratio))
			o.log("%s flat: centre %.0f%% of full scale, exposure -> %.0f us", tags[ch], 100*m, st.ExposureUS)
		}
		if o.Status != nil {
			o.Status(fmt.Sprintf("%s flat field at %.0f us ...", tags[ch], st.ExposureUS))
		}
		fr, _, err := o.grab(o.frames())
		if err != nil {
			return out, err
		}
		mf := hdrscan.MonoFrame{W: fr.W, H: fr.H, Pix: fr.Pix, Black16: fr.Black16}
		darkUsed := fmt.Sprintf("pedestal %.0f", fr.Black16)
		if d := o.Store.FindDark(fr.ExposureUS, fr.GainDB, fr.PixelFormat, fr.W, fr.H); d != nil {
			mf.Dark = d.Pix
			darkUsed = d.ID
		}
		gain, med, err := hdrscan.NormalizeFlat(hdrscan.SubtractBlack(mf))
		if err != nil {
			return out, fmt.Errorf("%s flat: %w", tags[ch], err)
		}
		now := tagTime()
		h := CalHeader{Kind: "flat", Created: shanghai(now), Software: o.Software, Camera: o.Cam.Info().Model,
			W: fr.W, H: fr.H, PixelFormat: fr.PixelFormat, ExposureUS: fr.ExposureUS, GainDB: fr.GainDB,
			BlackLevelDN: st.BlackLevel, Black16: fr.Black16, Frames: o.frames(), Channel: ch, LightRGB: o.LightRGB, Mean16: med, DarkUsed: darkUsed}
		h.ID = fmt.Sprintf("flat %s %s %dx%d x%d %s", tags[ch], h.PixelFormat, h.W, h.H, h.Frames, h.Created)
		lo, hi := gainRange(gain)
		path, err := o.Store.Save(CalFrame{CalHeader: h, Pix: gain})
		if err != nil {
			return out, err
		}
		o.log("Saved %s: centre median %.0f (16-bit), gain range %.3f..%.3f, %s subtracted", path, med, lo, hi, darkUsed)
		out = append(out, h)
	}
	return out, nil
}

func centreMedian(f *hikcam.Frame) float64 {
	var v []float64
	for y := f.H / 4; y < 3*f.H/4; y += 8 {
		for x := f.W / 4; x < 3*f.W/4; x += 8 {
			v = append(v, float64(f.Pix[y*f.W+x])-f.Black16)
		}
	}
	if len(v) == 0 {
		return 0
	}
	return quickMedian(v)
}

func quickMedian(v []float64) float64 {
	c := append([]float64(nil), v...)
	sortFloat(c)
	return c[len(c)/2]
}

func gainRange(g []uint16) (float64, float64) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := 0; i < len(g); i += 13 {
		v := float64(g[i]) / hdrscan.FlatScale
		lo = math.Min(lo, v)
		hi = math.Max(hi, v)
	}
	return lo, hi
}
