package hdrctl

import (
	"fmt"
	"testing"
	"time"

	"basetool/scanmeta"
)

// windowsRefreshLeveling is refreshLevelingUI of the Windows HDR page
// (levelmetadata.go) with the walk controls replaced by their values.
func windowsRefreshLeveling(c *Ctl, base [3]string) (checked, enabled bool, label string, sendSet, send bool, out [3]string) {
	out = base
	on := c.Light.cfg.UseLastLeveling
	checked, enabled = on, !c.Busy && !c.Working
	values := c.levelingValues()
	label = c.lastLevelingText(values)
	if !on {
		return
	}
	l := c.metadataLeveling()
	sendSet, send = true, !c.Busy && !c.Working && l.Matches(values)
	for i := range out {
		text := "-"
		if l.Matches(values) {
			text = fmt.Sprintf("%.5f", l.Base[i])
		}
		out[i] = text
	}
	return
}

func TestCameraCardLevelingSwitchMatchesWindows(t *testing.T) {
	isolate(t)
	h := newHost()
	c := New(h)
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	lev := &scanmeta.Leveling{At: at, Base: [3]float64{0.41234, 0.38765, 0.21111}, Values: [3]int{44, 67, 99}}
	cases := []struct {
		name   string
		lev    *scanmeta.Leveling
		on     bool
		values [3]int
		busy   bool
	}{
		{"no leveling, on", nil, true, [3]int{44, 67, 99}, false},
		{"no leveling, off", nil, false, [3]int{44, 67, 99}, false},
		{"matching, on", lev, true, [3]int{44, 67, 99}, false},
		{"matching, off", lev, false, [3]int{44, 67, 99}, false},
		{"other values, on", lev, true, [3]int{50, 50, 50}, false},
		{"matching, busy", lev, true, [3]int{44, 67, 99}, true},
	}
	for _, tc := range cases {
		c.Light.cfg.Leveling, c.Light.cfg.UseLastLeveling, c.Light.cfg.Values, c.Busy = tc.lev, tc.on, tc.values, tc.busy
		base := [3]string{"0.400", "0.380", "0.200"}
		wc, we, wl, wss, ws, wout := windowsRefreshLeveling(c, base)
		c.BaseR, c.BaseG, c.BaseB = base[0], base[1], base[2]
		r := c.RefreshLevelingUI()
		got := [3]string{c.BaseR, c.BaseG, c.BaseB}
		if r.On != wc || r.Enabled != we || r.Text != wl || r.SendSet != wss || r.SendOK != ws || got != wout {
			t.Errorf("%s: macOS %+v %v, Windows %v %v %q %v %v %v", tc.name, r, got, wc, we, wl, wss, ws, wout)
		}
	}
	c.Busy = false
	// The check box writes light_config (the Light page's file), as on Windows.
	c.SetUseLastLeveling(false)
	if c.UseLastLeveling() || loadLightConfig(0).UseLastLeveling {
		t.Error("switch off not stored")
	}
	c.SetUseLastLeveling(true)
	if !c.UseLastLeveling() || !loadLightConfig(0).UseLastLeveling {
		t.Error("switch on not stored")
	}
	if want := "Last leveling: R 0.41234 G 0.38765 B 0.21111 (after profile, 2026-11-26 20:00:00 CST (UTC+8))"; c.RefreshLevelingUI().Text != want {
		t.Errorf("label %q", c.RefreshLevelingUI().Text)
	}
}
