//go:build !windows

package rgblight

import (
	"errors"
	"io"
)

// ListPorts is only implemented on Windows.
func ListPorts() ([]PortInfo, error) { return nil, nil }

// OpenPort is only implemented on Windows.
func OpenPort(name string) (io.ReadWriteCloser, error) {
	return nil, errors.New("serial ports are only supported on Windows")
}
