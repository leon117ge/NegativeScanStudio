package hdrctl

// Camera-page side of the Light page link, ported from tripage.go and
// hikexp_ui.go: the v0.82 light handoff (a leveling or a loaded preset locks
// frame 0 by itself), the ISO and base shutter shared by both pages, and the
// shared Hikrobot exposure.

import (
	"fmt"
	"time"

	"basetool/camera"
	"basetool/hikbase"
	"basetool/scanmeta"

	"basetool/macapp/appstate"
)

func (c *Ctl) fixedShutter() uint32 { return appstate.FixedShutter(c.Cfg) }
func (c *Ctl) fixedISO() int        { return appstate.FixedISO(c.Cfg) }

// ShutterChanged: the Base shutter combo (Camera or Light page) changed Cfg.Shutter.
func (c *Ctl) ShutterChanged() {
	i := max(0, c.Cfg.Shutter)
	changed := i != c.shutPrev
	c.shutPrev = i
	c.Save()
	if !changed {
		return
	}
	sh := c.fixedShutter()
	c.say("Base shutter: " + camera.ShutterLabel(sh) + fmt.Sprintf(" (PTP %d us)", sh))
	if ho := c.handoff; ho != nil && ho.Shutter != sh {
		c.dropHandoff("base shutter changed to " + camera.ShutterLabel(sh))
	}
	s := c.sess
	if s != nil && c.lock != nil && appstate.Fixed(c.Cfg) && c.lock.Shutter != sh && !c.Busy {
		c.releaseLock("Base shutter changed - fixed frame 0 lock released, meter the base again.")
	}
}

// ISOChanged (v0.82): the Camera page's fixed ISO changed Cfg.ISO - synced
// to the Light page's leveling ISO; a fixed lock or handoff at another ISO
// is void.
func (c *Ctl) ISOChanged() {
	i := clampIdx(c.Cfg.ISO, len(isoVals))
	changed := i != c.isoPrev
	c.isoPrev = i
	c.Save()
	if l := c.Light; l != nil && l.cfg.ISO != i {
		l.cfg.ISO = i // the Light page's isoChanged (no-op back here)
		l.saveCfg()
	}
	if !changed {
		return
	}
	iso := isoVals[i]
	c.say(fmt.Sprintf("Fixed ISO: %d (Light page leveling ISO follows)", iso))
	if ho := c.handoff; ho != nil && ho.ISO != iso {
		c.dropHandoff(fmt.Sprintf("ISO changed to %d", iso))
	}
	s := c.sess
	if s != nil && c.lock != nil && appstate.Fixed(c.Cfg) && c.lock.ISO != iso && !c.Busy {
		c.releaseLock(fmt.Sprintf("ISO changed to %d - fixed frame 0 lock released.", iso))
	}
}

// setISO selects a fixed ISO (index) from the other page or a preset.
func (c *Ctl) setISO(i int) {
	if i < 0 || i >= len(isoVals) || c.Cfg.ISO == i {
		return
	}
	c.Cfg.ISO = i
	c.ISOChanged()
}

// setShutter selects a base shutter (index) from the other page or a preset.
func (c *Ctl) setShutter(i int) {
	if i < 0 || i >= len(fixedShutterVals) || c.Cfg.Shutter == i {
		return
	}
	c.Cfg.Shutter = i
	c.ShutterChanged()
}

// setFixedFrame0 selects Frame 0 = Fixed (leveling / stored handoff).
func (c *Ctl) setFixedFrame0() {
	if c.Cfg.Frame0 != 1 {
		c.Cfg.Frame0 = 1
		c.say("Frame 0 set to Fixed (ISO + base shutter) to match the RGB light leveling.")
	}
	c.Save()
}

// releaseLock drops the frame-0 lock (camera worker) with a reason.
func (c *Ctl) releaseLock(why string) {
	s := c.sess
	if s == nil || c.lock == nil {
		return
	}
	c.worker.do(func() {
		s.Locked = nil
		c.Host.Post(func() {
			c.clearLocked()
			c.say(why)
		})
	})
}

func (c *Ctl) dropHandoff(why string) {
	if c.handoff == nil {
		return
	}
	c.say("Light handoff (" + c.handoff.Source + ") void: " + why + " - meter the base, level or load a preset again.")
	c.handoff = nil
}

// takeHandoff (v0.82): a successful leveling or Apply stored. The Camera
// page takes its ISO and shutter (Frame 0 = Fixed) and locks frame 0 by
// itself with one probe at that exposure under the light as it is now - no
// Meter base press. Without a camera the lock waits until it is connected.
func (c *Ctl) takeHandoff(ho baseHandoff) {
	for i, v := range isoVals {
		if v == ho.ISO {
			c.setISO(i)
		}
	}
	if i := fixedShutterIndexOf(ho.Shutter); i >= 0 {
		c.setShutter(i)
	}
	c.setFixedFrame0()
	ho.ISO, ho.Shutter = c.fixedISO(), c.fixedShutter()
	ho.pending = true
	c.handoff = &ho
	c.say(fmt.Sprintf("Light handoff from %s: R %d G %d B %d at ISO %d, %s - frame 0 locks automatically (Meter base not needed).",
		ho.Source, ho.Values[0], ho.Values[1], ho.Values[2], ho.ISO, camera.ShutterLabel(ho.Shutter)))
	s := c.sess
	if s != nil && c.lock != nil && !c.Busy {
		s2 := s
		c.worker.do(func() {
			s2.Locked = nil
			c.Host.Post(c.clearLocked)
		})
	}
}

