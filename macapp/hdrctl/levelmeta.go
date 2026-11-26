package hdrctl

// Leveling metadata and scan metadata of the HDR page (levelmetadata.go,
// scanmetadata.go, hdrpage.go lightState), used by the Trichrome and Mono
// trichrome pages, and the HDR page's panic report (crash.go reportPanic).

import (
	"fmt"
	"time"

	"basetool/rgblight"
	"basetool/scanmeta"

	"basetool/macapp/crashlog"
)

func (c *Ctl) metadataLeveling() *scanmeta.Leveling {
	if c.Light == nil || !c.Light.cfg.UseLastLeveling {
		return nil
	}
	return c.Light.cfg.Leveling
}

func (c *Ctl) levelingValues() [3]int {
	if c.lockLight != nil {
		return *c.lockLight
	}
	if st := c.lightState(); st != nil {
		return st.Value
	}
	if c.Light != nil {
		return c.Light.cfg.Values
	}
	return [3]int{}
}

// lightState is the light's last confirmed state (nil = not connected).
func (c *Ctl) lightState() *rgblight.State {
	if c.Light == nil || !c.Light.connected() {
		return nil
	}
	if cs := c.Light.cl.CachedState(); cs != nil {
		st := *cs
		return &st
	}
	return nil
}

// UseLastLeveling is the "Film base metadata: Last leveling" check box.
func (c *Ctl) UseLastLeveling() bool { return c.Light != nil && c.Light.cfg.UseLastLeveling }

// SetUseLastLeveling is setUseLastLeveling: the check box was toggled.
func (c *Ctl) SetUseLastLeveling(on bool) {
	if c.Light == nil {
		return
	}
	c.Light.cfg.UseLastLeveling = on
	c.Light.saveCfg()
}

func (c *Ctl) lastLevelingText(values [3]int) string {
	if c.Light == nil || c.Light.cfg.Leveling == nil {
		return "Last leveling: unavailable. Run Auto film-base leveling; film_base will be omitted."
	}
	l := c.Light.cfg.Leveling
	stamp := "time unavailable"
	if at, err := time.Parse(time.RFC3339, l.At); err == nil {
		stamp = at.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05") + " CST (UTC+8)"
	}
	text := fmt.Sprintf("Last leveling: R %.5f G %.5f B %.5f (after profile, %s)", l.Base[0], l.Base[1], l.Base[2], stamp)
	if !c.Light.cfg.UseLastLeveling {
		return text + ". Metadata writing is off."
	}
	if !l.Matches(values) {
		return text + ". Light values differ: film_base will be omitted; re-level or load the matching preset."
	}
	return text
}

// scanMetadata captures the GUI settings once before a scan starts
// (scanmetadata.go).
func (c *Ctl) scanMetadata() scanmeta.Metadata {
	m := scanmeta.Metadata{Roll: c.Cfg.Roll, Mode: "white-light", Encoding: "linear"}
	if c.lockLight != nil {
		v := *c.lockLight
		m.LightRGB = &v
		m.LightModel = "CuteNewDesign RGB Light Air"
	}
	if l := c.Light; l != nil {
		values := l.cfg.Values
		if m.LightRGB != nil {
			values = *m.LightRGB
		} else if st := c.lightState(); st != nil {
			values = st.Value
		}
		m = m.WithLeveling(c.metadataLeveling(), values)
	}
	return m
}

// reportPanic logs a recovered panic of a background job with its stack,
// appends it to the crash log and tells the user; the app keeps running.
func (c *Ctl) reportPanic(where string, r any, stack string) {
	msg := fmt.Sprintf("PANIC in %s: %v", where, r)
	if c.Log != nil {
		c.Log.Printf("%s\n%s", msg, stack)
	}
	crashlog.Append(msg + "\n" + stack)
	c.Host.Post(func() {
		logPath := crashlog.File()
		if c.Log != nil {
			logPath = c.Log.Path()
		}
		c.Host.Message(appVersion+" - internal error",
			"An internal error stopped this action:\n\n"+fmt.Sprint(r)+"\n\nWhere: "+where+
				"\n\nThe program keeps running. Details (with the stack) are in\n"+logPath+"\nand "+crashlog.File()+
				"\n\nPlease send these files.")
	})
}
