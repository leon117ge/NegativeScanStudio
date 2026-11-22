package lightlevel

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

// FitProbeCode is the high light-brightness probe. FitHeadroomMax is the
// highest light code we try to leave the dimmest channel at so it keeps
// headroom below 100 while the coarsest channel's integer code is
// raised as far as the channel ratio allows. FitMinCode is still used as
// a low probe point. Exposure, which is continuous, is then the fine
// control for that coarsest integer code; the other channels are leveled
// by integer codes, whose steps are then fine.
const (
	FitProbeCode    = 100
	FitHeadroomCode = 85
	FitHeadroomMax  = 90
	FitMinCode      = 25
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
// light channel can be placed on target at an integer code within the
// feasible window.
type ExposureFit struct {
	StartedUS, FinalUS     float64
	StartedGain, FinalGain float64
	Changed                bool
	Reason                 string
	HasCoarse              bool
	CoarseCh               int
	CoarseCode             int
	StartCodes             [3]int
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
	scaleFrom := func(ref probePt, c int) (float64, bool) {
		d := ledDrive(ref.code)
		if d <= 1e-12 {
			return 0, false
		}
		return ref.level * ledDrive(c) / d, true
	}
	// Extrapolating below the lowest measured point or above the highest
	// measured point scales through (0, 0) in ledDrive space so sub-linear
	// pairs never produce a negative or inflated non-zero-intercept level.
	if code <= u[0].code {
		return scaleFrom(u[0], code)
	}
	last := len(u) - 1
	if code >= u[last].code {
		return scaleFrom(u[last], code)
	}
	for i := 0; i < last; i++ {
		if code >= u[i].code && code <= u[i+1].code {
			f1, f2, f := ledDrive(u[i].code), ledDrive(u[i+1].code), ledDrive(code)
			if math.Abs(f2-f1) < 1e-12 {
				return (u[i].level + u[i+1].level) / 2, true
			}
			return u[i].level + (u[i+1].level-u[i].level)*(f-f1)/(f2-f1), true
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

// exposureBounds computes the feasible exposure window from measured
// responses:
// - usDim100: minimum exposure so every channel reaches target at code <= 100.
// - usDimHead: lower bound with headroom so every channel reaches target at code <= FitHeadroomMax (90).
// - usBright1: maximum exposure so the brightest channel does not exceed target at code 1.
func exposureBounds(curUS float64, pts [3][]probePt, target float64) (usDim100, usDimHead, usBright1 float64, dimCh, brightCh int, ok bool) {
	have100, haveHead, haveBright := false, false, false
	for c := 0; c < 3; c++ {
		if v, okv := usForTargetAt(pts[c], FitProbeCode, curUS, target); okv {
			if !have100 || v > usDim100 {
				usDim100, have100, dimCh = v, true, c
			}
		}
		if v, okv := usForTargetAt(pts[c], FitHeadroomMax, curUS, target); okv {
			if !haveHead || v > usDimHead {
				usDimHead, haveHead = v, true
			}
		}
		if v, okv := usForTargetAt(pts[c], 1, curUS, target); okv {
			if !haveBright || v < usBright1 {
				usBright1, haveBright, brightCh = v, true, c
			}
		}
	}
	if !have100 {
		return 0, 0, 0, 0, 0, false
	}
	if !haveHead {
		usDimHead = usDim100
	}
	if !haveBright {
		usBright1 = MaxExposureUS
	}
	return usDim100, usDimHead, usBright1, dimCh, brightCh, true
}

// coarsePlan is the integer code and exposure that put the coarsest
// channel on target while keeping all other channels inside the
// feasible window [floorUS, ceilUS].
type coarsePlan struct {
	ch       int
	code     int
	us       float64
	floorUS  float64
	ceilUS   float64
	maxOther int
	reason   string
}

func coarsestChannel(pts [3][]probePt, measUS, atUS, target float64) (int, bool) {
	ch, best, found := 0, 0.0, false
	for c := 0; c < 3; c++ {
		n, ok := neededCodeAt(pts[c], measUS, atUS, target)
		if !ok {
			continue
		}
		if !found || n < best {
			ch, best, found = c, n, true
		}
	}
	return ch, found
}

func planFits(pts [3][]probePt, coarse, k int, us, curUS, target, floorUS float64, maxOther int) bool {
	if us < MinExposureUS || us > MaxExposureUS || us < floorUS*0.999 {
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

// chooseCoarsePlan finds the feasible exposure window [usDimHead..usBright1]
// (falling back to [usDim100..usBright1] only if headroom doesn't fit),
// identifies the coarsest channel (lowest needed light code), and picks
// the highest integer code for that channel whose matching exposure stays
// inside that feasible window.
func chooseCoarsePlan(curUS float64, pts [3][]probePt, target float64) (coarsePlan, bool) {
	if curUS <= 0 {
		curUS = 10_000
	}
	if target <= 0 {
		target = 0.50
	}
	usDim100, usDimHead, usBright1, dimCh, brightCh, ok := exposureBounds(curUS, pts, target)
	if !ok {
		return coarsePlan{}, false
	}
	coarse, ok := coarsestChannel(pts, curUS, math.Max(curUS, usDim100), target)
	if !ok {
		coarse = brightCh
	}
	pick := func(maxOther int, floorUS float64) (coarsePlan, bool) {
		for k := 100; k >= 1; k-- {
			us, ok := usForTargetAt(pts[coarse], k, curUS, target)
			if !ok {
				continue
			}
			if planFits(pts, coarse, k, us, curUS, target, floorUS, maxOther) {
				return coarsePlan{
					ch:       coarse,
					code:     k,
					us:       us,
					floorUS:  floorUS,
					ceilUS:   usBright1,
					maxOther: maxOther,
					reason: fmt.Sprintf("set common exposure to %.0f us so %c lands on target at light code %d; the other channels stay at or below code %d and are then leveled by integer steps (gain 0 dB)",
						us, "RGB"[coarse], k, maxOther),
				}, true
			}
		}
		return coarsePlan{}, false
	}
	if plan, ok := pick(FitHeadroomMax, usDimHead); ok {
		return plan, true
	}
	if plan, ok := pick(98, usDim100); ok {
		return plan, true
	}
	if plan, ok := pick(FitProbeCode, usDim100); ok {
		return plan, true
	}
	// Channel ratio exceeds 100:1 (usDim100 > usBright1): keep the dimmest
	// channel reachable at 100 so exposure never goes too low for it.
	us := clampExposureUS(usDim100)
	return coarsePlan{
		ch:       coarse,
		code:     1,
		us:       us,
		floorUS:  us,
		ceilUS:   usBright1,
		maxOther: FitProbeCode,
		reason: fmt.Sprintf("channel ratio exceeds 100:1 (%c vs %c); raised common exposure to %.0f us so %c can still reach the target at code 100 (gain 0 dB)",
			"RGB"[dimCh], "RGB"[brightCh], us, "RGB"[dimCh]),
	}, true
}

// trimExposureToReading scales the common exposure so a just-measured
// raw level becomes target. Exposure stays inside [minUS, maxUS].
func trimExposureToReading(cam VariableExposure, measured, target, minUS, maxUS float64) (newUS float64, changed bool, err error) {
	us, _ := cam.GetExposure()
	if measured <= 1e-9 || target <= 0 {
		return us, false, nil
	}
	adj := clampExposureUS(us * target / measured)
	if minUS > 0 && adj < minUS {
		adj = clampExposureUS(minUS)
	}
	if maxUS > 0 && adj > maxUS && maxUS >= minUS {
		adj = clampExposureUS(maxUS)
	}
	if math.Abs(adj-us)/math.Max(us, 1) < 0.0005 || math.Abs(adj-us) < 0.5 {
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
		for _, pt := range pts {
			if pt.code == code {
				return true
			}
		}
		return false
	}
	hasUsable := func(pts []probePt) bool {
		for _, pt := range pts {
			if pt.usable() {
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
			for _, pt := range pts[c] {
				if pt.usable() && pt.code < FitProbeCode {
					onlyHigh = false
					break
				}
			}
			if !onlyHigh {
				continue
			}
		}
		for _, code := range []int{15, 10, 5, 2, 1} {
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

	measUS := us
	plan, ok := chooseCoarsePlan(measUS, pts, p.Target)
	if !ok {
		fit.Reason = "could not measure a usable film-base response to choose an exposure"
		if say != nil {
			say("%s", fit.Reason)
		}
		fit.FinalUS = us
		return fit, nil
	}
	if math.Abs(plan.us-us)/math.Max(us, 1) > 0.005 {
		if err := cam.SetExposure(plan.us, 0); err != nil {
			return fit, fmt.Errorf("set exposure %.0f us: %w", plan.us, err)
		}
		us, _ = cam.GetExposure()
	}

	// Verify with a capture and fine-trim exposure inside the feasible window.
	// If trimming down to put the coarse channel on target would drop below
	// plan.floorUS (leaving a dim channel too dark), step the coarse integer
	// code down by 1 so exposure goes UP into the feasible window instead.
	tightTol := math.Min(p.Tol*0.4, 0.005)
	for i := 0; i < 4; i++ {
		rd, err := probe(plan.ch, plan.code)
		if err != nil {
			return fit, fmt.Errorf("exposure confirm %c at %d: %w", "RGB"[plan.ch], plan.code, err)
		}
		if say != nil {
			say("Exposure confirm: %c at brightness %d = %.5f FS (%.0f us)", "RGB"[plan.ch], plan.code, rd.Level, us)
		}
		if rd.Level <= 1e-9 || rd.BaseClipped() {
			if plan.code > 1 {
				plan.code--
				continue
			}
			break
		}
		if math.Abs(rd.Level/p.Target-1) <= tightTol && us >= plan.floorUS*0.999 {
			break
		}
		wantUS := clampExposureUS(us * p.Target / rd.Level)
		if wantUS < plan.floorUS*0.999 && plan.code > 1 {
			plan.code--
			if say != nil {
				say("Stepping %c to code %d to keep exposure above %.0f us headroom floor", "RGB"[plan.ch], plan.code, plan.floorUS)
			}
			continue
		}
		newUS, changed, err := trimExposureToReading(cam, rd.Level, p.Target, plan.floorUS, plan.ceilUS)
		if err != nil {
			return fit, fmt.Errorf("set exposure %.0f us: %w", newUS, err)
		}
		if !changed {
			break
		}
		us = newUS
		if say != nil {
			say("Refined common exposure to %.0f us so %c at light code %d is on target", us, "RGB"[plan.ch], plan.code)
		}
	}
	fit.FinalUS, fit.FinalGain = us, 0
	fit.Changed = math.Abs(us-fit.StartedUS) > 0.5
	fit.HasCoarse, fit.CoarseCh, fit.CoarseCode = true, plan.ch, plan.code
	for c := 0; c < 3; c++ {
		if c == plan.ch {
			fit.StartCodes[c] = plan.code
			continue
		}
		if n, okN := neededCodeAt(pts[c], measUS, us, p.Target); okN {
			k := int(math.Round(n))
			if k < 1 {
				k = 1
			}
			if k > 100 {
				k = 100
			}
			fit.StartCodes[c] = k
		}
	}
	fit.Reason = fmt.Sprintf("set common exposure to %.0f us so %c lands on target at light code %d; the other channels stay at or below code %d and are then leveled by integer steps (gain 0 dB)",
		us, "RGB"[plan.ch], plan.code, plan.maxOther)
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

// historyPoints converts solver measurements into probePt slices for
// re-fitting exposure when a bound or integer-limit is encountered.
func historyPoints(h [3]response) [3][]probePt {
	var pts [3][]probePt
	for c := 0; c < 3; c++ {
		for code, s := range h[c] {
			pts[c] = append(pts[c], probePt{code: code, level: s.reading.Level, clipped: s.reading.BaseClipped() || s.reading.Level >= 0.98})
		}
	}
	return pts
}
