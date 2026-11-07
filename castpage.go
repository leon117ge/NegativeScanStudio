package main

import (
	"basetool/colorsci"
	"basetool/filmdb"
	"fmt"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"path/filepath"
	"strconv"
	"time"
)

type castUI struct {
	owner     *LightUI
	kelvin    *walk.LineEdit
	choice    *walk.ComboBox
	stock     *walk.ComboBox
	enabled   *walk.CheckBox
	result    *walk.Label
	sync      func() string
	toDevelop func(filmdb.ColourCast)
}

func (u *castUI) calculate() (filmdb.ColourCast, error) {
	c := filmdb.ColourCast{Model: castModel, Enabled: u.enabled.Checked()}
	k, e := strconv.ParseFloat(u.kelvin.Text(), 64)
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
	u.result.SetText(fmt.Sprintf("%s — 500T @ %.0f K (Planck approximation)\nAdd density: R %+.4f  G %+.4f  B %+.4f\n3200K conversion: %+.1f mired; light unchanged.\nApproximate stock model, not measured WB. Next: Write to DCTL.", state, k, c.Offset[0], c.Offset[1], c.Offset[2], r.Mired))
	return c, nil
}
func (u *castUI) preview() {
	if _, e := u.calculate(); e != nil {
		u.warn(e)
	}
}
func (u *castUI) warn(e error) {
	walk.MsgBox(u.owner.win(), "500T colour cast", e.Error(), walk.MsgBoxIconWarning)
}
func (u *castUI) write() {
	if u.owner.busy || u.owner.hdr.busy {
		return
	}
	c, e := u.calculate()
	if e != nil {
		u.warn(e)
		return
	}
	if e = saveCastConfig(c); e != nil {
		u.warn(e)
		return
	}
	r := levelingRecord{Time: time.Now(), Version: appVersion, Mode: "Colour cast / DCTL only", Profile: castModel, Source: fmt.Sprintf("500T %.0fK; enabled=%v; additive density %v; light UNCHANGED", c.Kelvin, c.Enabled, c.Offset)}
	if e = appendLevelingLog(filepath.Join(configDir(), "leveling_log.csv"), r); e != nil {
		u.owner.log("Colour cast log: %v", e)
	}
	note := "Saved for the next Density_In export. Resolve: reload the DCTL/reset the node; existing slider values are not changed automatically."
	if u.sync != nil {
		if s := u.sync(); s != "" {
			note += "\n" + s
		}
	}
	u.owner.log("Colour cast: %.0fK, enabled %v, additive D %v; NO light commands sent.", c.Kelvin, c.Enabled, c.Offset)
	walk.MsgBox(u.owner.win(), "写入 DCTL", note, walk.MsgBoxIconInformation)
}
func (u *castUI) saveStock() {
	if u.owner.busy || u.owner.hdr.busy {
		return
	}
	c, e := u.calculate()
	if e != nil {
		u.warn(e)
		return
	}
	base := loadConfig().CurrentBase
	if r := u.owner.lastSuccess; r != nil && r.Measured {
		b := r.Base
		base = &b
	}
	if base == nil || !validLevelingBase(*base) {
		u.warn(fmt.Errorf("first level the base and write its measured RGB to DCTL; no base values will be invented"))
		return
	}
	db := u.owner.db()
	if db == nil {
		u.warn(fmt.Errorf("film database unavailable"))
		return
	}
	name := fmt.Sprintf("500T @ %.0fK", c.Kelvin)
	_, e = db.Put(filmdb.Entry{Name: name, R: base[0], G: base[1], B: base[2], Source: "500T illuminant model (approximate)", ColourCast: &c}, false)
	if e == nil {
		e = db.Save()
	}
	if e != nil {
		u.warn(e)
		return
	}
	u.refreshStocks()
	u.owner.log("Saved stock %s (base unchanged, DCTL-only offsets).", name)
	walk.MsgBox(u.owner.win(), "Stock preset", name+" saved in film_base_db.json. The scene presets above recall the same model; loading a stock never sends a light command here.", walk.MsgBoxIconInformation)
}
func (u *castUI) page() Widget {
	c, e := loadCastConfig()
	if e != nil {
		c = filmdb.ColourCast{Kelvin: 5500}
		if u.owner.hdr.log != nil {
			u.owner.hdr.log.Printf("Colour cast config: %v", e)
		}
	}
	if c.Kelvin < 2500 || c.Kelvin > 10000 {
		c.Kelvin = 5500
	}
	choiceIdx := 4
	for i, k := range []float64{5500, 3200, 4300, 6500} {
		if c.Kelvin == k {
			choiceIdx = i
			break
		}
	}
	return Card{Title: "Colour cast / 偏色校正 — 500T 3200K", Collapsed: false, Layout: VBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
		Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
			ComboBox{AssignTo: &u.stock, Model: u.stockNames(), CurrentIndex: 0, StretchFactor: 1},
			PushButton{Text: "Load stock", OnClicked: u.loadStock},
			PushButton{Text: "Refresh", OnClicked: u.refreshStocks},
		}},
		Label{Text: "500T is balanced for 3200K. Set the ORIGINAL scene light, not the scanning light. Camera WB and leveled light stay unchanged."},
		Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
			ComboBox{AssignTo: &u.choice, Model: []string{"500T @ 5500K (Planck daylight approx.)", "500T @ 3200K (no correction)", "500T @ 4300K (Planck)", "500T @ 6500K (Planck daylight approx.)", "500T @ custom K"}, CurrentIndex: choiceIdx, StretchFactor: 1, OnCurrentIndexChanged: func() {
				if u.kelvin == nil || u.choice == nil {
					return
				}
				i := u.choice.CurrentIndex()
				if i >= 0 && i < 4 {
					u.kelvin.SetText(strconv.Itoa([]int{5500, 3200, 4300, 6500}[i]))
					u.preview()
				}
			}},
			LineEdit{AssignTo: &u.kelvin, Text: strconv.FormatFloat(c.Kelvin, 'f', 0, 64), MaxSize: Size{Width: 65}}, Label{Text: "K"},
			CheckBox{AssignTo: &u.enabled, Text: "Enable", Checked: c.Enabled}, PushButton{Text: "Calculate", OnClicked: u.preview},
		}},
		Label{AssignTo: &u.result, Text: fmt.Sprintf("Saved export: enabled=%v, 500T @ %.0fK.\nAdd density R %+.4f  G %+.4f  B %+.4f.\nPlanck approximation, not measured WB. No light changes.", c.Enabled, c.Kelvin, c.Offset[0], c.Offset[1], c.Offset[2]), MinSize: Size{Height: 72}},
		Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
			PushButton{Text: "写入 DCTL", OnClicked: u.write}, PushButton{Text: "Save stock preset", OnClicked: u.saveStock},
			PushButton{Text: "Send to Develop", OnClicked: func() {
				if u.owner.busy || u.owner.hdr.busy {
					return
				}
				c, e := u.calculate()
				if e != nil {
					u.warn(e)
					return
				}
				if u.toDevelop != nil {
					u.toDevelop(c)
				}
			}},
		}},
	}}
}

