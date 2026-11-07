package main

// Camera (scanner input) profiles from Chart calibration (v0.84).
// Stored in camera_profiles.json next to config.txt.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"basetool/colorsci"
	"basetool/hdrscan"
)

type CamProfile struct {
	Name    string        `json:"name"`
	M       colorsci.Mat3 `json:"matrix"`          // grey-preserving camera -> camera
	Neutral colorsci.Mat3 `json:"neutral_matrix"` // grey -> equal RGB variant (DCTL option)
	Info    string        `json:"info"`
}

type CamSettings struct {
	Profiles []CamProfile `json:"profiles"`
	Active   string       `json:"active"`
	Enabled  bool         `json:"enabled"`
	Strength float64      `json:"strength"`
	LastRef  string       `json:"last_reference"`
	LastDir  string       `json:"last_dctl_dir"`
}

var (
	camMu  sync.Mutex
	camSet *CamSettings
	// lastTriOut is the newest trichrome TIFF written in this session ("Use last scan").
	lastTriOut string
)

func camPath() string { return filepath.Join(configDir(), "camera_profiles.json") }

// camSettings returns a copy of the stored settings.
func camSettings() CamSettings {
	camMu.Lock()
	defer camMu.Unlock()
	if camSet == nil {
		s := &CamSettings{Strength: 1}
		if b, err := os.ReadFile(camPath()); err == nil {
			json.Unmarshal(b, s)
		}
		if s.Strength < 0 || s.Strength > 1 {
			s.Strength = 1
		}
		camSet = s
	}
	c := *camSet
	c.Profiles = append([]CamProfile(nil), camSet.Profiles...)
	return c
}

func saveCamSettings(s CamSettings) error {
	camMu.Lock()
	defer camMu.Unlock()
	cp := s
	camSet = &cp
	os.MkdirAll(configDir(), 0755)
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(camPath(), b, 0644)
}

func (s CamSettings) find(name string) *CamProfile {
	for i := range s.Profiles {
		if s.Profiles[i].Name == name {
			return &s.Profiles[i]
		}
	}
	return nil
}

// activeCam returns the enabled profile and strength, or nil.
func activeCam() (*CamProfile, float64) {
	s := camSettings()
	if !s.Enabled {
		return nil, 0
	}
	p := s.find(s.Active)
	if p == nil {
		return nil, 0
	}
	return p, s.Strength
}

// applyCamToBase applies the active camera profile to the automatically
// measured trichrome film base (the matrix acts on the median clear-base
// colour, as the CameraCal DCTL would on those pixels) and returns a log line.
func applyCamToBase(r *hdrscan.TriResult) string {
	lastTriOut = r.Out
	p, k := activeCam()
	if p == nil || r.BaseErr != "" || r.BaseN == 0 {
		return ""
	}
	before := r.Base
	r.Base = colorsci.ApplyCam(p.M, k, before)
	return fmt.Sprintf("Camera profile \"%s\" (strength %.2f) applied before the film-base measurement: R %.4f G %.4f B %.4f -> R %.4f G %.4f B %.4f. Put CameraCal_Trichrome.dctl (same profile and strength) before FilmScan_Density_In in Resolve.",
		p.Name, k, before[0], before[1], before[2], r.Base[0], r.Base[1], r.Base[2])
}
