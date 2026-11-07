// Package scanmeta defines versioned, Resolve-independent scan provenance.
package scanmeta

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf16"
)

const Schema = "nss.scan/1"

type Metadata struct {
	Schema         string      `json:"schema"`
	Software       string      `json:"software"`
	Created        string      `json:"created"`
	Roll           string      `json:"roll,omitempty"`
	Mode           string      `json:"mode,omitempty"`
	LightModel     string      `json:"light_model,omitempty"`
	LightRGB       *[3]int     `json:"light_rgb,omitempty"` // device drive values; NOT linear film base
	FilmBase       *[3]float64 `json:"film_base,omitempty"` // normalized linear reference; FilmBaseSpace identifies its coordinates
	FilmBaseSpace  string      `json:"film_base_space,omitempty"`
	BaseSource     string      `json:"base_source,omitempty"`
	Encoding       string      `json:"encoding"`
	CameraMatrix   *[9]float64 `json:"camera_matrix,omitempty"`
	CameraStrength float64     `json:"camera_strength"`
	CameraApplied  bool        `json:"camera_applied"`
	CameraProfile  string      `json:"camera_profile,omitempty"`
	Processing     string      `json:"processing,omitempty"`
}

func (m Metadata) JSON(software string) (string, error) {
	m.Schema = Schema
	m.Software = software
	if m.Created == "" {
		m.Created = time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format(time.RFC3339)
	}
	if m.Encoding == "" {
		m.Encoding = "linear"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	var ascii strings.Builder
	for _, r := range string(b) {
		if r < 128 {
			ascii.WriteRune(r)
		} else if r <= 0xffff {
			fmt.Fprintf(&ascii, "\\u%04x", r)
		} else {
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&ascii, "\\u%04x\\u%04x", hi, lo)
		}
	}
	return ascii.String(), nil
}

// Leveling persists the accepted Light-page result, not a later image measurement.
type Leveling struct {
	At     string     `json:"at,omitempty"`
	Base   [3]float64 `json:"base"`
	Values [3]int     `json:"values"`
}

const AfterProfile = "after_profile"
const LevelingSource = "light leveling (after profile)"

func validBase(b [3]float64) bool {
	for _, v := range b {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < .0001 || v > 16 {
			return false
		}
	}
	return true
}
func (l *Leveling) Matches(values [3]int) bool {
	return l != nil && l.Values == values && validBase(l.Base)
}

// WithLeveling snapshots the saved reference so later GUI changes cannot affect a capture.
func (m Metadata) WithLeveling(l *Leveling, values [3]int) Metadata {
	m.FilmBase, m.FilmBaseSpace, m.BaseSource = nil, "", ""
	if l.Matches(values) {
		b := l.Base
		m.FilmBase = &b
		m.FilmBaseSpace = AfterProfile
		m.BaseSource = LevelingSource
	}
	return m
}

// ForCapture never substitutes a measurement of scene content for a leveling result.
func (m Metadata) ForCapture() Metadata {
	if m.FilmBase == nil || m.FilmBaseSpace != AfterProfile || m.BaseSource != LevelingSource || !validBase(*m.FilmBase) {
		m.FilmBase, m.FilmBaseSpace, m.BaseSource = nil, "", ""
	}
	return m
}
