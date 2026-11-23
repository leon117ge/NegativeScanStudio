package main

// Mono trichrome page (v0.97): capture R, G and B with a Hikrobot monochrome
// camera while switching the RGB light, then merge to a linear 16-bit RGB
// TIFF. Lateral chromatic aberration of R and B is measured against green
// and optionally corrected at merge time.

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"basetool/hdrscan"
	"basetool/hikbase"
	"basetool/hikcam"
	"basetool/monocap"
	"basetool/rgblight"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type MonoUI struct {
	hdr   *HDRUI
	light *LightUI

	camMu    sync.RWMutex
	cam      hikcam.Device
	devices  []hikcam.Info // real cameras from the last refresh; index 0 in the combo is always the simulator
	sdkMsg   string
	levelSim hikcam.Device

	ctrlOnce sync.Once
	ctrl     hikcam.Controller

	devCB, fmtCB, trigCB, presetCB, capModeCB, brCB  *walk.ComboBox
	expNE, gainNE, blackNE                           *walk.NumberEdit
	wNE, hNE, xNE, yNE                               *walk.NumberEdit
	fullCB, gammaCB, caCB, markCB, meterEachCB       *walk.CheckBox
	connBtn, applyBtn, capBtn, stopBtn, refreshBtn   *walk.PushButton
	camLbl, sdkLbl, lightLbl, valLbl, whyLbl, outLbl *walk.Label
	baseExpLbl                                       *walk.Label
	fpeNE                                            *walk.NumberEdit // v1.10 frames per exposure
	loading, optsLoaded                              bool             // saved capture options: being restored / restored
	repTE, logTE                                     *walk.TextEdit
	pb                                               *walk.ProgressBar
	view                                             *walk.CustomWidget
	bmp                                              *walk.Bitmap
	viewWhat                                         string

	previewOn atomic.Bool
	previewUI atomic.Bool // true while a live bitmap is being posted to the UI
	running   atomic.Bool
	connBusy  atomic.Bool

	cancelMu sync.Mutex
	cancel   func()
	why      string
}

func (u *MonoUI) worker() *hikcam.Controller {
	u.ctrlOnce.Do(func() {
		u.ctrl.OnPanic = func(r any, st string) {
			if u.hdr != nil {
				u.hdr.reportPanic("Hikrobot job", r, st)
			}
		}
	})
	return &u.ctrl
}

func (u *MonoUI) getCam() hikcam.Device {
	u.camMu.RLock()
	defer u.camMu.RUnlock()
	return u.cam
}

func (u *MonoUI) setCam(d hikcam.Device) {
	u.camMu.Lock()
	u.cam = d
	u.camMu.Unlock()
}

func (u *MonoUI) setCamIfNil(d hikcam.Device) {
	u.camMu.Lock()
	if u.cam == nil {
		u.cam = d
	}
	u.camMu.Unlock()
}

func (u *MonoUI) getSDKMsg() string {
	u.camMu.RLock()
	defer u.camMu.RUnlock()
	return u.sdkMsg
}

func (u *MonoUI) setSDK(msg string, devs []hikcam.Info) {
	u.camMu.Lock()
	u.sdkMsg = msg
	if devs != nil {
		u.devices = append([]hikcam.Info(nil), devs...)
	} else {
		u.devices = nil
	}
	u.camMu.Unlock()
}

func (u *MonoUI) getDevices() []hikcam.Info {
	u.camMu.RLock()
	defer u.camMu.RUnlock()
	return append([]hikcam.Info(nil), u.devices...)
}

func (u *MonoUI) setCancel(fn func()) {
	u.cancelMu.Lock()
	u.cancel = fn
	u.cancelMu.Unlock()
}

func (u *MonoUI) hasCancel() bool {
	u.cancelMu.Lock()
	defer u.cancelMu.Unlock()
	return u.cancel != nil
}

func (u *MonoUI) stopCapture() {
	u.cancelMu.Lock()
	fn := u.cancel
	u.cancelMu.Unlock()
	if fn != nil {
		u.say("Stopping capture after the current step ...")
		if u.stopBtn != nil {
			u.stopBtn.SetEnabled(false)
		}
		fn()
	}
}

func (u *MonoUI) say(s string) {
	if u.hdr != nil && u.hdr.log != nil {
		u.hdr.log.Printf("Mono trichrome: %s", s)
	}
	if u.logTE != nil {
		tx := u.logTE.Text()
		if len(tx) > 40000 {
			u.logTE.SetText(tx[len(tx)-20000:])
		}
		u.logTE.AppendText(time.Now().Format("15:04:05  ") + s + "\r\n")
		u.logTE.ScrollToCaret()
	}
}

func (u *MonoUI) ui(f func()) {
	if u.hdr != nil {
		u.hdr.ui(f)
	}
}

func scanLightValues(h *HDRUI, lu *LightUI) ([3]int, string, bool) {
	if h != nil {
		if ho := h.handoff; ho != nil {
			return ho.Values, ho.Source, true
		}
	}
	if lu != nil && lu.cl != nil {
		if cs := lu.cl.CachedState(); cs != nil {
			return cs.Value, "the light's current values", true
		}
	}
	return [3]int{}, "", false
}

func (u *MonoUI) reason() (short, full string) {
	cam := u.getCam()
	switch {
	case u.running.Load():
		return "scan in progress", "A mono trichrome scan is running."
	case u.connBusy.Load():
		return "camera busy", "Camera operation in progress."
	case cam == nil:
		return "camera not connected", "Hikrobot camera not connected (Connect above, or use the simulator)."
	case u.light == nil || !u.light.connected():
		if _, sim := cam.(*hikcam.Sim); sim {
			return "", ""
		}
		return "RGB light not connected", "RGB light not connected (Camera and Light > Light > Connect)."
	case u.light.busy:
		return "RGB light busy", "RGB light busy."
	}
	return "", ""
}

