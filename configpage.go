package main

// v0.83 scan config / presets on the Light page: the levelled light (R/G/B
// and on/off), the camera ISO and base shutter, and the film stock info.
// Loading one sets the light (03, confirmed), the ISO/shutter on both the
// Light and the camera side, and hands the result to the frame-0 lock.

import (
	"fmt"
	"strings"

	"basetool/camera"
	"basetool/rgblight"
	"basetool/scanconfig"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

const presetNone = "(choose a preset)"

func (u *LightUI) presetGroup() Widget {
	row := func(label string, w Widget) Widget {
		return Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
			Label{Text: label, MinSize: Size{Width: 62}, MaxSize: Size{Width: 62}}, w}}
	}
	return Card{
		Title:  "Scan config presets",
		Icon:   iconPreset,
		Layout: VBox{Spacing: 3, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 4}},
		Children: []Widget{
			Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
				ComboBox{AssignTo: &u.presetCB, Model: []string{presetNone}, CurrentIndex: 0, StretchFactor: 3, OnCurrentIndexChanged: func() { u.presetChanged() }},
				PushButton{AssignTo: &u.loadBtn, Text: "Load preset", Enabled: false, OnClicked: func() { u.loadPreset() },
					ToolTipText: "Apply the preset: light values (03, confirmed), camera ISO and base shutter, frame-0 handoff."},
				PushButton{AssignTo: &u.delBtn, Text: "Delete", Enabled: false, OnClicked: func() { u.deletePreset() }},
			}},
			row("Name", LineEdit{AssignTo: &u.nameEd, ToolTipText: "Preset name (Save preset replaces a preset with the same name)."}),
			row("Film stock", LineEdit{AssignTo: &u.stockEd, ToolTipText: "Film stock; its base values come from the film-base DB entry of the same name when present."}),
			row("Notes", LineEdit{AssignTo: &u.notesEd}),
			Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
				PushButton{AssignTo: &u.saveBtn, Text: "Save preset", OnClicked: func() { u.savePreset() },
					ToolTipText: "Store the current light values and on/off, ISO, base shutter and film info under this name."},
				PushButton{AssignTo: &u.fileSaveBtn, Text: "Save as file...", OnClicked: func() { u.savePresetFile() }},
				PushButton{AssignTo: &u.fileLoadBtn, Text: "Load from file...", OnClicked: func() { u.loadPresetFile() }},
				CheckBox{AssignTo: &u.autoCB, Text: "Apply when chosen", Checked: u.cfg.AutoLoad, OnCheckedChanged: func() {
					u.cfg.AutoLoad = u.autoCB.Checked()
					u.saveCfg()
				}},
			}},
			Label{AssignTo: &u.presetLbl, Text: "", MinSize: Size{Height: 48}},
		},
	}
}

// initPresets loads the store and migrates the film-base DB light settings once.
func (u *LightUI) initPresets() {
	scanconfig.Shutters = fixedShutterVals
	s, exists, err := scanconfig.Load(scanconfig.Path(configDir()))
	u.presets = s
	if err != nil {
		u.log("Preset file could not be read (%v) - starting with an empty list; the file is not overwritten until you save.", err)
	}
	if !exists {
		var ls []scanconfig.Legacy
		if d := u.db(); d != nil {
			for _, n := range d.Names() {
				e := d.Find(n)
				if e == nil || e.RGBLight == nil {
					continue
				}
				l := e.RGBLight
				ls = append(ls, scanconfig.Legacy{Name: e.Name, Base: [3]float64{e.R, e.G, e.B}, Values: l.Values, LightBase: l.Base,
					Target: l.Target, ISO: l.ISO, ShutterUS: l.Shutter, Levelled: l.Levelled, Date: l.Date})
			}
		}
		if n := s.Migrate(ls); n > 0 {
			if err := s.Save(); err != nil {
				u.log("Migrated presets could not be saved: %v", err)
			} else {
				u.log("Migrated %d light setting(s) from the film-base DB into presets (%s).", n, scanconfig.FileName)
			}
		}
	}
	u.refreshPresets(u.cfg.Stock)
}

func (u *LightUI) refreshPresets(sel string) {
	if u.presetCB == nil || u.presets == nil {
		return
	}
	items := append([]string{presetNone}, u.presets.Names()...)
	idx := 0
	for i, n := range items {
		if i > 0 && strings.EqualFold(n, sel) {
			idx = i
		}
	}
	u.inPreset = true
	u.presetCB.SetModel(items)
	u.presetCB.SetCurrentIndex(idx)
	u.inPreset = false
	u.showPreset()
}

func (u *LightUI) curPreset() *scanconfig.Config {
	if u.presetCB == nil || u.presets == nil || u.presetCB.CurrentIndex() <= 0 {
		return nil
	}
	return u.presets.Find(u.presetCB.Text())
}

