package main

// Focus Check page (v0.97): live view with zoom, green-only or white light,
// temporary auto-exposure so film grain is readable, and a sharpness meter
// with peak hold. Prefers the Hikrobot monochrome camera; falls back to the
// existing tethered camera's live view.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"basetool/camera"
	"basetool/hdrscan"
	"basetool/hikcam"
	"basetool/rgblight"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type FocusUI struct {
	hdr   *HDRUI
	light *LightUI
	mono  *MonoUI

	active atomic.Bool

	view                                        *walk.CustomWidget
	bmp                                         *walk.Bitmap
	srcW, srcH                                  int
	bmpX, bmpY                                  int  // origin of bmp in source pixels when it is a 1:1 crop
	bmpFit                                      bool // bmp is a downsampled full frame
	dispUI                                      atomic.Bool
	labelsUI                                    atomic.Bool
	zoomCB                                      *walk.ComboBox
	markCB                                      *walk.CheckBox
	greenBtn, whiteBtn, liveBtn, resetPeakBtn   *walk.PushButton
	srcLbl, expLbl, lightLbl, sharpLbl, peakLbl *walk.Label
	logTE                                       *walk.TextEdit
	meter                                       *walk.ProgressBar

	zoomIdx      atomic.Int32 // 0 = fit, else index into zoomVals
	viewW, viewH atomic.Int32
	drag         bool
	dragX, dragY int

	loopOn  atomic.Bool
	loopGen atomic.Int64

	fujiMu   sync.Mutex
	fujiDone chan struct{}

	mu           sync.Mutex
	cx, cy       float64 // pan centre, 0..1
	samples      []float64
	visW, visH   int
	score        hdrscan.FocusScore
	peak         hdrscan.PeakHold
	ae           hdrscan.LiveAE
	lastMeterVal int
	meterRangeOK bool
	lastPix      []uint16
	lastW, lastH int

	savedHik       hikcam.Settings
	haveSavedHik   bool
	savedFuji      camera.Exposure
	haveSavedFuji  bool
	savedLight     rgblight.State
	haveSavedLight bool
	lightMode      int // 0 = green, 1 = white
}

var zoomItems = []string{"Fit", "100 %", "200 %", "400 %"}
var zoomVals = []int{0, 1, 2, 4}

func (u *FocusUI) isActive() bool {
	return u.active.Load()
}

func (u *FocusUI) say(s string) {
	if u.hdr != nil && u.hdr.log != nil {
		u.hdr.log.Printf("Focus check: %s", s)
	}
	if u.logTE != nil {
		tx := u.logTE.Text()
		if len(tx) > 20000 {
			u.logTE.SetText(tx[len(tx)-12000:])
		}
		u.logTE.AppendText(time.Now().Format("15:04:05  ") + s + "\r\n")
		u.logTE.ScrollToCaret()
	}
}

func (u *FocusUI) hikCam() hikcam.Device {
	if u.mono == nil {
		return nil
	}
	return u.mono.getCam()
}

func (u *FocusUI) sourceName() string {
	if cam := u.hikCam(); cam != nil {
		return cam.Info().Label()
	}
	if u.hdr != nil && u.hdr.dev != nil {
		inf := u.hdr.dev.Info()
		return strings.TrimSpace(inf.Manufacturer + " " + inf.Model)
	}
	return ""
}

func (u *FocusUI) waitFujiLoopSync() {
	u.fujiMu.Lock()
	ch := u.fujiDone
	u.fujiDone = nil
	u.fujiMu.Unlock()
	if ch != nil {
		<-ch
	}
}

