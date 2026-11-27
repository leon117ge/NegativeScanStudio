package hdrctl

// Declarations copied verbatim from the Windows Chart calibration dialog
// (chartcal.go) and its helper (presets.go). copied_test.go fails when one
// of them drifts.

import "strings"

const calTip = "Camera + light input profile: scan a 24-patch transmission chart (e.g. SoftLoaf / IT8.7/1) with the trichrome light, load its CGATS reference, fit a grey-preserving 3x3 (mean ΔE2000), save it as a camera profile (applied to the automatic trichrome film-base measurement); TIFF scan metadata records the profile for the OFX plugin."

// CalTip is the Camera card's "Chart calibration..." tooltip.
func CalTip() string { return calTip }

// ---------- presets.go ----------

func cleanName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 40 {
		return "", false
	}
	for _, c := range s {
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == ' ' || c == '-' || c == '_' || c == '.'
		if !ok {
			return "", false
		}
	}
	return s, true
}