func (u *MonoUI) updateButtons() {
	if u.connBtn == nil {
		return
	}
	cam := u.getCam()
	running := u.running.Load()
	connBusy := u.connBusy.Load()
	u.connBtn.SetText(map[bool]string{true: "Disconnect", false: "Connect"}[cam != nil])
	u.connBtn.SetEnabled(!running && !connBusy)
	if u.devCB != nil {
		u.devCB.SetEnabled(cam == nil && !running && !connBusy)
	}
	if u.refreshBtn != nil {
		u.refreshBtn.SetEnabled(cam == nil && !running && !connBusy)
	}
	if u.applyBtn != nil {
		u.applyBtn.SetEnabled(cam != nil && !running && !connBusy)
	}
	why, whyFull := u.reason()
	if u.capBtn != nil {
		u.capBtn.SetEnabled(why == "" && !running)
	}
	if u.stopBtn != nil {
		u.stopBtn.SetEnabled(u.hasCancel())
	}
	if u.sdkLbl != nil {
		sdk := u.getSDKMsg()
		setMonoStatus(u.sdkLbl, "SDK: "+sdk)
	}
	if cam == nil {
		setMonoStatus(u.camLbl, "Camera: not connected")
	} else {
		setMonoStatus(u.camLbl, "Camera: "+cam.Info().Label())
	}
	if u.light == nil || !u.light.connected() {
		setMonoStatus(u.lightLbl, "Light: not connected")
	} else if cs := u.light.cl.CachedState(); cs != nil {
		setMonoStatus(u.lightLbl, "Light now: "+cs.String())
	} else {
		setMonoStatus(u.lightLbl, "Light: connected")
	}
	if v, src, ok := scanLightValues(u.hdr, u.light); ok {
		setMonoStatusTip(u.valLbl, fmt.Sprintf("Scan values: R %d  G %d  B %d", v[0], v[1], v[2]),
			fmt.Sprintf("Scan values: R %d  G %d  B %d (%s)", v[0], v[1], v[2], src))
	} else {
		setMonoStatus(u.valLbl, "Scan values: -")
	}
	if u.hdr != nil {
		roll := u.hdr.cfg.Roll
		if roll == "" {
			roll = "none"
		}
		setMonoStatusTip(u.outLbl, fmt.Sprintf("Output: %s  (roll: %s)", u.hdr.outDir(), roll),
			fmt.Sprintf("Output: %s, roll %s (same folder as White light scan)", u.hdr.outDir(), roll))
	}
	if why != u.why {
		if why == "" {
			u.say("Capture ready.")
		} else {
			u.say("Capture disabled: " + whyFull)
		}
		u.why = why
	}
	if u.brCB != nil {
		u.brCB.SetEnabled(!running && u.capModeCB != nil && u.capModeCB.CurrentIndex() == 1)
	}
	if u.whyLbl != nil {
		if why == "" {
			setMonoStatusTip(u.whyLbl, u.readyStatusText(), u.readyStatusTooltip())
		} else {
			setMonoStatusTip(u.whyLbl, "Capture disabled: "+why, "Capture disabled: "+whyFull)
		}
	}
}

func (u *MonoUI) refreshDevices() {
	if u.refreshBtn != nil {
		u.refreshBtn.SetEnabled(false)
	}
	u.worker().Do(func() {
		res := hikcam.Enumerate()
		u.setSDK(res.Message(), res.Cameras)
		items := []string{hikcam.OpenSim().Info().Label()}
		for _, d := range res.Cameras {
			items = append(items, d.Label())
		}
		u.ui(func() {
			if u.devCB != nil {
				cur := u.devCB.CurrentIndex()
				u.devCB.SetModel(items)
				if cur >= 0 && cur < len(items) {
					u.devCB.SetCurrentIndex(cur)
				} else if len(items) > 0 {
					u.devCB.SetCurrentIndex(0)
				}
			}
			u.updateButtons()
			if res.Err != nil {
				u.say(res.Message())
			} else if len(res.Cameras) == 0 {
				u.say(res.Message() + ". The simulator is always available.")
			} else {
				u.say(fmt.Sprintf("%s.", res.Message()))
			}
		})
	})
}

func (u *MonoUI) toggleConnect() {
	if u.running.Load() || u.connBusy.Load() {
		return
	}
	if cam := u.getCam(); cam != nil {
		u.connBusy.Store(true)
		u.stopPreviewAsync()
		u.setCam(nil)
		u.updateButtons()
		u.worker().Do(func() {
			u.worker().StopSync()
			_ = cam.Close()
			u.ui(func() {
				u.connBusy.Store(false)
				u.say("Disconnected.")
				u.updateButtons()
			})
		})
		return
	}
	idx := 0
	if u.devCB != nil {
		idx = u.devCB.CurrentIndex()
	}
	devs := u.getDevices()
	var realInfo *hikcam.Info
	if idx > 0 {
		realIdx := idx - 1
		if realIdx < 0 || realIdx >= len(devs) {
			u.say("Select a camera first.")
			return
		}
		info := devs[realIdx]
		realInfo = &info
	}
	st := u.readSettings()
	u.connBusy.Store(true)
	u.stopPreviewAsync()
	u.updateButtons()
	u.worker().Do(func() {
		u.worker().StopSync()
		var dev hikcam.Device
		var err error
		if realInfo == nil {
			dev = hikcam.OpenSim()
		} else {
			dev, err = hikcam.OpenReal(*realInfo)
		}
		if err != nil {
			u.ui(func() {
				u.connBusy.Store(false)
				u.say("Connect failed: " + err.Error())
				u.updateButtons()
				walk.MsgBox(u.hdr.win(), "Hikrobot camera", err.Error(), walk.MsgBoxIconWarning)
			})
			return
		}
		applyErr := dev.Apply(st)
		u.setCam(dev)
		u.ui(func() {
			u.connBusy.Store(false)
			u.say("Opened " + dev.Info().Label())
			if applyErr != nil {
				u.say("Apply settings: " + applyErr.Error())
			}
			u.updateButtons()
			u.startPreview()
		})
	})
}

