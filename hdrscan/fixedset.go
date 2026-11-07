package hdrscan

import (
	"errors"
	"fmt"
	"os"

	"basetool/camera"
)

// SetFixed (v0.80) sets ISO iso + the Fixed shutter (1/60 s or, v0.81, 1/125 s) - the HDR Fixed frame-0 exposure -
// for RGB light leveling, without shooting a probe, and checks that the
// camera reports it. The HDR lock is not touched.
func (s *Session) SetFixed(iso int, why string) (camera.Exposure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := s.FixedExposure(iso)
	if err := s.checkManual(); err != nil {
		return want, err
	}
	if err := s.setExposure(want, why); err != nil {
		return want, err
	}
	got, err := s.Dev.Exposure()
	if err != nil {
		return want, err
	}
	if got != want {
		return got, fmt.Errorf("camera is at %s, not %s - set M mode and a fixed ISO (not Auto)", got, want)
	}
	return got, nil
}

// ErrExposureChanged means the camera's exposure is no longer the one the
// leveling started with (dial moved): the shot is refused, never corrected.
var ErrExposureChanged = errors.New("camera exposure changed")

// ShootFixed shoots one RAW into dir after checking that the camera is
// still at want. It never changes the exposure.
func (s *Session) ShootFixed(want camera.Exposure, dir, prefix string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	got, err := s.Dev.Exposure()
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf("%w: camera is at %s, leveling uses %s", ErrExposureChanged, got, want)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return s.Dev.Capture(dir, prefix)
}