func presetSummary(c *scanconfig.Config) string {
	on := ""
	for i, b := range c.Light.On {
		if b {
			on += string("RGB"[i])
		}
	}
	if on == "" {
		on = "none"
	}
	sh := "keep current"
	if c.ShutterUS != 0 {
		sh = camera.ShutterLabel(c.ShutterUS)
	}
	t := fmt.Sprintf("%s: light R %d G %d B %d (on %s), ISO %d, shutter %s", c.Name, c.Light.Values[0], c.Light.Values[1], c.Light.Values[2], on, c.ISO, sh)
	if c.Film.Stock != "" {
		t += ", film " + c.Film.Stock
	}
	if b := c.Film.Base; b != [3]float64{} {
		t += fmt.Sprintf(", base %.3f / %.3f / %.3f", b[0], b[1], b[2])
	}
	if c.Film.CamProfile != "" {
		t += fmt.Sprintf(", levelled for camera profile \"%s\" x%.2f", c.Film.CamProfile, c.Film.CamStrength)
	}
	if b := c.Film.TIFFBase; b != [3]float64{} {
		t += fmt.Sprintf(", TIFF base %.3f / %.3f / %.3f", b[0], b[1], b[2])
	}
	if c.Film.Notes != "" {
		t += " - " + c.Film.Notes
	}
	if c.Date != "" {
		t += " (" + c.Date + ")"
	}
	return t
}

func (u *LightUI) showPreset() {
	c := u.curPreset()
	if c == nil {
		setLightStatusN(3, u.presetLbl, "A preset stores the levelled light, ISO, base shutter and film info. Level the light (or set it by hand), name it and press Save preset.")
		u.updatePresetButtons()
		return
	}
	u.nameEd.SetText(c.Name)
	u.stockEd.SetText(c.Film.Stock)
	u.notesEd.SetText(c.Film.Notes)
	setLightStatusN(3, u.presetLbl, presetSummary(c))
	u.updatePresetButtons()
}

func (u *LightUI) updatePresetButtons() {
	if u.presetCB == nil || u.saveBtn == nil {
		return
	}
	has := u.curPreset() != nil
	u.loadBtn.SetEnabled(!u.busy && has)
	u.delBtn.SetEnabled(!u.busy && has)
	u.saveBtn.SetEnabled(!u.busy)
	u.fileSaveBtn.SetEnabled(!u.busy)
	u.fileLoadBtn.SetEnabled(!u.busy)
}

func (u *LightUI) presetChanged() {
	if u.inPreset {
		return
	}
	if c := u.curPreset(); c != nil {
		u.cfg.Stock = c.Name
	} else {
		u.cfg.Stock = ""
	}
	u.saveCfg()
	u.showPreset()
	if c := u.curPreset(); c != nil && u.autoCB.Checked() && !u.busy {
		u.applyConfig(*c)
	}
}

// currentConfig builds a config from the Light page controls.
func (u *LightUI) currentConfig(name string) (scanconfig.Config, error) {
	on, v, err := u.edits()
	if err != nil {
		return scanconfig.Config{}, err
	}
	c := scanconfig.Config{Name: strings.TrimSpace(name), Light: scanconfig.Light{Values: v, On: on},
		ISO: isoVals[clampIdx(u.isoCB.CurrentIndex(), len(isoVals))], ShutterUS: u.hdr.fixedShutter(), Source: "saved"}
	c.Film.Stock = strings.TrimSpace(u.stockEd.Text())
	c.Film.Notes = strings.TrimSpace(u.notesEd.Text())
	if c.Film.Stock != "" {
		if d := u.db(); d != nil {
			if e := d.Find(c.Film.Stock); e != nil {
				c.Film.Base = [3]float64{e.R, e.G, e.B}
			}
		}
	}
	if r := u.lastRes; r != nil && r.Values == v {
		c.Film.LightBase, c.Film.Target, c.Film.Levelled = r.Readings, u.lastTgt, r.Converged()
		if u.lastPlan.on {
			c.Film.CamProfile, c.Film.CamStrength = u.lastPlan.name, u.lastPlan.k
		}
		if u.lastTIFF {
			c.Film.TIFFBase = u.lastPost
		}
	}
	return c, c.Valid()
}

