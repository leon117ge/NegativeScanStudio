package lightlevel

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// FitProbeCode is the high light-brightness probe. FitMinCode is still
// used as a low probe point. FitHeadroomMax is the highest light code
// we try to leave the dimmest other channels at, so they keep headroom
// below 100 while the coarsest channel's code is raised as far as the
// ratio allows. Exposure, which is continuous, is then the fine control
// for that coarsest integer code; the other channels are leveled by
// integer codes, whose steps are then fine.
const (
	FitProbeCode    = 100
	FitHeadroomCode = 85
	FitMinCode      = 25
	FitHeadroomMax  = 90
	MinExposureUS   = 32.0
	MaxExposureUS   = 10_000_000.0
)

// VariableExposure is a camera whose exposure can be raised, lowered
// and fine-tuned during monochrome leveling (Hikrobot). Bayer / X-T3
// cameras do not implement this: their exposure stays fixed while only
// the light codes change.
type VariableExposure interface {
	Camera
	GetExposure() (us, gainDB float64)
	SetExposure(us, gainDB float64) error
}

// ExposureFit records the common camera exposure chosen so the coarsest
// light channel can be placed on target at an integer code.
type ExposureFit struct {
	StartedUS, FinalUS     float64
	StartedGain, FinalGain float64
	Changed                bool
	Reason                 string
	HasCoarse              bool
	CoarseCh               int
	CoarseCode             int
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

// neededCodeAt is the continuous light code that would hit target at
// newUS, given measurements taken at curUS. ok is false when even code
// 100 would fall short (the returned code is then 100).
func neededCodeAt(pts []probePt, curUS, newUS, target float64) (float64, bool) {
	if curUS <= 0 || newUS <= 0 || target <= 0 {
		return 0, false
	}
	want := target * curUS / newUS
	lv100, ok100 := estimateLevel(pts, 100)
	if !ok100 {
		return 0, false
	}
	if lv100 < want {
		return 100, false
	}
	if lv1, ok1 := estimateLevel(pts, 1); ok1 && lv1 >= want {
		return 1, true
	}
	lo, hi := 1, 100
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		lv, ok := estimateLevel(pts, mid)
		if !ok {
			return 0, false
		}
		if lv < want {
			lo = mid
		} else {
			hi = mid
		}
	}
	lvLo, okLo := estimateLevel(pts, lo)
	lvHi, okHi := estimateLevel(pts, hi)
	if !okLo || !okHi {
		return float64(hi), okHi
	}
	if math.Abs(lvHi-lvLo) < 1e-12 {
		return float64(hi), true
	}
	frac := (want - lvLo) / (lvHi - lvLo)
	return float64(lo) + frac*float64(hi-lo), true
}

// coarsePlan is the integer code and exposure that put the coarsest
// channel on target, with the other channels still reachable at or
// below a headroom cap.
type coarsePlan struct {
	ch     int
	code   int
	us     float64
	reason string
}

func coarsestChannel(pts [3][]probePt, measUS, atUS, target float64) (int, bool) {
	ch, best, found := 0, 0.0, false
	for c := 0; c < 3; c++ {
		n, ok := neededCodeAt(pts[c], measUS, atUS, target)
		if !ok {
			// Unreachable at atUS: this is a dim channel, not the coarsest.
			continue
		}
		if !found || n < best {
			ch, best, found = c, n, true
		}
	}
	return ch, found
}

func planFits(pts [3][]probePt, coarse, k int, us, curUS, target float64, maxOther int) bool {
	if us < MinExposureUS || us > MaxExposureUS {
		return false
	}
	for c := 0; c < 3; c++ {
		if c == coarse {
			continue
		}
		n, ok := neededCodeAt(pts[c], curUS, us, target)
		if !ok || n > float64(maxOther)+1e-6 {
			return false
		}
	}
	return true
}