func (u *castUI) stockNames() []string {
	names := []string{"Saved 500T stock presets (DCTL only)"}
	db, e := filmdb.Load(filmdb.Path(configDir()))
	if e != nil {
		return names
	}
	for _, s := range db.Entries {
		if s.ColourCast != nil {
			names = append(names, s.Name)
		}
	}
	return names
}
func (u *castUI) refreshStocks() {
	if u.stock != nil {
		u.stock.SetModel(u.stockNames())
		u.stock.SetCurrentIndex(0)
	}
}
func (u *castUI) loadStock() {
	if u.owner.busy || u.owner.hdr.busy || u.stock.CurrentIndex() < 1 {
		return
	}
	db, e := filmdb.Load(filmdb.Path(configDir()))
	if e != nil {
		u.warn(e)
		return
	}
	s := db.Find(u.stock.Text())
	if s == nil || s.ColourCast == nil {
		return
	}
	c := s.ColourCast
	if c.Model != castModel {
		u.warn(fmt.Errorf("unsupported stock model; recalculate the illuminant"))
		return
	}
	u.choice.SetCurrentIndex(4)
	u.kelvin.SetText(strconv.FormatFloat(c.Kelvin, 'f', 0, 64))
	u.enabled.SetChecked(c.Enabled)
	u.preview()
	u.owner.log("Loaded colour-cast preset %s for preview. Base and light UNCHANGED. Press Write to DCTL to activate.", s.Name)
}