func (u *FocusUI) enter() {
	if !u.active.CompareAndSwap(false, true) {
		return
	}
	u.mu.Lock()
	u.cx, u.cy = 0.5, 0.5
	u.peak.Reset()
	u.ae.Reset(hdrscan.ExposureState{}, hdrscan.LiveAutoExp())
	u.mu.Unlock()
	if u.zoomCB != nil {
		u.zoomIdx.Store(int32(max(0, u.zoomCB.CurrentIndex())))
	}
	if u.view != nil {
		cb := u.view.ClientBoundsPixels()
		if cb.Width > 8 && cb.Height > 8 {
			u.viewW.Store(int32(cb.Width))
			u.viewH.Store(int32(cb.Height))
		}
	}

	var savedHik hikcam.Settings
	var haveHik bool
	if cam := u.hikCam(); cam != nil && u.mono != nil && !u.mono.running.Load() {
		savedHik = u.mono.readSettings()
		haveHik = true
		u.mu.Lock()
		u.ae.Reset(hdrscan.ExposureState{ExposureUS: savedHik.ExposureUS, GainDB: savedHik.GainDB}, hdrscan.LiveAutoExp())
		u.mu.Unlock()
	}

	gen := u.loopGen.Add(1)
	var hikGen int64
	if u.mono != nil {
		hikGen = u.mono.worker().Bump()
	}
	u.loopOn.Store(true)
	u.updateLabels()

	runSetup := func() {
		if u.mono != nil {
			u.mono.worker().StopSyncIfNeeded()
		}
		u.waitFujiLoopSync()
		if !u.active.Load() || u.loopGen.Load() != gen {
			return
		}
		if u.mono != nil && u.mono.worker().Gen() != hikGen {
			return
		}

		if haveHik && u.mono != nil && !u.mono.running.Load() {
			if cam := u.hikCam(); cam != nil {
				ae := hdrscan.ExposureState{ExposureUS: savedHik.ExposureUS, GainDB: savedHik.GainDB}
				ae, _ = hdrscan.AdjustExposure(nil, ae, hdrscan.LiveAutoExp())
				summary, err := applyLiveSettings(cam, savedHik, true)
				if err != nil {
					msg := "Live settings: " + err.Error()
					u.ui(func() { u.say(msg) })
				} else {
					_ = cam.SetExposureGain(ae.ExposureUS, ae.GainDB)
					s := "Live view: " + cam.Settings().LiveSummary()
					if summary != "" {
						s = summary
					}
					u.ui(func() { u.say(s) })
				}
				cur := cam.Settings()
				u.mu.Lock()
				u.savedHik = savedHik
				u.haveSavedHik = true
				u.ae.Reset(hdrscan.ExposureState{ExposureUS: cur.ExposureUS, GainDB: cur.GainDB}, hdrscan.LiveAutoExp())
				u.mu.Unlock()
			}
		} else {
			u.mu.Lock()
			u.haveSavedHik = false
			u.mu.Unlock()
		}

		if !haveHik && u.hdr != nil && u.hdr.dev != nil && u.hdr.worker != nil {
			done := make(chan struct{})
			u.hdr.worker.do(func() {
				defer close(done)
				if e, err := u.hdr.dev.Exposure(); err == nil {
					u.mu.Lock()
					u.savedFuji = e
					u.haveSavedFuji = true
					u.ae.Reset(hdrscan.ExposureState{ExposureUS: float64(e.Shutter), GainDB: 0}, hdrscan.LiveAutoExp())
					u.mu.Unlock()
				} else {
					u.mu.Lock()
					u.haveSavedFuji = false
					u.mu.Unlock()
				}
			})
			<-done
		} else {
			u.mu.Lock()
			u.haveSavedFuji = false
			u.mu.Unlock()
		}

		if u.light != nil && u.light.connected() {
			if st, err := u.light.cl.ReadState(); err == nil {
				u.mu.Lock()
				u.savedLight = st
				u.haveSavedLight = true
				u.mu.Unlock()
			} else {
				u.mu.Lock()
				u.haveSavedLight = false
				u.mu.Unlock()
			}
			u.applyLightMode(0)
		} else {
			u.mu.Lock()
			u.haveSavedLight = false
			u.mu.Unlock()
		}

		if !u.active.Load() || u.loopGen.Load() != gen {
			return
		}
		u.ui(func() {
			u.say("Entered Focus check: exposure adjusts automatically for the visible region; capture exposure is restored when you leave.")
			u.updateLabels()
		})
		u.startLoop(gen, hikGen)
	}

	if u.mono != nil {
		u.mono.worker().Do(runSetup)
	} else {
		go runSetup()
	}
}

func (u *FocusUI) leave() {
	if !u.active.Swap(false) {
		return
	}
	u.loopOn.Store(false)
	u.loopGen.Add(1)
	if u.mono != nil {
		u.mono.stopPreviewAsync()
	}

	runRestore := func() {
		if u.mono != nil {
			u.mono.worker().StopSync()
		}
		u.waitFujiLoopSync()
		u.restoreExposure()
		u.restoreLight()
		u.ui(func() {
			u.say("Left Focus check: previous capture exposure restored.")
		})
	}

	if u.mono != nil {
		u.mono.worker().Do(runRestore)
	} else {
		go runRestore()
	}
}