// chooseCoarsePlan picks the channel with the lowest needed light code
// (the coarsest integer step) and the highest integer code for it that
// still leaves the other channels at or below maxOther (then 100).
// The returned exposure places that channel on target at that integer.
func chooseCoarsePlan(curUS float64, pts [3][]probePt, target float64) (coarsePlan, bool) {
	if curUS <= 0 {
		curUS = 10_000
	}
	if target <= 0 {
		target = 0.50
	}
	coarse, ok := coarsestChannel(pts, curUS, curUS, target)
	if !ok {
		// Every channel is short at the current exposure: raise to the
		// dimmest channel at 100, then identify the coarsest there.
		// Measurements in pts are still at curUS.
		var usMin float64
		have := false
		for c := 0; c < 3; c++ {
			if v, ok := usForTargetAt(pts[c], FitProbeCode, curUS, target); ok {
				if !have || v > usMin {
					usMin, have = v, true
				}
			}
		}
		if !have {
			return coarsePlan{}, false
		}
		coarse, ok = coarsestChannel(pts, curUS, usMin, target)
		if !ok {
			return coarsePlan{}, false
		}
	}
	pick := func(maxOther int) (coarsePlan, bool) {
		for k := 100; k >= 1; k-- {
			us, ok := usForTargetAt(pts[coarse], k, curUS, target)
			if !ok {
				continue
			}
			if planFits(pts, coarse, k, us, curUS, target, maxOther) {
				return coarsePlan{
					ch:   coarse,
					code: k,
					us:   us,
					reason: fmt.Sprintf("set common exposure to %.0f us so %c lands on target at light code %d; the other channels stay at or below code %d and are then leveled by integer steps (gain 0 dB)",
						us, "RGB"[coarse], k, maxOther),
				}, true
			}
		}
		return coarsePlan{}, false
	}
	if plan, ok := pick(FitHeadroomMax); ok {
		return plan, true
	}
	if plan, ok := pick(FitProbeCode); ok {
		return plan, true
	}
	// Last resort: put the dimmest channel on target at 100 and use the
	// matching integer for the coarsest channel.
	var usMin float64
	have := false
	for c := 0; c < 3; c++ {
		if v, ok := usForTargetAt(pts[c], FitProbeCode, curUS, target); ok {
			if !have || v > usMin {
				usMin, have = v, true
			}
		}
	}
	if !have {
		return coarsePlan{}, false
	}
	n, ok := neededCodeAt(pts[coarse], curUS, usMin, target)
	if !ok {
		n = 1
	}
	k := int(math.Round(n))
	if k < 1 {
		k = 1
	}
	if k > 100 {
		k = 100
	}
	if us, ok := usForTargetAt(pts[coarse], k, curUS, target); ok {
		usMin = us
	}
	return coarsePlan{
		ch:   coarse,
		code: k,
		us:   clampExposureUS(usMin),
		reason: fmt.Sprintf("set common exposure to %.0f us so %c lands on target at light code %d (the other channels may be near code 100; gain 0 dB)",
			clampExposureUS(usMin), "RGB"[coarse], k),
	}, true
}

// trimExposureToReading scales the common exposure so a just-measured
// raw level becomes target. The light code is left unchanged: exposure
// is the continuous fine control.
func trimExposureToReading(cam VariableExposure, measured, target float64) (newUS float64, changed bool, err error) {
	us, _ := cam.GetExposure()
	if measured <= 1e-9 || target <= 0 {
		return us, false, nil
	}
	adj := clampExposureUS(us * target / measured)
	if math.Abs(adj-us)/math.Max(us, 1) < 0.001 {
		return us, false, nil
	}
	if err := cam.SetExposure(adj, 0); err != nil {
		return us, false, err
	}
	newUS, _ = cam.GetExposure()
	return newUS, math.Abs(newUS-us) > 0.5, nil
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
	fit := ExposureFit{StartedUS: us, FinalUS: us, StartedGain: gain, FinalGain: 0, Reason: "exposure already places the coarsest channel on target"}
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

	plan, ok := chooseCoarsePlan(us, pts, p.Target)
	if !ok {
		fit.Reason = "could not measure a usable film-base response to choose an exposure"
		if say != nil {
			say("%s", fit.Reason)
		}
		fit.FinalUS = us
		return fit, nil
	}
	if math.Abs(plan.us-us)/math.Max(us, 1) > 0.01 {
		if err := cam.SetExposure(plan.us, 0); err != nil {
			return fit, fmt.Errorf("set exposure %.0f us: %w", plan.us, err)
		}
		us, _ = cam.GetExposure()
		fit.Changed = true
	}
	// Verify with a capture and refine exposure once or twice so the
	// coarsest channel lands on target at the chosen integer code.
	for i := 0; i < 2; i++ {
		rd, err := probe(plan.ch, plan.code)
		if err != nil {
			return fit, fmt.Errorf("exposure confirm %c at %d: %w", "RGB"[plan.ch], plan.code, err)
		}
		if say != nil {
			say("Exposure confirm: %c at brightness %d = %.5f FS (%.0f us)", "RGB"[plan.ch], plan.code, rd.Level, us)
		}
		if rd.Level <= 1e-9 || rd.BaseClipped() {
			break
		}
		if math.Abs(rd.Level/p.Target-1) <= p.Tol {
			break
		}
		newUS, changed, err := trimExposureToReading(cam, rd.Level, p.Target)
		if err != nil {
			return fit, fmt.Errorf("set exposure %.0f us: %w", newUS, err)
		}
		if !changed {
			break
		}
		us = newUS
		fit.Changed = true
		if say != nil {
			say("Refined common exposure to %.0f us so %c at light code %d is on target", us, "RGB"[plan.ch], plan.code)
		}
	}
	fit.FinalUS, fit.FinalGain = us, 0
	fit.HasCoarse, fit.CoarseCh, fit.CoarseCode = true, plan.ch, plan.code
	fit.Reason = plan.reason
	if say != nil {
		say("%s", fit.Reason)
	}
	return fit, nil
}

// coarsestOffTarget is the off-tolerance channel with the lowest light
// code (the coarsest integer step). VariableExposure leveling uses this
// to decide which channel exposure should put on target.
func coarsestOffTarget(current [3]int, history [3]response, post [3]float64, target, tol float64) (int, bool) {
	ch, found := -1, false
	bestCode, bestErr := 101, 0.0
	for c, code := range current {
		s, ok := history[c][code]
		if !ok || s.reading.BaseClipped() {
			continue
		}
		rel := math.Abs(post[c]/target - 1)
		if rel <= tol {
			continue
		}
		if !found || code < bestCode || (code == bestCode && rel > bestErr) {
			ch, found, bestCode, bestErr = c, true, code, rel
		}
	}
	return ch, found
}
