package main

import (
	"context"
	"testing"
	"time"

	"basetool/hikcam"
	"basetool/lightlevel"
	"basetool/rgblight"
)

type hikTestLight struct {
	c *rgblight.Client
}

func (l hikTestLight) SetAll(on [3]bool, v [3]int) error { return l.c.SetAll(on, v) }

func TestHikSimLevelingRaisesExposureForDimGreen(t *testing.T) {
	d := rgblight.NewSimDevice()
	d.TelEvery = 0
	c := rgblight.NewClient(d)
	c.Start()
	t.Cleanup(func() { c.Close() })

	dev := hikcam.OpenSim()
	t.Cleanup(func() { _ = dev.Close() })

	// G is clearly the dimmest channel, similar to the real MV-CS240-90UM
	// result (G still short at code 100 with R/B closer to the target).
	gain := [3]float64{1.10, 0.80, 0.95}
	hk := &hikLevelCamera{
		dev:     dev,
		lights:  d.Lights,
		wantUS:  10_000,
		simGain: &gain,
		simSize: 160,
	}
	res, err := lightlevel.Run(context.Background(), hikTestLight{c}, hk, lightlevel.Params{
		Target: 0.50,
		Tol:    0.02,
		Settle: time.Millisecond,
		Start:  [3]int{29, 100, 54},
		Area:   lightlevel.Rect{0, 0, 1, 1},
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
	us, gainDB := hk.GetExposure()
	if us != res.Fit.FinalUS || gainDB != 0 {
		t.Fatalf("camera exposure %.0f us %.2f dB, fit %+v", us, gainDB, res.Fit)
	}
}
