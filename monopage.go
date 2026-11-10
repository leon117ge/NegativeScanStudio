package main

// Mono trichrome page (v0.97): capture R, G and B with a Hikrobot monochrome
// camera while switching the RGB light, then merge to a linear 16-bit RGB
// TIFF. Chromatic-aberration focus shift is estimated against green and
// optionally corrected at merge time.

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"basetool/hdrscan"
	"basetool/hikcam"
	"basetool/rgblight"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type MonoUI struct {
	hdr   *HDRUI
	light *LightUI

	cam     hikcam.Device
	devices []hikcam.Info // real cameras from the last refresh; index 0 in the combo is always the simulator
	sdkMsg  string

	devCB, fmtCB, trigCB, presetCB, capModeCB, brCB  *walk.ComboBox
	expNE, gainNE, blackNE                           *walk.NumberEdit
	wNE, hNE, xNE, yNE                               *walk.NumberEdit
	fullCB, gammaCB, caCB                            *walk.CheckBox
	connBtn, applyBtn, capBtn, stopBtn, refreshBtn   *walk.PushButton
	camLbl, sdkLbl, lightLbl, valLbl, whyLbl, outLbl *walk.Label
	presetTip                                        *walk.Label
	levelSim                                         hikcam.Device
	repTE, logTE                                     *walk.TextEdit
	pb                                               *walk.ProgressBar
	view                                             *walk.CustomWidget
	bmp                                              *walk.Bitmap
	viewWhat                                         string

	previewOn  atomic.Bool
	previewRun atomic.Bool
	running    bool
	cancel     func()
	why        string
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

func (u *MonoUI) reason() string {
	switch {
	case u.running:
		return "a mono trichrome scan is running"
	case u.cam == nil:
		return "Hikrobot camera not connected (Connect above, or use the simulator)"
	case u.light == nil || !u.light.connected():
		if _, sim := u.cam.(*hikcam.Sim); sim {
			return ""
		}
		return "RGB light not connected (Camera and Light > Light > Connect)"
	case u.light.busy:
		return "RGB light busy"
	}
	return ""
}

func (u *MonoUI) updateButtons() {
	if u.connBtn == nil {
		return
	}
	u.connBtn.SetText(map[bool]string{true: "Disconnect", false: "Connect"}[u.cam != nil])
	u.connBtn.SetEnabled(!u.running)
	if u.devCB != nil {
		u.devCB.SetEnabled(u.cam == nil && !u.running)
	}
	if u.refreshBtn != nil {
		u.refreshBtn.SetEnabled(u.cam == nil && !u.running)
	}
	if u.applyBtn != nil {
		u.applyBtn.SetEnabled(u.cam != nil && !u.running)
	}
	why := u.reason()
	if u.capBtn != nil {
		u.capBtn.SetEnabled(why == "" && !u.running)
	}
	if u.stopBtn != nil {
		u.stopBtn.SetEnabled(u.cancel != nil)
	}
	if u.sdkLbl != nil {
		setStatusWide(u.sdkLbl, "SDK: "+u.sdkMsg)
	}
	if u.cam == nil {
		setStatusWide(u.camLbl, "Camera: not connected")
	} else {
		setStatusWide(u.camLbl, "Camera: "+u.cam.Info().Label())
	}
	if u.light == nil || !u.light.connected() {
		setStatusWide(u.lightLbl, "Light: not connected")
	} else if cs := u.light.cl.CachedState(); cs != nil {
		setStatusWide(u.lightLbl, "Light now: "+cs.String())
	} else {
		setStatusWide(u.lightLbl, "Light: connected")
	}
	if v, src, ok := scanLightValues(u.hdr, u.light); ok {
		setStatusWide(u.valLbl, fmt.Sprintf("Scan values: R %d  G %d  B %d  (%s)", v[0], v[1], v[2], src))
	} else {
		setStatusWide(u.valLbl, "Scan values: -")
	}
	if u.hdr != nil {
		roll := u.hdr.cfg.Roll
		if roll == "" {
			roll = "(no roll name)"
		}
		setStatusWide(u.outLbl, fmt.Sprintf("Output: %s, roll %s (same folder as White light scan)", u.hdr.outDir(), roll))
	}
	if why != u.why {
		if why == "" {
			u.say("Capture ready.")
		} else {
			u.say("Capture disabled: " + why)
		}
		u.why = why
	}
	if u.whyLbl != nil {
		if why == "" {
			setStatusWide(u.whyLbl, "READY - press Capture for R, then G, then B. Exposure is metered automatically per channel.")
		} else {
			setStatusWide(u.whyLbl, "Capture disabled: "+why)
		}
	}
}

func (u *MonoUI) refreshDevices() {
	res := hikcam.Enumerate()
	u.sdkMsg = res.Message()
	u.devices = res.Cameras
	items := []string{hikcam.OpenSim().Info().Label()}
	for _, d := range u.devices {
		items = append(items, d.Label())
	}
	if u.devCB != nil {
		u.devCB.SetModel(items)
		if u.devCB.CurrentIndex() < 0 && len(items) > 0 {
			u.devCB.SetCurrentIndex(0)
		}
	}
	u.updateButtons()
	if res.Err != nil {
		u.say(res.Message())
	} else if len(u.devices) == 0 {
		u.say(res.Message() + ". The simulator is always available.")
	} else {
		u.say(fmt.Sprintf("%s.", res.Message()))
	}
}

func (u *MonoUI) toggleConnect() {
	if u.cam != nil {
		u.stopPreview()
		_ = u.cam.Close()
		u.cam = nil
		u.say("Disconnected.")
		u.updateButtons()
		return
	}
	idx := 0
	if u.devCB != nil {
		idx = u.devCB.CurrentIndex()
	}
	var dev hikcam.Device
	var err error
	if idx <= 0 {
		dev = hikcam.OpenSim()
		u.say("Opened " + dev.Info().Label())
	} else {
		realIdx := idx - 1
		if realIdx < 0 || realIdx >= len(u.devices) {
			u.say("Select a camera first.")
			return
		}
		dev, err = hikcam.OpenReal(u.devices[realIdx])
		if err != nil {
			u.say("Connect failed: " + err.Error())
			walk.MsgBox(u.hdr.win(), "Hikrobot camera", err.Error(), walk.MsgBoxIconWarning)
			return
		}
		u.say("Opened " + dev.Info().Label())
	}
	u.cam = dev
	if err := u.applySettings(); err != nil {
		u.say("Apply settings: " + err.Error())
	}
	u.updateButtons()
	u.startPreview()
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

func (u *MonoUI) applySettings() error {
	if u.cam == nil {
		return hikcam.ErrNotOpen
	}
	st := u.readSettings()
	if err := u.cam.Apply(st); err != nil {
		return err
	}
	got := u.cam.Settings()
	u.say(fmt.Sprintf("Applied: %.0f us, %.2f dB, black %.0f, %s, trigger=%v, %dx%d @ %d,%d",
		got.ExposureUS, got.GainDB, got.BlackLevel, got.PixelFormat, got.SoftwareTrigger,
		got.Width, got.Height, got.OffsetX, got.OffsetY))
	return nil
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
		u.expNE.SetValue(st.ExposureUS)
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
	if u.presetTip != nil {
		u.presetTip.SetText(hikcam.BestScanExplain())
	}
}

func (u *MonoUI) onPresetChanged() {
	if u.presetCB == nil {
		return
	}
	if u.presetCB.CurrentIndex() == 1 {
		u.fillBestScanUI()
		u.say("Preset: Best scan quality (MV-CS240-90UM). Press Capture — exposure is metered automatically per R, G and B so highlights do not clip.")
	} else if u.presetTip != nil {
		u.presetTip.SetText("Custom camera settings. Capture still meters each channel so highlights do not clip; use Best scan quality for the recommended MV-CS240-90UM film-scan values.")
	}
	u.updateCaptureLabel()
}

func (u *MonoUI) captureEVs() []int {
	if u.capModeCB != nil && u.capModeCB.CurrentIndex() == 1 {
		br := 0
		if u.brCB != nil {
			br = clampIdx(u.brCB.CurrentIndex(), len(bracketEVs))
		}
		return append([]int(nil), bracketEVs[br]...)
	}
	return []int{0}
}

func (u *MonoUI) updateCaptureLabel() {
	if u.capBtn == nil {
		return
	}
	evs := u.captureEVs()
	text := "Capture mono trichrome  (R, G, B"
	if len(evs) > 1 {
		text = "Capture mono trichrome HDR  (R, G, B x " + evText(evs)
	}
	text += "; auto exposure)"
	u.capBtn.SetText(text)
}

func (u *MonoUI) startPreview() {
	if u.cam == nil || u.previewRun.Load() {
		return
	}
	if err := u.cam.StartGrab(); err != nil {
		u.say("Live grab: " + err.Error())
		return
	}
	u.previewOn.Store(true)
	u.previewRun.Store(true)
	cam := u.cam
	go func() {
		defer u.previewRun.Store(false)
		for u.previewOn.Load() && u.cam == cam {
			fr, err := cam.Grab(1500 * time.Millisecond)
			if err != nil {
				if !u.previewOn.Load() {
					break
				}
				time.Sleep(200 * time.Millisecond)
				continue
			}
			img := grayImage(fr)
			u.ui(func() {
				if u.cam != cam || !u.previewOn.Load() {
					return
				}
				bmp, e := walk.NewBitmapFromImage(img)
				if e != nil {
					return
				}
				if u.bmp != nil {
					u.bmp.Dispose()
				}
				u.bmp = bmp
				u.viewWhat = fmt.Sprintf("Live  %dx%d  %s  %.0f us", fr.W, fr.H, fr.PixelFormat, fr.ExposureUS)
				if u.view != nil {
					u.view.Invalidate()
				}
			})
			time.Sleep(80 * time.Millisecond)
		}
		_ = cam.StopGrab()
	}()
}

func (u *MonoUI) stopPreview() {
	u.previewOn.Store(false)
	for i := 0; i < 30 && u.previewRun.Load(); i++ {
		time.Sleep(50 * time.Millisecond)
	}
}

func grayImage(fr *hikcam.Frame) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, fr.W, fr.H))
	for i, v := range fr.Pix {
		img.Pix[i] = byte(v >> 8)
	}
	return img
}

