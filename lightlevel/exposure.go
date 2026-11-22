package lightlevel

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// FitProbeCode is the high light-brightness probe. FitHeadroomCode is the
// comfortable code the dimmest channel should hit the target at.
// FitMinCode is the lowest code we want any channel to land on (about
// 25–30): below this, one integer step is a large fraction of the level
// and may miss the tolerance.
const (
	FitProbeCode    = 100
	FitHeadroomCode = 85
	FitMinCode      = 25
	MinExposureUS   = 32.0
	MaxExposureUS   = 10_000_000.0
)

// VariableExposure is a camera whose fixed exposure can be raised or
// lowered once before the light-code search and then held for all three
// channels. Hikrobot monochrome leveling uses this; Bayer / X-T3 cameras
// do not.
type VariableExposure interface {
	Camera
	GetExposure() (us, gainDB float64)
	SetExposure(us, gainDB float64) error
}

// ExposureFit records whether the common camera exposure was changed so
// the three light codes land in a well-resolved window.
type ExposureFit struct {
	StartedUS, FinalUS     float64
	StartedGain, FinalGain float64
	Changed                bool
	Reason                 string
}

// ledDrive is a fallback interpolator for the RGB Light Air curve when
// a code has not been measured. Measured (code, level) pairs always
// take precedence; the solver still measures every candidate code.
func ledDrive(code int) float64 {
	x := float64(code) / 100
	if x < 0 {
		x = 0
	}
	if x > 1 {
		x = 1
	}
	return 0.85*x + 0.15*x*x
}

func clampExposureUS(us float64) float64 {
	if us < MinExposureUS {
		return MinExposureUS
	}
	if us > MaxExposureUS {
		return MaxExposureUS
	}
	return us
}

type probePt struct {
	code    int
	level   float64
	clipped bool
}

func (p probePt) usable() bool { return !p.clipped && p.level > 1e-9 && p.level < 0.98 }

func estimateLevel(pts []probePt, code int) (float64, bool) {
	var u []probePt
	for _, p := range pts {
		if p.usable() {
			u = append(u, p)
		}
	}
	if len(u) == 0 {
		return 0, false
	}
	sort.Slice(u, func(i, j int) bool { return u[i].code < u[j].code })
	for _, p := range u {
		if p.code == code {
			return p.level, true
		}
	}
	interp := func(a, b probePt, c int) float64 {
		f1, f2, f := ledDrive(a.code), ledDrive(b.code), ledDrive(c)
		if math.Abs(f2-f1) < 1e-12 {
			return (a.level + b.level) / 2
		}
		return a.level + (b.level-a.level)*(f-f1)/(f2-f1)
	}
	if len(u) == 1 {
		d := ledDrive(u[0].code)
		if d <= 1e-12 {
			return 0, false
		}
		return u[0].level * ledDrive(code) / d, true
	}
	if code <= u[0].code {
		return interp(u[0], u[1], code), true
	}
	last := len(u) - 1
	if code >= u[last].code {
		return interp(u[last-1], u[last], code), true
	}
	for i := 0; i < last; i++ {
		if code >= u[i].code && code <= u[i+1].code {
			return interp(u[i], u[i+1], code), true
		}
	}
	return 0, false
}

// usForTargetAt is the exposure that would place this channel at target
// when the light is set to code, given measurements taken at curUS.
func usForTargetAt(pts []probePt, code int, curUS, target float64) (float64, bool) {
	lv, ok := estimateLevel(pts, code)
	if !ok || lv <= 1e-9 {
		return 0, false
	}
	return clampExposureUS(curUS * target / lv), true
}

