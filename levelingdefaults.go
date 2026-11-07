package main

import (
	"fmt"
	"strings"
)

const levelingPresetName = "Light leveling"

// Keep a dedicated preset rather than overwriting a user's selected film stock.
func withLevelingBase(c Config, b [3]float64) (Config, error) {
	if !validLevelingBase(b) {
		return c, fmt.Errorf("film base must be finite linear values in 0.0001..16")
	}
	ps := append([]Preset(nil), c.Presets...)
	found := false
	for i := range ps {
		if strings.EqualFold(ps[i].Name, levelingPresetName) {
			ps[i] = Preset{levelingPresetName, b[0], b[1], b[2]}
			found = true
			break
		}
	}
	if !found {
		if len(ps) >= maxPresets {
			return c, fmt.Errorf("film-base preset list is full; free one slot first")
		}
		ps = append(ps, Preset{levelingPresetName, b[0], b[1], b[2]})
	}
	c.Presets, c.CurrentBase = ps, &b
	return c, nil
}

// Preserve all existing exports byte-for-byte unless a current base was saved.
func buildDCTLWithBase(ps []Preset, b *[3]float64) string {
	out := buildDCTL(ps)
	if b == nil || !validLevelingBase(*b) {
		return out
	}
	for ch, v := range b {
		prefix := fmt.Sprintf("DEFINE_UI_PARAMS(base_%c, Film base %c, DCTLUI_SLIDER_FLOAT, ", "rgb"[ch], "RGB"[ch])
		i := strings.Index(out, prefix)
		if i < 0 {
			continue
		}
		start := i + len(prefix)
		end := strings.Index(out[start:], ",")
		if end < 0 {
			continue
		}
		out = out[:start] + fmt.Sprintf("%.8f", v) + out[start+end:]
	}
	return out
}