func (u *MonoUI) paint(c *walk.Canvas, _ walk.Rectangle) error {
	if u.view == nil {
		return nil
	}
	cb := u.view.ClientBoundsPixels()
	bg, _ := walk.NewSolidColorBrush(walk.RGB(22, 22, 25))
	if bg != nil {
		defer bg.Dispose()
		c.FillRectanglePixels(bg, cb)
	}
	if u.bmp == nil {
		f, _ := walk.NewFont("Segoe UI", 10, 0)
		if f != nil {
			defer f.Dispose()
			msg := "No Hikrobot image yet.\r\nConnect the monochrome camera (or the simulator) to preview."
			c.DrawTextPixels(msg, f, walk.RGB(128, 128, 136), cb, walk.TextCenter|walk.TextWordbreak)
		}
		return nil
	}
	sz := u.bmp.Size()
	ir := letterbox(cb, sz.Width, sz.Height)
	c.DrawImageStretchedPixels(u.bmp, ir)
	if u.viewWhat != "" {
		if f, _ := walk.NewFont("Segoe UI", 8, 0); f != nil {
			c.DrawTextPixels(u.viewWhat, f, walk.RGB(180, 180, 186), walk.Rectangle{X: ir.X + 6, Y: ir.Y + 4, Width: ir.Width - 12, Height: 16}, walk.TextLeft)
			f.Dispose()
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
	if why := u.reason(); why != "" {
		u.say("Capture refused: " + why)
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
	if u.usingBestScan() {
		u.fillBestScanUI()
	}
	if err := u.applySettings(); err != nil {
		u.say("Apply settings: " + err.Error())
		walk.MsgBox(u.hdr.win(), "Mono trichrome", "Could not apply camera settings:\n\n"+err.Error(), walk.MsgBoxIconWarning)
		return
	}
	evs := u.captureEVs()
	po := u.hdr.processOptions(dir)
	u.stopPreview()
	if u.light != nil {
		u.light.stopHikPreview()
	}
	u.running = true
	u.updateButtons()
	var sw *rgblight.ChannelSwitch
	if u.light != nil && u.light.connected() {
		sw = &rgblight.ChannelSwitch{C: u.light.cl, Values: vals, Log: func(m string) { u.hdr.log.Printf("Mono trichrome: %s", m) }}
	}
	cam := u.cam
	chVar := 1
	if sim, ok := cam.(*hikcam.Sim); ok {
		sim.LightChannel = func() int { return chVar }
	}
	u.say(fmt.Sprintf("==== Mono trichrome: light R %d G %d B %d (%s), settle %v, CA correction %v, bracket %v ====",
		vals[0], vals[1], vals[2], src, settle, correct, evs))
	if u.repTE != nil {
		u.repTE.SetText("Capturing R, G, B ...")
	}
	if u.pb != nil {
		u.pb.SetMarqueeMode(true)
	}
	type result struct {
		path   string
		report hdrscan.FocusShiftReport
		err    error
		ms     float64
	}
	done := make(chan result, 1)
	go func() {
		var res result
		defer func() { done <- res }()
		var before rgblight.State
		if sw != nil {
			var e error
			before, e = sw.Snapshot()
			if e != nil {
				res.err = fmt.Errorf("could not read the light: %w", e)
				return
			}
			defer func() { _ = sw.Restore() }()
			_ = before
		}
		if err := cam.StartGrab(); err != nil {
			res.err = err
			return
		}
		defer func() { _ = cam.StopGrab() }()
		var planes [3]hdrscan.MonoPlane
		box := hdrscan.Rect{}
		if u.hdr != nil {
			box = u.hdr.roi
		}
		cfg := hdrscan.ScanMeterConfig()
		for ch := 0; ch < 3; ch++ {
			chVar = ch
			if sw != nil {
				if err := sw.Only(ch); err != nil {
					res.err = fmt.Errorf("light %s only: %w", hdrscan.TriTags[ch], err)
					return
				}
			}
			time.Sleep(settle)
			st := cam.Settings()
			var last *hikcam.Frame
			for attempt := 0; attempt < 4; attempt++ {
				fr, err := cam.Grab(5 * time.Second)
				if err != nil {
					res.err = fmt.Errorf("%s meter grab: %w", hdrscan.TriTags[ch], err)
					return
				}
				last = fr
				full := hdrscan.FrameSamples(fr.Pix, fr.W, fr.H, hdrscan.Rect{})
				base := hdrscan.FrameSamples(fr.Pix, fr.W, fr.H, box)
				cur := hdrscan.ExposureState{ExposureUS: st.ExposureUS, GainDB: st.GainDB}
				next, why := hdrscan.MeterScanExposure(full, base, cur, cfg)
				u.ui(func() {
					u.say(fmt.Sprintf("%s meter %d: %s (%.0f us, %.2f dB)", hdrscan.TriTags[ch], attempt+1, why, next.ExposureUS, next.GainDB))
				})
				if why == "on target" || (math.Abs(next.ExposureUS-st.ExposureUS) < 1 && math.Abs(next.GainDB-st.GainDB) < 0.05) {
					break
				}
				st.ExposureUS, st.GainDB = next.ExposureUS, next.GainDB
				if err := cam.Apply(st); err != nil {
					res.err = fmt.Errorf("%s apply metered exposure: %w", hdrscan.TriTags[ch], err)
					return
				}
			}
			frames := []hdrscan.MonoPlane{{W: last.W, H: last.H, Pix: append([]uint16(nil), last.Pix...)}}
			gotEVs := []int{0}
			for _, ev := range evs {
				if ev == 0 {
					continue
				}
				evSt := st
				evSt.ExposureUS = math.Min(hikcam.CS240ExpMaxUS, st.ExposureUS*hdrscan.EVScale(ev))
				if err := cam.Apply(evSt); err != nil {
					res.err = fmt.Errorf("%s +%d EV apply: %w", hdrscan.TriTags[ch], ev, err)
					return
				}
				fr, err := cam.Grab(8 * time.Second)
				if err != nil {
					res.err = fmt.Errorf("%s +%d EV grab: %w", hdrscan.TriTags[ch], ev, err)
					return
				}
				frames = append(frames, hdrscan.MonoPlane{W: fr.W, H: fr.H, Pix: append([]uint16(nil), fr.Pix...)})
				gotEVs = append(gotEVs, ev)
				u.ui(func() {
					u.say(fmt.Sprintf("Captured %s +%d EV: %dx%d %s (%.0f us)", hdrscan.TriTags[ch], ev, fr.W, fr.H, fr.PixelFormat, fr.ExposureUS))
				})
			}
			_ = cam.Apply(st)
			if len(frames) == 1 {
				planes[ch] = frames[0]
			} else {
				merged, err := hdrscan.MergeMonoBracket(frames, gotEVs, po)
				if err != nil {
					res.err = fmt.Errorf("%s HDR merge: %w", hdrscan.TriTags[ch], err)
					return
				}
				planes[ch] = merged
			}
			u.ui(func() {
				u.say(fmt.Sprintf("Captured %s: %dx%d (bracket %v)", hdrscan.TriTags[ch], planes[ch].W, planes[ch].H, gotEVs))
			})
		}
		roll, last := u.hdr.cfg.Roll, u.hdr.cfg.Counter
		base, num := hdrscan.NextShot(dir, roll, time.Now(), last)
		u.ui(func() {
			u.hdr.cfg.Counter = num
			u.hdr.save()
		})
		out := filepath.Join(dir, base+"_RGB.tiff")
		meta := u.hdr.scanMetadata()
		meta.Mode = "mono-trichrome"
		meta.Encoding = "linear"
		meta.LightModel = "CuteNewDesign RGB Light Air"
		meta.LightRGB = &vals
		meta = meta.WithLeveling(u.hdr.metadataLeveling(), vals)
		meta.CameraProfile = cam.Info().Model
		meta.CameraMatrix = nil
		info := cam.Info()
		t0 := time.Now()
		r, err := hdrscan.ProcessMonoTrichrome(planes[0], planes[1], planes[2], hdrscan.MonoTriOptions{
			OutPath:   out,
			LZW:       true,
			CorrectCA: correct,
			Software:  appVersion + monoHDRTag(evs),
			Make:      info.Vendor,
			Model:     info.Model,
			Metadata:  meta,
			Progress:  func(m string) { u.ui(func() { u.say(m) }) },
		})
		if err != nil {
			res.err = err
			return
		}
		res.path = r.Out
		res.report = r.Report
		res.ms = time.Since(t0).Seconds()
	}()
	go func() {
		res := <-done
		u.ui(func() {
			u.running = false
			if u.pb != nil {
				u.pb.SetMarqueeMode(false)
				u.pb.SetValue(0)
			}
			if res.err != nil {
				msg := "Mono trichrome failed: " + res.err.Error()
				u.say(msg)
				if u.repTE != nil {
					u.repTE.SetText(msg)
				}
			} else {
				msg := fmt.Sprintf("Saved %s  (%.1f s)\r\n%s", res.path, res.ms, res.report.String())
				u.say(strings.ReplaceAll(msg, "\r\n", " | "))
				if u.repTE != nil {
					u.repTE.SetText(msg)
				}
			}
			u.updateButtons()
			u.startPreview()
		})
	}()
}

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
				Label{Text: "Hikrobot monochrome live view. Capture shoots R, then G, then B with the RGB light and merges them into one linear 16-bit RGB TIFF.", TextColor: grey},
				TextEdit{AssignTo: &u.logTE, ReadOnly: true, VScroll: true, StretchFactor: 1, MinSize: Size{Height: 110}},
			}},
			Composite{StretchFactor: 2, MinSize: Size{Width: 390}, MaxSize: Size{Width: 490}, Layout: VBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
				Card{
					Title:  "Hikrobot monochrome camera",
					Icon:   iconCamera,
					Layout: VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 4, Right: 8, Bottom: 6}},
					Children: []Widget{
						Label{AssignTo: &u.sdkLbl, Text: "SDK: -", TextColor: grey},
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							ComboBox{AssignTo: &u.devCB, Model: []string{hikcam.OpenSim().Info().Label()}, CurrentIndex: 0, StretchFactor: 1},
							PushButton{AssignTo: &u.refreshBtn, Text: "Refresh", MaxSize: Size{Width: 70}, OnClicked: u.refreshDevices},
						}},
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							PushButton{AssignTo: &u.connBtn, Text: "Connect", MinSize: Size{Width: 100}, OnClicked: u.toggleConnect},
							HSpacer{},
						}},
						Label{AssignTo: &u.camLbl, Text: "Camera: not connected", Font: bold},
						Composite{Layout: Grid{Columns: 2, MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Preset", MinSize: Size{Width: 110}},
							ComboBox{AssignTo: &u.presetCB, Model: []string{"Custom", "Best scan quality (MV-CS240-90UM)"}, CurrentIndex: 1,
								ToolTipText:           hikcam.BestScanExplain(),
								OnCurrentIndexChanged: u.onPresetChanged},
							Label{Text: "Pixel format", MinSize: Size{Width: 110}},
							ComboBox{AssignTo: &u.fmtCB, Model: pixItems, CurrentIndex: hikcam.PixelFormatIndex(hikcam.PixelMono12),
								ToolTipText: "Auto prefers Mono12, then Mono10, then Mono8 (native bit depth, unpacked to 16-bit linear). Best scan quality selects Mono12."},
							Label{Text: "Exposure"},
							NumberEdit{AssignTo: &u.expNE, Value: 10000, MinValue: 0, MaxValue: 10000000, Decimals: 0, Increment: 100,
								Suffix: " us", SpinButtonsVisible: true, ToolTipText: "ExposureTime in microseconds (MV-CS240-90UM: 32 us to 10 s)."},
							Label{Text: "Gain"},
							NumberEdit{AssignTo: &u.gainNE, Value: 0, MinValue: 0, MaxValue: 24, Decimals: 2, Increment: 0.1,
								Suffix: " dB", SpinButtonsVisible: true, ToolTipText: "Analog gain in dB (MV-CS240-90UM: 0 to 24)."},
							Label{Text: "Black level"},
							NumberEdit{AssignTo: &u.blackNE, Value: float64(hikcam.BestScanBlackLevel), MinValue: 0, MaxValue: 4095, Decimals: 0, Increment: 1,
								SpinButtonsVisible: true,
								ToolTipText:        "BlackLevel pedestal in DN at the current bit depth. Best scan quality uses 32 (~0.8% of 12-bit full scale) so readout noise is not clipped at 0."},
							Label{Text: "Trigger"},
							ComboBox{AssignTo: &u.trigCB, Model: []string{"Free run", "Software trigger"}, CurrentIndex: 1,
								ToolTipText: "Software trigger grabs after the RGB light has settled. Best scan quality uses this so the frame is not a free-running rolling exposure."},
						}},
						CheckBox{AssignTo: &u.gammaCB, Text: "Enable camera gamma (off: linear readout)", Checked: false},
						CheckBox{AssignTo: &u.fullCB, Text: "Full frame (ignore ROI)", Checked: true},
						Composite{Layout: Grid{Columns: 4, MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Width"},
							NumberEdit{AssignTo: &u.wNE, Value: 6016, MinValue: 0, MaxValue: 8192, Decimals: 0, Increment: 16, SpinButtonsVisible: true},
							Label{Text: "Height"},
							NumberEdit{AssignTo: &u.hNE, Value: 4096, MinValue: 0, MaxValue: 8192, Decimals: 0, Increment: 16, SpinButtonsVisible: true},
							Label{Text: "Offset X"},
							NumberEdit{AssignTo: &u.xNE, Value: 0, MinValue: 0, MaxValue: 8192, Decimals: 0, Increment: 16, SpinButtonsVisible: true},
							Label{Text: "Offset Y"},
							NumberEdit{AssignTo: &u.yNE, Value: 0, MinValue: 0, MaxValue: 8192, Decimals: 0, Increment: 16, SpinButtonsVisible: true},
						}},
						Label{AssignTo: &u.presetTip, Text: hikcam.BestScanExplain()},
						PushButton{AssignTo: &u.applyBtn, Text: "Apply camera settings", Enabled: false, OnClicked: func() {
							if u.usingBestScan() {
								u.fillBestScanUI()
							}
							if err := u.applySettings(); err != nil {
								u.say("Apply failed: " + err.Error())
							}
						}},
					},
				},
				Card{
					Title:  "Mono trichrome capture",
					Icon:   iconTri,
					Layout: VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 4, Right: 8, Bottom: 6}},
					Children: []Widget{
						Label{AssignTo: &u.lightLbl, Text: "Light: -"},
						Label{AssignTo: &u.valLbl, Text: "Scan values: -"},
						Label{AssignTo: &u.outLbl, Text: "Output: -"},
						CheckBox{AssignTo: &u.caCB, Text: "Correct chromatic-aberration focus shift (green reference)", Checked: true,
							ToolTipText: "Estimate R and B translation, magnification and blur against green, warp them onto green, and match sharpness only when a channel is measurably softer. Green is never sharpened."},
						Composite{Layout: Grid{Columns: 2, MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Mode"},
							ComboBox{AssignTo: &u.capModeCB, Model: []string{"Single frame per channel", "HDR bracket per channel (same options as HDR Scan)"}, CurrentIndex: 0,
								ToolTipText:           "HDR reuses the White light / Trichrome bracket list (0/+2 or 0/+2/+3), merge method, alignment and LZW options. Each R, G and B channel captures the bracket on the mono camera, merges to linear, then CA correction and the trichrome merge run as usual.",
								OnCurrentIndexChanged: u.updateCaptureLabel},
							Label{Text: "HDR bracket"},
							ComboBox{AssignTo: &u.brCB, Model: bracketItems, CurrentIndex: 0,
								ToolTipText:           "Same presets as White light scan and Trichrome: 0 / +2 EV or 0 / +2 / +3 EV. Used only when Mode is HDR.",
								OnCurrentIndexChanged: u.updateCaptureLabel},
						}},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
							PushButton{AssignTo: &u.capBtn, Text: "Capture mono trichrome  (R, G, B; auto exposure)", Enabled: false,
								MinSize: Size{Height: 34}, StretchFactor: 3,
								Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}, OnClicked: u.capture},
							PushButton{AssignTo: &u.stopBtn, Text: "Stop", Enabled: false, MinSize: Size{Height: 34}, StretchFactor: 1,
								OnClicked: func() {
									u.say("Stop is not yet wired for an in-flight grab; wait for the current channel, then disconnect if needed.")
								}},
						}},
						Label{AssignTo: &u.whyLbl, Text: "", Font: bold},
						ProgressBar{AssignTo: &u.pb, MaxSize: Size{Height: 8}},
						TextEdit{AssignTo: &u.repTE, ReadOnly: true, VScroll: true, MinSize: Size{Height: 80},
							Text: "Output: <Save to>\\<roll>\\<name>_RGB.tiff (linear 16-bit, nss.scan/1). The CA report (shift, scale, blur of R and B vs green) is written here and into the TIFF Processing field."},
					},
				},
				VSpacer{},
			}},
		},
	}
}

func (u *MonoUI) AfterCreate() {
	u.sdkMsg = hikcam.SDKStatus()
	u.refreshDevices()
	u.fillBestScanUI()
	u.updateCaptureLabel()
	u.say(appVersion + " Mono trichrome page ready. The monochrome camera is not required to open the app; Connect uses the MVS SDK when it is installed. Best scan quality is selected; Capture meters each channel automatically.")
	u.updateButtons()
}

func monoHDRTag(evs []int) string {
	if len(evs) <= 1 {
		return ""
	}
	return " HDR " + evText(evs)
}

// Keep color import used (preview may later tint the overlay).
var _ = color.Gray{}
