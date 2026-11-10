package hdrscan

import "math"

// ScanMeterConfig is the capture-time meter used by Mono trichrome: drive
// the film-base median to 0.50 while keeping full-frame highlights
// (P99.5) at or below 0.88 so they do not clip.
func ScanMeterConfig() AutoExpConfig {
	c := DefaultAutoExp()
	c.TargetPercentile = 50
	c.TargetLevel = 0.50
	c.ClipPercentile = 99.5
	c.ClipLimit = 0.88
	c.MaxStep = 4
	c.Damping = 0.85
	return c
}

// MeterScanExposure proposes a new exposure from a probe frame.
// full is the whole frame (0..1); base is optional samples from the
// user-drawn clear-base rectangle. Highlights are judged on the full
// frame (or on base if full is empty). The target is the median of the
// base samples, or of the full frame when no rectangle was drawn.
func MeterScanExposure(full, base []float64, cur ExposureState, cfg AutoExpConfig) (ExposureState, string) {
	cfg = cfg.norm()
	cur = clampState(cur, cfg)
	clipSrc := full
	if len(clipSrc) == 0 {
		clipSrc = base
	}
	targetSrc := base
	if len(targetSrc) == 0 {
		targetSrc = full
	}
	if len(clipSrc) == 0 && len(targetSrc) == 0 {
		return cur, "no samples"
	}
	pC := percentile(clipSrc, cfg.ClipPercentile)
	pT := percentile(targetSrc, cfg.TargetPercentile)
	var ratio float64
	reason := "hold"
	switch {
	case pC > cfg.ClipLimit:
		ratio = cfg.ClipLimit / math.Max(pC, 1e-6)
		reason = "reduce (highlight clip)"
	case pT < 1e-4:
		ratio = cfg.MaxStep
		reason = "increase (near black)"
	default:
		ratio = cfg.TargetLevel / math.Max(pT, 1e-6)
		if pC*ratio > cfg.ClipLimit {
			ratio = cfg.ClipLimit / math.Max(pC, 1e-6)
			reason = "match base, limited by highlight headroom"
		} else if ratio > 1.02 {
			reason = "increase toward film base 0.50"
		} else if ratio < 0.98 {
			reason = "decrease toward film base 0.50"
		} else {
			reason = "on target"
		}
	}
	if ratio > cfg.MaxStep {
		ratio = cfg.MaxStep
	}
	if ratio < 1/cfg.MaxStep {
		ratio = 1 / cfg.MaxStep
	}
	if cfg.Damping > 0 && cfg.Damping < 1 && ratio > 0 {
		ratio = math.Pow(ratio, cfg.Damping)
	}
	if reason == "on target" && math.Abs(ratio-1) < 0.03 {
		return cur, reason
	}
	return applyRatio(cur, ratio, cfg), reason
}

// FrameSamples returns 0..1 samples from a 16-bit monochrome plane,
// optionally restricted to a normalized rectangle (empty = full frame).
// The lattice matches LevelingImage so a film-base pick meters the same
// kind of pixels the leveling solver uses.
func FrameSamples(pix []uint16, w, h int, box Rect) []float64 {
	if w <= 0 || h <= 0 || len(pix) < w*h {
		return nil
	}
	x0, y0, x1, y1 := 0, 0, w, h
	if !box.Empty() {
		x0 = clampInt(int(box.X0*float64(w)), 0, w-1)
		y0 = clampInt(int(box.Y0*float64(h)), 0, h-1)
		x1 = clampInt(int(math.Ceil(box.X1*float64(w))), x0+1, w)
		y1 = clampInt(int(math.Ceil(box.Y1*float64(h))), y0+1, h)
	}
	step := max(1, min(16, min(x1-x0, y1-y0)/8))
	out := make([]float64, 0, ((x1-x0)/step)*((y1-y0)/step))
	for y := y0; y < y1; y += step {
		for x := x0; x < x1; x += step {
			out = append(out, float64(pix[y*w+x])/65535)
		}
	}
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