func (u *FocusUI) restoreExposure() {
	u.mu.Lock()
	haveHik, savedHik := u.haveSavedHik, u.savedHik
	haveFuji, savedFuji := u.haveSavedFuji, u.savedFuji
	u.haveSavedHik = false
	u.haveSavedFuji = false
	u.mu.Unlock()

	if haveHik {
		if cam := u.hikCam(); cam != nil && (u.mono == nil || !u.mono.running.Load()) {
			if err := cam.Apply(savedHik); err != nil {
				msg := "Could not restore Hikrobot exposure: " + err.Error()
				u.ui(func() { u.say(msg) })
			} else {
				msg := fmt.Sprintf("Restored Hikrobot exposure: %.0f us, %.2f dB", savedHik.ExposureUS, savedHik.GainDB)
				u.ui(func() { u.say(msg) })
			}
		}
	}
	if haveFuji && u.hdr != nil && u.hdr.dev != nil && u.hdr.worker != nil {
		done := make(chan struct{})
		u.hdr.worker.do(func() {
			defer close(done)
			if _, err := u.hdr.dev.SetExposure(savedFuji); err != nil {
				msg := "Could not restore camera exposure: " + err.Error()
				u.ui(func() { u.say(msg) })
			} else {
				msg := "Restored camera exposure: " + savedFuji.String()
				u.ui(func() { u.say(msg) })
			}
		})
		<-done
	}
}

func (u *FocusUI) restoreLight() {
	u.mu.Lock()
	haveLight, saved := u.haveSavedLight, u.savedLight
	u.haveSavedLight = false
	u.mu.Unlock()
	if !haveLight || u.light == nil || !u.light.connected() {
		return
	}
	if _, err := u.light.cl.SetAllConfirmed(saved.On, saved.Value); err != nil {
		msg := "Could not restore the light: " + err.Error()
		u.ui(func() { u.say(msg) })
		return
	}
	u.ui(func() { u.light.showState(saved, false) })
}

func (u *FocusUI) applyLightMode(mode int) {
	u.mu.Lock()
	u.lightMode = mode
	u.mu.Unlock()
	if u.light == nil || !u.light.connected() {
		u.ui(func() {
			u.say("RGB light not connected - cannot switch green/white.")
			u.updateLabels()
		})
		return
	}
	vals, _, ok := scanLightValues(u.hdr, u.light)
	if !ok {
		if cs := u.light.cl.CachedState(); cs != nil {
			vals = cs.Value
		} else {
			vals = [3]int{50, 50, 50}
		}
	}
	sw := &rgblight.ChannelSwitch{C: u.light.cl, Values: vals}
	var err error
	var okMsg string
	if mode == 0 {
		err = sw.Green()
		okMsg = "Light: green only (trichrome focus baseline)."
	} else {
		err = sw.White()
		okMsg = "Light: white (all channels) for white-light scanning."
	}
	u.unlockAE()
	cs := u.light.cl.CachedState()
	u.ui(func() {
		if err != nil {
			u.say("Light switch failed: " + err.Error())
		} else {
			u.say(okMsg)
			if cs != nil {
				u.light.showState(*cs, false)
			}
		}
		u.updateLabels()
	})
}

func (u *FocusUI) unlockAE() {
	u.mu.Lock()
	u.ae.Unlock()
	u.mu.Unlock()
}

func (u *FocusUI) setLight(mode int) {
	u.mu.Lock()
	u.lightMode = mode
	u.ae.Unlock()
	u.mu.Unlock()
	u.updateLabels()
	if u.light != nil && u.light.lw != nil {
		u.light.lw.do(func() { u.applyLightMode(mode) })
		return
	}
	go u.applyLightMode(mode)
}

func (u *FocusUI) updateLabels() {
	u.mu.Lock()
	lightMode := u.lightMode
	aeState := u.ae.State
	aeWhy := u.ae.Why
	score := u.score
	peak := u.peak
	u.mu.Unlock()

	if u.srcLbl != nil {
		n := u.sourceName()
		if n == "" {
			setFocusStatusTip(u.srcLbl, "Camera: none connected",
				"Open the Hikrobot camera on Mono trichrome, or the Fuji body on Camera.")
		} else {
			setFocusStatus(u.srcLbl, "Camera: "+n)
		}
	}
	if u.lightLbl != nil {
		mode := "green only"
		if lightMode != 0 {
			mode = "white"
		}
		if u.light == nil || !u.light.connected() {
			setFocusStatus(u.lightLbl, "Light: not connected (wanted: "+mode+")")
		} else {
			setFocusStatus(u.lightLbl, "Light: "+mode)
		}
	}
	if u.expLbl != nil {
		if aeWhy == "" {
			setFocusStatusTip(u.expLbl,
				fmt.Sprintf("Auto exposure: %.0f us, %.2f dB\n", aeState.ExposureUS, aeState.GainDB),
				fmt.Sprintf("Auto exposure: %.0f us, %.2f dB", aeState.ExposureUS, aeState.GainDB))
		} else {
			setFocusStatusTip(u.expLbl,
				fmt.Sprintf("Auto exposure: %.0f us, %.2f dB\n(%s)", aeState.ExposureUS, aeState.GainDB, aeWhy),
				fmt.Sprintf("Auto exposure: %.0f us, %.2f dB (%s)", aeState.ExposureUS, aeState.GainDB, aeWhy))
		}
	}
	if u.sharpLbl != nil {
		setFocusStatusTip(u.sharpLbl,
			fmt.Sprintf("Sharpness: %.4f\nLap %.4f, grain %.4f", score.Combined, score.Laplacian, score.Grain),
			fmt.Sprintf("Sharpness: %.4f (Lap %.4f, grain %.4f)", score.Combined, score.Laplacian, score.Grain))
	}
	if u.peakLbl != nil {
		if peak.Have {
			setFocusStatus(u.peakLbl, fmt.Sprintf("Peak hold: %.4f", peak.Peak))
		} else {
			setFocusStatus(u.peakLbl, "Peak hold: -")
		}
	}
	if u.meter != nil {
		v := int(score.Combined * 20000)
		if v < 0 {
			v = 0
		}
		if v > 1000 {
			v = 1000
		}
		if !u.meterRangeOK {
			u.meter.SetRange(0, 1000)
			u.meterRangeOK = true
			u.lastMeterVal = -1
		}
		if v != u.lastMeterVal {
			u.meter.SetValue(v)
			u.lastMeterVal = v
		}
	}
	if u.greenBtn != nil {
		en := u.light != nil && u.light.connected()
		if u.greenBtn.Enabled() != en {
			u.greenBtn.SetEnabled(en)
		}
		if u.whiteBtn != nil && u.whiteBtn.Enabled() != en {
			u.whiteBtn.SetEnabled(en)
		}
	}
}

