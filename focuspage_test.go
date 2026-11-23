package main

import (
	"testing"
	"time"

	"basetool/hdrscan"
	"basetool/hikcam"
)

func TestFocusUIApplyAEConvergesAndLocksSim(t *testing.T) {
	sim := hikcam.OpenSim()
	sim.SetFlatField(true)
	st := hikcam.LiveViewSettings(hikcam.DefaultSettings(), 1)
	st.ExposureUS = 120_000
	st.GainDB = 0
	if err := sim.Apply(st); err != nil {
		t.Fatal(err)
	}
	if err := sim.StartGrab(); err != nil {
		t.Fatal(err)
	}
	defer sim.Close()

	mono := &MonoUI{}
	mono.setCam(sim)
	u := &FocusUI{mono: mono}
	cfg := hdrscan.LiveAutoExp()
	cur := sim.Settings()
	u.ae.Reset(hdrscan.ExposureState{ExposureUS: cur.ExposureUS, GainDB: cur.GainDB}, cfg)

	var lockedState hdrscan.ExposureState
	lockedAt := -1
	for i := 0; i < 60; i++ {
		fr, err := sim.Grab(time.Second)
		if err != nil {
			t.Fatal(err)
		}
		samples := hdrscan.RegionSamples(fr.Pix, fr.W, fr.H, fr.W/4, fr.H/4, 3*fr.W/4, 3*fr.H/4, 4)
		u.mu.Lock()
		next, why, epoch, needApply := u.ae.Prepare(samples, cfg)
		u.mu.Unlock()
		if needApply {
			applied, changed, err := u.applyAE(next)
			if err != nil {
				t.Fatal(err)
			}
			u.mu.Lock()
			u.ae.Record(samples, cfg, why, epoch, applied, changed)
			u.mu.Unlock()
		}
		u.mu.Lock()
		locked := u.ae.Locked
		stNow := u.ae.State
		u.mu.Unlock()
		if locked && lockedAt < 0 {
			lockedAt = i
			lockedState = stNow
		}
		if lockedAt >= 0 {
			camNow := sim.Settings()
			if camNow.ExposureUS != lockedState.ExposureUS || camNow.GainDB != lockedState.GainDB {
				t.Fatalf("frame %d: exposure drifted after lock: got %.2f us / %.2f dB, want %.2f us / %.2f dB",
					i, camNow.ExposureUS, camNow.GainDB, lockedState.ExposureUS, lockedState.GainDB)
			}
		}
	}
	if lockedAt < 0 {
		t.Fatalf("FocusUI AE did not lock: state=%+v why=%q", u.ae.State, u.ae.Why)
	}
	u.unlockAE()
	if u.ae.Locked {
		t.Fatalf("unlockAE did not clear lock")
	}
}
