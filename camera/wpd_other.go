//go:build !windows

package camera

import "errors"

type WPDDevice struct{ ID, Name, Manufacturer, Description string }

func (d WPDDevice) String() string { return d.Manufacturer + " " + d.Name }

func ListWPD() ([]WPDDevice, error) {
	return nil, errors.New("Windows Portable Devices is only available on Windows")
}

func OpenWPD(id string) (Transport, error) { return nil, errors.New("WPD is Windows-only") }