func (u *MonoUI) readSettings() hikcam.Settings {
	st := hikcam.DefaultSettings()
	if u.expNE != nil {
		st.ExposureUS = u.expNE.Value()
	}
	if u.gainNE != nil {
		st.GainDB = u.gainNE.Value()
	}
	if u.blackNE != nil {
		st.BlackLevel = u.blackNE.Value()
	}
	if u.fmtCB != nil {
		i := u.fmtCB.CurrentIndex()
		if i >= 0 && i < len(hikcam.PixelFormatChoices) {
			st.PixelFormat = hikcam.PixelFormatChoices[i]
		}
	}
	if u.presetCB != nil && u.presetCB.CurrentIndex() == 1 {
		st.QualityPreset = true
	}
	if u.gammaCB != nil {
		st.GammaEnable = u.gammaCB.Checked()
	}
	if u.fullCB != nil {
		st.FullFrame = u.fullCB.Checked()
	}
	if u.wNE != nil {
		st.Width = int(u.wNE.Value())
	}
	if u.hNE != nil {
		st.Height = int(u.hNE.Value())
	}
	if u.xNE != nil {
		st.OffsetX = int(u.xNE.Value())
	}
	if u.yNE != nil {
		st.OffsetY = int(u.yNE.Value())
	}
	if u.trigCB != nil {
		st.SoftwareTrigger = u.trigCB.CurrentIndex() == 1
	}
	return st.Clamp()
}

// applyScanExposure writes a common film-base-leveling exposure into the
// shared Hikrobot exposure (Mono Trichrome and Camera and Light). Gain stays at 0 dB.
func (u *MonoUI) applyScanExposure(us, gain float64) {
	if u == nil {
		return
	}
	if u.hdr != nil {
		u.hdr.setSharedHikExposure(us, "leveling")
	} else if u.expNE != nil {
		u.expNE.SetValue(us)
	}
	if u.gainNE != nil {
		u.gainNE.SetValue(gain)
	}
	u.say(fmt.Sprintf("Scan exposure updated from film-base leveling: %.0f us, %.2f dB.", us, gain))
}

func applyLiveSettings(cam hikcam.Device, base hikcam.Settings, fullRes bool) (string, error) {
	if cam == nil {
		return "", hikcam.ErrNotOpen
	}
	bin := 2
	if fullRes {
		bin = 1
	}
	if sim, ok := cam.(*hikcam.Sim); ok {
		sim.FlatField = false
	}
	live := hikcam.LiveViewSettings(base, bin)
	if err := cam.Apply(live); err != nil {
		return "", err
	}
	got := cam.Settings()
	extra := ""
	if fullRes {
		extra = ", full resolution"
	}
	return "Live view: " + got.LiveSummary() + extra, nil
}

func (u *MonoUI) usingBestScan() bool {
	return u.presetCB != nil && u.presetCB.CurrentIndex() == 1
}

func (u *MonoUI) fillBestScanUI() {
	st := hikcam.BestScanQuality()
	if u.fmtCB != nil {
		u.fmtCB.SetCurrentIndex(hikcam.PixelFormatIndex(st.PixelFormat))
	}
	if u.expNE != nil {
		us := st.ExposureUS
		if u.hdr != nil && u.hdr.cfg.HikExposureUS >= hikcam.CS240ExpMinUS {
			us = hikbase.ClampExposureUS(u.hdr.cfg.HikExposureUS)
		}
		u.expNE.SetValue(us)
	}
	if u.gainNE != nil {
		u.gainNE.SetValue(st.GainDB)
	}
	if u.blackNE != nil {
		u.blackNE.SetValue(st.BlackLevel)
	}
	if u.gammaCB != nil {
		u.gammaCB.SetChecked(false)
	}
	if u.fullCB != nil {
		u.fullCB.SetChecked(true)
	}
	if u.trigCB != nil {
		u.trigCB.SetCurrentIndex(1)
	}
	if u.wNE != nil {
		u.wNE.SetValue(float64(hikcam.CS240Width))
	}
	if u.hNE != nil {
		u.hNE.SetValue(float64(hikcam.CS240Height))
	}
	if u.xNE != nil {
		u.xNE.SetValue(0)
	}
	if u.yNE != nil {
		u.yNE.SetValue(0)
	}
}

func (u *MonoUI) onPresetChanged() {
	if u.presetCB == nil {
		return
	}
	if u.presetCB.CurrentIndex() == 1 {
		u.fillBestScanUI()
		u.say("Preset: Best scan quality (MV-CS240-90UM). R, G and B use the shared exposure (gain 0 dB) unless Auto-meter each channel is checked.")
	} else {
		u.say("Preset: Custom camera settings.")
	}
	u.updateCaptureLabel()
}

func (u *MonoUI) captureEVs() []int {
	if u.capModeCB != nil && u.capModeCB.CurrentIndex() == 1 {
		br := 0
		if u.brCB != nil {
			br = clampIdx(u.brCB.CurrentIndex(), len(monoBracketEVs))
		}
		return append([]int(nil), monoBracketEVs[br]...)
	}
	return []int{0}
}

func (u *MonoUI) readyStatusText() string {
	evs := u.captureEVs()
	meterEach := u.hdr != nil && u.hdr.hikMeterEach()
	if meterEach {
		return "Ready. Auto-meter each channel is on."
	}
	baseUS := hikbase.DefaultExposureUS
	if u.hdr != nil {
		baseUS = u.hdr.sharedHikExposure()
	}
	return fmt.Sprintf("Ready. Capture about %.1f s (exposure + readout, excludes light settle).",
		monocap.EstimateCapture(baseUS, evs, u.framesPerExposure()).Seconds())
}

