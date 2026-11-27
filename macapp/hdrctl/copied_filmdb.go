package hdrctl

// Declarations copied verbatim from the Windows film-base DB UI (hdrdb.go)
// and the Film base presets it imports (presets.go). copied_test.go fails
// when one of them drifts.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"basetool/camera"
	"basetool/filmdb"
)

// ---------- hdrdb.go ----------

const stockNone = "None"

// frame0Base is the clear base of the last captured frame 0.
type frame0Base struct {
	RGB  [3]float64
	N    int
	Exp  camera.Exposure
	Pure bool   // frame 0 was a usable pure-base exposure (no clipping, enough base)
	Why  string // why not
	Src  string
	Fn   float64 // f-number the camera reported at capture (0 = unknown)
}

func rgbText(v [3]float64) string { return fmt.Sprintf("R %.4f  G %.4f  B %.4f", v[0], v[1], v[2]) }

func entryText(e *filmdb.Entry) string {
	return fmt.Sprintf("%s: %s, %s, %d sample(s), %s", e.Name, rgbText([3]float64{e.R, e.G, e.B}), filmdb.Cond(*e), e.Samples, e.Date)
}

// compareText checks a frame-0 base against the selected stock.
func compareText(ent *filmdb.Entry, rgb [3]float64, e camera.Exposure, tol float64) (string, bool) {
	if ent == nil {
		return "", true
	}
	c := filmdb.Compare(*ent, rgb, e.ISO, e.Shutter, tol)
	return ent.Name + ": " + c.String(), c.OK
}

// fnText formats the f-number the camera reported; 0 means the lens reports
// none (manual or adapted lens).
func fnText(f float64) string {
	if f > 0 {
		return fmt.Sprintf("f/%g", f)
	}
	return "aperture unknown (lens reports none)"
}

// ---------- presets.go ----------

type Preset struct {
	Name    string
	R, G, B float64
}

const maxPresets = 60

func defaultPresets() []Preset {
	return []Preset{{"Session 1", 0.1933, 0.1883, 0.0593}}
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
