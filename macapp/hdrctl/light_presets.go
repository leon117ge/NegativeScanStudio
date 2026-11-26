package hdrctl

// Scan config presets (configpage.go), the 500T illuminant calculator
// (castpage.go) and the page start-up (LightUI.AfterCreate).

import (
	"fmt"
	"strconv"
	"strings"

	"basetool/colorsci"
	"basetool/filmdb"
	"basetool/rgblight"
	"basetool/scanconfig"
)

// Start is AfterCreate: camera source, rectangle, ports, presets.
func (l *Light) Start() {
	l.hdr.applySharedHikFromConfig()
	l.CamSourceChanged()
	if box, ok := l.manualBase(); ok {
		l.publishRect(box)
	}
	l.RefreshPorts()
	l.initPresets()
	for c := 0; c < 3; c++ {
		l.En[c] = true
	}
	l.StateText = "Light reports: -"
	l.ResText = "Clear film base on the light; exposure fixed, only the light changes."
	l.log("%s Light page ready. Only the documented customer commands are sent to the light.", appVersion)
}

// initPresets loads the store and migrates the film-base DB light settings once.
func (l *Light) initPresets() {
	scanconfig.Shutters = fixedShutterVals
	s, exists, err := scanconfig.Load(scanconfig.Path(configDir()))
	l.presets = s
	if err != nil {
		l.log("Preset file could not be read (%v) - starting with an empty list; the file is not overwritten until you save.", err)
	}
	if !exists {
		var ls []scanconfig.Legacy
		if d := l.hdr.DB; d != nil {
			for _, n := range d.Names() {
				e := d.Find(n)
				if e == nil || e.RGBLight == nil {
					continue
				}
				lr := e.RGBLight
				ls = append(ls, scanconfig.Legacy{Name: e.Name, Base: [3]float64{e.R, e.G, e.B}, Values: lr.Values, LightBase: lr.Base,
					Target: lr.Target, ISO: lr.ISO, ShutterUS: lr.Shutter, Levelled: lr.Levelled, Date: lr.Date})
			}
		}
		if n := s.Migrate(ls); n > 0 {
			if err := s.Save(); err != nil {
				l.log("Migrated presets could not be saved: %v", err)
			} else {
				l.log("Migrated %d light setting(s) from the film-base DB into presets (%s).", n, scanconfig.FileName)
			}
		}
	}
	l.refreshPresets(l.cfg.Stock)
}

func (l *Light) refreshPresets(sel string) {
	if l.presets == nil {
		return
	}
	items := append([]string{presetNone}, l.presets.Names()...)
	idx := 0
	for i, n := range items {
		if i > 0 && strings.EqualFold(n, sel) {
			idx = i
		}
	}
	l.inPreset = true
	l.PresetItems, l.PresetIdx = items, idx
	l.inPreset = false
	l.showPreset()
}

func (l *Light) curPreset() *scanconfig.Config {
	if l.presets == nil || l.PresetIdx <= 0 || l.PresetIdx >= len(l.PresetItems) {
		return nil
	}
	return l.presets.Find(l.PresetItems[l.PresetIdx])
}

func (l *Light) showPreset() {
	c := l.curPreset()
	if c == nil {
		l.PresetText = "A preset stores the levelled light, ISO, base shutter and film info. Level the light (or set it by hand), name it and press Save preset."
		return
	}
	l.NameText, l.StockText, l.NotesText = c.Name, c.Film.Stock, c.Film.Notes
	l.FieldSeq++
	l.PresetText = presetSummary(c)
}

// PresetChanged: the preset combo changed PresetIdx.
func (l *Light) PresetChanged() {
	if l.inPreset {
		return
	}
	if c := l.curPreset(); c != nil {
		l.cfg.Stock = c.Name
	} else {
		l.cfg.Stock = ""
	}
	l.saveCfg()
	l.showPreset()
	if c := l.curPreset(); c != nil && l.cfg.AutoLoad && !l.busy {
		l.applyConfig(*c)
	}
}

// AutoLoadChanged: the "Apply when chosen" box changed Settings().AutoLoad.
func (l *Light) AutoLoadChanged() { l.saveCfg() }