func (u *MonoUI) readyStatusTooltip() string {
	evs := u.captureEVs()
	meterEach := u.hdr != nil && u.hdr.hikMeterEach()
	if meterEach {
		return fmt.Sprintf("Ready. Auto-meter each channel is on (each channel meters separately from %.0f us). Mode: %s.",
			u.hdr.sharedHikExposure(), evText(evs))
	}
	baseUS := hikbase.DefaultExposureUS
	if u.hdr != nil {
		baseUS = u.hdr.sharedHikExposure()
	}
	return fmt.Sprintf("Ready. Shared exposure %.0f us, 0 dB (%s). Estimate %.1f s covers exposure + readout only; RGB light settling, HDR merge, CA correction and TIFF write add extra time.",
		baseUS, evText(evs), monocap.EstimateCapture(baseUS, evs, u.framesPerExposure()).Seconds())
}

func (u *MonoUI) updateCaptureLabel() {
	u.saveCaptureOptions()
	evs := u.captureEVs()
	if u.brCB != nil {
		u.brCB.SetEnabled(!u.running.Load() && u.capModeCB != nil && u.capModeCB.CurrentIndex() == 1)
	}
	if u.capBtn != nil {
		if len(evs) > 1 {
			u.capBtn.SetText("Capture mono trichrome HDR")
		} else {
			u.capBtn.SetText("Capture mono trichrome")
		}
	}
	if u.whyLbl != nil {
		why, _ := u.reason()
		if why == "" {
			setMonoStatusTip(u.whyLbl, u.readyStatusText(), u.readyStatusTooltip())
		}
	}
}

func (u *MonoUI) stopPreviewAsync() {
	u.previewOn.Store(false)
	u.worker().Bump()
}

func (u *MonoUI) stopPreview() {
	u.stopPreviewAsync()
}

func (u *MonoUI) startPreview() {
	cam := u.getCam()
	if cam == nil || u.running.Load() || u.connBusy.Load() {
		return
	}
	st := u.readSettings()
	gen := u.worker().Bump()
	u.worker().Do(func() {
		u.worker().StopSyncIfNeeded()
		if u.worker().Gen() != gen || u.running.Load() || u.connBusy.Load() {
			return
		}
		cur := u.getCam()
		if cur == nil || cur != cam {
			return
		}
		summary, err := applyLiveSettings(cur, st, false)
		if err != nil {
			msg := "Live settings: " + err.Error()
			u.ui(func() { u.say(msg) })
		} else if summary != "" {
			u.ui(func() { u.say(summary) })
		}
		if err := cur.StartGrab(); err != nil {
			msg := "Live grab: " + err.Error()
			u.ui(func() { u.say(msg) })
			return
		}
		u.previewOn.Store(true)
		if !u.worker().StartLoop(gen, func(valid func() bool) {
			defer func() {
				u.previewOn.Store(false)
				_ = cur.StopGrab()
			}()
			misses := 0
			for valid() && u.previewOn.Load() && u.getCam() == cur {
				fr, err := cur.Grab(hikcam.GrabTimeout(cur.Settings().ExposureUS))
				if err != nil {
					if !valid() || !u.previewOn.Load() {
						break
					}
					misses++
					if msg, log := liveGrabMessage(err, misses); log {
						m := msg
						u.ui(func() { u.say(m) })
					}
					time.Sleep(50 * time.Millisecond)
					continue
				}
				misses = 0
				if u.previewUI.CompareAndSwap(false, true) {
					img := hikcam.PreviewGray(fr.Pix, fr.W, fr.H, hikcam.LivePreviewMaxEdge)
					bmp, e := walk.NewBitmapFromImage(img)
					if e != nil {
						u.previewUI.Store(false)
					} else {
						what := fmt.Sprintf("Live  %dx%d  %s  %.0f us", fr.W, fr.H, fr.PixelFormat, fr.ExposureUS)
						u.ui(func() {
							defer u.previewUI.Store(false)
							if u.getCam() != cur || !valid() || !u.previewOn.Load() {
								bmp.Dispose()
								return
							}
							if u.bmp != nil {
								u.bmp.Dispose()
							}
							u.bmp = bmp
							u.viewWhat = what
							if u.view != nil {
								u.view.Invalidate()
							}
						})
					}
				}
				if cur.Info().Simulated {
					time.Sleep(40 * time.Millisecond)
				}
			}
		}) {
			u.previewOn.Store(false)
			_ = cur.StopGrab()
		}
	})
}

func grayImage(fr *hikcam.Frame) *image.Gray {
	return hikcam.PreviewGray(fr.Pix, fr.W, fr.H, 0)
}

func (u *MonoUI) paint(c *walk.Canvas, _ walk.Rectangle) error {
	if u.view == nil {
		return nil
	}
	cb := u.view.ClientBoundsPixels()
	if bg := cachedBrush(walk.RGB(22, 22, 25)); bg != nil {
		c.FillRectanglePixels(bg, cb)
	}
	if u.bmp == nil {
		if f := cachedFont("Segoe UI", 10, 0); f != nil {
			msg := "No Hikrobot image yet.\r\nConnect the monochrome camera (or the simulator) to preview."
			c.DrawTextPixels(msg, f, walk.RGB(128, 128, 136), cb, walk.TextCenter|walk.TextWordbreak)
		}
		return nil
	}
	sz := u.bmp.Size()
	ir := letterbox(cb, sz.Width, sz.Height)
	c.DrawImageStretchedPixels(u.bmp, ir)
	if centerMarkOn(u.hdr) {
		paintCenterMarks(c, walk.Rectangle{Width: sz.Width, Height: sz.Height}, ir, sz.Width, sz.Height)
	}
	if u.viewWhat != "" {
		if f := cachedFont("Segoe UI", 8, 0); f != nil {
			c.DrawTextPixels(u.viewWhat, f, walk.RGB(180, 180, 186), walk.Rectangle{X: ir.X + 6, Y: ir.Y + 4, Width: ir.Width - 12, Height: 16}, walk.TextLeft)
		}
	}
	return nil
}

