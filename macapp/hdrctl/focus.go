package hdrctl

// HDR Scan > Focus check (Windows focuspage.go FocusUI): live view with
// zoom, green-only or white light, temporary auto-exposure so film grain is
// readable, and a sharpness meter with peak hold. Prefers the Hikrobot
// monochrome camera; falls back to the X-T3's live view.
//
// Threading is the Windows one: setup, the frame loop, the focus metric,
// auto-exposure and the restore run on the Hikrobot worker (or a goroutine
// for the X-T3, whose calls go through the camera worker); light switching
// runs on the light worker; results come back with Host.Post. Values the
// Windows code reads from the UI on a worker (the X-T3 device, the light
// client, the scan light values) are read on the UI goroutine first and
// handed over.

import (
	"bytes"
	"fmt"
	"image"
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
)

// Focus is the Focus check page controller.
type Focus struct {
	hdr *Ctl

	active atomic.Bool

	// display (UI goroutine)
	Frame        image.Image
	FrameSeq     int
	SrcW, SrcH   int
	BmpX, BmpY   int  // origin of Frame in source pixels when it is a 1:1 crop
	BmpFit       bool // Frame is a downsampled full frame
	dispUI       atomic.Bool
	labelsUI     atomic.Bool
	ZoomIdx      int // the Zoom combo
	zoomIdx      atomic.Int32
	viewW, viewH atomic.Int32
	refreshSeq   atomic.Int64 // newest refreshDisplay wins
	drag         bool
	dragX, dragY int

	loopOn  atomic.Bool
	loopGen atomic.Int64

	fujiMu   sync.Mutex
	fujiDone chan struct{}
	fuji     atomic.Value // devRef: the X-T3 (read on the UI goroutine)

	mu           sync.Mutex
	cx, cy       float64 // pan centre, 0..1
	samples      []float64
	visW, visH   int
	score        hdrscan.FocusScore
	peak         hdrscan.PeakHold
	ae           hdrscan.LiveAE
	lastPix      []uint16
	lastW, lastH int

	savedHik       hikcam.Settings
	haveSavedHik   bool
	savedFuji      camera.Exposure
	haveSavedFuji  bool
	savedLight     rgblight.State
	haveSavedLight bool
	lightMode      int // 0 = green, 1 = white

	// readouts (UI goroutine)
	SrcText, SrcTip     string
	LightText           string
	ExpText, ExpTip     string
	SharpText, SharpTip string
	PeakText            string
	Meter               int // 0..1000
	LightOK             bool

	logLines []string
	LogSeq   int
}

type devRef struct{ d camera.Device }

func newFocus(h *Ctl) *Focus {
	f := &Focus{hdr: h}
	f.SrcText, f.LightText, f.ExpText, f.SharpText, f.PeakText = "Camera: -", "Light: -", "Auto exposure: -", "Sharpness: -", "Peak hold: -"
	f.fuji.Store(devRef{})
	return f
}

// ZoomItems is the Zoom combo.
func ZoomItems() []string { return zoomItems }

func (u *Focus) isActive() bool { return u.active.Load() }

// Active reports whether the page is shown (tests).
func (u *Focus) Active() bool { return u.isActive() }

func (u *Focus) say(s string) {
	if u.hdr.Log != nil {
		u.hdr.Log.Printf("Focus check: %s", s)
	}
	u.logLines = append(u.logLines, time.Now().Format("15:04:05  ")+s)
	if len(u.logLines) > maxLog {
		u.logLines = u.logLines[len(u.logLines)-maxLog:]
	}
	u.LogSeq++
}

// LogText is the page's log pane.
func (u *Focus) LogText() string { return strings.Join(u.logLines, "\n") }

func (u *Focus) ui(f func()) { u.hdr.Host.Post(f) }

func (u *Focus) mono() *Mono { return u.hdr.Mono }

func (u *Focus) hikCam() hikcam.Device {
	if u.mono() == nil {
		return nil
	}
	return u.mono().getCam()
}

// fujiDev is the X-T3 as last seen on the UI goroutine.
func (u *Focus) fujiDev() camera.Device { return u.fuji.Load().(devRef).d }

// Sync runs on the UI goroutine every frame: it publishes the X-T3 device
// to the worker goroutines (Windows reads hdr.dev there directly).
func (u *Focus) Sync() {
	if u.fujiDev() != u.hdr.dev {
		u.fuji.Store(devRef{u.hdr.dev})
	}
}

