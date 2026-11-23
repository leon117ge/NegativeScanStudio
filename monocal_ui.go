package main

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"basetool/hdrscan"
	"basetool/hikbase"
	"basetool/hikcam"
	"basetool/monocap"
	"basetool/rgblight"

	"github.com/lxn/walk"
)

// monoNoise is the HDR merge noise model from the config (defaults when unset).
func (c HDRConfig) monoNoise() hdrscan.MonoNoise {
	nz := hdrscan.DefaultMonoNoise()
	if c.HikFullWellE >= 100 {
		nz.FullWellE = c.HikFullWellE
	}
	if c.HikReadNoiseE > 0 {
		nz.ReadE = c.HikReadNoiseE
	}
	return nz
}

// Frames-per-exposure range shown on the Mono page (v1.10).
const (
	framesPerExpMin = 1
	framesPerExpMax = 16
)

func monoCalStore() monocap.Store {
	return monocap.Store{Dir: filepath.Join(configDir(), "hikcal")}
}

// clampFloat limits v to [lo, hi] when lo != hi. A walk NumberEdit rejects
// a SetValue outside that range with "value out of range".
func clampFloat(v, lo, hi float64) float64 {
	if lo != hi {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
	}
	return v
}

// setNumberEdit sets a NumberEdit after clamping to the widget's current
// range, so a saved config value cannot crash the UI.
func setNumberEdit(ne *walk.NumberEdit, v float64) {
	if ne == nil {
		return
	}
	_ = ne.SetValue(clampFloat(v, ne.MinValue(), ne.MaxValue()))
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// captureOptions reads the v1.10 capture options from the page.
func (u *MonoUI) captureOptions() monocap.Options {
	o := monocap.Options{FramesPerExposure: 1, Calib: monoCalStore()}
	if u.fpeNE != nil {
		o.FramesPerExposure = int(math.Round(clampFloat(u.fpeNE.Value(), framesPerExpMin, framesPerExpMax)))
	}
	o.UseDark = u.darkCB != nil && u.darkCB.Checked()
	o.UseFlat = u.flatCB != nil && u.flatCB.Checked()
	if u.hdr != nil {
		o.Noise = u.hdr.cfg.monoNoise()
	} else {
		o.Noise = hdrscan.DefaultMonoNoise()
	}
	return o
}

func (u *MonoUI) loadCalOptions() {
	if u.hdr == nil {
		return
	}
	c := u.hdr.cfg
	if u.fpeNE != nil {
		n := c.HikFramesPerExp
		if n < framesPerExpMin {
			n = framesPerExpMin
		}
		setNumberEdit(u.fpeNE, float64(n))
	}
	if u.darkCB != nil {
		u.darkCB.SetChecked(c.HikApplyDark)
	}
	if u.flatCB != nil {
		u.flatCB.SetChecked(c.HikApplyFlat)
	}
	u.updateCalLabel()
}

func (u *MonoUI) saveCalOptions() {
	if u.hdr == nil || u.fpeNE == nil || u.darkCB == nil || u.flatCB == nil {
		return
	}
	u.hdr.cfg.HikFramesPerExp = int(math.Round(clampFloat(u.fpeNE.Value(), framesPerExpMin, framesPerExpMax)))
	u.hdr.cfg.HikApplyDark = u.darkCB.Checked()
	u.hdr.cfg.HikApplyFlat = u.flatCB.Checked()
	u.hdr.save()
}

func (u *MonoUI) updateCalLabel() {
	if u.calLbl != nil {
		u.calLbl.SetText("Calibration: " + monoCalStore().Summary())
	}
}

// calibrationExposures is the shared exposure times every bracket EV.
func (u *MonoUI) calibrationExposures() []float64 {
	base := hikbase.DefaultExposureUS
	if u.hdr != nil {
		base = u.hdr.sharedHikExposure()
	}
	var out []float64
	seen := map[int]bool{}
	for _, ev := range append([]int{0}, u.captureEVs()...) {
		if seen[ev] {
			continue
		}
		seen[ev] = true
		out = append(out, math.Min(hikcam.CS240ExpMaxUS, base*hdrscan.EVScale(ev)))
	}
	return out
}

func (u *MonoUI) captureDarks() {
	exps := u.calibrationExposures()
	var parts []string
	for _, e := range exps {
		parts = append(parts, fmt.Sprintf("%.0f us", e))
	}
	msg := "Dark frames: the RGB light will be switched off. Also cover the lens or put an opaque card over the gate.\r\n\r\n" +
		"16 grabs are averaged at " + strings.Join(parts, ", ") + " (gain 0 dB, current pixel format).\r\n\r\nContinue?"
	if walk.MsgBox(u.hdr.win(), "Capture dark frames", msg, walk.MsgBoxOKCancel|walk.MsgBoxIconInformation) != walk.DlgCmdOK {
		return
	}
	u.runCalibration("dark frames", func(o monocap.CalOptions) error {
		_, err := monocap.CaptureDarks(o, exps)
		return err
	})
}

func (u *MonoUI) captureFlats() {
	msg := "Flat field: remove the film (empty gate, holder in place, same lens focus and aperture as for scanning).\r\n\r\n" +
		"R, G and B are each exposed to about 45% of full scale at the scan light values and 16 grabs are averaged.\r\n\r\nContinue?"
	if walk.MsgBox(u.hdr.win(), "Capture flat field", msg, walk.MsgBoxOKCancel|walk.MsgBoxIconInformation) != walk.DlgCmdOK {
		return
	}
	start := hikbase.DefaultExposureUS
	if u.hdr != nil {
		start = u.hdr.sharedHikExposure()
	}
	u.runCalibration("flat field", func(o monocap.CalOptions) error {
		_, err := monocap.CaptureFlats(o, start/2)
		return err
	})
}

// runCalibration owns the camera and light for one calibration capture.
func (u *MonoUI) runCalibration(what string, run func(monocap.CalOptions) error) {
	if u.running {
		return
	}
	if u.cam == nil {
		walk.MsgBox(u.hdr.win(), "Calibration", "Connect the Hikrobot camera first.", walk.MsgBoxIconWarning)
		return
	}
	vals, _, ok := scanLightValues(u.hdr, u.light)
	if !ok {
		vals = [3]int{50, 50, 50}
	}
	settle := 300 * time.Millisecond
	if u.light != nil && u.light.settleEd != nil {
		var ms int
		if _, err := fmt.Sscanf(strings.TrimSpace(u.light.settleEd.Text()), "%d", &ms); err == nil && ms >= 0 && ms <= 5000 {
			settle = time.Duration(ms) * time.Millisecond
		}
	}
	u.stopPreview()
	if u.light != nil {
		u.light.stopHikPreview()
	}
	if err := u.applySettings(); err != nil {
		u.say("Apply settings: " + err.Error())
		u.startPreview()
		return
	}
	var sw *rgblight.ChannelSwitch
	var light monocap.Light
	if u.light != nil && u.light.connected() {
		sw = &rgblight.ChannelSwitch{C: u.light.cl, Values: vals, Log: func(m string) { u.hdr.log.Printf("Calibration: %s", m) }}
		light = sw
	} else if sim, ok := u.cam.(*hikcam.Sim); ok {
		light = monocap.SimLight{S: sim}
	} else {
		walk.MsgBox(u.hdr.win(), "Calibration", "Connect the RGB light first: calibration switches it.", walk.MsgBoxIconWarning)
		u.startPreview()
		return
	}
	u.running = true
	u.updateButtons()
	if u.pb != nil {
		u.pb.SetMarqueeMode(true)
	}
	cam := u.cam
	u.say("==== Capturing " + what + " ====")
	go func() {
		err := func() error {
			if sw != nil {
				if _, e := sw.Snapshot(); e != nil {
					return fmt.Errorf("could not read the light: %w", e)
				}
				defer func() { _ = sw.Restore() }()
			}
			if err := cam.StartGrab(); err != nil {
				return err
			}
			defer func() { _ = cam.StopGrab() }()
			return run(monocap.CalOptions{
				Cam: cam, Light: light, Settle: settle, Frames: 16, Store: monoCalStore(), Software: appVersion, LightRGB: vals,
				Log:    func(m string) { u.ui(func() { u.say(m) }) },
				Status: func(m string) { u.ui(func() { u.repTE.SetText(m) }) },
			})
		}()
		u.ui(func() {
			u.running = false
			if u.pb != nil {
				u.pb.SetMarqueeMode(false)
				u.pb.SetValue(0)
			}
			if err != nil {
				u.say("Calibration (" + what + ") failed: " + err.Error())
				if u.repTE != nil {
					u.repTE.SetText("Calibration (" + what + ") failed: " + err.Error())
				}
			} else {
				u.say("Calibration (" + what + ") saved in " + monoCalStore().Dir)
				if u.repTE != nil {
					u.repTE.SetText("Calibration (" + what + ") saved. " + monoCalStore().Summary())
				}
			}
			u.updateCalLabel()
			u.updateButtons()
			u.startPreview()
		})
	}()
}
