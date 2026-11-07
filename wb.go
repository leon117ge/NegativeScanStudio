package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type WBPreset struct {
	Name    string
	R, G, B float64 // neutral gray point, density codes
}

const maxWB = 60

func parseWBLine(line string) (WBPreset, bool) {
	f := strings.Split(strings.TrimSpace(line), "|")
	if len(f) != 5 || f[0] != "// FBM_WB" {
		return WBPreset{}, false
	}
	name, ok := cleanName(f[1])
	if !ok {
		return WBPreset{}, false
	}
	r, e1 := strconv.ParseFloat(f[2], 64)
	g, e2 := strconv.ParseFloat(f[3], 64)
	b, e3 := strconv.ParseFloat(f[4], 64)
	if e1 != nil || e2 != nil || e3 != nil || r < 50 || g < 50 || b < 50 || r > 1100 || g > 1100 || b > 1100 {
		return WBPreset{}, false
	}
	return WBPreset{name, r, g, b}, true
}

func parseWBPresets(text string) []WBPreset {
	var out []WBPreset
	for _, ln := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		if p, ok := parseWBLine(ln); ok && len(out) < maxWB {
			out = append(out, p)
		}
	}
	return out
}

type WBConfig struct {
	Presets []WBPreset
}

func wbConfigFile() string { return filepath.Join(configDir(), "wb_config.txt") }

func loadWBConfig() WBConfig {
	c := WBConfig{}
	b, err := os.ReadFile(wbConfigFile())
	if err != nil {
		return c
	}
	txt := strings.ReplaceAll(string(b), "\r", "")
	c.Presets = parseWBPresets(txt)
	return c
}
