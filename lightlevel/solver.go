package lightlevel

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

type sample struct {
	reading Reading
	image   *Image
}

type response map[int]sample

func deviation(post [3]float64, target float64) float64 {
	worst := 0.0
	for _, v := range post {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return math.Inf(1)
		}
		worst = math.Max(worst, math.Abs(v/target-1))
	}
	return worst
}

// The matrix is known, so J = Matrix * diag(measured slopes). Solving
// J*delta = target-post is equivalent to inverse(Matrix)*(target-post)/slope.
// Only a positive measured local response is trusted; otherwise use a bounded
// proportional probe until two distinct, unsaturated codes have been measured.
func (h response) next(cur int, target, rawError float64) (int, string) {
	now := h[cur].reading
	codes := make([]int, 0, len(h))
	for code := range h {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	if math.Abs(rawError) < 1e-8 && !now.BaseClipped() {
		return cur, "at continuous target"
	}
	if now.BaseClipped() {
		return clampV(cur / 2), "film base clipped, halving"
	}
	lo, hi := 0, 101
	for _, code := range codes {
		s := h[code]
		if s.reading.BaseClipped() {
			if code > cur && code < hi {
				hi = code
			}
			continue
		}
		if s.reading.Level <= target && code > lo {
			lo = code
		}
		if s.reading.Level >= target && code < hi {
			hi = code
		}
	}
	slope := 0.0
	distance := 101
	// Prefer a sufficiently separated local pair: a one-code noise difference
	// must not dominate when a two-code or wider measurement already exists.
	for _, code := range codes {
		s := h[code]
		d := abs(code - cur)
		if d < 2 || s.reading.BaseClipped() {
			continue
		}
		v := (now.Level - s.reading.Level) / float64(cur-code)
		if v > 1e-6 && d < distance {
			slope, distance = v, d
		}
	}
	if slope == 0 {
		for _, code := range codes {
			s := h[code]
			if code == cur || s.reading.BaseClipped() {
				continue
			}
			v := (now.Level - s.reading.Level) / float64(cur-code)
			if v > 1e-6 && abs(code-cur) < distance {
				slope, distance = v, abs(code-cur)
			}
		}
	}
	proposal := float64(cur)
	note := "bounded proportional probe"
	if slope > 0 {
		proposal += rawError / slope
		note = fmt.Sprintf("measured secant slope %.6f FS/code", slope)
	} else if now.Level > .002 {
		proposal *= target / now.Level
	} else {
		proposal = float64(cur * 4)
		note = "almost no signal, probing upward"
	}
	step := max(4, int(math.Ceil(float64(cur)*.5)))
	limit := func(v int) int { return clampV(max(cur-step, min(cur+step, v))) }
	code := limit(int(math.Round(proposal)))
	if lo > 0 && hi < 101 && lo < hi {
		if hi-lo == 1 {
			return cur, "adjacent integer bracket; neighbour search"
		}
		if code <= lo || code >= hi {
			code = limit((lo + hi) / 2)
			note = "bisection inside measured bracket"
		}
	}
	if _, seen := h[code]; seen {
		if lo > 0 && hi < 101 && lo < hi {
			code = limit((lo + hi) / 2)
			note = "oscillation detected; bisection"
			if _, seen = h[code]; !seen {
				return code, note
			}
		}
		direction := 1
		if rawError < 0 {
			direction = -1
		}
		code = clampV(cur + direction)
		if _, seen = h[code]; !seen {
			return code, "integer response probe"
		}
		return cur, "repeated integer code; neighbour search"
	}
	return code, note
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// bestCombination evaluates measured per-channel frames through the known
// matrix. It never manufactures a light code or interpolates a saved reading.
func bestCombination(h [3]response, p Params) ([3]int, float64, bool) {
	var keys [3][]int
	for c := 0; c < 3; c++ {
		for k, s := range h[c] {
			if !s.reading.BaseClipped() {
				keys[c] = append(keys[c], k)
			}
		}
		sort.Ints(keys[c])
		if len(keys[c]) == 0 {
			return [3]int{}, math.Inf(1), false
		}
	}
	best := math.Inf(1)
	bestSum := math.Inf(1)
	var values [3]int
	for _, r := range keys[0] {
		for _, g := range keys[1] {
			for _, b := range keys[2] {
				v := [3]int{r, g, b}
				lv := [3]float64{h[0][r].reading.Level, h[1][g].reading.Level, h[2][b].reading.Level}
				post := p.Matrix.Apply(lv)
				score := deviation(post, p.Target)
				sum := 0.0
				for _, x := range post {
					sum += (x - p.Target) * (x - p.Target)
				}
				if score < best || (math.Abs(score-best) < 1e-12 && sum < bestSum) {
					best, values, bestSum = score, v, sum
				}
			}
		}
	}
	return values, best, true
}

// Run separates candidate codes from measured results. A candidate is exposed
// before it can enter the cache, and final success always requires fresh R/G/B.
func Run(ctx context.Context, l Light, cam Camera, p Params, restore [3]bool, progress func(string)) (res Result, err error) {
	p.Defaults()
	res = Result{Values: p.Start, Camera: cam.Name(), Area: p.Area}
	say := func(f string, a ...any) {
		if progress != nil {
			progress(fmt.Sprintf(f, a...))
		}
	}
	inv, e := p.Matrix.Inverse()
	if e != nil {
		return res, fmt.Errorf("camera matrix: %w", e)
	}
	target := inv.Apply([3]float64{p.Target, p.Target, p.Target})
	for c, v := range target {
		if v <= 0 || v >= 1 || math.IsNaN(v) || math.IsInf(v, 0) {
			return res, fmt.Errorf("unreachable camera matrix target: %c requires %.5f FS before profile; change profile or exposure", "RGB"[c], v)
		}
	}
	exp, e := cam.Prepare()
	if e != nil {
		return res, fmt.Errorf("camera: %w", e)
	}
	say("Fixed camera exposure: %s (not changed during leveling)", exp)
	defer func() {
		if e := l.SetAll(restore, res.Values); e != nil {
			say("Could not restore measured light values: %v", e)
			if err == nil {
				err = fmt.Errorf("restore light: %w", e)
				res.Verified = false
			}
		}
	}()
	var history [3]response
	for c := range history {
		history[c] = make(response)
	}
	commanded := p.Start
	take := func(c, code, round int, phase string) (sample, error) {
		if e := ctx.Err(); e != nil {
			return sample{}, e
		}
		commanded[c] = code
		var on [3]bool
		on[c] = true
		if e := l.SetAll(on, commanded); e != nil {
			return sample{}, fmt.Errorf("light: %w", e)
		}
		select {
		case <-ctx.Done():
			return sample{}, ctx.Err()
		case <-time.After(p.Settle):
		}
		im, e := cam.Shoot(ctx, c, fmt.Sprintf("light_%c%d_%s_", "RGB"[c], round, phase))
		if e != nil {
			return sample{}, fmt.Errorf("%c capture: %w", "RGB"[c], e)
		}
		rd, e := Measure(im, p.Area)
		if e != nil {
			return sample{}, fmt.Errorf("%c measure: %w", "RGB"[c], e)
		}
		s := sample{reading: rd, image: im}
		history[c][code] = s
		if !res.Captured[c] {
			res.Captured[c] = true
			res.Values[c] = code
			res.Readings[c] = rd.Level
			res.Images[c] = im
			res.Post = p.Matrix.Apply(res.Readings)
		}
		st := Step{Round: round, Channel: c, Value: code, Reading: rd, Next: code, OK: math.Abs(rd.Level/target[c]-1) <= p.Tol, Note: phase}
		if rd.BaseClipped() {
			st.Note += "; film base broadly clipped"
		} else if rd.ClipPct > 0 {
			st.Note += "; isolated bright samples ignored"
		}
		res.Steps = append(res.Steps, st)
		say("%s; base P90 %.5f FS", st, rd.P90)
		return s, nil
	}
	selectResult := func(v [3]int) {
		res.Values = v
		for c, code := range v {
			s, ok := history[c][code]
			res.Captured[c] = ok
			if ok {
				res.Readings[c] = s.reading.Level
				res.Images[c] = s.image
			}
		}
		res.Post = p.Matrix.Apply(res.Readings)
	}
	selectArea := p.SelectArea
	current := p.Start
	reason := "round limit reached"
	unreachable := false
	for round := 1; round <= p.MaxIter; round++ {
		res.Rounds = round
		for c, code := range current {
			if _, ok := history[c][code]; ok {
				continue
			}
			if _, e := take(c, code, round, "solve"); e != nil {
				return res, e
			}
		}
		canSelect := selectArea != nil
		for c, code := range current {
			if history[c][code].reading.BaseClipped() {
				canSelect = false
			}
		}
		if canSelect {
			var images [3]*Image
			for c := range images {
				images[c] = history[c][current[c]].image
			}
			a, e := selectArea(images)
			if e != nil {
				return res, fmt.Errorf("auto film base: %w", e)
			}
			if a.Empty() {
				return res, fmt.Errorf("auto film base: no clean base region; choose Rectangle")
			}
			p.Area, res.Area = a, a
			selectArea = nil
			for c := range history {
				s := history[c][current[c]]
				s.reading, e = Measure(s.image, a)
				if e != nil {
					return res, e
				}
				history[c] = response{current[c]: s}
			}
			say("Auto film base locked to rectangle %.4f,%.4f-%.4f,%.4f", a.X0, a.Y0, a.X1, a.Y1)
		}
		selectResult(current)
		currentPost := res.Post
		if v, _, ok := bestCombination(history, p); ok {
			selectResult(v)
		}
		score := deviation(res.Post, p.Target)
		say("round %d best measured R %d G %d B %d: after-profile %.5f/%.5f/%.5f FS; worst %.3f%%", round, res.Values[0], res.Values[1], res.Values[2], res.Post[0], res.Post[1], res.Post[2], 100*score)
		if score <= p.Tol {
			reason = "within tolerance"
			break
		}
		next := current
		rawError := inv.Apply([3]float64{p.Target - currentPost[0], p.Target - currentPost[1], p.Target - currentPost[2]})
		for c, code := range current {
			rawTol := 0.0
			for _, x := range inv[c] {
				rawTol += math.Abs(x) * p.Target * p.Tol
			}
			if bound, ok := history[c][1]; ok && (bound.reading.BaseClipped() || bound.reading.Level > target[c]+rawTol) {
				res.TooBright[c], res.Boundary[c] = true, bound.reading.Level
				unreachable = true
				reason = fmt.Sprintf("unreachable: %c too bright at code 1; reduce camera exposure", "RGB"[c])
			}
			if bound, ok := history[c][100]; ok && bound.reading.Level < target[c]-rawTol {
				res.Limited[c], res.Boundary[c] = true, bound.reading.Level
				res.Needed[c] = 100 * target[c] / math.Max(bound.reading.Level, 1e-9)
				unreachable = true
				reason = fmt.Sprintf("unreachable: %c too dark at code 100; increase camera exposure", "RGB"[c])
			}
			n, note := history[c].next(code, target[c], rawError[c])
			next[c] = n
			say("%c %d -> %d: %s", "RGB"[c], code, n, note)
		}
		if unreachable {
			break
		}
		if next == current {
			reason = "integer resolution limit"
			break
		}
		current = next
	}
	// An integer plateau is not convergence. Actually measure both neighbours,
	// then evaluate their combinations in after-profile space using cached frames.
	if !unreachable && deviation(res.Post, p.Target) < .20 {
		for refinement := 0; refinement < 3; refinement++ {
			centre := res.Values
			say("Integer neighbour search %d: measuring +/-1 code and evaluating cached RGB combinations through the camera matrix", refinement+1)
			for c, code := range centre {
				for _, n := range []int{code - 1, code + 1} {
					if n < 1 || n > 100 {
						continue
					}
					if _, ok := history[c][n]; ok {
						continue
					}
					if _, e := take(c, n, res.Rounds, "neighbour"); e != nil {
						return res, e
					}
				}
			}
			if v, _, ok := bestCombination(history, p); ok {
				selectResult(v)
			}
			if res.Values == centre {
				break
			}
		}
	}

	// Always recapture all three, including channels that were previously done.
	// x.last in the real camera now refers to these exact verified codes.
	chosen := res.Values
	say("Final full R/G/B verification at R %d G %d B %d", chosen[0], chosen[1], chosen[2])
	for c, code := range chosen {
		if _, e := take(c, code, res.Rounds, "verify"); e != nil {
			return res, e
		}
	}
	selectResult(chosen)
	res.Verified = true
	for c, v := range res.Post {
		res.Done[c] = !history[c][chosen[c]].reading.BaseClipped() && math.Abs(v/p.Target-1) <= p.Tol
	}
	if res.Converged() {
		res.Reason = ""
		say("Control verification passed: after-profile %.5f/%.5f/%.5f FS", res.Post[0], res.Post[1], res.Post[2])
	} else {
		if reason == "within tolerance" {
			reason = "final verification outside tolerance (measurement changed)"
		}
		res.Reason = fmt.Sprintf("Closest reachable within search budget: %s; worst after-profile error %.3f%% (tolerance %.3f%%)", reason, 100*deviation(res.Post, p.Target), 100*p.Tol)
		say("%s", res.Reason)
	}
	return res, nil
}
