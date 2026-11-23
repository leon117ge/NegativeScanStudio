package main

import (
	"math"

	"basetool/hdrscan"
	"basetool/monocap"

	"github.com/lxn/walk"
)

// Mono HDR bracket presets (v1.11), Mono page only. The White light and
// Trichrome pages keep their own bracketEVs. 0/+2/+3/+5 is the default:
// see monocap.DefaultEVs for the sensor-model reasoning. Config:
// HIK_BRACKET=0,2,3,5 (evKey format); an unknown key loads the default.
var monoBracketEVs = [][]int{monocap.DefaultEVs, {0, 2}, {0, 2, 3}}
var monoBracketItems = []string{"0 / +2 / +3 / +5 EV (recommended)", "0 / +2 EV (fast)", "0 / +2 / +3 EV"}

const monoBracketDefault = 0

// monoBracketIndex finds a saved HIK_BRACKET key; -1 when unknown.
func monoBracketIndex(key string) int {
	for i, e := range monoBracketEVs {
		if evKey(e) == key {
			return i
		}
	}
	return -1
}

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

// setNumberEdit sets a NumberEdit after clamping to the widget's current
// range, so a saved config value cannot crash the UI.
func setNumberEdit(ne *walk.NumberEdit, v float64) {
	if ne == nil {
		return
	}
	_ = ne.SetValue(clampFloat(v, ne.MinValue(), ne.MaxValue()))
}

func (u *MonoUI) framesPerExposure() int {
	if u.fpeNE == nil {
		return 1
	}
	return int(math.Round(clampFloat(u.fpeNE.Value(), framesPerExpMin, framesPerExpMax)))
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// captureOptions reads the capture options from the page. The noise model
// is always the MV-CS240-90UM sensor model (hdrscan.DefaultMonoNoise).
func (u *MonoUI) captureOptions() monocap.Options {
	o := monocap.Options{FramesPerExposure: 1, Noise: hdrscan.DefaultMonoNoise()}
	if u.fpeNE != nil {
		o.FramesPerExposure = int(math.Round(clampFloat(u.fpeNE.Value(), framesPerExpMin, framesPerExpMax)))
	}
	return o
}

// loadCaptureOptions restores mode, bracket and frames per exposure.
func (u *MonoUI) loadCaptureOptions() {
	if u.hdr == nil {
		return
	}
	c := u.hdr.cfg
	u.loading = true
	defer func() { u.loading, u.optsLoaded = false, true }()
	if u.capModeCB != nil {
		if c.HikHDR {
			u.capModeCB.SetCurrentIndex(1)
		} else {
			u.capModeCB.SetCurrentIndex(0)
		}
	}
	if u.brCB != nil {
		u.brCB.SetCurrentIndex(clampIdx(c.HikBracket, len(monoBracketEVs)))
	}
	if u.fpeNE != nil {
		setNumberEdit(u.fpeNE, float64(max(framesPerExpMin, c.HikFramesPerExp)))
	}
}

func (u *MonoUI) saveCaptureOptions() {
	if u.hdr == nil || u.loading || !u.optsLoaded {
		return // never overwrite the config before it was restored
	}
	if u.capModeCB != nil {
		u.hdr.cfg.HikHDR = u.capModeCB.CurrentIndex() == 1
	}
	if u.brCB != nil {
		u.hdr.cfg.HikBracket = clampIdx(u.brCB.CurrentIndex(), len(monoBracketEVs))
	}
	if u.fpeNE != nil {
		u.hdr.cfg.HikFramesPerExp = int(math.Round(clampFloat(u.fpeNE.Value(), framesPerExpMin, framesPerExpMax)))
	}
	u.hdr.save()
}