func (u *FocusUI) startLoop(gen, hikGen int64) {
	if !u.active.Load() || u.loopGen.Load() != gen {
		return
	}
	if cam := u.hikCam(); cam != nil && u.mono != nil {
		if u.mono.running.Load() {
			return
		}
		if err := cam.StartGrab(); err != nil {
			msg := "Focus grab: " + err.Error()
			u.ui(func() { u.say(msg) })
			return
		}
		if !u.mono.worker().StartLoop(hikGen, func(valid func() bool) {
			defer func() { _ = cam.StopGrab() }()
			u.loopBody(func() bool {
				return valid() && u.loopOn.Load() && u.active.Load() && u.loopGen.Load() == gen
			})
		}) {
			_ = cam.StopGrab()
		}
		return
	}
	if u.hdr != nil && u.hdr.dev != nil {
		u.fujiMu.Lock()
		if u.fujiDone != nil || u.loopGen.Load() != gen {
			u.fujiMu.Unlock()
			return
		}
		done := make(chan struct{})
		u.fujiDone = done
		u.fujiMu.Unlock()
		go func() {
			defer close(done)
			u.loopBody(func() bool {
				return u.loopOn.Load() && u.active.Load() && u.loopGen.Load() == gen
			})
		}()
		return
	}
	u.ui(func() {
		u.say("No camera to preview.")
		u.updateLabels()
	})
}

