package main

import (
	"time"

	"basetool/hikbase"
	"basetool/scanmeta"
)

// sharedHikExposure is the one persisted Hikrobot exposure (us, gain 0 dB)
// used by Camera and Light leveling and by Mono Trichrome capture.
func (u *HDRUI) sharedHikExposure() float64 {
	if u == nil {
		return hikbase.DefaultExposureUS
	}
	if u.cfg.HikExposureUS >= hikbase.ExpMinUS {
		return hikbase.ClampExposureUS(u.cfg.HikExposureUS)
	}
	return hikbase.DefaultExposureUS
}

func (u *HDRUI) hikSnapshot() hikbase.Snapshot {
	if u == nil {
		return hikbase.Snapshot{}
	}
	return hikbase.Snapshot{
		US:  u.cfg.HikLeveledUS,
		RGB: u.cfg.HikLeveledRGB,
		At:  hikbase.ParseLeveledAt(u.cfg.HikLeveledAt),
	}
}

func (u *HDRUI) currentLightRGB() [3]int {
	if u == nil || u.light == nil {
		return [3]int{}
	}
	if _, v, err := u.light.edits(); err == nil {
		return v
	}
	return u.light.cfg.Values
}

// setSharedHikExposure writes the shared exposure into config and both
// NumberEdit fields. source is "user", "leveling", or "load". A re-entrant
// call from OnValueChanged is ignored so the two fields cannot loop.
func (u *HDRUI) setSharedHikExposure(us float64, source string) {
	if u == nil || u.syncingHikExp {
		return
	}
	us = hikbase.ClampExposureUS(us)
	if source == "user" && !u.hikExpReady {
		return
	}
	u.syncingHikExp = true
	defer func() { u.syncingHikExp = false }()

	u.cfg.HikExposureUS = us
	if u.mono != nil && u.mono.expNE != nil && u.mono.expNE.Value() != us {
		u.mono.expNE.SetValue(us)
	}
	if u.light != nil && u.light.hikExpNE != nil && u.light.hikExpNE.Value() != us {
		u.light.hikExpNE.SetValue(us)
	}
	if source == "user" || source == "leveling" {
		u.save()
	}
	if u.mono != nil {
		u.mono.updateBaseExposureLabel()
		u.mono.updateCaptureLabel()
	}
}

func (u *HDRUI) recordLeveledBase(us float64, rgb [3]int, at time.Time) {
	if u == nil {
		return
	}
	us = hikbase.ClampExposureUS(us)
	if at.IsZero() {
		at = time.Now()
	}
	u.cfg.HikLeveledUS = us
	u.cfg.HikLeveledRGB = rgb
	u.cfg.HikLeveledAt = at.Format(time.RFC3339)
	u.setSharedHikExposure(us, "leveling")
	if u.log != nil {
		u.log.Printf("Leveled base recorded: %.0f us, lights R %d G %d B %d at %s.", us, rgb[0], rgb[1], rgb[2], at.Format("15:04"))
	}
}

// applySharedHikFromConfig pushes the persisted exposure into both fields
// after the window exists, then enables user-edit sync.
func (u *HDRUI) applySharedHikFromConfig() {
	if u == nil {
		return
	}
	u.hikExpReady = true
	u.setSharedHikExposure(u.sharedHikExposure(), "load")
	if u.mono != nil {
		if u.mono.meterEachCB != nil {
			u.mono.meterEachCB.SetChecked(u.cfg.HikMeterEach)
		}
		u.mono.updateBaseExposureLabel()
		u.mono.updateCaptureLabel()
	}
}

func (u *HDRUI) withHikExposureMeta(m scanmeta.Metadata, us float64, meterEach bool) scanmeta.Metadata {
	m.ExposureUS = us
	m.GainDB = 0
	if meterEach {
		m.ExposureMode = hikbase.ModeMetered
	} else {
		m.ExposureMode = hikbase.ModeShared
	}
	leveled := false
	if u != nil {
		leveled = hikbase.IsLeveled(u.hikSnapshot(), us, u.currentLightRGB())
		if u.cfg.HikLeveledAt != "" {
			m.BaseLeveledAt = u.cfg.HikLeveledAt
		}
	}
	m.BaseLeveled = &leveled
	return m
}

func (u *HDRUI) hikMeterEach() bool {
	if u == nil {
		return false
	}
	if u.mono != nil && u.mono.meterEachCB != nil {
		return u.mono.meterEachCB.Checked()
	}
	return u.cfg.HikMeterEach
}
