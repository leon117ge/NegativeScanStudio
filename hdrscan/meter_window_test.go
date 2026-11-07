package hdrscan

import (
	"errors"
	"testing"

	"basetool/camera"
)

func TestLockWindow(t *testing.T) {
	cases := []struct {
		max, target float64
		ok          bool
	}{
		{0.80, 0.80, true}, {0.64, 0.80, true}, {0.63, 0.80, false}, {0.87, 0.80, true}, {0.88, 0.80, false},
		{0.85, 0.85, true}, {0.90, 0.85, false}, // safety cap 0.87
		{0.50, 0.50, true}, {0.40, 0.50, true}, {0.39, 0.50, false}, {0.56, 0.50, true}, {0.57, 0.50, false},
	}
	for _, c := range cases {
		r := Reading{Max: c.max, Peak: c.max * 1.05}
		if got := InTolerance(r, c.target); got != c.ok {
			t.Errorf("base %.2f target %.2f: lock %v, want %v", c.max, c.target, got, c.ok)
		}
	}
}

func TestSolveTargets(t *testing.T) {
	at := camera.Exposure{ISO: 160, Shutter: 15625} // 1/60
	for _, tg := range []float64{0.50, 0.60, 0.70, 0.80, 0.85} {
		r := Reading{Max: 0.499, Peak: 0.53}
		next, pred, note, dark := Solve(r, at, tg, 125000)
		t.Logf("target %.2f: %s pred %.3f %s", tg, next, pred, note)
		if dark || !inWindow(pred, tg) {
			t.Errorf("target %.2f: pred %.3f outside the window (%s)", tg, pred, note)
		}
	}
}

// falloff: the brightest part of the base limits the exposure
func TestSolvePeakLimit(t *testing.T) {
	at := camera.Exposure{ISO: 160, Shutter: 15625}
	r := Reading{Max: 0.40, Peak: 0.60} // peak 0.58 EV above the median
	_, pred, _, _ := Solve(r, at, 0.80, 0)
	if pred*0.60/0.40 >= PeakMax {
		t.Fatalf("peak would clip: pred %.3f", pred)
	}
}

type modeDev struct{ camera.Device }

func (modeDev) ExposureMode() (bool, string, error) { return false, "A (aperture priority)", nil }
func (modeDev) Exposure() (camera.Exposure, error) {
	return camera.Exposure{ISO: 160, Shutter: 15625}, nil
}

func TestMeterNotManual(t *testing.T) {
	popup := false
	s := &Session{Dev: modeDev{}, Dials: func(camera.Exposure, string) bool { popup = true; return true }}
	_, _, err := s.Meter()
	if !errors.Is(err, camera.ErrNotManual) || popup {
		t.Fatalf("err %v popup %v", err, popup)
	}
	t.Log(err)
}
