package main

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestLevelingWritesDensityInSlidersAndDedicatedPreset(t *testing.T) {
	old := Config{Presets: []Preset{{"My roll", .1, .2, .3}}, DCTLPath: "example.dctl", AutoSync: true}
	b := [3]float64{.50123, .49876, .50001}
	next, err := withLevelingBase(old, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(old.Presets) != 1 || old.CurrentBase != nil {
		t.Fatal("mutated old config")
	}
	if len(next.Presets) != 2 || next.Presets[0] != old.Presets[0] || *next.CurrentBase != b || next.DCTLPath != old.DCTLPath || !next.AutoSync {
		t.Fatalf("wrong config: %+v", next)
	}
	text := buildDCTLWithBase(next.Presets, next.CurrentBase)
	for ch, v := range b {
		want := fmt.Sprintf("DEFINE_UI_PARAMS(base_%c, Film base %c, DCTLUI_SLIDER_FLOAT, %.8f,", "rgb"[ch], "RGB"[ch], v)
		if !strings.Contains(text, want) {
			t.Fatalf("missing slider %q", want)
		}
	}
	if !strings.Contains(text, "Light leveling") || !strings.Contains(text, "case 2: return make_float3(0.50123f, 0.49876f, 0.50001f)") {
		t.Fatal("preset not present")
	}
	if buildDCTLWithBase(old.Presets, nil) != buildDCTL(old.Presets) {
		t.Fatal("legacy export changed")
	}
	next, err = withLevelingBase(next, [3]float64{.6, .6, .6})
	if err != nil || len(next.Presets) != 2 {
		t.Fatal("repeated write duplicated preset")
	}
	for _, v := range []float64{0, math.NaN(), math.Inf(1), 17} {
		if _, err = withLevelingBase(old, [3]float64{v, .5, .5}); err == nil {
			t.Fatalf("accepted %v", v)
		}
	}
	full := Config{Presets: make([]Preset, maxPresets)}
	if _, err = withLevelingBase(full, b); err == nil {
		t.Fatal("expected full-list error")
	}
	full.Presets[0].Name = levelingPresetName
	if _, err = withLevelingBase(full, b); err != nil {
		t.Fatal("updating existing full-list preset should succeed", err)
	}
}
