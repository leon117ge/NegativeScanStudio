// Package filmdb is the film-base database (v0.63): per film stock the base
// R/G/B (linear, fraction of raw full scale as module 1 / frame 0 measure
// it) and the conditions it was recorded at.
package filmdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileName is stored next to the config.
const FileName = "film_base_db.json"

// ApertureUnknown is stored when the lens reported no f-number (manual or
// adapted lens). 0 in old files still means the f/4 default.
const ApertureUnknown = -1.0

// Entry is one film stock.
type Entry struct {
	Name     string  `json:"name"`
	R        float64 `json:"r"`
	G        float64 `json:"g"`
	B        float64 `json:"b"`
	ISO      int     `json:"iso"`        // 0 = unknown (imported preset)
	Shutter  uint32  `json:"shutter_us"` // 0 = unknown; microseconds as set over PTP
	Aperture float64 `json:"aperture"`   // f-number as reported by the camera; -1 = unknown, 0 (old files) = 4
	Light    string  `json:"light,omitempty"`
	Date     string  `json:"date"`
	Samples  int     `json:"samples"`
	// Source: "record" (Record base), "frame0" (saved from a scan),
	// "preset" (module 1 preset), "manual".
	Source string `json:"source,omitempty"`
	// Crosstalk (v0.66) is a dye-crosstalk correction matrix (row-major,
	// density above base, rows summing to 1) calibrated from a ColorChecker
	// shot on this stock; nil = none. CrosstalkInfo says how it was made.
	Crosstalk     *[9]float64 `json:"crosstalk,omitempty"`
	CrosstalkInfo string      `json:"crosstalk_info,omitempty"`
	// RGBLight (v0.80) is the RGB Light Air setting found by auto film-base
	// leveling for this stock; nil = none. It does not change R/G/B above.
	RGBLight *RGBLight `json:"rgb_light,omitempty"`
}

// RGBLight is a levelled RGB light setting: brightness 1..100 per channel
// and the clear base measured with only that channel lit (fraction of raw
// full scale in the matching camera channel), at a fixed exposure.
type RGBLight struct {
	Values   [3]int     `json:"values"`
	Base     [3]float64 `json:"base"`
	Target   float64    `json:"target"`
	ISO      int        `json:"iso"`
	Shutter  uint32     `json:"shutter_us"`
	Camera   string     `json:"camera,omitempty"`
	Device   string     `json:"device,omitempty"` // light serial number
	Date     string     `json:"date"`
	Rounds   int        `json:"rounds,omitempty"`
	Levelled bool       `json:"levelled"` // all channels within tolerance
}

// Valid checks the brightness values.
func (l *RGBLight) Valid() error {
	if l == nil {
		return errors.New("no light setting")
	}
	for _, v := range l.Values {
		if v < 1 || v > 100 {
			return fmt.Errorf("light value %d is outside 1..100", v)
		}
	}
	return nil
}

// SetRGBLight stores a light setting on the existing entry named name;
// the entry's base R/G/B and conditions are not changed.
func (db *DB) SetRGBLight(name string, l RGBLight) (*Entry, error) {
	if err := l.Valid(); err != nil {
		return nil, err
	}
	if l.Date == "" {
		l.Date = time.Now().Format("2006-01-02 15:04")
	}
	e := db.Find(name)
	if e == nil {
		return nil, fmt.Errorf("no film stock %q in the film-base DB", name)
	}
	e.RGBLight = &l
	return e, nil
}

// DB is the whole file.
type DB struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
	// Hidden: module 1 presets deleted from the DB (not imported again).
	Hidden []string `json:"hidden_presets,omitempty"`
	path   string
}

// Path returns the DB path in dir.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// Load reads the DB (a missing file is an empty DB).
func Load(path string) (*DB, error) {
	db := &DB{Version: 1, path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return db, nil
	}
	if err != nil {
		return db, err
	}
	if err := json.Unmarshal(b, db); err != nil {
		return &DB{Version: 1, path: path}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	db.path = path
	for i := range db.Entries {
		if db.Entries[i].Aperture == 0 {
			db.Entries[i].Aperture = 4
		}
		if db.Entries[i].Samples < 1 {
			db.Entries[i].Samples = 1
		}
	}
	db.sort()
	return db, nil
}

// Save writes the DB atomically.
func (db *DB) Save() error {
	if db.path == "" {
		return errors.New("film base DB has no path")
	}
	db.Version = 1
	db.sort()
	b, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(db.path), 0o755)
	tmp := db.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, db.path)
}

