// Package scanconfig (v0.83) stores named scan configurations (presets):
// the levelled RGB light, the camera ISO / base shutter and the film stock
// info. It replaces the film-stock block on the Light page.
package scanconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileName is the preset store in the app config folder.
const FileName = "scan_presets.json"

// Light is the light part of a config.
type Light struct {
	Values [3]int  `json:"values"` // brightness 0..100 per channel (R, G, B)
	On     [3]bool `json:"on"`
}

// Film is the film / stock part of a config.
type Film struct {
	Stock     string     `json:"stock"`
	Notes     string     `json:"notes,omitempty"`
	Base      [3]float64 `json:"base"`                 // clear-base white-light meter (R/G/B), 0 = unknown
	LightBase [3]float64 `json:"light_base,omitempty"` // per-channel leveling base (fraction of full scale)
	Target    float64    `json:"target,omitempty"`
	Levelled  bool       `json:"levelled"`
	// v0.85: camera profile the light was levelled for (empty = none) and
	// the film base measured in the trichrome TIFF after the matrix.
	CamProfile  string     `json:"cam_profile,omitempty"`
	CamStrength float64    `json:"cam_strength,omitempty"`
	TIFFBase    [3]float64 `json:"tiff_base,omitempty"`
}

// Config is one named preset.
type Config struct {
	Name      string `json:"name"`
	Light     Light  `json:"light"`
	ISO       int    `json:"iso"`
	ShutterUS uint32 `json:"shutter_us"` // 15625 (1/60) or 7812 (1/125), PTP values
	Film      Film   `json:"film"`
	Source    string `json:"source,omitempty"` // "saved", "migrated from film-base DB", "file"
	Date      string `json:"date"`
}

// Shutters are the allowed base shutters (µs) - set by the caller.
var Shutters = []uint32{}

// Valid checks a config before it is applied or saved.
func (c *Config) Valid() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("the preset needs a name")
	}
	for i, v := range c.Light.Values {
		if v < 0 || v > 100 {
			return fmt.Errorf("light value %d for %c is outside 0..100", v, "RGB"[i])
		}
	}
	if c.ISO < 0 {
		return errors.New("invalid ISO")
	}
	if len(Shutters) > 0 && c.ShutterUS != 0 {
		ok := false
		for _, s := range Shutters {
			ok = ok || s == c.ShutterUS
		}
		if !ok {
			return fmt.Errorf("base shutter %d µs is not one of the allowed base shutters", c.ShutterUS)
		}
	}
	return nil
}

// Store is the preset file.
type Store struct {
	path    string
	Presets []Config `json:"presets"`
}

// Path is the store file in dir.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// Load reads the store; a missing file gives an empty store (Exists false).
func Load(path string) (*Store, bool, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, false, nil
	}
	if err != nil {
		return s, false, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return s, true, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	s.sort()
	return s, true, nil
}

// Save writes the store atomically.
func (s *Store) Save() error {
	if s.path == "" {
		return errors.New("store has no path")
	}
	s.sort()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) sort() {
	sort.SliceStable(s.Presets, func(i, j int) bool {
		return strings.ToLower(s.Presets[i].Name) < strings.ToLower(s.Presets[j].Name)
	})
}

// Names lists the preset names.
func (s *Store) Names() []string {
	n := make([]string, len(s.Presets))
	for i, p := range s.Presets {
		n[i] = p.Name
	}
	return n
}

// Find returns the preset named name (case-insensitive) or nil.
func (s *Store) Find(name string) *Config {
	for i := range s.Presets {
		if strings.EqualFold(s.Presets[i].Name, strings.TrimSpace(name)) {
			return &s.Presets[i]
		}
	}
	return nil
}

// Put adds or replaces a preset (by name). It reports whether one was replaced.
func (s *Store) Put(c Config) (bool, error) {
	c.Name = strings.TrimSpace(c.Name)
	if err := c.Valid(); err != nil {
		return false, err
	}
	if c.Date == "" {
		c.Date = time.Now().Format("2006-01-02 15:04")
	}
	if p := s.Find(c.Name); p != nil {
		*p = c
		return true, nil
	}
	s.Presets = append(s.Presets, c)
	s.sort()
	return false, nil
}

// Delete removes a preset.
func (s *Store) Delete(name string) bool {
	for i := range s.Presets {
		if strings.EqualFold(s.Presets[i].Name, name) {
			s.Presets = append(s.Presets[:i], s.Presets[i+1:]...)
			return true
		}
	}
	return false
}

// fileDoc is a single config exported to a file.
type fileDoc struct {
	Kind   string `json:"kind"`
	Config Config `json:"config"`
}

const fileKind = "NegativeScanStudio scan config"

// WriteFile saves one config to path ("Save as file").
func WriteFile(path string, c Config) error {
	if err := c.Valid(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(fileDoc{Kind: fileKind, Config: c}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ReadFile loads one config from path ("Load from file"). A bare Config
// object is accepted too.
func ReadFile(path string) (Config, error) {
	var d fileDoc
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return Config{}, fmt.Errorf("%s is not a scan config: %w", filepath.Base(path), err)
	}
	c := d.Config
	if d.Kind == "" {
		if err := json.Unmarshal(b, &c); err != nil {
			return Config{}, err
		}
	}
	if strings.TrimSpace(c.Name) == "" {
		c.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if err := c.Valid(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Legacy is the part of an old film-base DB entry that migrates.
type Legacy struct {
	Name      string
	Base      [3]float64
	Values    [3]int
	LightBase [3]float64
	Target    float64
	ISO       int
	ShutterUS uint32
	Levelled  bool
	Date      string
}

// Migrate adds a preset for every legacy entry whose name is not taken yet
// and returns how many were added.
func (s *Store) Migrate(ls []Legacy) int {
	n := 0
	for _, l := range ls {
		if s.Find(l.Name) != nil {
			continue
		}
		c := Config{Name: l.Name, Light: Light{Values: l.Values, On: [3]bool{true, true, true}}, ISO: l.ISO, ShutterUS: l.ShutterUS,
			Film:   Film{Stock: l.Name, Base: l.Base, LightBase: l.LightBase, Target: l.Target, Levelled: l.Levelled},
			Source: "migrated from film-base DB", Date: l.Date}
		if len(Shutters) > 0 && c.ShutterUS != 0 && c.Valid() != nil {
			c.ShutterUS = 0 // unknown base shutter: keep the current selection on load
		}
		if _, err := s.Put(c); err == nil {
			n++
		}
	}
	return n
}
