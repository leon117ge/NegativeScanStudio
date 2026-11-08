package main

import (
	"basetool/scanmeta"
	"os"
	"strings"
	"testing"
)

func TestLastLevelingOptionSurvivesRestart(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	if !loadLightConfig(0).UseLastLeveling {
		t.Fatal("new and legacy configs must default to last leveling")
	}
	values := [3]int{24, 73, 43}
	l := &scanmeta.Leveling{Base: [3]float64{.49844, .49639, .50210}, Values: values, At: "2026-11-07T22:57:46+08:00"}
	light := &LightUI{cfg: lightConfig{UseLastLeveling: true, Leveling: l, Values: values}}
	light.saveCfg()
	light.cfg = loadLightConfig(0)
	h := HDRUI{light: light}
	m := h.scanMetadata()
	if m.FilmBase == nil || *m.FilmBase != l.Base {
		t.Fatal("restart dropped last leveling")
	}
	text := h.lastLevelingText(values)
	if !strings.Contains(text, "R 0.49844 G 0.49639 B 0.50210") || !strings.Contains(text, "22:57:46 CST (UTC+8)") {
		t.Fatal(text)
	}
	light.cfg.UseLastLeveling = false
	light.saveCfg()
	light.cfg = loadLightConfig(0)
	if light.cfg.UseLastLeveling || h.scanMetadata().FilmBase != nil {
		t.Fatal("off setting lost or still writes base")
	}
	// A legacy config lacks the option and still defaults on, without inventing a base.
	if err := os.WriteFile(lightConfigFile(), []byte(`{"values":[24,73,43]}`), 0644); err != nil {
		t.Fatal(err)
	}
	light.cfg = loadLightConfig(0)
	if !light.cfg.UseLastLeveling || h.scanMetadata().FilmBase != nil {
		t.Fatal("legacy config default or unknown-base handling changed")
	}
}

func TestLevelingRoundDefaultMigration(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	if c := loadLightConfig(0); c.MaxIter != 10 {
		t.Fatal(c.MaxIter)
	}
	for _, tc := range []struct {
		json string
		want int
	}{{`{"max_iter":4}`, 10}, {`{"max_iter":3}`, 3}, {`{"max_iter":4,"solver_version":92}`, 4}} {
		if err := os.WriteFile(lightConfigFile(), []byte(tc.json), 0644); err != nil {
			t.Fatal(err)
		}
		if c := loadLightConfig(0); c.MaxIter != tc.want {
			t.Fatalf("%s -> %d", tc.json, c.MaxIter)
		}
	}
}
