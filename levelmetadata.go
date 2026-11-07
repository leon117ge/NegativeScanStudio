package main

import (
	"basetool/scanmeta"
	"fmt"
	"github.com/lxn/walk"
	"time"
)

const levelMetadataTip = "Write the last accepted Light-page leveling result (after profile), not a measurement of the scanned image. Off, no result, or different light values: omit film_base."

func (u *HDRUI) metadataLeveling() *scanmeta.Leveling {
	if u.light == nil || !u.light.cfg.UseLastLeveling {
		return nil
	}
	return u.light.cfg.Leveling
}
func (u *HDRUI) levelingValues() [3]int {
	if u.lockLight != nil {
		return *u.lockLight
	}
	if st := u.lightState(); st != nil {
		return st.Value
	}
	if u.light != nil {
		return u.light.cfg.Values
	}
	return [3]int{}
}
func (u *HDRUI) setUseLastLeveling(on bool) {
	if u.light == nil {
		return
	}
	u.light.cfg.UseLastLeveling = on
	u.light.saveCfg()
	u.updateButtons()
}
func (u *HDRUI) lastLevelingText(values [3]int) string {
	if u.light == nil || u.light.cfg.Leveling == nil {
		return "Last leveling: unavailable. Run Auto film-base leveling; film_base will be omitted."
	}
	l := u.light.cfg.Leveling
	stamp := "time unavailable"
	if at, err := time.Parse(time.RFC3339, l.At); err == nil {
		stamp = at.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05") + " CST (UTC+8)"
	}
	text := fmt.Sprintf("Last leveling: R %.5f G %.5f B %.5f (after profile, %s)", l.Base[0], l.Base[1], l.Base[2], stamp)
	if !u.light.cfg.UseLastLeveling {
		return text + ". Metadata writing is off."
	}
	if !l.Matches(values) {
		return text + ". Light values differ: film_base will be omitted; re-level or load the matching preset."
	}
	return text
}
func (u *HDRUI) refreshLevelingUI() {
	if u.light == nil {
		return
	}
	on := u.light.cfg.UseLastLeveling
	if u.levelMetaCB != nil {
		u.levelMetaCB.SetChecked(on)
		u.levelMetaCB.SetEnabled(!u.busy && !u.processing)
	}
	values := u.levelingValues()
	if u.levelMetaLbl != nil {
		setStatusWide(u.levelMetaLbl, u.lastLevelingText(values))
	}
	if !on {
		return
	}
	l := u.metadataLeveling()
	if u.sendBtn != nil {
		u.sendBtn.SetEnabled(!u.busy && !u.processing && !u.sending && l.Matches(values) && u.SendBase != nil)
	}
	if u.baseR == nil {
		return
	}
	for i, ed := range []*walk.LineEdit{u.baseR, u.baseG, u.baseB} {
		if ed == nil {
			continue
		}
		text := "-"
		if l.Matches(values) {
			text = fmt.Sprintf("%.5f", l.Base[i])
		}
		ed.SetText(text)
	}
}

func levelMetadataSummary(m scanmeta.Metadata) string {
	m = m.ForCapture()
	if m.FilmBase == nil {
		return "Film base metadata omitted (option off or no matching accepted leveling)."
	}
	b := *m.FilmBase
	return fmt.Sprintf("Film base metadata: R %.5f G %.5f B %.5f (%s)", b[0], b[1], b[2], m.BaseSource)
}