func (u *LightUI) savePreset() {
	name := strings.TrimSpace(u.nameEd.Text())
	if name == "" {
		name = strings.TrimSpace(u.stockEd.Text())
	}
	c, err := u.currentConfig(name)
	if err != nil {
		walk.MsgBox(u.win(), "Save preset", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	if u.presets.Find(c.Name) != nil && walk.MsgBox(u.win(), "Save preset", "Replace the preset "+c.Name+"?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	u.presets.Put(c)
	if err := u.presets.Save(); err != nil {
		walk.MsgBox(u.win(), "Save preset", "Could not write the preset file: "+err.Error(), walk.MsgBoxIconError)
		return
	}
	u.log("Preset saved: %s", presetSummary(&c))
	u.cfg.Stock = c.Name
	u.saveCfg()
	u.refreshPresets(c.Name)
}

func (u *LightUI) deletePreset() {
	c := u.curPreset()
	if c == nil || walk.MsgBox(u.win(), "Delete preset", "Delete the preset "+c.Name+"?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	name := c.Name
	u.presets.Delete(name)
	if err := u.presets.Save(); err != nil {
		walk.MsgBox(u.win(), "Delete preset", err.Error(), walk.MsgBoxIconError)
	}
	u.log("Preset deleted: %s", name)
	u.refreshPresets("")
}

const presetFilter = "Scan config (*.nssconfig;*.json)|*.nssconfig;*.json|All files (*.*)|*.*"

func (u *LightUI) savePresetFile() {
	name := strings.TrimSpace(u.nameEd.Text())
	if name == "" {
		name = "scan config"
	}
	c, err := u.currentConfig(name)
	if err != nil {
		walk.MsgBox(u.win(), "Save config as file", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	dlg := &walk.FileDialog{Title: "Save scan config", Filter: presetFilter, FilePath: name + ".nssconfig"}
	if ok, _ := dlg.ShowSave(u.win()); !ok {
		return
	}
	p := dlg.FilePath
	if !strings.Contains(strings.ToLower(p[strings.LastIndexAny(p, `\/`)+1:]), ".") {
		p += ".nssconfig"
	}
	if err := scanconfig.WriteFile(p, c); err != nil {
		walk.MsgBox(u.win(), "Save config as file", err.Error(), walk.MsgBoxIconError)
		return
	}
	u.log("Config saved to %s: %s", p, presetSummary(&c))
}

func (u *LightUI) loadPresetFile() {
	dlg := &walk.FileDialog{Title: "Load scan config", Filter: presetFilter}
	if ok, _ := dlg.ShowOpen(u.win()); !ok {
		return
	}
	c, err := scanconfig.ReadFile(dlg.FilePath)
	if err != nil {
		walk.MsgBox(u.win(), "Load config from file", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	c.Source = "file " + dlg.FilePath
	u.log("Config loaded from %s: %s", dlg.FilePath, presetSummary(&c))
	if u.presets.Find(c.Name) == nil || walk.MsgBox(u.win(), "Load config from file", "Also store it as preset "+c.Name+" (replaces the one with that name)?", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes {
		u.presets.Put(c)
		if err := u.presets.Save(); err == nil {
			u.cfg.Stock = c.Name
			u.saveCfg()
			u.refreshPresets(c.Name)
		}
	}
	u.applyConfig(c)
}

func (u *LightUI) loadPreset() {
	if c := u.curPreset(); c != nil {
		u.applyConfig(*c)
	}
}

// applyConfig puts the preset on the page and the camera side, then (when
// the light is connected) sends the light values with 03, confirms them and
// hands light + exposure to the frame-0 lock.
func (u *LightUI) applyConfig(c scanconfig.Config) {
	if err := c.Valid(); err != nil {
		walk.MsgBox(u.win(), "Load preset", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	if u.busy {
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
		u.log("Preset %s has every channel off - all three switched on for scanning.", c.Name)
	}
	u.restoreExposure(c.ISO, c.ShutterUS)
	for i := 0; i < 3; i++ {
		u.enCB[i].SetChecked(on[i])
		u.valEd[i].SetText(fmt.Sprint(v[i]))
	}
	u.cfg.Values = v
	u.saveCfg()
	iso, sh := c.ISO, c.ShutterUS
	if iso == 0 {
		iso = u.hdr.fixedISO()
	}
	if fixedShutterIndexOf(sh) < 0 {
		sh = u.hdr.fixedShutter()
	}
	if !u.connected() {
		u.log("Preset %s: ISO/shutter set; light not connected - values put in the controls, connect and Load preset again to light it.", c.Name)
		setLightStatusN(3, u.presetLbl, "Loaded (light not connected): "+presetSummary(&c))
		return
	}
	u.hdrLightChanged(v)
	name := c.Name
	u.lightJobThen("Preset "+name+" (03, confirmed)", func(cl *rgblight.Client) (rgblight.State, error) {
		return setAllBusyOnce(cl, on, v)
	}, func(st rgblight.State) {
		u.hdr.takeHandoff(baseHandoff{ISO: iso, Shutter: sh, Values: v, Source: "preset " + name})
		setLightStatusN(3, u.presetLbl, "Applied: "+presetSummary(&c))
	})
}
