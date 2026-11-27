//go:build !windows && (!darwin || !cgo)

package camera

import (
	"errors"
	"time"
)

// ICCAvailable reports whether this build has the ImageCaptureCore transport.
const ICCAvailable = false

// errNoICC is built on use (no package-level initialiser), so the Windows
// binary's package init stays identical to the build without this file.
func errNoICC() error {
	return errors.New("ImageCaptureCore is only available in macOS builds with cgo")
}

// ListICC lists cameras seen by ImageCaptureCore (macOS only).
func ListICC(timeout time.Duration) ([]ICCCamera, error) { return nil, errNoICC() }

// ICCAuthorization reports ImageCaptureCore authorization (macOS only).
func ICCAuthorization(timeout time.Duration) (control, contents string, err error) {
	return "", "", errNoICC()
}

// OpenICC opens a camera by ICDevice UUID ("" = the first camera that accepts PTP commands).
func OpenICC(uuid string, timeout time.Duration) (*ICCTransport, ICCCamera, error) {
	return nil, ICCCamera{}, errNoICC()
}
