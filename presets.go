package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Preset struct {
	Name    string
	R, G, B float64
}

const maxPresets = 60

func defaultPresets() []Preset {
	return []Preset{{"Session 1", 0.1933, 0.1883, 0.0593}}
}

func cleanName(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 40 {
		return "", false
	}
	for _, c := range s {
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == ' ' || c == '-' || c == '_' || c == '.'
		if !ok {
			return "", false
		}
	}
	return s, true
}

func configDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "NegativeScanStudio")
}

// legacyConfigDir is the settings folder of Film Base Meter (<= v0.67).
func legacyConfigDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "FilmBaseMeter")
}

// legacyFiles are the settings / DB files carried over from Film Base Meter.
var legacyFiles = []string{"config.txt", "wb_config.txt", "hdr_config.txt", "raw_config.txt", "crop_config.txt", "film_base_db.json"}

// migrateLegacyConfig copies Film Base Meter's settings and film base DB into
// the NegativeScan Studio folder. A file is copied only when the new folder does
// not have it yet; the old folder is only read, never changed or deleted.
// Returns the names copied.
func migrateLegacyConfig() []string {
	old, nw := legacyConfigDir(), configDir()
	if old == nw {
		return nil
	}
	var done []string
	for _, f := range legacyFiles {
		dst := filepath.Join(nw, f)
		if _, err := os.Stat(dst); err == nil || !os.IsNotExist(err) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(old, f))
		if err != nil {
			continue
		}
		if os.MkdirAll(nw, 0o755) != nil {
			return done
		}
		tmp := dst + ".part"
		if os.WriteFile(tmp, b, 0o644) != nil {
			os.Remove(tmp)
			continue
		}
		if os.Rename(tmp, dst) != nil {
			os.Remove(tmp)
			continue
		}
		done = append(done, f)
	}
	if len(done) > 0 {
		note := fmt.Sprintf("%s: copied from %s (%s) on %s: %s\n", appVersion, old, legacyAppName, time.Now().Format("2006-01-02 15:04:05"), strings.Join(done, ", "))
		if f, err := os.OpenFile(filepath.Join(nw, "migrated_from_FilmBaseMeter.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			f.WriteString(note)
			f.Close()
		}
	}
	return done
}

type Config struct {
	Presets []Preset
}

func parsePresetLine(line string) (Preset, bool) {
	f := strings.Split(strings.TrimSpace(line), "|")
	if len(f) != 5 || f[0] != "// FBM_PRESET" {
		return Preset{}, false
	}
	name, ok := cleanName(f[1])
	if !ok {
		return Preset{}, false
	}
	r, e1 := strconv.ParseFloat(f[2], 64)
	g, e2 := strconv.ParseFloat(f[3], 64)
	b, e3 := strconv.ParseFloat(f[4], 64)
	if e1 != nil || e2 != nil || e3 != nil || r <= 0 || g <= 0 || b <= 0 {
		return Preset{}, false
	}
	return Preset{name, r, g, b}, true
}

// parsePresets reads the legacy machine-readable film-base settings.
func parsePresets(text string) []Preset {
	var out []Preset
	for _, ln := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		if p, ok := parsePresetLine(ln); ok && len(out) < maxPresets {
			out = append(out, p)
		}
	}
	return out
}

func loadConfig() Config {
	c := Config{Presets: defaultPresets()}
	b, err := os.ReadFile(filepath.Join(configDir(), "config.txt"))
	if err != nil {
		return c
	}
	txt := strings.ReplaceAll(string(b), "\r", "")
	if ps := parsePresets(txt); len(ps) > 0 {
		c.Presets = ps
	}
	return c
}