// currentConfig builds a config from the Light page controls.
func (l *Light) currentConfig(name string) (scanconfig.Config, error) {
	on, v, err := l.edits()
	if err != nil {
		return scanconfig.Config{}, err
	}
	c := scanconfig.Config{Name: strings.TrimSpace(name), Light: scanconfig.Light{Values: v, On: on},
		ISO: isoVals[clampIdx(l.cfg.ISO, len(isoVals))], ShutterUS: l.hdr.fixedShutter(), Source: "saved"}
	c.Film.Stock = strings.TrimSpace(l.StockText)
	c.Film.Notes = strings.TrimSpace(l.NotesText)
	if c.Film.Stock != "" {
		if d := l.hdr.DB; d != nil {
			if e := d.Find(c.Film.Stock); e != nil {
				c.Film.Base = [3]float64{e.R, e.G, e.B}
			}
		}
	}
	if r := l.lastRes; r != nil && r.Values == v {
		c.Film.LightBase, c.Film.Target, c.Film.Levelled = r.Readings, l.lastTgt, r.Converged()
		if l.lastPlan.on {
			c.Film.CamProfile, c.Film.CamStrength = l.lastPlan.name, l.lastPlan.k
		}
		if l.lastTIFF {
			c.Film.TIFFBase = l.lastPost
		}
	}
	c.RecordLeveling(l.cfg.Leveling)
	return c, c.Valid()
}

// SavePreset is the Save preset button.
func (l *Light) SavePreset() {
	name := strings.TrimSpace(l.NameText)
	if name == "" {
		name = strings.TrimSpace(l.StockText)
	}
	c, err := l.currentConfig(name)
	if err != nil {
		l.msg("Save preset", err.Error())
		return
	}
	put := func() {
		l.presets.Put(c)
		if err := l.presets.Save(); err != nil {
			l.msg("Save preset", "Could not write the preset file: "+err.Error())
			return
		}
		l.log("Preset saved: %s", presetSummary(&c))
		l.cfg.Stock = c.Name
		l.saveCfg()
		l.refreshPresets(c.Name)
	}
	if l.presets.Find(c.Name) != nil {
		l.hdr.Host.Confirm("Save preset", "Replace the preset "+c.Name+"?", func(ok bool) {
			if ok {
				put()
			}
		})
		return
	}
	put()
}

// DeletePreset is the Delete button.
func (l *Light) DeletePreset() {
	c := l.curPreset()
	if c == nil {
		return
	}
	name := c.Name
	l.hdr.Host.Confirm("Delete preset", "Delete the preset "+name+"?", func(ok bool) {
		if !ok {
			return
		}
		l.presets.Delete(name)
		if err := l.presets.Save(); err != nil {
			l.msg("Delete preset", err.Error())
		}
		l.log("Preset deleted: %s", name)
		l.refreshPresets("")
	})
}

// presetExts are the scan config file types (Windows presetFilter).
var presetExts = []string{"nssconfig", "json"}

// SavePresetFile is Save as file... (native save panel).
func (l *Light) SavePresetFile() {
	name := strings.TrimSpace(l.NameText)
	if name == "" {
		name = "scan config"
	}
	c, err := l.currentConfig(name)
	if err != nil {
		l.msg("Save config as file", err.Error())
		return
	}
	l.hdr.Host.PickFile("Save scan config", true, name+".nssconfig", presetExts, func(p string, ok bool) {
		if !ok {
			return
		}
		if !strings.Contains(strings.ToLower(p[strings.LastIndexAny(p, `\/`)+1:]), ".") {
			p += ".nssconfig"
		}
		if err := scanconfig.WriteFile(p, c); err != nil {
			l.msg("Save config as file", err.Error())
			return
		}
		l.log("Config saved to %s: %s", p, presetSummary(&c))
	})
}

// LoadPresetFile is Load from file... (native open panel).
func (l *Light) LoadPresetFile() {
	l.hdr.Host.PickFile("Load scan config", false, "", presetExts, func(path string, ok bool) {
		if !ok {
			return
		}
		c, err := scanconfig.ReadFile(path)
		if err != nil {
			l.msg("Load config from file", err.Error())
			return
		}
		c.Source = "file " + path
		l.log("Config loaded from %s: %s", path, presetSummary(&c))
		store := func() {
			l.presets.Put(c)
			if err := l.presets.Save(); err == nil {
				l.cfg.Stock = c.Name
				l.saveCfg()
				l.refreshPresets(c.Name)
			}
		}
		if l.presets.Find(c.Name) == nil {
			store()
			l.applyConfig(c)
			return
		}
		l.hdr.Host.Confirm("Load config from file", "Also store it as preset "+c.Name+" (replaces the one with that name)?", func(ok bool) {
			if ok {
				store()
			}
			l.applyConfig(c)
		})
	})
}

// LoadPreset is the Load preset button.
func (l *Light) LoadPreset() {
	if c := l.curPreset(); c != nil {
		l.applyConfig(*c)
	}
}

