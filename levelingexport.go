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
