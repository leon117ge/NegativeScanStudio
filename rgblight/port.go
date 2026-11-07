package rgblight

import "strings"

// USB identity of the light.
const (
	VID = "0483"
	PID = "5740"
)

// PortInfo is one serial port on the PC.
type PortInfo struct {
	Name    string // "COM5"
	Light   bool   // matches VID 0483 / PID 5740
	Present bool   // listed as an active serial port right now
}

func (p PortInfo) Label() string {
	s := p.Name
	if p.Light {
		s += "  (RGB Light Air, USB 0483:5740)"
	}
	return s
}

// hwMatch reports whether a USB enumerator key name is the light.
func hwMatch(key string) bool {
	return strings.HasPrefix(strings.ToUpper(key), "VID_"+VID+"&PID_"+PID)
}
