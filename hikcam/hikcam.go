// Package hikcam talks to a Hikrobot (Hikvision) Machine Vision camera
// through the MVS SDK (MvCameraControl.dll), loaded at runtime so the
// application still starts when the SDK is not installed.
//
// Target body: MV-CS240-90UM (24 MP USB3 monochrome, GMAX2424,
// 6016 x 4096, Mono8/10/12). USB3 and GigE MVS devices are enumerated;
// the matching color body is MV-CS240-90UC.
package hikcam

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Published MV-CS240-90UM limits (Hikrobot datasheet, GMAX2424).
const (
	CS240Model     = "MV-CS240-90UM"
	CS240Width     = 6016
	CS240Height    = 4096
	CS240ExpMinUS  = 32.0
	CS240ExpMaxUS  = 10_000_000.0
	CS240GainMinDB = 0.0
	CS240GainMaxDB = 24.0
)

var (
	ErrNoSDK     = errors.New("Hikrobot MVS SDK not found (install MVS so MvCameraControl.dll is on PATH)")
	ErrNoCamera  = errors.New("no Hikrobot camera found")
	ErrNotOpen   = errors.New("camera is not open")
	ErrNotGrab   = errors.New("camera is not grabbing")
	ErrBadFormat = errors.New("unsupported pixel format")
)

// PixelFormat is the requested sensor readout format.
type PixelFormat int

const (
	PixelAuto PixelFormat = iota // Mono12, else Mono10, else Mono8
	PixelMono8
	PixelMono10
	PixelMono12
)

func (p PixelFormat) String() string {
	switch p {
	case PixelMono8:
		return "Mono8"
	case PixelMono10:
		return "Mono10"
	case PixelMono12:
		return "Mono12"
	default:
		return "Auto (highest bit depth)"
	}
}

// GenICamNames are the PixelFormat strings tried for p, preferred first.
func (p PixelFormat) GenICamNames() []string {
	switch p {
	case PixelMono8:
		return []string{"Mono8"}
	case PixelMono10:
		return []string{"Mono10", "Mono10Packed"}
	case PixelMono12:
		return []string{"Mono12", "Mono12Packed"}
	default:
		return []string{"Mono12", "Mono12Packed", "Mono10", "Mono10Packed", "Mono8"}
	}
}

// PixelFormatChoices is the UI order: Auto (highest), Mono12, Mono10, Mono8.
var PixelFormatChoices = []PixelFormat{PixelAuto, PixelMono12, PixelMono10, PixelMono8}

// PixelFormatIndex returns the index of p in PixelFormatChoices (0 if unknown).
func PixelFormatIndex(p PixelFormat) int {
	for i, x := range PixelFormatChoices {
		if x == p {
			return i
		}
	}
	return 0
}

// Info describes an enumerated device.
type Info struct {
	Index     int
	Model     string
	Serial    string
	Vendor    string
	Interface string // "USB3", "GigE" or "Unknown"
	Simulated bool
}

func (i Info) Label() string {
	if i.Simulated {
		return "Simulated Hikrobot " + CS240Model
	}
	iface := i.Interface
	if iface == "" {
		iface = "MVS"
	}
	name := strings.TrimSpace(i.Vendor + " " + i.Model)
	if name == "" {
		name = "Hikrobot camera"
	}
	if i.Serial != "" {
		return fmt.Sprintf("%s  (%s, SN %s)", name, iface, i.Serial)
	}
	return fmt.Sprintf("%s  (%s)", name, iface)
}

// Settings are the GenICam features this app exposes.
type Settings struct {
	ExposureUS      float64
	GainDB          float64
	BlackLevel      float64
	PixelFormat     PixelFormat
	GammaEnable     bool
	FullFrame       bool
	Width           int
	Height          int
	OffsetX         int
	OffsetY         int
	SoftwareTrigger bool
	// QualityPreset asks Apply to also try noise-relevant GenICam nodes
	// (ExposureAuto/GainAuto Off, DigitalShift 0, sharpness/denoise/LUT
	// off, ADC 12-bit). Missing nodes are ignored so older firmware still
	// applies the required settings above.
	QualityPreset bool
}

