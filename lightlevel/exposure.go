package lightlevel

import (
	"context"
	"fmt"
	"time"
)

// FitProbeCode is the light brightness used to see whether a channel can
// still reach the target. FitHeadroomCode is the comfortable code the
// dimmest channel should hit the target at after a common exposure bump.
const (
	FitProbeCode    = 100
	FitHeadroomCode = 85
	MinExposureUS   = 32.0
	MaxExposureUS   = 10_000_000.0
)

// VariableExposure is a camera whose fixed exposure can be raised once
// before the light-code search and then held for all three channels.
// Hikrobot monochrome leveling uses this; Bayer / X-T3 cameras do not.
type VariableExposure interface {
	Camera
	GetExposure() (us, gainDB float64)
	SetExposure(us, gainDB float64) error
}

// ExposureFit records whether the common camera exposure was changed so
// the dimmest channel can reach the target with headroom on the light.
type ExposureFit struct {
	StartedUS, FinalUS     float64
	StartedGain, FinalGain float64
	Changed                bool
	Reason                 string
}

// ledDrive is the assumed RGB Light Air response used only to scale
// exposure. The solver still measures every candidate code.
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

func fitCommonExposure(ctx context.Context, l Light, cam VariableExposure, p Params, say func(string, ...any)) (ExposureFit, error) {
	us, gain := cam.GetExposure()
	if us <= 0 {
		us = 10_000
	}
	fit := ExposureFit{StartedUS: us, FinalUS: us, StartedGain: gain, FinalGain: 0, Reason: "exposure already sufficient for the dimmest channel"}
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

	var levels [3]float64
	for c := 0; c < 3; c++ {
		rd, err := probe(c, FitProbeCode)
		if err != nil {
			return fit, fmt.Errorf("exposure probe %c at %d: %w", "RGB"[c], FitProbeCode, err)
		}
		levels[c] = rd.Level
		if say != nil {
			say("Exposure probe: %c at brightness %d = %.5f FS (%.0f us, 0 dB)", "RGB"[c], FitProbeCode, rd.Level, us)
		}
	}

	dim := 0
	for c := 1; c < 3; c++ {
		if levels[c] < levels[dim] {
			dim = c
		}
	}
	if levels[dim] <= 1e-9 {
		return fit, fmt.Errorf("no measurable film base at brightness 100; check the light, the lens cap and the measure area")
	}

	head := ledDrive(FitHeadroomCode) / ledDrive(FitProbeCode)
	pred := levels[dim] * head
	if pred >= p.Target*(1-p.Tol) {
		if say != nil {
			say("Dimmest channel %c is already reachable around light code %d at %.0f us; leaving exposure unchanged", "RGB"[dim], FitHeadroomCode, us)
		}
		return fit, nil
	}

	need := clampExposureUS(us * p.Target / (levels[dim] * head))
	if need <= us*1.01 {
		return fit, nil
	}
	if err := cam.SetExposure(need, 0); err != nil {
		return fit, fmt.Errorf("set exposure %.0f us: %w", need, err)
	}
	us, _ = cam.GetExposure()
	fit.FinalUS, fit.FinalGain, fit.Changed = us, 0, true
	fit.Reason = fmt.Sprintf("raised common exposure from %.0f us to %.0f us so %c can reach the target at light code %d (gain 0 dB)", fit.StartedUS, us, "RGB"[dim], FitHeadroomCode)
	if say != nil {
		say("%s", fit.Reason)
	}

	rd, err := probe(dim, FitHeadroomCode)
	if err != nil {
		return fit, fmt.Errorf("exposure confirm %c at %d: %w", "RGB"[dim], FitHeadroomCode, err)
	}
	if say != nil {
		say("Exposure confirm: %c at brightness %d = %.5f FS (%.0f us)", "RGB"[dim], FitHeadroomCode, rd.Level, us)
	}
	if rd.Level > 1e-9 && rd.Level < p.Target*(1-p.Tol) && us < MaxExposureUS-0.5 {
		need = clampExposureUS(us * p.Target / rd.Level)
		if need > us*1.01 {
			if err := cam.SetExposure(need, 0); err != nil {
				return fit, fmt.Errorf("set exposure %.0f us: %w", need, err)
			}
			us, _ = cam.GetExposure()
			fit.FinalUS = us
			fit.Reason = fmt.Sprintf("raised common exposure from %.0f us to %.0f us so %c can reach the target at light code %d (gain 0 dB)", fit.StartedUS, us, "RGB"[dim], FitHeadroomCode)
			if say != nil {
				say("Dimmest channel still short at code %d; exposure now %.0f us", FitHeadroomCode, us)
			}
		}
	}
	return fit, nil
}
