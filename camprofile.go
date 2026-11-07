package main

// Camera (scanner input) profiles from Chart calibration (v0.84).
// Stored in camera_profiles.json next to config.txt.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"basetool/colorsci"
)

type CamProfile struct {
	Name    string        `json:"name"`
	M       colorsci.Mat3 `json:"matrix"`         // grey-preserving camera -> camera
	Neutral colorsci.Mat3 `json:"neutral_matrix"` // grey -> equal RGB variant (neutral variant)
	Info    string        `json:"info"`
}

type CamSettings struct {
	Profiles []CamProfile `json:"profiles"`
	Active   string       `json:"active"`
	Enabled  bool         `json:"enabled"`
	Strength float64      `json:"strength"`
	LastRef  string       `json:"last_reference"`
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
