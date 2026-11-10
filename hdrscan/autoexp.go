package hdrscan

import (
	"math"
	"sort"
)

// AutoExpConfig controls the Focus Check page's temporary exposure loop.
// Levels are 0..1 (fraction of full scale).
type AutoExpConfig struct {
	TargetPercentile float64 // e.g. 90: bring this percentile to TargetLevel
	TargetLevel      float64 // e.g. 0.42, so grain sits in the mid-tones
	ClipPercentile   float64 // e.g. 99.8
	ClipLimit        float64 // e.g. 0.90: that percentile must stay at or below this
	MinExposureUS    float64
	MaxExposureUS    float64
	MinGainDB        float64
	MaxGainDB        float64
	MaxStep          float64 // max multiplicative change of the linear gain per update
	Damping          float64 // 0..1, applied as ratio^Damping
}

// DefaultAutoExp is tuned so film grain is visible without clipping the
// brightest rebate / base, on both the full frame and a zoomed crop.
func DefaultAutoExp() AutoExpConfig {
	return AutoExpConfig{
		TargetPercentile: 90,
		TargetLevel:      0.42,
		ClipPercentile:   99.8,
		ClipLimit:        0.90,
		MinExposureUS:    32,
		MaxExposureUS:    10_000_000,
		MinGainDB:        0,
		MaxGainDB:        24,
		MaxStep:          2.0,
		Damping:          0.7,
	}
}

// ExposureState is the camera's current (or proposed) exposure pair.
type ExposureState struct {
	ExposureUS float64
	GainDB     float64
}

// LinearGain is exposure time times the analog gain as a linear factor.
func (e ExposureState) LinearGain() float64 {
	g := math.Pow(10, e.GainDB/20)
	if e.ExposureUS < 1e-9 {
		return g
	}
	return e.ExposureUS * g
}

// AdjustExposure proposes a new exposure for the samples in the visible
// (possibly zoomed) region. samples are 0..1. The function is pure: it does
// not touch a camera.
func AdjustExposure(samples []float64, cur ExposureState, cfg AutoExpConfig) (ExposureState, string) {
	cfg = cfg.norm()
	cur = clampState(cur, cfg)
	if len(samples) == 0 {
		return cur, "no samples"
	}
	pT := percentile(samples, cfg.TargetPercentile)
	pC := percentile(samples, cfg.ClipPercentile)
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
		ratio = cfg.TargetLevel / pT
		if ratio > 1.02 {
			reason = "increase"
		} else if ratio < 0.98 {
			reason = "decrease"
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
	// On target: do not keep walking from damping / quantisation.
	if reason == "on target" && math.Abs(ratio-1) < 0.03 {
		return cur, reason
	}
	return applyRatio(cur, ratio, cfg), reason
}

func (c AutoExpConfig) norm() AutoExpConfig {
	d := DefaultAutoExp()
	if c.TargetPercentile <= 0 || c.TargetPercentile >= 100 {
		c.TargetPercentile = d.TargetPercentile
	}
	if c.TargetLevel <= 0 || c.TargetLevel >= 1 {
		c.TargetLevel = d.TargetLevel
	}
	if c.ClipPercentile <= 0 || c.ClipPercentile > 100 {
		c.ClipPercentile = d.ClipPercentile
	}
	if c.ClipLimit <= 0 || c.ClipLimit > 1 {
		c.ClipLimit = d.ClipLimit
	}
	if c.MinExposureUS <= 0 {
		c.MinExposureUS = d.MinExposureUS
	}
	if c.MaxExposureUS <= c.MinExposureUS {
		c.MaxExposureUS = d.MaxExposureUS
	}
	if c.MaxGainDB < c.MinGainDB {
		c.MaxGainDB = d.MaxGainDB
	}
	if c.MaxStep < 1.05 {
		c.MaxStep = d.MaxStep
	}
	if c.Damping <= 0 || c.Damping > 1 {
		c.Damping = d.Damping
	}
	return c
}

func clampState(e ExposureState, c AutoExpConfig) ExposureState {
	if e.ExposureUS < c.MinExposureUS {
		e.ExposureUS = c.MinExposureUS
	}
	if e.ExposureUS > c.MaxExposureUS {
		e.ExposureUS = c.MaxExposureUS
	}
	if e.GainDB < c.MinGainDB {
		e.GainDB = c.MinGainDB
	}
	if e.GainDB > c.MaxGainDB {
		e.GainDB = c.MaxGainDB
	}
	return e
}

func applyRatio(cur ExposureState, ratio float64, c AutoExpConfig) ExposureState {
	// Prefer shutter: keep gain at its current value until the shutter
	// hits a stop, then spend the remainder on gain.
	exp := cur.ExposureUS * ratio
	gain := cur.GainDB
	if exp > c.MaxExposureUS {
		left := exp / c.MaxExposureUS
		exp = c.MaxExposureUS
		gain += 20 * math.Log10(left)
	} else if exp < c.MinExposureUS {
		left := exp / c.MinExposureUS
		exp = c.MinExposureUS
		gain += 20 * math.Log10(left)
	}
	if gain > c.MaxGainDB {
		// Cannot go brighter: dump leftover back into exposure (already max).
		gain = c.MaxGainDB
	}
	if gain < c.MinGainDB {
		// Need to go darker than min gain allows: shorten exposure further.
		short := math.Pow(10, (gain-c.MinGainDB)/20)
		gain = c.MinGainDB
		exp *= short
	}
	return clampState(ExposureState{ExposureUS: exp, GainDB: gain}, c)
}

func percentile(samples []float64, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	cp := append([]float64(nil), samples...)
	sort.Float64s(cp)
	if p <= 0 {
		return cp[0]
	}
	if p >= 100 {
		return cp[len(cp)-1]
	}
	x := p / 100 * float64(len(cp)-1)
	i := int(math.Floor(x))
	if i >= len(cp)-1 {
		return cp[len(cp)-1]
	}
	t := x - float64(i)
	return cp[i]*(1-t) + cp[i+1]*t
}

// RegionSamples extracts 0..1 samples from a 16-bit gray frame over the
// inclusive pixel rectangle r (empty = full frame). stride skips pixels
// so a 24 MP live frame stays cheap.
func RegionSamples(pix []uint16, w, h, x0, y0, x1, y1, stride int) []float64 {
	if w <= 0 || h <= 0 || len(pix) < w*h {
		return nil
	}
	if x1 <= x0 || y1 <= y0 {
		x0, y0, x1, y1 = 0, 0, w, h
	}
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
	if stride < 1 {
		stride = 1
	}
	out := make([]float64, 0, ((x1-x0)/stride+1)*((y1-y0)/stride+1))
	for y := y0; y < y1; y += stride {
		row := y * w
		for x := x0; x < x1; x += stride {
			out = append(out, float64(pix[row+x])/65535)
		}
	}
	return out
}
