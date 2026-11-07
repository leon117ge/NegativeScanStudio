package hdrscan

import (
	"errors"
	"testing"

	"basetool/camera"
)

type expDev struct {
	camera.Device
	e      camera.Exposure
	shots  int
	writes int
}

func (d *expDev) Exposure() (camera.Exposure, error) { return d.e, nil }
func (d *expDev) SetExposure(e camera.Exposure) (camera.Exposure, error) {
	d.writes++
	d.e = e
	return e, nil
}
func (d *expDev) Capture(dir, prefix string) (string, error) { d.shots++; return dir + "/x.RAF", nil }

func TestSetFixedAndShoot(t *testing.T) {
	d := &expDev{e: camera.Exposure{ISO: 400, Shutter: 1000}}
	s := &Session{Dev: d}
	e, err := s.SetFixed(250, "test")
	if err != nil || e != (camera.Exposure{ISO: 250, Shutter: FixedShutter}) || s.Locked != nil {
		t.Fatal(e, err)
	}
	if _, err := s.ShootFixed(e, t.TempDir(), "p"); err != nil || d.shots != 1 {
		t.Fatal(err)
	}
	d.e.ISO = 320 // dial moved
	if _, err := s.ShootFixed(e, t.TempDir(), "p"); !errors.Is(err, ErrExposureChanged) || d.shots != 1 || d.writes != 1 {
		t.Fatalf("shot at a changed exposure: %v", err)
	}
}

// v0.81: the Fixed shutter is selectable (1/60 default, 1/125 = "1/120").
func TestSetFixedShutterChoice(t *testing.T) {
	d := &expDev{e: camera.Exposure{ISO: 400, Shutter: 1000}}
	s := &Session{Dev: d}
	if e := s.FixedExposure(250); e.Shutter != camera.Shutter1_60 {
		t.Fatalf("default %v", e)
	}
	s.Fixed = camera.Shutter1_125
	e, err := s.SetFixed(250, "test")
	if err != nil || e != (camera.Exposure{ISO: 250, Shutter: 7812}) || d.e.Shutter != 7812 {
		t.Fatal(e, err, d.e)
	}
	s.Fixed = 8333 // not an X-T3 value: falls back to 1/60
	if e := s.FixedExposure(160); e.Shutter != camera.Shutter1_60 {
		t.Fatalf("invalid choice used: %v", e)
	}
}