func letterbox(cb walk.Rectangle, iw, ih int) walk.Rectangle {
	if iw <= 0 || ih <= 0 {
		return cb
	}
	s := float64(cb.Width) / float64(iw)
	if sy := float64(cb.Height) / float64(ih); sy < s {
		s = sy
	}
	w := int(float64(iw) * s)
	h := int(float64(ih) * s)
	return walk.Rectangle{X: cb.X + (cb.Width-w)/2, Y: cb.Y + (cb.Height-h)/2, Width: w, Height: h}
}

func (u *MonoUI) capture() {
	if why, whyFull := u.reason(); why != "" {
		u.say("Capture refused: " + whyFull)
		u.updateButtons()
		return
	}
	if u.hdr != nil {
		u.hdr.save()
	}
	dir, err := u.hdr.rollDir()
	if err != nil {
		walk.MsgBox(u.hdr.win(), "Output folder", err.Error(), walk.MsgBoxIconError)
		return
	}
	vals, src, ok := scanLightValues(u.hdr, u.light)
	if !ok {
		vals = [3]int{50, 50, 50}
		src = "default 50 (light values unknown)"
	}
	settle := 300 * time.Millisecond
	if u.light != nil && u.light.settleEd != nil {
		if ms, perr := strconv.Atoi(strings.TrimSpace(u.light.settleEd.Text())); perr == nil && ms >= 0 && ms <= 5000 {
			settle = time.Duration(ms) * time.Millisecond
		}
	}
	correct := u.caCB == nil || u.caCB.Checked()
	meterEach := u.hdr != nil && u.hdr.hikMeterEach()
	baseUS := hikbase.DefaultExposureUS
	var snap hikbase.Snapshot
	if u.hdr != nil {
		baseUS = u.hdr.sharedHikExposure()
		snap = u.hdr.hikSnapshot()
	} else if u.expNE != nil {
		baseUS = hikbase.ClampExposureUS(u.expNE.Value())
	}
	if !meterEach {
		if u.expNE != nil {
			u.expNE.SetValue(baseUS)
		}
		if u.gainNE != nil {
			u.gainNE.SetValue(0)
		}
	}
	evs := u.captureEVs()
	cam := u.getCam()
	if cam == nil {
		return
	}
	st := u.readSettings()
	var sw *rgblight.ChannelSwitch
	if u.light != nil && u.light.connected() {
		sw = &rgblight.ChannelSwitch{C: u.light.cl, Values: vals, Log: func(m string) { u.hdr.log.Printf("Mono trichrome: %s", m) }}
	}
	var light monocap.Light
	if sw != nil {
		light = sw
	} else if sim, ok := cam.(*hikcam.Sim); ok {
		light = monocap.SimLight{S: sim}
	}
	opt := u.captureOptions()
	box := hdrscan.Rect{}
	roll, last := "", 0
	var meta scanmetaStub
	_ = meta
	if u.hdr != nil {
		box = u.hdr.roi
		roll, last = u.hdr.cfg.Roll, u.hdr.cfg.Counter
	}
	scanMeta := u.hdr.scanMetadata()
	levelMeta := u.hdr.metadataLeveling()

	ctx, cancel := context.WithCancel(context.Background())
	u.setCancel(cancel)
	u.running.Store(true)
	u.stopPreviewAsync()
	u.updateButtons()

	u.say(fmt.Sprintf("==== Mono trichrome: light R %d G %d B %d (%s), settle %v + stability check, CA correction %v, bracket %v, %d frame(s) per exposure, noise model %.0f e- / %.2f e- (MV-CS240-90UM datasheet) ====",
		vals[0], vals[1], vals[2], src, settle, correct, evs, opt.FramesPerExposure, opt.Noise.FullWellE, opt.Noise.ReadE))
	u.say(hikbase.CaptureLog(meterEach, baseUS, snap, vals))
	if u.repTE != nil {
		if len(evs) > 1 {
			u.repTE.SetText(fmt.Sprintf("Capturing R, G, B HDR (%s) ...", evText(evs)))
		} else {
			u.repTE.SetText("Capturing R, G, B (single frame per channel) ...")
		}
	}
	if u.pb != nil {
		u.pb.SetMarqueeMode(true)
	}

	type result struct {
		path     string
		report   hdrscan.FocusShiftReport
		move     hdrscan.MovementReport
		notes    []string
		hdr      string
		err      error
		applyErr error
		ms       float64
		capS     float64
	}
	u.worker().Do(func() {
		u.worker().StopSync()
		var res result
		defer func() {
			u.ui(func() {
				u.setCancel(nil)
				u.running.Store(false)
				if u.pb != nil {
					u.pb.SetMarqueeMode(false)
					u.pb.SetValue(0)
				}
				switch {
				case res.applyErr != nil:
					u.say("Apply settings: " + res.applyErr.Error())
					if u.repTE != nil {
						u.repTE.SetText("Apply settings failed: " + res.applyErr.Error())
					}
					walk.MsgBox(u.hdr.win(), "Mono trichrome", "Could not apply camera settings:\n\n"+res.applyErr.Error(), walk.MsgBoxIconWarning)
				case errors.Is(res.err, context.Canceled):
					msg := "Mono trichrome stopped."
					u.say(msg)
					if u.repTE != nil {
						u.repTE.SetText(msg)
					}
				case res.err != nil:
					msg := "Mono trichrome failed: " + res.err.Error()
					u.say(msg)
					if u.repTE != nil {
						u.repTE.SetText(msg)
					}
				default:
					modeTag := "Single"
					if len(evs) > 1 {
						modeTag = "HDR " + evText(evs)
					}
					var warn string
					if res.move.Warn {
						warn += res.move.String() + "\r\n"
					}
					for _, n := range res.notes {
						warn += n + "\r\n"
					}
					msg := fmt.Sprintf("%sSaved %s  (%s, capture %.1f s, total %.1f s)\r\n%s\r\n%s\r\n%s", warn, res.path, modeTag, res.capS, res.ms, res.move.String(), res.hdr, res.report.String())
					if u.whyLbl != nil && res.move.Warn {
						setMonoStatus(u.whyLbl, "WARNING: R/G/B film movement detected - see the report")
					}
					u.say(strings.ReplaceAll(msg, "\r\n", " | "))
					if u.repTE != nil {
						u.repTE.SetText(msg)
					}
				}
				u.updateButtons()
				u.startPreview()
			})
		}()

		if err := ctx.Err(); err != nil {
			res.err = err
			return
		}
		if err := cam.Apply(st); err != nil {
			res.applyErr = err
			return
		}
		got := cam.Settings()
		u.ui(func() {
			u.say(fmt.Sprintf("Applied: %.0f us, %.2f dB, black %.0f, %s, trigger=%v, %dx%d @ %d,%d",
				got.ExposureUS, got.GainDB, got.BlackLevel, got.PixelFormat, got.SoftwareTrigger,
				got.Width, got.Height, got.OffsetX, got.OffsetY))
		})

		captureStart := time.Now()
		if sw != nil {
			if _, e := sw.Snapshot(); e != nil {
				res.err = fmt.Errorf("could not read the light: %w", e)
				return
			}
			defer func() { _ = sw.Restore() }()
		}
		if err := cam.StartGrab(); err != nil {
			res.err = err
			return
		}
		defer func() { _ = cam.StopGrab() }()

		opt.Cam = cam
		opt.Light = light
		opt.Settle = settle
		opt.SettleTimeout = max(monocap.DefaultSettleTimeout, 4*settle)
		opt.BaseUS = baseUS
		opt.MeterEach = meterEach
		opt.MeterBox = box
		opt.EVs = evs
		opt.Ctx = ctx
		opt.Log = func(m string) { u.ui(func() { u.say(m) }) }
		opt.Status = func(m string) {
			u.ui(func() {
				if u.repTE != nil {
					u.repTE.SetText(m)
				}
			})
		}
		cr, err := monocap.Capture(opt)
		if err != nil {
			res.err = err
			return
		}
		if err := ctx.Err(); err != nil {
			res.err = err
			return
		}
		res.notes = cr.Notes
		res.hdr = cr.Mono.HDR
		res.capS = cr.Elapsed.Seconds()
		planes := cr.Planes
		base, num := hdrscan.NextShot(dir, roll, time.Now(), last)
		u.ui(func() {
			u.hdr.cfg.Counter = num
			u.hdr.save()
		})
		out := filepath.Join(dir, base+"_RGB.tiff")
		meta := scanMeta
		meta.Mode = "mono-trichrome"
		meta.Encoding = "linear"
		meta.LightModel = "CuteNewDesign RGB Light Air"
		meta.LightRGB = &vals
		meta = meta.WithLeveling(levelMeta, vals)
		meta.CameraProfile = cam.Info().Model
		meta.CameraMatrix = nil
		meta = u.hdr.withHikExposureMeta(meta, baseUS, meterEach)
		mc := cr.Mono
		meta.Mono = &mc
		info := cam.Info()
		r, err := hdrscan.ProcessMonoTrichrome(planes[0], planes[1], planes[2], hdrscan.MonoTriOptions{
			OutPath:       out,
			LZW:           true,
			CorrectCA:     correct,
			CheckMovement: true,
			Software:      appVersion + monoHDRTag(evs),
			Make:          info.Vendor,
			Model:         info.Model,
			Metadata:      meta,
			Progress:      func(m string) { u.ui(func() { u.say(m) }) },
		})
		if err != nil {
			res.err = err
			return
		}
		res.path = r.Out
		res.report = r.Report
		res.move = r.Movement
		res.ms = time.Since(captureStart).Seconds()
	})
}

