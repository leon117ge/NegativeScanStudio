// Package hikbase holds the shared Hikrobot exposure used by film-base
// leveling and Mono Trichrome capture.
package hikbase

import (
	"fmt"
	"math"
	"time"
)

const (
	DefaultExposureUS float64 = 10000
	ExpMinUS          float64 = 32
	ExpMaxUS          float64 = 10_000_000
	usMatchTol                = 0.5
)

// ExposureMode values written to logs and TIFF metadata.
const (
	ModeShared  = "shared"
	ModeMetered = "metered"
)

// StatusUnleveled is the Mono Trichrome warning when the shared exposure
// or the light values no longer match the last successful leveling.
const StatusUnleveled = "Base not leveled - re-run leveling"

func ClampExposureUS(us float64) float64 {
	if us < ExpMinUS {
		return ExpMinUS
	}
	if us > ExpMaxUS {
		return ExpMaxUS
	}
	return us
}

// Snapshot is the exposure and light codes recorded after a successful
// monochrome film-base leveling run.
type Snapshot struct {
	US  float64
	RGB [3]int
	At  time.Time
}

func (s Snapshot) Recorded() bool {
	return !s.At.IsZero() && s.US >= ExpMinUS
}

// IsLeveled reports whether the current shared exposure and light codes
// still match the recorded leveled base.
func IsLeveled(s Snapshot, us float64, rgb [3]int) bool {
	if !s.Recorded() {
		return false
	}
	if math.Abs(us-s.US) >= usMatchTol {
		return false
	}
	return s.RGB == rgb
}

// Status is the Mono Trichrome base-exposure label.
func Status(s Snapshot, us float64, rgb [3]int) string {
	if IsLeveled(s, us, rgb) {
		return fmt.Sprintf("Base exposure: %.0f us (leveled %s)", us, s.At.Format("15:04"))
	}
	return StatusUnleveled
}

// CaptureLog is the English line written at the start of a Mono Trichrome capture.
func CaptureLog(meterEach bool, us float64, s Snapshot, rgb [3]int) string {
	if meterEach {
		return fmt.Sprintf("Capture metering each channel separately (overrides the shared exposure of %.0f us)", us)
	}
	if IsLeveled(s, us, rgb) {
		return fmt.Sprintf("Capture at shared base %.0f us, 0.00 dB (leveled %s)", us, s.At.Format("15:04"))
	}
	return fmt.Sprintf("Capture at %.0f us, 0.00 dB (base not leveled; using manual exposure)", us)
}

// BracketExposures returns the exposure for each EV offset from a common base.
func BracketExposures(baseUS float64, evs []int) []float64 {
	out := make([]float64, len(evs))
	for i, ev := range evs {
		us := baseUS * math.Pow(2, float64(ev))
		out[i] = ClampExposureUS(us)
	}
	return out
}

func FormatRGB(rgb [3]int) string {
	return fmt.Sprintf("%d,%d,%d", rgb[0], rgb[1], rgb[2])
}

func ParseRGB(s string) ([3]int, bool) {
	var rgb [3]int
	n, err := fmt.Sscanf(s, "%d,%d,%d", &rgb[0], &rgb[1], &rgb[2])
	return rgb, err == nil && n == 3
}

func ParseLeveledAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Shared is a tiny two-widget model used to test the sync guard.
type Shared struct {
	US      float64
	A, B    float64
	Syncing bool
	Sets    int
}

// Set updates the shared exposure and both views. A re-entrant call
// (the usual NumberEdit OnValueChanged loop) is ignored.
func (s *Shared) Set(us float64) {
	us = ClampExposureUS(us)
	if s.Syncing {
		return
	}
	s.Syncing = true
	defer func() { s.Syncing = false }()
	s.US = us
	s.A = us
	s.B = us
	s.Sets++
}
