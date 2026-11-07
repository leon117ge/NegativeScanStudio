// Package camera talks to a tethered camera over PTP (Windows Portable Devices
// MTP-extension commands) and provides a simulated camera for testing.
package camera

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Exposure is an ISO + shutter pair. Shutter uses Fujifilm's PTP encoding of
// ExposureTime (0x500D): nominal microseconds, e.g. 1/60 s = 15625.
type Exposure struct {
	ISO     int
	Shutter uint32
}

func (e Exposure) String() string {
	return fmt.Sprintf("%s, %s", ISOLabel(e.ISO), ShutterLabel(e.Shutter))
}

// ISOLabel is "ISO 160" or "ISO AUTO1..3" (Fuji reports auto as -1..-3).
func ISOLabel(iso int) string {
	if iso < 0 {
		return fmt.Sprintf("ISO AUTO%d", -iso)
	}
	return fmt.Sprintf("ISO %d", iso)
}

// Seconds returns the nominal exposure time.
func (e Exposure) Seconds() float64 { return float64(e.Shutter) / 1e6 }

// shutterRow is one 1/3-stop entry, fastest first.
type shutterRow struct {
	Value uint32
	Label string
}

// Shutters is the Fujifilm 1/3-stop shutter table (from libgphoto2), fastest first.
var Shutters = []shutterRow{
	{122, "1/8000"}, {153, "1/6400"}, {193, "1/5000"}, {244, "1/4000"}, {307, "1/3200"}, {387, "1/2500"},
	{488, "1/2000"}, {615, "1/1600"}, {775, "1/1250"}, {976, "1/1000"}, {1230, "1/800"}, {1550, "1/640"},
	{1953, "1/500"}, {2460, "1/400"}, {3100, "1/320"}, {3906, "1/250"}, {4921, "1/200"}, {6200, "1/160"},
	{7812, "1/125"}, {9843, "1/100"}, {12401, "1/80"}, {15625, "1/60"}, {19686, "1/50"}, {24803, "1/40"},
	{31250, "1/30"}, {39372, "1/25"}, {49606, "1/20"}, {62500, "1/15"}, {78745, "1/13"}, {99212, "1/10"},
	{125000, "1/8"}, {157490, "1/6"}, {198425, "1/5"}, {250000, "1/4"}, {314980, "1/3"}, {396850, "0.4s"},
	{500000, "1/2"}, {629960, "0.6s"}, {793700, "0.8s"}, {1000000, "1s"}, {1259921, "1.3s"}, {1587401, "1.6s"},
	{2000000, "2s"}, {2519842, "2.5s"}, {3174802, "3s"}, {4000000, "4s"}, {5039684, "5s"}, {6349604, "6s"},
	{8000000, "8s"}, {10079368, "10s"}, {12699208, "13s"}, {16000000, "15s"}, {20158736, "20s"},
	{25398416, "25s"}, {32000000, "30s"},
}

// Shutter1_60 is the slowest shutter allowed for frame 0.
const Shutter1_60 uint32 = 15625

// Shutter1_125 (v0.81) is the X-T3's next full stop above 1/60 s: PTP
// ExposureTime 7812 µs, exactly half of 1/60's 15625. The body's 0x500D
// list (field log v0.80) has 7812 and 9843 (1/100) but no 1/120 value
// (8333): "1/120 s" on the light/scan pages is this setting.
const Shutter1_125 uint32 = 7812

// ISOs are the native 1/3-stop ISO steps (X-T3 base ISO is 160).
var ISOs = []int{160, 200, 250, 320, 400, 500, 640, 800, 1000, 1250, 1600, 2000, 2500, 3200, 4000, 5000, 6400, 8000, 10000, 12800}

// ShutterLabel returns "1/60" etc. for a Fuji ExposureTime value.
func ShutterLabel(v uint32) string {
	if v == 0xffffffff {
		return "Bulb"
	}
	for _, s := range Shutters {
		if s.Value == v {
			return s.Label
		}
	}
	if v >= 1000000 {
		return fmt.Sprintf("%.1fs", float64(v)/1e6)
	}
	if v > 0 {
		return fmt.Sprintf("1/%.0f", 1e6/float64(v))
	}
	return "?"
}