func (db *DB) sort() {
	sort.SliceStable(db.Entries, func(i, j int) bool {
		return strings.ToLower(db.Entries[i].Name) < strings.ToLower(db.Entries[j].Name)
	})
}

// Names lists the stock names.
func (db *DB) Names() []string {
	var n []string
	for _, e := range db.Entries {
		n = append(n, e.Name)
	}
	return n
}

// Find returns the entry named name (case-insensitive), or nil.
func (db *DB) Find(name string) *Entry {
	for i := range db.Entries {
		if strings.EqualFold(db.Entries[i].Name, name) {
			return &db.Entries[i]
		}
	}
	return nil
}

// Put adds a new entry or updates an existing one. With average, the base
// values are averaged with the stored ones weighted by sample count (in log
// space, i.e. a geometric mean); the conditions must then match. Without
// average the entry is replaced (samples = 1).
func (db *DB) Put(e Entry, average bool) (*Entry, error) {
	e.Name = strings.TrimSpace(e.Name)
	if e.Name == "" {
		return nil, errors.New("film stock name is empty")
	}
	if !(e.R > 0 && e.G > 0 && e.B > 0) {
		return nil, errors.New("base values must be positive")
	}
	if e.Aperture == 0 {
		e.Aperture = 4
	}
	if e.Date == "" {
		e.Date = time.Now().Format("2006-01-02 15:04")
	}
	if e.Samples < 1 {
		e.Samples = 1
	}
	old := db.Find(e.Name)
	if old == nil {
		db.Entries = append(db.Entries, e)
		db.sort()
		return db.Find(e.Name), nil
	}
	if average {
		if old.ISO != 0 && (old.ISO != e.ISO || old.Shutter != e.Shutter) {
			return nil, fmt.Errorf("cannot average: %q was recorded at %s, this reading at %s", old.Name, Cond(*old), Cond(e))
		}
		n0, n1 := float64(old.Samples), float64(e.Samples)
		avg := func(a, b float64) float64 { return math.Exp2((n0*math.Log2(a) + n1*math.Log2(b)) / (n0 + n1)) }
		e.R, e.G, e.B = avg(old.R, e.R), avg(old.G, e.G), avg(old.B, e.B)
		e.Samples = old.Samples + e.Samples
		if e.Light == "" {
			e.Light = old.Light
		}
	}
	e.Name = old.Name
	if e.Crosstalk == nil { // a new base reading keeps the calibrated matrix
		e.Crosstalk, e.CrosstalkInfo = old.Crosstalk, old.CrosstalkInfo
	}
	if e.RGBLight == nil { // ... and the levelled light setting
		e.RGBLight = old.RGBLight
	}
	*old = e
	return old, nil
}

// SetCrosstalk stores a calibrated crosstalk matrix on the entry named
// name (nil m clears it).
func (db *DB) SetCrosstalk(name string, m *[9]float64, info string) error {
	e := db.Find(name)
	if e == nil {
		return fmt.Errorf("no film stock %q", name)
	}
	if m != nil {
		for _, v := range m {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return errors.New("crosstalk matrix has invalid values")
			}
		}
		c := *m
		m = &c
	} else {
		info = ""
	}
	e.Crosstalk, e.CrosstalkInfo = m, info
	return nil
}

// Rename renames an entry.
func (db *DB) Rename(from, to string) error {
	to = strings.TrimSpace(to)
	e := db.Find(from)
	if e == nil {
		return fmt.Errorf("no film stock %q", from)
	}
	if to == "" {
		return errors.New("new name is empty")
	}
	if o := db.Find(to); o != nil && o != e {
		return fmt.Errorf("%q already exists", to)
	}
	e.Name = to
	db.sort()
	return nil
}

