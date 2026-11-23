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

// FrameReadout is the estimated time to read one full 6016 x 4096 Mono12
// frame over USB3 (24.6 MP x 2 bytes at about 365 MB/s). Derived, not
// measured: the datasheet gives 15 fps for Mono8 at full resolution, and
// Mono12 moves twice the bytes.
const FrameReadout = 135 * time.Millisecond

// EstimateCapture is the expected R+G+B grab time for a bracket: every
// exposure plus one frame readout per grab, without light settling or
// the merge. It is what the Mono page shows before Capture.
func EstimateCapture(baseUS float64, evs []int, framesPerExposure int) time.Duration {
	if framesPerExposure < 1 {
		framesPerExposure = 1
	}
	var us float64
	for _, ev := range append([]int{0}, nonZero(evs)...) {
		us += math.Min(hikcam.CS240ExpMaxUS, baseUS*hdrscan.EVScale(ev)) + float64(FrameReadout.Microseconds())
	}
	return time.Duration(3*us*float64(framesPerExposure)) * time.Microsecond
}

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

// DefaultEVs is the recommended mono HDR bracket (v1.11): frame 0 at the
// shared film-base exposure, then +2, +3 and +5 EV.
//
// Why: with the film base leveled to 0.5 of full scale, frame 0 holds about
// 6,000 e- per pixel at the base, but the dense parts of a negative (scene
// highlights, 1.5-2.4 D above base) get only 25-190 e-, where the 2.7 e-
// read noise and Mono12 quantisation still matter. On the MV-CS240-90UM
// sensor model (12.1 ke- full well, 2.7 e- read noise) the +5 EV frame puts
// 32x the light there: SNR at 1.8 D above base goes from 35 (0/+2/+3) to
// 65, at 2.4 D from 17 to 32. Because 0/+2/+3 is kept as a subset, no
// density gets worse (0/+3/+5 would be faster but drops the +2 frame and
// loses about 40% SNR at 0.45-0.6 D, the positive's shadows). Cost: one
// more grab per channel (about 0.6 s at a 14 ms base exposure).
var DefaultEVs = []int{0, 2, 3, 5}

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
	Noise             hdrscan.MonoNoise // zero value: the MV-CS240-90UM sensor model
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
	nz := o.Noise
	if !(nz.FullWellE > 100) || !(nz.ReadE > 0) {
		nz = hdrscan.DefaultMonoNoise()
	}
	o.Noise = nz
	res.Mono.NoiseModel = fmt.Sprintf("full well %.0f e-, read noise %.2f e- at 0 dB", nz.FullWellE, nz.ReadE)
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
		res.Planes[ch] = plane
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