func (u *FocusUI) loopBody(stillValid func() bool) {
	cfg := hdrscan.LiveAutoExp()
	misses := 0
	for stillValid() {
		pix, w, h, err := u.grabFrame()
		if !stillValid() {
			return
		}
		if err != nil || pix == nil || w <= 0 || h <= 0 || len(pix) < w*h {
			if err == nil {
				err = fmt.Errorf("empty frame")
			}
			misses++
			msg, log := liveGrabMessage(err, misses)
			u.mu.Lock()
			u.ae.Why = "waiting for a frame"
			u.mu.Unlock()
			if log {
				m := msg
				u.ui(func() {
					u.say(m)
					u.updateLabels()
				})
			} else if u.labelsUI.CompareAndSwap(false, true) {
				u.ui(func() {
					defer u.labelsUI.Store(false)
					u.updateLabels()
				})
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		misses = 0
		u.mu.Lock()
		u.lastPix, u.lastW, u.lastH = pix, w, h
		u.mu.Unlock()
		if u.dispUI.CompareAndSwap(false, true) {
			img, ox, oy, fit := u.makeDisplay(pix, w, h)
			bmp, be := walk.NewBitmapFromImage(img)
			if be != nil {
				u.dispUI.Store(false)
			} else {
				u.ui(func() {
					defer u.dispUI.Store(false)
					if !stillValid() {
						bmp.Dispose()
						return
					}
					u.showBitmap(bmp, w, h, ox, oy, fit)
				})
			}
		}
		x0, y0, x1, y1 := u.meterRect(w, h)
		samples := hdrscan.RegionSamples(pix, w, h, x0, y0, x1, y1, 2)
		crop, cw, ch := hdrscan.CropGray16ToFloat64(pix, w, h, x0, y0, x1, y1)
		score := hdrscan.MeasureFocus(crop, cw, ch)
		u.mu.Lock()
		next, why, epoch, needApply := u.ae.Prepare(samples, cfg)
		u.mu.Unlock()
		if needApply {
			applied, changed, err := u.applyAE(next)
			if err != nil {
				msg := "Live exposure: " + err.Error()
				u.ui(func() { u.say(msg) })
			} else {
				u.mu.Lock()
				u.ae.Record(samples, cfg, why, epoch, applied, changed)
				u.mu.Unlock()
			}
		}
		u.mu.Lock()
		u.samples = samples
		u.visW, u.visH = cw, ch
		u.score = score
		u.peak.Update(score.Combined)
		u.mu.Unlock()
		if u.labelsUI.CompareAndSwap(false, true) {
			u.ui(func() {
				defer u.labelsUI.Store(false)
				u.updateLabels()
			})
		}
		cam := u.hikCam()
		if cam == nil || cam.Info().Simulated {
			time.Sleep(40 * time.Millisecond)
		}
	}
}

func liveGrabMessage(err error, misses int) (string, bool) {
	if hikcam.IsNoData(err) {
		if misses == 1 || misses%20 == 0 {
			return fmt.Sprintf("Live: waiting for a frame (GetImageBuffer timeout, x%d)", misses), true
		}
		return "", false
	}
	if misses == 1 || misses%10 == 0 {
		return "Live: " + err.Error(), true
	}
	return "", false
}

func (u *FocusUI) grabFrame() ([]uint16, int, int, error) {
	if cam := u.hikCam(); cam != nil {
		fr, err := cam.Grab(hikcam.GrabTimeout(cam.Settings().ExposureUS))
		if err != nil {
			return nil, 0, 0, err
		}
		return fr.Pix, fr.W, fr.H, nil
	}
	if u.hdr != nil && u.hdr.dev != nil && u.hdr.worker != nil {
		done := make(chan struct{})
		var jpg []byte
		var err error
		u.hdr.worker.do(func() {
			defer close(done)
			jpg, err = u.hdr.dev.LiveView()
		})
		<-done
		if err != nil {
			return nil, 0, 0, err
		}
		im, err := jpeg.Decode(bytes.NewReader(jpg))
		if err != nil {
			return nil, 0, 0, err
		}
		g := imageToGray16(im)
		b := g.Bounds()
		return gray16Pix(g), b.Dx(), b.Dy(), nil
	}
	return nil, 0, 0, fmt.Errorf("no camera")
}

func (u *FocusUI) makeDisplay(pix []uint16, w, h int) (img *image.Gray, ox, oy int, fit bool) {
	z := u.zoomFactor()
	if z == 0 {
		return hikcam.PreviewGray(pix, w, h, hikcam.LivePreviewMaxEdge), 0, 0, true
	}
	x0, y0, x1, y1 := u.visibleRect(w, h, 640, 400)
	return hikcam.CropGray8(pix, w, h, x0, y0, x1, y1), x0, y0, false
}

func (u *FocusUI) refreshDisplay() {
	u.mu.Lock()
	pix, w, h := u.lastPix, u.lastW, u.lastH
	u.mu.Unlock()
	if pix == nil || w <= 0 || h <= 0 {
		if u.view != nil {
			u.view.Invalidate()
		}
		return
	}
	img, ox, oy, fit := u.makeDisplay(pix, w, h)
	u.showDisplay(img, w, h, ox, oy, fit)
}

func (u *FocusUI) meterRect(w, h int) (x0, y0, x1, y1 int) {
	z := u.zoomFactor()
	if z < 1 {
		z = 1
	}
	return u.visibleRectAt(w, h, 640, 400, z)
}

func (u *FocusUI) applyAE(st hdrscan.ExposureState) (hdrscan.ExposureState, bool, error) {
	if cam := u.hikCam(); cam != nil {
		cur := cam.Settings()
		if math.Abs(cur.ExposureUS-st.ExposureUS)/math.Max(cur.ExposureUS, 1) < 0.01 && math.Abs(cur.GainDB-st.GainDB) < 0.05 {
			return hdrscan.ExposureState{ExposureUS: cur.ExposureUS, GainDB: cur.GainDB}, false, nil
		}
		if err := cam.SetExposureGain(st.ExposureUS, st.GainDB); err != nil {
			return hdrscan.ExposureState{ExposureUS: cur.ExposureUS, GainDB: cur.GainDB}, false, err
		}
		after := cam.Settings()
		changed := math.Abs(after.ExposureUS-cur.ExposureUS) > 0.5 || math.Abs(after.GainDB-cur.GainDB) > 0.005
		return hdrscan.ExposureState{ExposureUS: after.ExposureUS, GainDB: after.GainDB}, changed, nil
	}
	if u.hdr == nil || u.hdr.dev == nil || u.hdr.worker == nil {
		return st, false, nil
	}
	us := uint32(math.Max(1, st.ExposureUS))
	i := camera.ShutterIndex(us)
	next := camera.Exposure{ISO: 160, Shutter: camera.Shutters[i].Value}
	u.mu.Lock()
	if u.haveSavedFuji {
		next.ISO = u.savedFuji.ISO
	}
	u.mu.Unlock()
	done := make(chan struct{})
	var err error
	var applied camera.Exposure
	var changed bool
	u.hdr.worker.do(func() {
		defer close(done)
		cur, e := u.hdr.dev.Exposure()
		if e == nil && cur.Shutter == next.Shutter && cur.ISO == next.ISO {
			applied = cur
			return
		}
		got, se := u.hdr.dev.SetExposure(next)
		err = se
		if se != nil {
			if e == nil {
				applied = cur
			} else {
				applied = got
			}
			return
		}
		applied = got
		changed = e != nil || got.Shutter != cur.Shutter || got.ISO != cur.ISO
	})
	<-done
	if applied.Shutter == 0 {
		return st, false, err
	}
	return hdrscan.ExposureState{ExposureUS: float64(applied.Shutter), GainDB: 0}, changed, err
}

func (u *FocusUI) ui(f func()) {
	if u.hdr != nil {
		u.hdr.ui(f)
	}
}

func (u *FocusUI) showDisplay(img image.Image, fullW, fullH, ox, oy int, fit bool) {
	if img == nil || u.view == nil {
		return
	}
	bmp, err := walk.NewBitmapFromImage(img)
	if err != nil {
		return
	}
	u.showBitmap(bmp, fullW, fullH, ox, oy, fit)
}

func (u *FocusUI) showBitmap(bmp *walk.Bitmap, fullW, fullH, ox, oy int, fit bool) {
	if bmp == nil || u.view == nil {
		if bmp != nil {
			bmp.Dispose()
		}
		return
	}
	u.srcW, u.srcH = fullW, fullH
	u.bmpX, u.bmpY = ox, oy
	u.bmpFit = fit
	if u.bmp != nil {
		u.bmp.Dispose()
	}
	u.bmp = bmp
	u.view.Invalidate()
}

func (u *FocusUI) zoomFactor() int {
	i := int(u.zoomIdx.Load())
	if i < 0 || i >= len(zoomVals) {
		return 0
	}
	return zoomVals[i]
}

func (u *FocusUI) visibleRect(imgW, imgH, widgetW, widgetH int) (x0, y0, x1, y1 int) {
	return u.visibleRectAt(imgW, imgH, widgetW, widgetH, u.zoomFactor())
}

func (u *FocusUI) visibleRectAt(imgW, imgH, widgetW, widgetH, z int) (x0, y0, x1, y1 int) {
	if imgW <= 0 || imgH <= 0 {
		return 0, 0, 0, 0
	}
	if z == 0 {
		return 0, 0, imgW, imgH
	}
	if vwStored := int(u.viewW.Load()); vwStored > 8 {
		if vhStored := int(u.viewH.Load()); vhStored > 8 {
			widgetW, widgetH = vwStored, vhStored
		}
	}
	vw := widgetW / z
	vh := widgetH / z
	if vw < 16 {
		vw = 16
	}
	if vh < 16 {
		vh = 16
	}
	if vw > imgW {
		vw = imgW
	}
	if vh > imgH {
		vh = imgH
	}
	u.mu.Lock()
	fcx, fcy := u.cx, u.cy
	u.mu.Unlock()
	cx := int(fcx * float64(imgW))
	cy := int(fcy * float64(imgH))
	x0 = cx - vw/2
	y0 = cy - vh/2
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x0+vw > imgW {
		x0 = imgW - vw
	}
	if y0+vh > imgH {
		y0 = imgH - vh
	}
	return x0, y0, x0 + vw, y0 + vh
}

func (u *FocusUI) paint(c *walk.Canvas, _ walk.Rectangle) error {
	if u.view == nil {
		return nil
	}
	cb := u.view.ClientBoundsPixels()
	if cb.Width > 8 && cb.Height > 8 {
		u.viewW.Store(int32(cb.Width))
		u.viewH.Store(int32(cb.Height))
	}
	if bg := cachedBrush(walk.RGB(16, 16, 18)); bg != nil {
		c.FillRectanglePixels(bg, cb)
	}
	if u.bmp == nil {
		if f := cachedFont("Segoe UI", 10, 0); f != nil {
			c.DrawTextPixels("No live image.\r\nConnect the Hikrobot camera on Mono trichrome, or the scan camera on Camera, then open this page.",
				f, walk.RGB(128, 128, 136), cb, walk.TextCenter|walk.TextWordbreak)
		}
		return nil
	}
	sz := u.bmp.Size()
	z := u.zoomFactor()
	var src, dst walk.Rectangle
	markW, markH := u.srcW, u.srcH
	if markW <= 0 {
		markW = sz.Width
	}
	if markH <= 0 {
		markH = sz.Height
	}
	if z == 0 || u.bmpFit {
		dst = letterbox(cb, sz.Width, sz.Height)
		src = walk.Rectangle{Width: sz.Width, Height: sz.Height}
		markW, markH = sz.Width, sz.Height
		c.DrawImageStretchedPixels(u.bmp, dst)
	} else {
		src = walk.Rectangle{X: u.bmpX, Y: u.bmpY, Width: sz.Width, Height: sz.Height}
		dw, dh := src.Width*z, src.Height*z
		dst = walk.Rectangle{X: cb.X + (cb.Width-dw)/2, Y: cb.Y + (cb.Height-dh)/2, Width: dw, Height: dh}
		c.DrawImageStretchedPixels(u.bmp, dst)
	}
	if centerMarkOn(u.hdr) {
		paintCenterMarks(c, src, dst, markW, markH)
	}
	if pen := cachedPen(walk.RGB(80, 220, 120), 1); pen != nil {
		cx := cb.X + cb.Width/2
		cy := cb.Y + cb.Height/2
		c.DrawLinePixels(pen, walk.Point{X: cx - 18, Y: cy}, walk.Point{X: cx + 18, Y: cy})
		c.DrawLinePixels(pen, walk.Point{X: cx, Y: cy - 18}, walk.Point{X: cx, Y: cy + 18})
	}
	return nil
}

func (u *FocusUI) click(x, y int, b walk.MouseButton) {
	if b != walk.LeftButton || u.srcW == 0 || u.view == nil {
		return
	}
	cb := u.view.ClientBoundsPixels()
	if cb.Width > 8 && cb.Height > 8 {
		u.viewW.Store(int32(cb.Width))
		u.viewH.Store(int32(cb.Height))
	}
	z := u.zoomFactor()
	var ix, iy float64
	if z == 0 {
		ir := letterbox(cb, u.srcW, u.srcH)
		if ir.Width <= 0 || ir.Height <= 0 {
			return
		}
		ix = float64(x-ir.X) / float64(ir.Width)
		iy = float64(y-ir.Y) / float64(ir.Height)
		if u.zoomCB != nil {
			u.zoomIdx.Store(2)
			u.zoomCB.SetCurrentIndex(2) // 200 %
		}
	} else {
		x0, y0, x1, y1 := u.visibleRect(u.srcW, u.srcH, cb.Width, cb.Height)
		ix = (float64(x0) + float64(x-cb.X)/float64(z)) / float64(u.srcW)
		iy = (float64(y0) + float64(y-cb.Y)/float64(z)) / float64(u.srcH)
		_ = x1
		_ = y1
	}
	if ix < 0 {
		ix = 0
	}
	if iy < 0 {
		iy = 0
	}
	if ix > 1 {
		ix = 1
	}
	if iy > 1 {
		iy = 1
	}
	u.mu.Lock()
	u.cx, u.cy = ix, iy
	u.ae.Unlock()
	u.mu.Unlock()
	u.drag = true
	u.dragX, u.dragY = x, y
	u.refreshDisplay()
}

func (u *FocusUI) move(x, y int, b walk.MouseButton) {
	if !u.drag || b != walk.LeftButton || u.zoomFactor() == 0 || u.srcW == 0 {
		return
	}
	z := float64(u.zoomFactor())
	u.mu.Lock()
	u.cx -= float64(x-u.dragX) / z / float64(u.srcW)
	u.cy -= float64(y-u.dragY) / z / float64(u.srcH)
	if u.cx < 0 {
		u.cx = 0
	}
	if u.cy < 0 {
		u.cy = 0
	}
	if u.cx > 1 {
		u.cx = 1
	}
	if u.cy > 1 {
		u.cy = 1
	}
	u.ae.Unlock()
	u.mu.Unlock()
	u.dragX, u.dragY = x, y
	u.refreshDisplay()
}

func (u *FocusUI) up(_ int, _ int, _ walk.MouseButton) { u.drag = false }

func imageToGray16(src image.Image) *image.Gray16 {
	b := src.Bounds()
	dst := image.NewGray16(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	return dst
}

func gray16Pix(g *image.Gray16) []uint16 {
	n := g.Bounds().Dx() * g.Bounds().Dy()
	out := make([]uint16, n)
	i := 0
	for y := g.Bounds().Min.Y; y < g.Bounds().Max.Y; y++ {
		for x := g.Bounds().Min.X; x < g.Bounds().Max.X; x++ {
			r, _, _, _ := g.At(x, y).RGBA()
			out[i] = uint16(r)
			i++
		}
	}
	return out
}

func (u *FocusUI) Page() TabPage {
	grey := walk.RGB(110, 110, 110)
	bold := Font{Family: "Segoe UI", PointSize: 9, Bold: true}
	return TabPage{
		Title:  " Focus check ",
		Layout: HBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 8},
		Children: []Widget{
			Composite{StretchFactor: 3, Layout: VBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				CustomWidget{
					AssignTo:            &u.view,
					Paint:               u.paint,
					PaintMode:           PaintBuffered,
					InvalidatesOnResize: true,
					MinSize:             Size{Width: 420, Height: 280},
					StretchFactor:       4,
					OnMouseDown:         u.click,
					OnMouseMove:         u.move,
					OnMouseUp:           u.up,
				},
				Composite{Layout: HBox{MarginsZero: true, Spacing: 8}, Children: []Widget{
					CheckBox{AssignTo: &u.markCB, Text: "Center mark", Checked: centerMarkOn(u.hdr),
						ToolTipText: "High-contrast crosshair at the sensor centre, plus corner and edge marks at a 10% inset, to help level the film plane. Stays on the true image centre when you zoom or pan; hidden if that point is off-screen. Display only.",
						OnCheckedChanged: func() {
							if u.hdr != nil && u.markCB != nil {
								u.hdr.setCenterMark(u.markCB.Checked())
							}
						}},
					HSpacer{},
				}},
				Label{Text: "Click to zoom to 200%; drag to pan when zoomed.\nWhite marks = sensor centre and 10% inset; green crosshair = exposure & sharpness sample.", TextColor: grey},
			}},
			Composite{StretchFactor: 2, MinSize: Size{Width: 340}, MaxSize: Size{Width: 420}, Layout: VBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
				Card{
					Title:  "Focus check",
					Icon:   iconHDR,
					Layout: VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 4, Right: 8, Bottom: 6}},
					Children: []Widget{
						Label{AssignTo: &u.srcLbl, Text: "Camera: -", Font: bold, MinSize: Size{Height: 16}},
						Label{AssignTo: &u.lightLbl, Text: "Light: -", MinSize: Size{Height: 16}},
						Label{AssignTo: &u.expLbl, Text: "Auto exposure: -", MinSize: Size{Height: 34}},
						Composite{Layout: Grid{Columns: 2, MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Zoom", MinSize: Size{Width: 80}},
							ComboBox{AssignTo: &u.zoomCB, Model: zoomItems, CurrentIndex: 0, OnCurrentIndexChanged: func() {
								if u.zoomCB != nil {
									u.zoomIdx.Store(int32(max(0, u.zoomCB.CurrentIndex())))
								}
								u.unlockAE()
								u.refreshDisplay()
							}},
						}},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
							PushButton{AssignTo: &u.greenBtn, Text: "Green only", OnClicked: func() { u.setLight(0) },
								ToolTipText: "Trichrome focus baseline: only the green LED at the scan brightness."},
							PushButton{AssignTo: &u.whiteBtn, Text: "White", OnClicked: func() { u.setLight(1) },
								ToolTipText: "All three LEDs on, for focusing a white-light scan."},
						}},
						Label{Text: "Sharpness (zoomed region)", Font: bold},
						ProgressBar{AssignTo: &u.meter, MaxSize: Size{Height: 14}},
						Label{AssignTo: &u.sharpLbl, Text: "Sharpness: -", MinSize: Size{Height: 34}},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
							Label{AssignTo: &u.peakLbl, Text: "Peak hold: -", StretchFactor: 1},
							PushButton{AssignTo: &u.resetPeakBtn, Text: "Reset peak", OnClicked: func() {
								u.mu.Lock()
								u.peak.Reset()
								u.mu.Unlock()
								u.updateLabels()
							}},
						}},
						Label{Text: "Auto exposure keeps the visible region readable.\nCapture exposure is restored when you leave.",
							ToolTipText: "While this page is open the camera exposure is adjusted automatically so the visible (or zoomed) region is neither clipped nor too dark, and film grain can be judged. The capture exposure used on the scan pages is restored when you leave.",
							TextColor:   grey, MinSize: Size{Height: 30}},
						TextEdit{AssignTo: &u.logTE, ReadOnly: true, VScroll: true, MinSize: Size{Height: 120}, StretchFactor: 1},
					},
				},
				VSpacer{},
			}},
		},
	}
}

func (u *FocusUI) AfterCreate() {
	u.say(appVersion + " Focus check ready. Green is the focus baseline for trichrome; white is for white-light scans.")
	u.updateLabels()
}

var _ = color.Gray{}
