package main

import (
	"basetool/colorsci"
	"basetool/filmdb"
	"fmt"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"strconv"
)

const castModel = "5219 H-1-5219 rev3-26 / Planck visible 10nm / nominal ECN-2 (approximate)"

type castUI struct {
	owner     *LightUI
	kelvin    *walk.LineEdit
	choice    *walk.ComboBox
	enabled   *walk.CheckBox
	result    *walk.Label
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
	u.result.SetText(fmt.Sprintf("%s — 500T @ %.0f K (Planck approximation)\nAdd density: R %+.4f  G %+.4f  B %+.4f\n3200K conversion: %+.1f mired; light unchanged.\nApproximate stock model, not measured WB. Send to Develop to apply.", state, k, c.Offset[0], c.Offset[1], c.Offset[2], r.Mired))
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
func (u *castUI) page() Widget {
	c := filmdb.ColourCast{Kelvin: 5500}
	choiceIdx := 4
	for i, k := range []float64{5500, 3200, 4300, 6500} {
		if c.Kelvin == k {
			choiceIdx = i
			break
		}
	}
	return Card{Title: "500T illuminant — Develop preview", Collapsed: false, Layout: VBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
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
		Label{AssignTo: &u.result, Text: fmt.Sprintf("Preview: enabled=%v, 500T @ %.0fK.\nAdd density R %+.4f  G %+.4f  B %+.4f.\nPlanck approximation, not measured WB. No light changes.", c.Enabled, c.Kelvin, c.Offset[0], c.Offset[1], c.Offset[2]), MinSize: Size{Height: 72}},
		Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
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