// applyConfig puts the preset on the page and the camera side, then (when
// the light is connected) sends the light values with 03, confirms them and
// hands light + exposure to the frame-0 lock.
func (l *Light) applyConfig(c scanconfig.Config) {
	if err := c.Valid(); err != nil {
		l.msg("Load preset", err.Error())
		return
	}
	if l.busy {
		return
	}
	v, on := c.Light.Values, c.Light.On
	for i := range v {
		if v[i] < 1 { // 0 = off: the light takes 1..100, a channel is switched off with its enable
			v[i], on[i] = 1, false
		}
	}
	if on == [3]bool{} {
		on = [3]bool{true, true, true}
		l.log("Preset %s has every channel off - all three switched on for scanning.", c.Name)
	}
	l.restoreExposure(c.ISO, c.ShutterUS)
	for i := 0; i < 3; i++ {
		l.En[i] = on[i]
		l.Val[i] = fmt.Sprint(v[i])
	}
	l.FieldSeq++
	l.cfg.Values = v
	l.cfg.Leveling = c.LevelingReference()
	l.saveCfg()
	iso, sh := c.ISO, c.ShutterUS
	if iso == 0 {
		iso = l.hdr.fixedISO()
	}
	if fixedShutterIndexOf(sh) < 0 {
		sh = l.hdr.fixedShutter()
	}
	if !l.connected() {
		l.log("Preset %s: ISO/shutter set; light not connected - values put in the controls, connect and Load preset again to light it.", c.Name)
		l.PresetText = "Loaded (light not connected): " + presetSummary(&c)
		return
	}
	l.hdrLightChanged(v)
	name := c.Name
	l.lightJobThen("Preset "+name+" (03, confirmed)", func(cl *rgblight.Client) (rgblight.State, error) {
		return setAllBusyOnce(cl, on, v)
	}, func(st rgblight.State) {
		l.hdr.takeHandoff(baseHandoff{ISO: iso, Shutter: sh, Values: v, Source: "preset " + name})
		l.PresetText = "Applied: " + presetSummary(&c)
	})
}

// ---------- 500T illuminant (castpage.go) ----------

// CastItems are the 500T illuminant choices.
var CastItems = []string{"500T @ 5500K (Planck daylight approx.)", "500T @ 3200K (no correction)", "500T @ 4300K (Planck)", "500T @ 6500K (Planck daylight approx.)", "500T @ custom K"}

// CastChoiceChanged: the illuminant combo changed CastIdx.
func (l *Light) CastChoiceChanged() {
	i := l.CastIdx
	if i >= 0 && i < 4 {
		l.KelvinText = strconv.Itoa([]int{5500, 3200, 4300, 6500}[i])
		l.FieldSeq++
		l.CastPreview()
	}
}

func (l *Light) castCalculate() (filmdb.ColourCast, error) {
	c := filmdb.ColourCast{Model: castModel, Enabled: l.CastOn}
	k, e := strconv.ParseFloat(l.KelvinText, 64)
	if e != nil {
		return c, fmt.Errorf("scene illuminant must be a number (2500..10000 K)")
	}
	r, e := colorsci.Vision3Illuminant(k)
	if e != nil {
		return c, e
	}
	c.Kelvin = k
	for i := range c.Offset {
		c.Offset[i] = -r.Density[i]
	}
	state := "OFF (preview only)"
	if c.Enabled {
		state = "ON"
	}
	l.CastText = fmt.Sprintf("%s — 500T @ %.0f K (Planck approximation)\nAdd density: R %+.4f  G %+.4f  B %+.4f\n3200K conversion: %+.1f mired; light unchanged.\nApproximate stock model, not measured WB. Send to Develop to apply.", state, k, c.Offset[0], c.Offset[1], c.Offset[2], r.Mired)
	return c, nil
}

// CastPreview is the Calculate button.
func (l *Light) CastPreview() {
	if _, e := l.castCalculate(); e != nil {
		l.msg("500T colour cast", e.Error())
	}
}

// CastToDevelop is Send to Develop (main.go's toDevelop on Windows: Convert
// does not use the Develop cast controls, so it only explains that).
func (l *Light) CastToDevelop() {
	if l.busy || l.hdr.Busy {
		return
	}
	if _, e := l.castCalculate(); e != nil {
		l.msg("500T colour cast", e.Error())
		return
	}
	l.msg("500T colour cast", "Convert models a digital-to-negative exposure and does not use the Develop Density In cast controls. Colour cast stays on the 500T calculator page.")
}