type scanmetaStub struct{}

func (u *MonoUI) Page() TabPage {
	grey := walk.RGB(110, 110, 110)
	bold := Font{Family: "Segoe UI", PointSize: 9, Bold: true}
	pixItems := []string{hikcam.PixelAuto.String(), hikcam.PixelMono12.String(), hikcam.PixelMono10.String(), hikcam.PixelMono8.String()}
	return TabPage{
		Title:  " Mono trichrome ",
		Layout: HBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 8},
		Children: []Widget{
			Composite{StretchFactor: 3, Layout: VBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				CustomWidget{
					AssignTo:            &u.view,
					Paint:               u.paint,
					PaintMode:           PaintBuffered,
					InvalidatesOnResize: true,
					MinSize:             Size{Width: 360, Height: 240},
					StretchFactor:       3,
				},
				Composite{Layout: HBox{MarginsZero: true, Spacing: 8}, Children: []Widget{
					CheckBox{AssignTo: &u.markCB, Text: "Center mark", Checked: centerMarkOn(u.hdr),
						ToolTipText: "High-contrast crosshair at the sensor centre, plus corner and edge marks at a 10% inset, to help level the film plane. Display only: never written into captured TIFFs.",
						OnCheckedChanged: func() {
							if u.hdr != nil && u.markCB != nil {
								u.hdr.setCenterMark(u.markCB.Checked())
							}
						}},
					HSpacer{},
				}},
				Label{Text: "Hikrobot monochrome live view.\nCapture shoots R, G, and B with the RGB light and merges them into one linear 16-bit RGB TIFF.", TextColor: grey},
				TextEdit{AssignTo: &u.logTE, ReadOnly: true, VScroll: true, StretchFactor: 1, MinSize: Size{Height: 110}},
			}},
			Composite{StretchFactor: 2, MinSize: Size{Width: 390}, MaxSize: Size{Width: 490}, Layout: VBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
				Card{
					Title:  "Hikrobot monochrome camera",
					Icon:   iconCamera,
					Layout: VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 4, Right: 8, Bottom: 6}},
					Children: []Widget{
						Label{AssignTo: &u.sdkLbl, Text: "SDK: -", TextColor: grey, MinSize: Size{Height: 30}},
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							ComboBox{AssignTo: &u.devCB, Model: []string{hikcam.OpenSim().Info().Label()}, CurrentIndex: 0, StretchFactor: 1},
							PushButton{AssignTo: &u.refreshBtn, Text: "Refresh", MaxSize: Size{Width: 70}, OnClicked: u.refreshDevices},
						}},
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							PushButton{AssignTo: &u.connBtn, Text: "Connect", MinSize: Size{Width: 100}, OnClicked: u.toggleConnect},
							HSpacer{},
						}},
						Label{AssignTo: &u.camLbl, Text: "Camera: not connected", Font: bold, MinSize: Size{Height: 16}},
						Composite{Layout: Grid{Columns: 2, MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Preset", MinSize: Size{Width: 110}},
							ComboBox{AssignTo: &u.presetCB, Model: []string{"Custom", "Best scan quality (MV-CS240-90UM)"}, CurrentIndex: 1,
								ToolTipText:           hikcam.BestScanExplain(),
								OnCurrentIndexChanged: u.onPresetChanged},
							Label{Text: "Pixel format", MinSize: Size{Width: 110}},
							ComboBox{AssignTo: &u.fmtCB, Model: pixItems, CurrentIndex: hikcam.PixelFormatIndex(hikcam.PixelMono12),
								ToolTipText: "Auto prefers Mono12, then Mono10, then Mono8 (native bit depth, unpacked to 16-bit linear). Best scan quality selects Mono12."},
							Label{Text: "Exposure"},
							NumberEdit{AssignTo: &u.expNE, Value: 10000.0, MinValue: 0, MaxValue: 10000000, Decimals: 0, Increment: 100,
								Suffix: " us", SpinButtonsVisible: true, MinSize: Size{Width: 160},
								ToolTipText: "Shared Hikrobot ExposureTime in microseconds (MV-CS240-90UM: 32 us to 10 s, gain 0 dB). This is the same value as Camera and Light leveling. R, G and B capture all use this exposure unless Auto-meter each channel is checked.",
								OnValueChanged: func() {
									if u.hdr != nil && u.expNE != nil {
										u.hdr.setSharedHikExposure(u.expNE.Value(), "user")
									}
								}},
							Label{AssignTo: &u.baseExpLbl, Text: hikbase.StatusUnleveled, ColumnSpan: 2, MinSize: Size{Height: 16}},
							Label{Text: "Gain"},
							NumberEdit{AssignTo: &u.gainNE, Value: 0.0, MinValue: 0, MaxValue: 24, Decimals: 2, Increment: 0.1,
								Suffix: " dB", SpinButtonsVisible: true, ToolTipText: "Analog gain in dB (MV-CS240-90UM: 0 to 24)."},
							Label{Text: "Black level"},
							NumberEdit{AssignTo: &u.blackNE, Value: float64(hikcam.BestScanBlackLevel), MinValue: 0, MaxValue: 4095, Decimals: 0, Increment: 1,
								SpinButtonsVisible: true,
								ToolTipText:        "BlackLevel pedestal in DN at the current bit depth. Best scan quality uses 32 (~0.8% of 12-bit full scale) so readout noise is not clipped at 0."},
							Label{Text: "Trigger"},
							ComboBox{AssignTo: &u.trigCB, Model: []string{"Free run", "Software trigger"}, CurrentIndex: 1,
								ToolTipText: "Software trigger grabs after the RGB light has settled. Best scan quality uses this so the frame is not a free-running rolling exposure."},
						}},
						CheckBox{AssignTo: &u.gammaCB, Text: "Enable camera gamma", Checked: false,
							ToolTipText: "Off (default): linear readout, recommended for film scanning."},
						CheckBox{AssignTo: &u.fullCB, Text: "Full frame", Checked: true,
							ToolTipText: "Ignore ROI and read all 6016 x 4096 photosites."},
						Composite{Layout: Grid{Columns: 4, MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Width"},
							NumberEdit{AssignTo: &u.wNE, Value: 6016.0, MinValue: 0, MaxValue: 8192, Decimals: 0, Increment: 16, SpinButtonsVisible: true},
							Label{Text: "Height"},
							NumberEdit{AssignTo: &u.hNE, Value: 4096.0, MinValue: 0, MaxValue: 8192, Decimals: 0, Increment: 16, SpinButtonsVisible: true},
							Label{Text: "Offset X"},
							NumberEdit{AssignTo: &u.xNE, Value: 0.0, MinValue: 0, MaxValue: 8192, Decimals: 0, Increment: 16, SpinButtonsVisible: true},
							Label{Text: "Offset Y"},
							NumberEdit{AssignTo: &u.yNE, Value: 0.0, MinValue: 0, MaxValue: 8192, Decimals: 0, Increment: 16, SpinButtonsVisible: true},
						}},
						PushButton{AssignTo: &u.applyBtn, Text: "Apply camera settings", Enabled: false, OnClicked: func() {
							if u.usingBestScan() {
								u.fillBestScanUI()
							}
							cam := u.getCam()
							if cam == nil || u.running.Load() || u.connBusy.Load() {
								return
							}
							st := u.readSettings()
							live := u.previewOn.Load()
							u.connBusy.Store(true)
							u.stopPreviewAsync()
							u.updateButtons()
							u.worker().Do(func() {
								u.worker().StopSync()
								err := cam.Apply(st)
								got := cam.Settings()
								u.ui(func() {
									u.connBusy.Store(false)
									if err != nil {
										u.say("Apply failed: " + err.Error())
									} else {
										u.say(fmt.Sprintf("Applied: %.0f us, %.2f dB, black %.0f, %s, trigger=%v, %dx%d @ %d,%d",
											got.ExposureUS, got.GainDB, got.BlackLevel, got.PixelFormat, got.SoftwareTrigger,
											got.Width, got.Height, got.OffsetX, got.OffsetY))
									}
									u.updateButtons()
									if live {
										u.startPreview()
									}
								})
							})
						}},
					},
				},
				Card{
					Title:  "Mono trichrome capture",
					Icon:   iconTri,
					Layout: VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 4, Right: 8, Bottom: 6}},
					Children: []Widget{
						Label{AssignTo: &u.lightLbl, Text: "Light: -", MinSize: Size{Height: 16}},
						Label{AssignTo: &u.valLbl, Text: "Scan values: -", MinSize: Size{Height: 16}},
						Label{AssignTo: &u.outLbl, Text: "Output: -", MinSize: Size{Height: 30}},
						CheckBox{AssignTo: &u.caCB, Text: "Correct lateral chromatic aberration", Checked: true,
							ToolTipText: "Measure where R and B sit against green across the frame, fit a radial lateral-CA model and move R and B onto green with a windowed-sinc resampler. Areas with less than about 0.12 px of CA (the frame centre) are left untouched. No sharpening is applied; a channel whose corners or centre would get worse is left as captured."},
						CheckBox{AssignTo: &u.meterEachCB, Text: "Auto-meter each channel", Checked: false,
							ToolTipText: "Off (default): R, G and B all use the shared exposure from film-base leveling (gain 0 dB). HDR bracket frames are EV offsets from that base. On: each channel auto-meters separately, as in earlier versions.",
							OnCheckedChanged: func() {
								if u.hdr != nil && u.meterEachCB != nil {
									u.hdr.cfg.HikMeterEach = u.meterEachCB.Checked()
									u.hdr.save()
								}
								u.updateCaptureLabel()
							}},
						Composite{Layout: Grid{Columns: 2, MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Mode"},
							ComboBox{AssignTo: &u.capModeCB, Model: []string{"Single frame per channel", "HDR bracket per channel (recommended)"}, CurrentIndex: 1,
								ToolTipText:           "HDR (default): each R, G and B channel captures the bracket on the mono camera and merges it natively on the mono plane (black-subtracted, weighted by the MV-CS240-90UM noise model, ghost rejection), then CA correction and the trichrome merge run as usual.",
								OnCurrentIndexChanged: u.updateCaptureLabel},
							Label{Text: "HDR bracket"},
							ComboBox{AssignTo: &u.brCB, Model: monoBracketItems, CurrentIndex: monoBracketDefault,
								ToolTipText:           "0 / +2 / +3 / +5 EV (recommended): the +5 EV frame puts 32x the light on the dense parts of the negative (the positive's highlights) and roughly doubles their signal-to-noise ratio versus 0 / +2 / +3, which it contains, so no density gets worse. Costs one more grab per channel. 0 / +2 is the fastest. Active only when Mode is HDR.",
								OnCurrentIndexChanged: u.updateCaptureLabel},
						}},
						Composite{Layout: Grid{Columns: 2, MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Frames per exposure"},
							NumberEdit{AssignTo: &u.fpeNE, Value: float64(framesPerExpMin), MinValue: framesPerExpMin, MaxValue: framesPerExpMax, Decimals: 0, Increment: 1, SpinButtonsVisible: true, MaxSize: Size{Width: 70},
								ToolTipText:    "Same-exposure grabs averaged for every bracket step (1 = off). N frames cut temporal noise by the square root of N; exposure and readout time grow N times (light settle, merge, CA and TIFF write stay fixed).",
								OnValueChanged: u.updateCaptureLabel},
						}},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
							PushButton{AssignTo: &u.capBtn, Text: "Capture mono trichrome HDR", Enabled: false,
								MinSize: Size{Height: 34}, StretchFactor: 3,
								Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}, OnClicked: u.capture},
							PushButton{AssignTo: &u.stopBtn, Text: "Stop", Enabled: false, MinSize: Size{Height: 34}, StretchFactor: 1,
								ToolTipText: "Stop the capture safely between steps and restore the RGB light.",
								OnClicked:   u.stopCapture},
						}},
						Label{AssignTo: &u.whyLbl, Text: "", Font: bold, MinSize: Size{Height: 32}},
						ProgressBar{AssignTo: &u.pb, MaxSize: Size{Height: 8}},
						TextEdit{AssignTo: &u.repTE, ReadOnly: true, VScroll: true, MinSize: Size{Height: 80},
							Text:        "Output: <Save to>\\<roll>\\<name>_RGB.tiff",
							ToolTipText: "Linear 16-bit TIFF, black level subtracted, nss.scan/1 metadata. CA, HDR, settle and R/G/B movement reports appear here and in mono_capture metadata; Processing stays empty so the OFX plugin loads the scan."},
					},
				},
				VSpacer{},
			}},
		},
	}
}