// Delete removes an entry.
func (db *DB) Delete(name string) bool {
	for i := range db.Entries {
		if strings.EqualFold(db.Entries[i].Name, name) {
			if db.Entries[i].Source == "preset" {
				db.Hidden = append(db.Hidden, db.Entries[i].Name)
			}
			db.Entries = append(db.Entries[:i], db.Entries[i+1:]...)
			return true
		}
	}
	return false
}

// Preset is a module 1 preset (name + base RGB).
type Preset struct {
	Name    string
	R, G, B float64
}

// Import adds module 1 presets that are not in the DB yet (conditions
// unknown). It returns how many were added.
func (db *DB) Import(ps []Preset) int {
	n := 0
	for _, p := range ps {
		if db.Find(p.Name) != nil || !(p.R > 0 && p.G > 0 && p.B > 0) || db.hidden(p.Name) {
			continue
		}
		db.Entries = append(db.Entries, Entry{Name: p.Name, R: p.R, G: p.G, B: p.B, Aperture: 4, Samples: 1, Source: "preset"})
		n++
	}
	db.sort()
	return n
}

// Cond formats the recording conditions.
func Cond(e Entry) string {
	if e.ISO == 0 {
		return "unknown conditions (module 1 preset)"
	}
	if e.Aperture < 0 {
		return fmt.Sprintf("ISO %d, %s, aperture unknown", e.ISO, shutterLabel(e.Shutter))
	}
	s := fmt.Sprintf("ISO %d, %s, f/%g", e.ISO, shutterLabel(e.Shutter), e.Aperture)
	return s
}

func shutterLabel(us uint32) string {
	switch {
	case us == 0:
		return "shutter ?"
	case us == 15625:
		return "1/60 s"
	case us == 7812: // X-T3 1/125 s (v0.81 base shutter)
		return "1/125 s"
	case us < 1000000:
		return fmt.Sprintf("1/%.0f s", 1e6/float64(us))
	}
	return fmt.Sprintf("%.1f s", float64(us)/1e6)
}

// Comparison of a frame-0 reading with an entry.
type Comparison struct {
	DevEV    [3]float64 // log2(measured / stored) per channel
	MaxDev   float64    // largest |DevEV|
	OK       bool
	Warnings []string
}

// Compare checks a measured base (same units) at iso / shutter against e;
// tol is the per-channel limit in EV.
func Compare(e Entry, rgb [3]float64, iso int, shutter uint32, tol float64) Comparison {
	var c Comparison
	ref := [3]float64{e.R, e.G, e.B}
	ch := [3]string{"R", "G", "B"}
	for i := 0; i < 3; i++ {
		if rgb[i] <= 0 || ref[i] <= 0 {
			c.DevEV[i] = math.Inf(1)
		} else {
			c.DevEV[i] = math.Log2(rgb[i] / ref[i])
		}
		if a := math.Abs(c.DevEV[i]); a > c.MaxDev {
			c.MaxDev = a
		}
	}
	for i := 0; i < 3; i++ {
		if math.Abs(c.DevEV[i]) > tol {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s differs by %+.2f EV (limit %.2f)", ch[i], c.DevEV[i], tol))
		}
	}
	if e.ISO == 0 {
		c.Warnings = append(c.Warnings, "the DB entry has no recorded ISO/shutter (module 1 preset) - values may not be comparable")
	} else {
		if e.ISO != iso {
			c.Warnings = append(c.Warnings, fmt.Sprintf("ISO mismatch: frame 0 ISO %d, DB ISO %d", iso, e.ISO))
		}
		if e.Shutter != shutter {
			c.Warnings = append(c.Warnings, fmt.Sprintf("shutter mismatch: frame 0 %s, DB %s", shutterLabel(shutter), shutterLabel(e.Shutter)))
		}
	}
	c.OK = len(c.Warnings) == 0
	return c
}

// String is the one-line message.
func (c Comparison) String() string {
	d := fmt.Sprintf("R %+.2f G %+.2f B %+.2f EV", c.DevEV[0], c.DevEV[1], c.DevEV[2])
	if c.OK {
		return "Film base OK vs DB: " + d
	}
	return "Film base WARNING vs DB (" + d + "): " + strings.Join(c.Warnings, "; ")
}

func (db *DB) hidden(name string) bool {
	for _, h := range db.Hidden {
		if strings.EqualFold(h, name) {
			return true
		}
	}
	return false
}