// lightClient is the connected light's client (UI goroutine), or nil.
func (u *Focus) lightClient() *rgblight.Client {
	if l := u.hdr.Light; l != nil && l.connected() {
		return l.cl
	}
	return nil
}

func (u *Focus) sourceName() string {
	if cam := u.hikCam(); cam != nil {
		return cam.Info().Label()
	}
	if d := u.fujiDev(); d != nil {
		inf := d.Info()
		return strings.TrimSpace(inf.Manufacturer + " " + inf.Model)
	}
	return ""
}

func (u *Focus) waitFujiLoopSync() {
	u.fujiMu.Lock()
	ch := u.fujiDone
	u.fujiDone = nil
	u.fujiMu.Unlock()
	if ch != nil {
		<-ch
	}
}

// Enter is enter(): the page was shown.
func (u *Focus) Enter() {
	if !u.active.CompareAndSwap(false, true) {
		return
	}
	u.Sync()
	u.mu.Lock()
	u.cx, u.cy = 0.5, 0.5
	u.peak.Reset()
	u.ae.Reset(hdrscan.ExposureState{}, hdrscan.LiveAutoExp())
	u.mu.Unlock()
	u.zoomIdx.Store(int32(max(0, u.ZoomIdx)))

	var savedHik hikcam.Settings
	var haveHik bool
	m := u.mono()
	if cam := u.hikCam(); cam != nil && m != nil && !m.running.Load() {
		savedHik = m.readSettings()
		haveHik = true
		u.mu.Lock()
		u.ae.Reset(hdrscan.ExposureState{ExposureUS: savedHik.ExposureUS, GainDB: savedHik.GainDB}, hdrscan.LiveAutoExp())
		u.mu.Unlock()
	}
	cl := u.lightClient()
	vals, ok := u.lightValues()

	gen := u.loopGen.Add(1)
	var hikGen int64
	if m != nil {
		hikGen = m.worker().Bump()
	}
	u.loopOn.Store(true)
	u.updateLabels()

	runSetup := func() {
		if m != nil {
			m.worker().StopSyncIfNeeded()
		}
		u.waitFujiLoopSync()
		if !u.active.Load() || u.loopGen.Load() != gen {
			return
		}
		if m != nil && m.worker().Gen() != hikGen {
			return
		}

		if haveHik && m != nil && !m.running.Load() {
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

		if d := u.fujiDev(); !haveHik && d != nil {
			done := make(chan struct{})
			u.hdr.worker.do(func() {
				defer close(done)
				if e, err := d.Exposure(); err == nil {
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

		if cl != nil && !cl.Closed() {
			if st, err := cl.ReadState(); err == nil {
				u.mu.Lock()
				u.savedLight = st
				u.haveSavedLight = true
				u.mu.Unlock()
			} else {
				u.mu.Lock()
				u.haveSavedLight = false
				u.mu.Unlock()
			}
			u.applyLightMode(0, cl, vals, ok)
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

	if m != nil {
		m.worker().Do(runSetup)
	} else {
		go runSetup()
	}
}

// Leave is leave(): another page was shown.
func (u *Focus) Leave() { u.LeaveThen(nil) }

// LeaveThen is Leave with then run on the UI goroutine after the restore
// (false: the page was not active and then is not run). Windows starts the
// Mono trichrome preview right after leave(); the restore's StopSync then
// bumps the Hikrobot generation and cancels that preview, so the Mac
// starts it after the restore instead.
func (u *Focus) LeaveThen(then func()) bool {
	if !u.active.Swap(false) {
		return false
	}
	u.loopOn.Store(false)
	u.loopGen.Add(1)
	m := u.mono()
	if m != nil {
		m.stopPreviewAsync()
	}
	cl := u.lightClient()

	runRestore := func() {
		if m != nil {
			m.worker().StopSync()
		}
		u.waitFujiLoopSync()
		u.restoreExposure()
		u.restoreLight(cl)
		u.ui(func() {
			u.say("Left Focus check: previous capture exposure restored.")
			if then != nil {
				then()
			}
		})
	}

	if m != nil {
		m.worker().Do(runRestore)
	} else {
		go runRestore()
	}
	return true
}

func (u *Focus) restoreExposure() {
	u.mu.Lock()
	haveHik, savedHik := u.haveSavedHik, u.savedHik
	haveFuji, savedFuji := u.haveSavedFuji, u.savedFuji
	u.haveSavedHik = false
	u.haveSavedFuji = false
	u.mu.Unlock()

	if haveHik {
		if cam := u.hikCam(); cam != nil && (u.mono() == nil || !u.mono().running.Load()) {
			if err := cam.Apply(savedHik); err != nil {
				msg := "Could not restore Hikrobot exposure: " + err.Error()
				u.ui(func() { u.say(msg) })
			} else {
				msg := fmt.Sprintf("Restored Hikrobot exposure: %.0f us, %.2f dB", savedHik.ExposureUS, savedHik.GainDB)
				u.ui(func() { u.say(msg) })
			}
		}
	}
	if d := u.fujiDev(); haveFuji && d != nil {
		done := make(chan struct{})
		u.hdr.worker.do(func() {
			defer close(done)
			if _, err := d.SetExposure(savedFuji); err != nil {
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

func (u *Focus) restoreLight(cl *rgblight.Client) {
	u.mu.Lock()
	haveLight, saved := u.haveSavedLight, u.savedLight
	u.haveSavedLight = false
	u.mu.Unlock()
	if !haveLight || cl == nil || cl.Closed() {
		return
	}
	if _, err := cl.SetAllConfirmed(saved.On, saved.Value); err != nil {
		msg := "Could not restore the light: " + err.Error()
		u.ui(func() { u.say(msg) })
		return
	}
	u.ui(func() {
		if l := u.hdr.Light; l != nil {
			l.showState(saved, false)
		}
	})
}

// lightValues is scanLightValues (UI goroutine).
func (u *Focus) lightValues() ([3]int, bool) {
	if m := u.mono(); m != nil {
		v, _, ok := m.scanLightValues()
		return v, ok
	}
	return [3]int{}, false
}

func (u *Focus) applyLightMode(mode int, cl *rgblight.Client, vals [3]int, ok bool) {
	u.mu.Lock()
	u.lightMode = mode
	u.mu.Unlock()
	if cl == nil || cl.Closed() {
		u.ui(func() {
			u.say("RGB light not connected - cannot switch green/white.")
			u.updateLabels()
		})
		return
	}
	if !ok {
		if cs := cl.CachedState(); cs != nil {
			vals = cs.Value
		} else {
			vals = [3]int{50, 50, 50}
		}
	}
	sw := &rgblight.ChannelSwitch{C: cl, Values: vals}
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
	cs := cl.CachedState()
	u.ui(func() {
		if err != nil {
			u.say("Light switch failed: " + err.Error())
		} else {
			u.say(okMsg)
			if cs != nil && u.hdr.Light != nil {
				u.hdr.Light.showState(*cs, false)
			}
		}
		u.updateLabels()
	})
}

func (u *Focus) unlockAE() {
	u.mu.Lock()
	u.ae.Unlock()
	u.mu.Unlock()
}

// SetLight is the Green only (0) and White (1) buttons.
func (u *Focus) SetLight(mode int) {
	u.mu.Lock()
	u.lightMode = mode
	u.ae.Unlock()
	u.mu.Unlock()
	u.updateLabels()
	cl := u.lightClient()
	vals, ok := u.lightValues()
	if l := u.hdr.Light; l != nil && l.lw != nil {
		l.lw.do(func() { u.applyLightMode(mode, cl, vals, ok) })
		return
	}
	go u.applyLightMode(mode, cl, vals, ok)
}

// ResetPeak is the Reset peak button.
func (u *Focus) ResetPeak() {
	u.mu.Lock()
	u.peak.Reset()
	u.mu.Unlock()
	u.updateLabels()
}

// ZoomChanged: the Zoom combo changed ZoomIdx.
func (u *Focus) ZoomChanged() {
	u.zoomIdx.Store(int32(max(0, u.ZoomIdx)))
	u.unlockAE()
	u.refreshDisplay()
}

// Refresh is updateLabels for the view (UI goroutine).
func (u *Focus) Refresh() {
	u.Sync()
	u.updateLabels()
}

func (u *Focus) updateLabels() {
	u.mu.Lock()
	lightMode := u.lightMode
	aeState := u.ae.State
	aeWhy := u.ae.Why
	score := u.score
	peak := u.peak
	u.mu.Unlock()

	if n := u.sourceName(); n == "" {
		u.SrcText, u.SrcTip = "Camera: none connected", "Open the Hikrobot camera on Mono trichrome, or the Fuji body on Camera."
	} else {
		u.SrcText, u.SrcTip = "Camera: "+n, "Camera: "+n
	}
	mode := "green only"
	if lightMode != 0 {
		mode = "white"
	}
	u.LightOK = u.lightClient() != nil
	if !u.LightOK {
		u.LightText = "Light: not connected (wanted: " + mode + ")"
	} else {
		u.LightText = "Light: " + mode
	}
	if aeWhy == "" {
		u.ExpText = fmt.Sprintf("Auto exposure: %.0f us, %.2f dB\n", aeState.ExposureUS, aeState.GainDB)
		u.ExpTip = fmt.Sprintf("Auto exposure: %.0f us, %.2f dB", aeState.ExposureUS, aeState.GainDB)
	} else {
		u.ExpText = fmt.Sprintf("Auto exposure: %.0f us, %.2f dB\n(%s)", aeState.ExposureUS, aeState.GainDB, aeWhy)
		u.ExpTip = fmt.Sprintf("Auto exposure: %.0f us, %.2f dB (%s)", aeState.ExposureUS, aeState.GainDB, aeWhy)
	}
	u.SharpText = fmt.Sprintf("Sharpness: %.4f\nLap %.4f, grain %.4f", score.Combined, score.Laplacian, score.Grain)
	u.SharpTip = fmt.Sprintf("Sharpness: %.4f (Lap %.4f, grain %.4f)", score.Combined, score.Laplacian, score.Grain)
	if peak.Have {
		u.PeakText = fmt.Sprintf("Peak hold: %.4f", peak.Peak)
	} else {
		u.PeakText = "Peak hold: -"
	}
	v := int(score.Combined * 20000)
	if v < 0 {
		v = 0
	}
	if v > 1000 {
		v = 1000
	}
	u.Meter = v
}

func (u *Focus) startLoop(gen, hikGen int64) {
	if !u.active.Load() || u.loopGen.Load() != gen {
		return
	}
	m := u.mono()
	if cam := u.hikCam(); cam != nil && m != nil {
		if m.running.Load() {
			return
		}
		if err := cam.StartGrab(); err != nil {
			msg := "Focus grab: " + err.Error()
			u.ui(func() { u.say(msg) })
			return
		}
		if !m.worker().StartLoop(hikGen, func(valid func() bool) {
			defer func() { _ = cam.StopGrab() }()
			u.loopBody(func() bool {
				return valid() && u.loopOn.Load() && u.active.Load() && u.loopGen.Load() == gen
			})
		}) {
			_ = cam.StopGrab()
		}
		return
	}
	if u.fujiDev() != nil {
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

func (u *Focus) loopBody(stillValid func() bool) {
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
			u.ui(func() {
				defer u.dispUI.Store(false)
				if !stillValid() {
					return
				}
				u.showDisplay(img, w, h, ox, oy, fit)
			})
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

func (u *Focus) grabFrame() ([]uint16, int, int, error) {
	if cam := u.hikCam(); cam != nil {
		fr, err := cam.Grab(hikcam.GrabTimeout(cam.Settings().ExposureUS))
		if err != nil {
			return nil, 0, 0, err
		}
		return fr.Pix, fr.W, fr.H, nil
	}
	if d := u.fujiDev(); d != nil {
		done := make(chan struct{})
		var jpg []byte
		var err error
		u.hdr.worker.do(func() {
			defer close(done)
			jpg, err = d.LiveView()
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

func (u *Focus) makeDisplay(pix []uint16, w, h int) (img *image.Gray, ox, oy int, fit bool) {
	z := u.zoomFactor()
	if z == 0 {
		return hikcam.PreviewGray(pix, w, h, hikcam.LivePreviewMaxEdge), 0, 0, true
	}
	x0, y0, x1, y1 := u.visibleRect(w, h, 640, 400)
	return hikcam.CropGray8(pix, w, h, x0, y0, x1, y1), x0, y0, false
}

// refreshDisplay redraws the last frame after a zoom or pan. Windows builds
// the bitmap on the UI thread; here it is built on a goroutine (a full
// 6016 x 4096 frame takes tens of ms) and posted back.
func (u *Focus) refreshDisplay() {
	u.mu.Lock()
	pix, w, h := u.lastPix, u.lastW, u.lastH
	u.mu.Unlock()
	if pix == nil || w <= 0 || h <= 0 {
		return
	}
	seq := u.refreshSeq.Add(1)
	go func() {
		img, ox, oy, fit := u.makeDisplay(pix, w, h)
		u.ui(func() {
			if u.refreshSeq.Load() == seq {
				u.showDisplay(img, w, h, ox, oy, fit)
			}
		})
	}()
}

func (u *Focus) meterRect(w, h int) (x0, y0, x1, y1 int) {
	z := u.zoomFactor()
	if z < 1 {
		z = 1
	}
	return u.visibleRectAt(w, h, 640, 400, z)
}

func (u *Focus) applyAE(st hdrscan.ExposureState) (hdrscan.ExposureState, bool, error) {
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
	d := u.fujiDev()
	if d == nil {
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
		cur, e := d.Exposure()
		if e == nil && cur.Shutter == next.Shutter && cur.ISO == next.ISO {
			applied = cur
			return
		}
		got, se := d.SetExposure(next)
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

// showDisplay is showBitmap: the new frame for the view (UI goroutine).
func (u *Focus) showDisplay(img image.Image, fullW, fullH, ox, oy int, fit bool) {
	if img == nil {
		return
	}
	u.SrcW, u.SrcH = fullW, fullH
	u.BmpX, u.BmpY = ox, oy
	u.BmpFit = fit
	u.Frame = img
	u.FrameSeq++
}

func (u *Focus) zoomFactor() int {
	i := int(u.zoomIdx.Load())
	if i < 0 || i >= len(zoomVals) {
		return 0
	}
	return zoomVals[i]
}

// ZoomFactor is the current zoom (0 = Fit).
func (u *Focus) ZoomFactor() int { return u.zoomFactor() }

func (u *Focus) visibleRect(imgW, imgH, widgetW, widgetH int) (x0, y0, x1, y1 int) {
	return u.visibleRectAt(imgW, imgH, widgetW, widgetH, u.zoomFactor())
}

func (u *Focus) visibleRectAt(imgW, imgH, widgetW, widgetH, z int) (x0, y0, x1, y1 int) {
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

// SetViewSize records the view's size in pixels (paint).
func (u *Focus) SetViewSize(w, h int) {
	if w > 8 && h > 8 {
		u.viewW.Store(int32(w))
		u.viewH.Store(int32(h))
	}
}

// ViewSize is the last view size in pixels.
func (u *Focus) ViewSize() (int, int) { return int(u.viewW.Load()), int(u.viewH.Load()) }

// Click is the view's mouse-down at (x, y) in view pixels; ir is where the
// full frame is letterboxed at Fit.
func (u *Focus) Click(x, y, viewW, viewH int, ir image.Rectangle) {
	if u.SrcW == 0 {
		return
	}
	u.SetViewSize(viewW, viewH)
	z := u.zoomFactor()
	var ix, iy float64
	if z == 0 {
		if ir.Dx() <= 0 || ir.Dy() <= 0 {
			return
		}
		ix = float64(x-ir.Min.X) / float64(ir.Dx())
		iy = float64(y-ir.Min.Y) / float64(ir.Dy())
		u.ZoomIdx = 2 // 200 %
		u.zoomIdx.Store(2)
	} else {
		x0, y0, _, _ := u.visibleRect(u.SrcW, u.SrcH, viewW, viewH)
		ix = (float64(x0) + float64(x)/float64(z)) / float64(u.SrcW)
		iy = (float64(y0) + float64(y)/float64(z)) / float64(u.SrcH)
	}
	ix = math.Min(1, math.Max(0, ix))
	iy = math.Min(1, math.Max(0, iy))
	u.mu.Lock()
	u.cx, u.cy = ix, iy
	u.ae.Unlock()
	u.mu.Unlock()
	u.drag = true
	u.dragX, u.dragY = x, y
	u.refreshDisplay()
}

// Move is the view's mouse-move while the button is down (pan).
func (u *Focus) Move(x, y int) {
	if !u.drag || u.zoomFactor() == 0 || u.SrcW == 0 {
		return
	}
	z := float64(u.zoomFactor())
	u.mu.Lock()
	u.cx -= float64(x-u.dragX) / z / float64(u.SrcW)
	u.cy -= float64(y-u.dragY) / z / float64(u.SrcH)
	u.cx = math.Min(1, math.Max(0, u.cx))
	u.cy = math.Min(1, math.Max(0, u.cy))
	u.ae.Unlock()
	u.mu.Unlock()
	u.dragX, u.dragY = x, y
	u.refreshDisplay()
}

// Up is the view's mouse-up.
func (u *Focus) Up() { u.drag = false }

// Start is AfterCreate.
func (u *Focus) Start() {
	u.say(appVersion + " Focus check ready. Green is the focus baseline for trichrome; white is for white-light scans.")
	u.updateLabels()
}
