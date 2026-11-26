package hdrctl

// Declarations copied verbatim from the Windows Trichrome and Mono
// trichrome pages and their helpers (tripage.go, levelmetadata.go,
// scanmetadata.go, monopage.go, monoopts_ui.go). copied_test.go fails when
// one of them drifts from its original, so the mode lists, the interrupted
// frame cleanup, the simulator light model, the scan-camera metadata and the
// Hikrobot live settings stay the same as on Windows.

import (
	"os"

	"basetool/hdrscan"
	"basetool/hikcam"
	"basetool/rgblight"
	"basetool/scanmeta"
)

// ---------- tripage.go ----------

var triModeItems = []string{"Trichrome R/G/B (one shot per channel)", "Trichrome R/G/B + HDR (bracket per channel)"}

const triModeTip = "Trichrome: R only, then G only, then B only, one frame 0 per channel; each frame's own colour channel goes into <name>_RGB.tiff.\n" +
	"Trichrome + HDR: the bracket per channel, merged per channel, then combined into <name>_RGB_HDR.tiff.\n" +
	"At scan start the light's state is read (20); each channel is set with 03 at the levelled / stored values;\n" +
	"after the scan (and on Stop or an error) the light is put back exactly as it was before the scan."

// dropShot (v0.83) deletes the RAWs of an interrupted frame.
func dropShot(sh *hdrscan.TriShot) int {
	n := 0
	if sh == nil {
		return 0
	}
	for _, rs := range sh.Raws {
		for _, r := range rs {
			if os.Remove(r) == nil {
				n++
			}
		}
	}
	return n
}

// simLightGain: what the simulated camera sees of the light's state.
func simLightGain(c *rgblight.Client) [3]float64 {
	g := [3]float64{1, 1, 1}
	if cs := c.CachedState(); cs != nil {
		for k := 0; k < 3; k++ {
			g[k] = 0.002 // stray light
			if cs.On[k] {
				g[k] = float64(cs.Value[k]) / 50
			}
		}
	}
	return g
}

// ---------- levelmetadata.go ----------

const levelMetadataTip = "Write the last accepted Light-page leveling result (after profile), not a measurement of the scanned image. Off, no result, or different light values: omit film_base."

// ---------- scanmetadata.go ----------

func withScanCamera(m scanmeta.Metadata) scanmeta.Metadata {
	if p, k := activeCam(); p != nil {
		var a [9]float64
		for r := 0; r < 3; r++ {
			for c := 0; c < 3; c++ {
				a[3*r+c] = p.M[r][c]
			}
		}
		m.CameraMatrix = &a
		m.CameraStrength = k
		m.CameraProfile = p.Name
	}
	return m
}

// ---------- monopage.go ----------

func applyLiveSettings(cam hikcam.Device, base hikcam.Settings, fullRes bool) (string, error) {
	if cam == nil {
		return "", hikcam.ErrNotOpen
	}
	bin := 2
	if fullRes {
		bin = 1
	}
	if sim, ok := cam.(*hikcam.Sim); ok {
		sim.FlatField = false
	}
	live := hikcam.LiveViewSettings(base, bin)
	if err := cam.Apply(live); err != nil {
		return "", err
	}
	got := cam.Settings()
	extra := ""
	if fullRes {
		extra = ", full resolution"
	}
	return "Live view: " + got.LiveSummary() + extra, nil
}

func monoHDRTag(evs []int) string {
	if len(evs) <= 1 {
		return ""
	}
	return " HDR " + evText(evs)
}

// ---------- monoopts_ui.go ----------

// Frames-per-exposure range shown on the Mono page (v1.10).
const (
	framesPerExpMin = 1
	framesPerExpMax = 16
)

// clampFloat limits v to [lo, hi] when lo != hi. A walk NumberEdit rejects
// a SetValue outside that range with "value out of range".
func clampFloat(v, lo, hi float64) float64 {
	if lo != hi {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
	}
	return v
}
