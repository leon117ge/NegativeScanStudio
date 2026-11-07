package main

import "github.com/lxn/walk"

func (u *LightUI) copyLeveling() {
	if u.lastSuccess == nil {
		return
	}
	if err := walk.Clipboard().SetText(u.lastSuccess.text()); err != nil {
		walk.MsgBox(u.win(), "Copy leveling", err.Error(), walk.MsgBoxIconWarning)
	}
}
func (u *LightUI) writeLevelingDCTL() {
	r := u.lastSuccess
	if r == nil || !r.Measured || u.busy || u.sendDCTL == nil {
		return
	}
	if err := u.sendDCTL(r.Base); err != nil {
		walk.MsgBox(u.win(), "写入 DCTL", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	msg := "DCTL base saved: " + f3(r.Base) + " (" + r.Profile + "). Next Density_In export uses it; existing Resolve nodes do not change automatically."
	u.log("%s", msg)
	walk.MsgBox(u.win(), "写入 DCTL", msg, walk.MsgBoxIconInformation)
}
