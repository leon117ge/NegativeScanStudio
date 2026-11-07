package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lxn/walk"
)

//go:embed template.dctl
var dctlTemplate string

type Preset struct {
	Name    string
	R, G, B float64
}

type PresetModel struct {
	walk.TableModelBase
	items []Preset
}

func (m *PresetModel) RowCount() int { return len(m.items) }
func (m *PresetModel) Value(row, col int) interface{} {
	p := m.items[row]
	switch col {
	case 0:
		return fmt.Sprintf("%d", row+1)
	case 1:
		return p.Name
	case 2:
		return fmt.Sprintf("%.5f", p.R)
	case 3:
		return fmt.Sprintf("%.5f", p.G)
	case 4:
		return fmt.Sprintf("%.5f", p.B)
	}
	return ""
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
	Presets     []Preset
	DCTLPath    string
	AutoSync    bool
	CurrentBase *[3]float64 // default Density_In sliders from Light leveling
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

// parsePresets reads the machine-readable preset lines of a DCTL file produced by this program.
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
	for _, ln := range strings.Split(txt, "\n") {
		if b, ok := parseCurrentBase(ln); ok {
			c.CurrentBase = &b
		}
		if strings.HasPrefix(ln, "DCTL_PATH=") {
			c.DCTLPath = strings.TrimPrefix(ln, "DCTL_PATH=")
		}
		if ln == "AUTOSYNC=1" {
			c.AutoSync = true
		}
	}
	return c
}

func saveConfig(c Config) error {
	if err := os.MkdirAll(configDir(), 0755); err != nil {
		return err
	}
	var sb strings.Builder
	for _, p := range c.Presets {
		sb.WriteString(fmt.Sprintf("// FBM_PRESET|%s|%.5f|%.5f|%.5f\n", p.Name, p.R, p.G, p.B))
	}
	if c.CurrentBase != nil {
		sb.WriteString(currentBaseLine(*c.CurrentBase) + "\n")
	}
	sb.WriteString("DCTL_PATH=" + c.DCTLPath + "\n")
	if c.AutoSync {
		sb.WriteString("AUTOSYNC=1\n")
	}
	return os.WriteFile(filepath.Join(configDir(), "config.txt"), []byte(sb.String()), 0644)
}

func buildDCTL(ps []Preset) string {
	var h strings.Builder
	h.WriteString("// ===== FILM BASE PRESETS =====\n")
	h.WriteString("// This list is written by Film Base Meter. Edit it there (or add a preset and a matching case by hand).\n")
	h.WriteString("// Resolve can not move sliders from a dropdown, so choosing a preset overrides the three Film base sliders.\n")
	h.WriteString("// Choose Custom to use the sliders again.\n")
	for _, p := range ps {
		h.WriteString(fmt.Sprintf("// FBM_PRESET|%s|%.5f|%.5f|%.5f\n", p.Name, p.R, p.G, p.B))
	}
	ids := "P_CUSTOM"
	labels := "Custom - use sliders below"
	for i, p := range ps {
		ids += fmt.Sprintf(", P_%d", i+1)
		labels += ", " + p.Name
	}
	h.WriteString(fmt.Sprintf("DEFINE_UI_PARAMS(preset, Film base preset, DCTLUI_COMBO_BOX, 0, {%s}, {%s})", ids, labels))

	var f strings.Builder
	f.WriteString("__DEVICE__ float3 preset_base(int id, float3 slider)\n{\n    switch (id)\n    {\n")
	for i, p := range ps {
		f.WriteString(fmt.Sprintf("        case %d: return make_float3(%.5ff, %.5ff, %.5ff);\n", i+1, p.R, p.G, p.B))
	}
	f.WriteString("        default: return slider;\n    }\n}")

	out := strings.Replace(dctlTemplate, "@@PRESET_HEADER@@", h.String(), 1)
	out = strings.Replace(out, "@@PRESET_FUNC@@", f.String(), 1)
	return out
}

// importMode asks how imported presets join a non-empty list:
// 1 = replace the list, 2 = merge by name, 0 = cancel.
func importMode(owner walk.Form, have, got int, src string) int {
	if have == 0 {
		return 1
	}
	switch walk.MsgBox(owner, "Import presets",
		fmt.Sprintf("%s has %d preset(s); the list has %d now.\n\n"+
			"Yes = replace the list with the imported presets\n"+
			"No = merge: same name updates the preset, new names are added at the end\n"+
			"Cancel = keep the list as it is", src, got, have),
		walk.MsgBoxYesNoCancel|walk.MsgBoxIconQuestion) {
	case walk.DlgCmdYes:
		return 1
	case walk.DlgCmdNo:
		return 2
	}
	return 0
}

// mergeByName updates presets whose name matches (case-insensitive) and
// appends the rest up to max. It returns the list and the counts.
func mergeByName[T any](cur, add []T, name func(T) string, max int) (out []T, updated, added, skipped int) {
	out = append([]T{}, cur...)
	for _, p := range add {
		hit := false
		for i := range out {
			if strings.EqualFold(name(out[i]), name(p)) {
				out[i], hit = p, true
				updated++
				break
			}
		}
		if hit {
			continue
		}
		if len(out) >= max {
			skipped++
			continue
		}
		out = append(out, p)
		added++
	}
	return
}

func importSummary(mode, n, upd, add, skip int) string {
	if mode == 1 {
		return fmt.Sprintf("Imported %d preset(s) (list replaced).", n)
	}
	s := fmt.Sprintf("Merged %d preset(s): %d updated, %d added", n, upd, add)
	if skip > 0 {
		s += fmt.Sprintf(", %d not added (list full, max %d)", skip, maxPresets)
	}
	return s + "."
}
