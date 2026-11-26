package hdrscan

import (
	"context"
	"fmt"
	"testing"
	"time"

	"basetool/hikcam"
	"basetool/lightlevel"
	"basetool/rgblight"
)

type hikSimCam struct {
	dev         *hikcam.Sim
	lights      func() (on [3]bool, v [3]int)
	gain        [3]float64
	appliedUS   float64
	appliedGain float64
	last        [3]MonoPlane
}

func (h *hikSimCam) Name() string {
	return fmt.Sprintf("simulated monochrome, %.0f us, 0.00 dB (fixed exposure)", h.appliedUS)
}
func (h *hikSimCam) Mono() bool { return true }
func (h *hikSimCam) GetExposure() (float64, float64) {
	return h.appliedUS, h.appliedGain
}
func (h *hikSimCam) SetExposure(us, _ float64) error {
	st := hikcam.BestScanQuality()
	st.ExposureUS = us
	st.GainDB = 0
	st.FullFrame = false
	st.Width, st.Height = 160, 160
	st.OffsetX, st.OffsetY = 0, 0
	if err := h.dev.Apply(st); err != nil {
		return err
	}
	if err := h.dev.StartGrab(); err != nil {
		return err
	}
	got := h.dev.Settings()
	h.appliedUS, h.appliedGain = got.ExposureUS, got.GainDB
	return nil
}
func (h *hikSimCam) Prepare() (string, error) {
	if h.appliedUS <= 0 {
		h.appliedUS = 10_000
	}
	if err := h.SetExposure(h.appliedUS, 0); err != nil {
		return "", err
	}
	return fmt.Sprintf("%.0f us, 0.00 dB", h.appliedUS), nil
}
func (h *hikSimCam) Shoot(ctx context.Context, ch int, prefix string) (*lightlevel.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fr, err := h.dev.Grab(5 * time.Second)
	if err != nil {
		return nil, err
	}
	on, v := h.lights()
	if fr.Black16 != 512 {
		return nil, fmt.Errorf("sim BestScan frame pedestal %.0f, want 512", fr.Black16)
	}
	pix := ScaleMonoForSimLightBlack(append([]uint16(nil), fr.Pix...), fr.Black16, ch, on, v, h.gain)
	h.last[ch] = SubtractBlack(MonoFrame{W: fr.W, H: fr.H, Pix: pix, Black16: fr.Black16})
	im := LevelingImageMonoBlack(pix, fr.W, fr.H, fr.Black16)
	im.Path = prefix
	return im, nil
}

type hikSimLight struct{ c *rgblight.Client }

func (l hikSimLight) SetAll(on [3]bool, v [3]int) error { return l.c.SetAll(on, v) }

func TestSimHikrobotDimGreenRaisesExposureAndLevels(t *testing.T) {
	d := rgblight.NewSimDevice()
	d.TelEvery = 0
	c := rgblight.NewClient(d)
	c.Start()
	t.Cleanup(func() { c.Close() })

	dev := hikcam.OpenSim()
	dev.FlatField = true
	t.Cleanup(func() { _ = dev.Close() })

	cam := &hikSimCam{dev: dev, lights: d.Lights, gain: [3]float64{1.10, 0.80, 0.95}, appliedUS: 10_000}
	res, err := lightlevel.Run(context.Background(), hikSimLight{c}, cam, lightlevel.Params{
		Target: 0.50,
		Tol:    0.02,
		Settle: time.Millisecond,
		Start:  [3]int{29, 100, 54},
		Area:   lightlevel.Rect{X0: 0, Y0: 0, X1: 1, Y1: 1},
	}, [3]bool{true, true, true}, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.Mono {
		t.Fatal("expected a monochrome result")
	}
	if !res.Fit.Changed || res.Fit.FinalUS <= 10_000 {
		t.Fatalf("expected a raised Hikrobot exposure, got %+v", res.Fit)
	}
	if res.Fit.FinalGain != 0 {
		t.Fatalf("gain must stay 0 dB, got %.2f", res.Fit.FinalGain)
	}
	if !res.Converged() {
		t.Fatalf("sim Hikrobot + dim G should converge after the exposure bump: %v (exposure %.0f us, values %v, post %v)",
			res.Problems(), res.Fit.FinalUS, res.Values, res.Post)
	}
	if res.Values[1] > 95 {
		t.Fatalf("green should have headroom below 100, got %d at %.0f us", res.Values[1], res.Fit.FinalUS)
	}
	// v1.10: leveling and the scan both subtract the BlackLevel pedestal,
	// so the TIFF check of the black-subtracted planes reads the leveled base.
	tr, err := ProcessMonoTrichrome(cam.last[0], cam.last[1], cam.last[2], MonoTriOptions{
		OutPath: t.TempDir() + "/lv_RGB.tiff", Software: "test", MeasureLevelingBase: true, BaseNorm: Rect{X0: 0, Y0: 0, X1: 1, Y1: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	for c := 0; c < 3; c++ {
		if d := tr.Base[c] - res.Post[c]; d > 0.01 || d < -0.01 || tr.BaseErr != "" {
			t.Fatalf("TIFF check %v vs leveling %v (%s)", tr.Base, res.Post, tr.BaseErr)
		}
		if tr.Base[c] < 0.48 || tr.Base[c] > 0.52 {
			t.Fatalf("black-subtracted base %v not at the 0.50 target", tr.Base)
		}
	}
}