// BestScanBlackLevel is a small 12-bit pedestal (~0.8 % of full scale)
// so readout noise is not clipped at code 0.
const BestScanBlackLevel = 32

// DefaultSettings matches a conservative CS240 setup: 10 ms, 0 dB, gamma
// off, full frame, free-run, highest available bit depth.
func DefaultSettings() Settings {
	return Settings{
		ExposureUS:  10_000,
		GainDB:      0,
		BlackLevel:  0,
		PixelFormat: PixelAuto,
		GammaEnable: false,
		FullFrame:   true,
		Width:       CS240Width,
		Height:      CS240Height,
	}
}

// BestScanQuality is the MV-CS240-90UM preset for maximum film-scan
// quality: highest bit depth, lowest analog gain, linear readout, full
// frame, software trigger, and extras that turn off in-camera processing.
// ExposureTime starts at 10 ms; capture meters each R/G/B channel so the
// film base / highlights do not clip.
func BestScanQuality() Settings {
	return Settings{
		ExposureUS:      10_000,
		GainDB:          0,
		BlackLevel:      BestScanBlackLevel,
		PixelFormat:     PixelMono12,
		GammaEnable:     false,
		FullFrame:       true,
		Width:           CS240Width,
		Height:          CS240Height,
		SoftwareTrigger: true,
		QualityPreset:   true,
	}
}

// BestScanExplain is the English justification shown in the UI and docs.
func BestScanExplain() string {
	return "Best scan quality (MV-CS240-90UM): Mono12 (highest native bit depth, unpacked to 16-bit linear); " +
		"gain 0 dB (base / lowest analog noise — brightness comes from exposure time, not gain); " +
		"black level 32 (a ~0.8% 12-bit pedestal so readout noise is not clipped at 0); " +
		"gamma off (linear radiometric response for HDR merge and film-base math); " +
		"full frame 6016x4096 (every photosite); software trigger (grab after the RGB light has settled); " +
		"ExposureAuto and GainAuto off (the software meters each R, G and B channel so highlights do not clip); " +
		"DigitalShift 0, sharpness / denoise / LUT off, ADC 12-bit when the camera exposes those nodes. " +
		"After this preset is applied, press Capture — exposure is set automatically per channel."
}

func (s Settings) Clamp() Settings {
	if s.ExposureUS < CS240ExpMinUS {
		s.ExposureUS = CS240ExpMinUS
	}
	if s.ExposureUS > CS240ExpMaxUS {
		s.ExposureUS = CS240ExpMaxUS
	}
	if s.GainDB < CS240GainMinDB {
		s.GainDB = CS240GainMinDB
	}
	if s.GainDB > CS240GainMaxDB {
		s.GainDB = CS240GainMaxDB
	}
	if s.BlackLevel < 0 {
		s.BlackLevel = 0
	}
	if s.Width < 16 {
		s.Width = 16
	}
	if s.Height < 16 {
		s.Height = 16
	}
	if s.OffsetX < 0 {
		s.OffsetX = 0
	}
	if s.OffsetY < 0 {
		s.OffsetY = 0
	}
	return s
}

// Frame is one grabbed monochrome image, linearized to 16-bit (the native
// 8/10/12-bit values are left-shifted into the 16-bit range).
type Frame struct {
	W, H        int
	Pix         []uint16
	PixelFormat string
	ExposureUS  float64
	GainDB      float64
}

// Device is an opened monochrome camera (real MVS body or the simulator).
type Device interface {
	Info() Info
	Settings() Settings
	Apply(Settings) error
	StartGrab() error
	StopGrab() error
	Grab(timeout time.Duration) (*Frame, error)
	Close() error
}

// ListResult is what Enumerate returns so the UI can show why a list is empty.
type ListResult struct {
	Cameras []Info
	SDK     string // human-readable SDK status
	Err     error  // nil, ErrNoSDK, or a load/enum error
}

func (r ListResult) Message() string {
	if r.Err != nil {
		if errors.Is(r.Err, ErrNoSDK) {
			return r.SDK
		}
		return r.Err.Error()
	}
	if len(r.Cameras) == 0 {
		return "MVS SDK loaded, no camera found"
	}
	return fmt.Sprintf("MVS SDK loaded, %d camera(s)", len(r.Cameras))
}
