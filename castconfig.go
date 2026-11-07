package main

import (
	"basetool/colorsci"
	"basetool/filmdb"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const castModel = "5219 H-1-5219 rev3-26 / Planck visible 10nm / nominal ECN-2 (approximate)"

func castConfigPath() string { return filepath.Join(configDir(), "colour_cast.json") }
func loadCastConfig() (filmdb.ColourCast, error) {
	c := filmdb.ColourCast{Kelvin: 5500, Model: castModel}
	b, e := os.ReadFile(castConfigPath())
	if errors.Is(e, os.ErrNotExist) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	if _, e = colorsci.Vision3Illuminant(c.Kelvin); e != nil {
		return c, e
	}
	_, e = colorsci.ApplyCastDCTL(dctlTemplate, c.Enabled, c.Offset)
	if e == nil && c.Model != castModel {
		e = fmt.Errorf("unknown colour-cast model; calculate and write it again")
	}
	return c, e
}
func saveCastConfig(c filmdb.ColourCast) error {
	if _, e := colorsci.ApplyCastDCTL(dctlTemplate, c.Enabled, c.Offset); e != nil {
		return e
	}
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(configDir(), 0755); e != nil {
		return e
	}
	p := castConfigPath()
	if e = os.WriteFile(p+".tmp", append(b, '\n'), 0644); e != nil {
		return e
	}
	return os.Rename(p+".tmp", p)
}
func writeDensityDCTL(path string, ps []Preset, base *[3]float64) error {
	c, e := loadCastConfig()
	if e != nil {
		return fmt.Errorf("colour-cast settings: %w", e)
	}
	body, e := colorsci.ApplyCastDCTL(buildDCTLWithBase(ps, base), c.Enabled, c.Offset)
	if e != nil {
		return e
	}
	body = fmt.Sprintf("// Colour cast: enabled=%v; 500T @ %.0fK; %s\n// DCTL-only additive density: R %+.6f G %+.6f B %+.6f; film-base values and light unchanged.\n", c.Enabled, c.Kelvin, c.Model, c.Offset[0], c.Offset[1], c.Offset[2]) + body
	return os.WriteFile(path, []byte(body), 0644)
}