// chooseCommonUS picks one exposure so light codes land in [FitMinCode,
// FitHeadroomCode] when possible. If the channel ratio cannot satisfy both
// ends, it keeps the dimmest channel reachable at 100 and raises the
// brightest channel's code as far as that allows.
func chooseCommonUS(curUS float64, pts [3][]probePt, target, tol float64) (need float64, reason string, tight bool) {
	if curUS <= 0 {
		curUS = 10_000
	}
	if target <= 0 {
		target = 0.50
	}
	var usDim100, usDimHead, usBrightMin float64
	haveDim, haveHead, haveBright := false, false, false
	dimCh, brightCh := -1, -1
	for c := 0; c < 3; c++ {
		if v, ok := usForTargetAt(pts[c], FitProbeCode, curUS, target); ok {
			if !haveDim || v > usDim100 {
				usDim100, haveDim, dimCh = v, true, c
			}
		}
		if v, ok := usForTargetAt(pts[c], FitHeadroomCode, curUS, target); ok {
			if !haveHead || v > usDimHead {
				usDimHead, haveHead, dimCh = v, true, c
			}
		}
		if v, ok := usForTargetAt(pts[c], FitMinCode, curUS, target); ok {
			if !haveBright || v < usBrightMin {
				usBrightMin, haveBright, brightCh = v, true, c
			}
		}
	}
	if !haveDim && haveHead {
		usDim100, haveDim = usDimHead, true
	}
	if !haveHead && haveDim {
		usDimHead, haveHead = usDim100, true
	}
	if !haveDim {
		return curUS, "could not measure a usable film-base response to choose an exposure", false
	}
	_ = tol
	if haveBright && usDimHead <= usBrightMin*1.01 {
		// A window exists: dimmest at <= 85 and brightest at >= 30.
		if curUS >= usDimHead*0.99 && curUS <= usBrightMin*1.01 {
			return curUS, fmt.Sprintf("exposure already places the light codes in a well-resolved window (about %d to %d)", FitMinCode, FitHeadroomCode), false
		}
		need = curUS
		if curUS < usDimHead {
			need = usDimHead
		} else if curUS > usBrightMin {
			need = usBrightMin
		}
		need = clampExposureUS(need)
		why := "adjusted"
		if need > curUS*1.005 {
			why = "raised"
		} else if need < curUS*0.995 {
			why = "lowered"
		}
		ch := "a channel"
		if need <= curUS && brightCh >= 0 {
			ch = string([]byte{"RGB"[brightCh]})
		} else if dimCh >= 0 {
			ch = string([]byte{"RGB"[dimCh]})
		}
		return need, fmt.Sprintf("%s common exposure from %.0f us to %.0f us so %s can reach the target at a light code in %d–%d (gain 0 dB)",
			why, curUS, need, ch, FitMinCode, FitHeadroomCode), false
	}
	// Impossible to satisfy both ends. Keep the dimmest channel reachable
	// at code 100; that choice also makes the brightest code as high as
	// possible without putting the dimmest out of range.
	need = clampExposureUS(usDim100)
	msg := fmt.Sprintf("channel ratio cannot place every light code in %d–%d; using %.0f us so the dimmest channel still reaches the target at code 100 and the brightest code is as high as possible (gain 0 dB)",
		FitMinCode, FitHeadroomCode, need)
	if dimCh >= 0 && brightCh >= 0 {
		msg = fmt.Sprintf("channel ratio cannot place every light code in %d–%d (%c vs %c); using %.0f us so %c still reaches the target at code 100 and the brightest code is as high as possible (gain 0 dB)",
			FitMinCode, FitHeadroomCode, "RGB"[dimCh], "RGB"[brightCh], need, "RGB"[dimCh])
	}
	return need, msg, true
}

func fitCommonExposure(ctx context.Context, l Light, cam VariableExposure, p Params, say func(string, ...any)) (ExposureFit, error) {
	return fitCommonExposureSeeded(ctx, l, cam, p, say, [3][]probePt{})
}