func (u *MonoUI) updateBaseExposureLabel() {
	if u == nil || u.baseExpLbl == nil {
		return
	}
	us := hikbase.DefaultExposureUS
	var snap hikbase.Snapshot
	rgb := [3]int{}
	if u.hdr != nil {
		us = u.hdr.sharedHikExposure()
		snap = u.hdr.hikSnapshot()
		rgb = u.hdr.currentLightRGB()
	} else if u.expNE != nil {
		us = u.expNE.Value()
	}
	text := hikbase.Status(snap, us, rgb)
	setMonoStatus(u.baseExpLbl, text)
	if hikbase.IsLeveled(snap, us, rgb) {
		u.baseExpLbl.SetTextColor(walk.RGB(70, 140, 80))
	} else {
		u.baseExpLbl.SetTextColor(walk.RGB(190, 70, 50))
	}
}

func (u *MonoUI) AfterCreate() {
	u.setSDK(hikcam.SDKStatus(), nil)
	u.fillBestScanUI()
	if u.hdr != nil {
		u.hdr.applySharedHikFromConfig()
	}
	u.loadCaptureOptions()
	u.updateCaptureLabel()
	u.updateBaseExposureLabel()
	u.say(appVersion + " Mono trichrome page ready. Best scan quality is selected; Capture uses the shared exposure for R, G and B unless Auto-meter each channel is checked.")
	u.updateButtons()
	u.refreshDevices()
}

func monoHDRTag(evs []int) string {
	if len(evs) <= 1 {
		return ""
	}
	return " HDR " + evText(evs)
}

// Keep color import used (preview may later tint the overlay).
var _ = color.Gray{}