// autoLock runs a pending handoff's lock when the camera is free.
func (c *Ctl) autoLock() {
	ho := c.handoff
	if ho == nil || !ho.pending || c.dev == nil || c.sess == nil || c.Busy || c.autoLocking {
		return
	}
	c.autoLocking = true
	c.Host.Post(func() {
		c.autoLocking = false
		if c.handoff != ho || !ho.pending || c.dev == nil || c.Busy {
			return
		}
		ho.pending = false
		c.meterSrc(ho)
	})
}

// HandoffText describes a pending or taken light handoff ("" = none).
func (c *Ctl) HandoffText() string {
	ho := c.handoff
	switch {
	case ho == nil:
		return ""
	case ho.pending:
		return fmt.Sprintf("Frame 0: lock pending - ISO %d, %s from %s", ho.ISO, camera.ShutterLabel(ho.Shutter), ho.Source)
	}
	return "Frame 0 lock from " + ho.Source
}

// ---------- shared Hikrobot exposure (hikexp_ui.go) ----------

// sharedHikExposure is the one persisted Hikrobot exposure (us, gain 0 dB)
// used by Camera and Light leveling and by Mono Trichrome capture.
func (c *Ctl) sharedHikExposure() float64 {
	if c.Cfg.HikExposureUS >= hikbase.ExpMinUS {
		return hikbase.ClampExposureUS(c.Cfg.HikExposureUS)
	}
	return hikbase.DefaultExposureUS
}

// hikSnapshot is the exposure and light codes of the last successful
// monochrome leveling.
func (c *Ctl) hikSnapshot() hikbase.Snapshot {
	return hikbase.Snapshot{
		US:  c.Cfg.HikLeveledUS,
		RGB: c.Cfg.HikLeveledRGB,
		At:  hikbase.ParseLeveledAt(c.Cfg.HikLeveledAt),
	}
}

// currentLightRGB is the light values on the Light page (its fields, else
// the saved values).
func (c *Ctl) currentLightRGB() [3]int {
	if c.Light == nil {
		return [3]int{}
	}
	if _, v, err := c.Light.edits(); err == nil {
		return v
	}
	return c.Light.cfg.Values
}

// setSharedHikExposure writes the shared exposure into the config and both
// exposure fields (Light page and Mono trichrome). source is "user",
// "leveling", or "load". A re-entrant call is ignored so the two fields
// cannot loop.
func (c *Ctl) setSharedHikExposure(us float64, source string) {
	if c.syncingHikExp {
		return
	}
	us = hikbase.ClampExposureUS(us)
	if source == "user" && !c.hikExpReady {
		return
	}
	c.syncingHikExp = true
	defer func() { c.syncingHikExp = false }()
	c.Cfg.HikExposureUS = us
	if m := c.Mono; m != nil {
		m.setExpField(us)
	}
	if l := c.Light; l != nil {
		l.setHikField(us)
	}
	if source == "user" || source == "leveling" {
		c.Save()
	}
	if m := c.Mono; m != nil {
		m.updateCaptureLabel()
	}
}

func (c *Ctl) recordLeveledBase(us float64, rgb [3]int, at time.Time) {
	us = hikbase.ClampExposureUS(us)
	if at.IsZero() {
		at = time.Now()
	}
	c.Cfg.HikLeveledUS = us
	c.Cfg.HikLeveledRGB = rgb
	c.Cfg.HikLeveledAt = at.Format(time.RFC3339)
	c.setSharedHikExposure(us, "leveling")
	c.Log.Printf("Leveled base recorded: %.0f us, lights R %d G %d B %d at %s.", us, rgb[0], rgb[1], rgb[2], at.Format("15:04"))
}

// applySharedHikFromConfig pushes the persisted exposure into both fields
// once the window exists, then enables user-edit sync.
func (c *Ctl) applySharedHikFromConfig() {
	c.hikExpReady = true
	c.setSharedHikExposure(c.sharedHikExposure(), "load")
	if m := c.Mono; m != nil {
		m.MeterEach = c.Cfg.HikMeterEach
		m.updateCaptureLabel()
	}
}

func (c *Ctl) withHikExposureMeta(m scanmeta.Metadata, us float64, meterEach bool) scanmeta.Metadata {
	m.ExposureUS = us
	m.GainDB = 0
	if meterEach {
		m.ExposureMode = hikbase.ModeMetered
	} else {
		m.ExposureMode = hikbase.ModeShared
	}
	leveled := hikbase.IsLeveled(c.hikSnapshot(), us, c.currentLightRGB())
	if c.Cfg.HikLeveledAt != "" {
		m.BaseLeveledAt = c.Cfg.HikLeveledAt
	}
	m.BaseLeveled = &leveled
	return m
}

// hikMeterEach is Mono trichrome's "Auto-meter each channel" check box.
func (c *Ctl) hikMeterEach() bool {
	if c.Mono != nil {
		return c.Mono.MeterEach
	}
	return c.Cfg.HikMeterEach
}