// ShutterIndex returns the table index of v (nearest entry).
func ShutterIndex(v uint32) int {
	best, bd := 0, math.Inf(1)
	for i, s := range Shutters {
		d := math.Abs(math.Log2(float64(s.Value) / float64(v)))
		if d < bd {
			best, bd = i, d
		}
	}
	return best
}

// PlusStops returns the shutter value thirds*1/3 EV slower than v.
func PlusStops(v uint32, thirds int) (uint32, error) {
	i := ShutterIndex(v) + thirds
	if i < 0 || i >= len(Shutters) {
		return 0, fmt.Errorf("no shutter speed %+d/3 EV from %s", thirds, ShutterLabel(v))
	}
	return Shutters[i].Value, nil
}

// Info describes a connected device.
type Info struct {
	Driver       string // "WPD" or "Simulated"
	Manufacturer string
	Model        string
	Version      string
	Serial       string
	VendorExt    uint32
	VendorDesc   string
	Ops          []uint16
	Props        []uint16
	Events       []uint16
	CaptureFmts  []uint16
	ImageFmts    []uint16
}

func (i Info) Supports(op uint16) bool {
	for _, o := range i.Ops {
		if o == op {
			return true
		}
	}
	return false
}

func (i Info) HasProp(p uint16) bool {
	for _, o := range i.Props {
		if o == p {
			return true
		}
	}
	return false
}

// Device is a tethered camera.
type Device interface {
	Info() Info
	// Exposure reads the current ISO + shutter from the camera.
	Exposure() (Exposure, error)
	// SetExposure writes ISO + shutter and returns what the camera reports
	// afterwards. ErrNotWritable means the user must set it on the dials.
	SetExposure(e Exposure) (Exposure, error)
	// LiveView returns one JPEG frame (starting live view if needed).
	LiveView() ([]byte, error)
	StopLiveView() error
	// Capture takes one picture, downloads every new object into dir,
	// deletes them from the camera and drains events. Returns the RAW path.
	Capture(dir string, prefix string) (string, error)
	// Diagnostics writes a detailed report to the log.
	Diagnostics() error
	Close() error
}

// StatusReader reads ISO, shutter and aperture (f-number, 0 = unknown/manual
// lens) quietly, for the always-visible exposure readout.
type StatusReader interface {
	Status() (Exposure, float64, error)
}

// ErrNotWritable is returned by SetExposure when the camera refuses PTP
// exposure changes (dial positions not on T / A-command, or firmware).
var ErrNotWritable = errors.New("camera does not accept exposure changes over USB")

// ErrNotManual is returned when the body is not in manual exposure (Fuji
// 0x500E != 1, i.e. P/A/S): the shutter cannot be set over USB and setting it
// by hand would not hold either. The user must switch to M.
var ErrNotManual = errors.New("Set the camera to M (shutter dial T)")

// ModeReader reports whether the body is in manual exposure (M) and the
// mode's name.
type ModeReader interface {
	ExposureMode() (manual bool, name string, err error)
}

// Logger appends timestamped lines to %APPDATA%\NegativeScanStudio\camera_log.txt
// and forwards them to an optional UI callback.
type Logger struct {
	mu     sync.Mutex
	path   string
	f      *os.File
	OnLine func(string)
}

// NewLogger opens (appends to) the log file in dir.
func NewLogger(dir string) *Logger {
	l := &Logger{}
	if dir != "" {
		os.MkdirAll(dir, 0o755)
		l.path = filepath.Join(dir, "camera_log.txt")
		if f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			l.f = f
		}
	}
	return l
}

func (l *Logger) Path() string { return l.path }

func (l *Logger) Printf(format string, a ...any) {
	if l == nil {
		return
	}
	line := fmt.Sprintf(format, a...)
	l.mu.Lock()
	if l.f != nil {
		fmt.Fprintf(l.f, "%s  %s\r\n", time.Now().Format("2006-01-02 15:04:05.000"), line)
	}
	cb := l.OnLine
	l.mu.Unlock()
	if cb != nil {
		cb(line)
	}
}

func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
}