func fitCommonExposureSeeded(ctx context.Context, l Light, cam VariableExposure, p Params, say func(string, ...any), seed [3][]probePt) (ExposureFit, error) {
	p.Defaults()
	us, gain := cam.GetExposure()
	if us <= 0 {
		us = 10_000
	}
	fit := ExposureFit{StartedUS: us, FinalUS: us, StartedGain: gain, FinalGain: 0, Reason: "exposure already in a well-resolved window"}
	if gain != 0 {
		if err := cam.SetExposure(us, 0); err != nil {
			return fit, fmt.Errorf("set gain 0 dB: %w", err)
		}
		us, _ = cam.GetExposure()
		fit.FinalUS, fit.FinalGain = us, 0
	}

	probe := func(ch, code int) (Reading, error) {
		if err := ctx.Err(); err != nil {
			return Reading{}, err
		}
		var on [3]bool
		on[ch] = true
		v := [3]int{code, code, code}
		if err := l.SetAll(on, v); err != nil {
			return Reading{}, fmt.Errorf("light: %w", err)
		}
		select {
		case <-ctx.Done():
			return Reading{}, ctx.Err()
		case <-time.After(p.Settle):
		}
		im, err := cam.Shoot(ctx, ch, "exposure-fit")
		if err != nil {
			return Reading{}, err
		}
		return Measure(im, p.Area)
	}

	hasCode := func(pts []probePt, code int) bool {
		for _, p := range pts {
			if p.code == code {
				return true
			}
		}
		return false
	}
	hasUsable := func(pts []probePt) bool {
		for _, p := range pts {
			if p.usable() {
				return true
			}
		}
		return false
	}

	pts := seed
	add := func(ch, code int) error {
		if hasCode(pts[ch], code) {
			return nil
		}
		rd, err := probe(ch, code)
		if err != nil {
			return fmt.Errorf("exposure probe %c at %d: %w", "RGB"[ch], code, err)
		}
		pt := probePt{code: code, level: rd.Level, clipped: rd.BaseClipped() || rd.Level >= 0.98}
		pts[ch] = append(pts[ch], pt)
		if say != nil {
			extra := ""
			if pt.clipped {
				extra = " (clipped)"
			}
			say("Exposure probe: %c at brightness %d = %.5f FS%s (%.0f us, 0 dB)", "RGB"[ch], code, rd.Level, extra, us)
		}
		return nil
	}

	for _, code := range []int{FitProbeCode, FitMinCode} {
		for c := 0; c < 3; c++ {
			if err := add(c, code); err != nil {
				return fit, err
			}
		}
	}
	for c := 0; c < 3; c++ {
		if hasUsable(pts[c]) {
			// If the only usable points are at 100, also try a mid/low code
			// that may already have been taken; otherwise go lower.
			onlyHigh := true
			for _, p := range pts[c] {
				if p.usable() && p.code < FitProbeCode {
					onlyHigh = false
					break
				}
			}
			if !onlyHigh {
				continue
			}
		}
		for _, code := range []int{15, 10, 5} {
			if err := add(c, code); err != nil {
				return fit, err
			}
			if hasUsable(pts[c]) {
				break
			}
		}
		if !hasUsable(pts[c]) {
			return fit, fmt.Errorf("no measurable film base on %c; check the light, the lens cap and the measure area", "RGB"[c])
		}
	}

	need, reason, _ := chooseCommonUS(us, pts, p.Target, p.Tol)
	if math.Abs(need-us)/math.Max(us, 1) <= 0.01 {
		fit.Reason = reason
		if say != nil {
			say("%s", reason)
		}
		fit.FinalUS = us
		return fit, nil
	}
	if err := cam.SetExposure(need, 0); err != nil {
		return fit, fmt.Errorf("set exposure %.0f us: %w", need, err)
	}
	us, _ = cam.GetExposure()
	fit.FinalUS, fit.FinalGain, fit.Changed = us, 0, true
	fit.Reason = reason
	if say != nil {
		say("%s", reason)
	}

	// One confirm at the new exposure, using the dimmest channel at 85
	// and the brightest at 30 when those readings are unclipped.
	dim, bright := 0, 0
	bestDim, bestBright := math.Inf(1), 0.0
	for c := 0; c < 3; c++ {
		if lv, ok := estimateLevel(pts[c], FitProbeCode); ok && lv < bestDim {
			bestDim, dim = lv, c
		}
		if lv, ok := estimateLevel(pts[c], FitProbeCode); ok && lv > bestBright {
			bestBright, bright = lv, c
		}
	}
	if rd, err := probe(dim, FitHeadroomCode); err == nil && rd.Level > 1e-9 && !rd.BaseClipped() {
		if say != nil {
			say("Exposure confirm: %c at brightness %d = %.5f FS (%.0f us)", "RGB"[dim], FitHeadroomCode, rd.Level, us)
		}
		if rd.Level < p.Target*(1-p.Tol) && us < MaxExposureUS-0.5 {
			adj := clampExposureUS(us * p.Target / rd.Level)
			if adj > us*1.01 {
				if err := cam.SetExposure(adj, 0); err != nil {
					return fit, fmt.Errorf("set exposure %.0f us: %w", adj, err)
				}
				us, _ = cam.GetExposure()
				fit.FinalUS = us
				fit.Reason = fmt.Sprintf("raised common exposure from %.0f us to %.0f us so %c can reach the target at light code %d (gain 0 dB)", fit.StartedUS, us, "RGB"[dim], FitHeadroomCode)
				if say != nil {
					say("Dimmest channel still short at code %d; exposure now %.0f us", FitHeadroomCode, us)
				}
			}
		}
	}
	_ = bright
	return fit, nil
}

func historyPoints(h [3]response) [3][]probePt {
	var pts [3][]probePt
	for c := 0; c < 3; c++ {
		for code, s := range h[c] {
			pts[c] = append(pts[c], probePt{code: code, level: s.reading.Level, clipped: s.reading.BaseClipped() || s.reading.Level >= 0.98})
		}
	}
	return pts
}
