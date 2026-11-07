package main

// v0.81: selectable fixed base shutter. v0.82: the trichrome (R/G/B) scan
// has its own page (the HDR scan page is white light only), and leveling /
// Apply stored hand their exposure and light to the frame-0 lock.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"basetool/camera"
	"basetool/hdrscan"
	"basetool/rgblight"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// Fixed base shutter (fixed frame 0, Record base, light leveling,
// trichrome). Config FIXED_SHUTTER=1/60|1/125. The X-T3 has no 1/120 s:
// its PTP shutter list (0x500D) goes 1/100 (9843 us) -> 1/125 (7812 us),
// so the requested "1/120" is 1/125 s, exactly one stop above 1/60 s.
var fixedShutterVals = []uint32{camera.Shutter1_60, camera.Shutter1_125}
var fixedShutterItems = []string{"1/60 s (default)", "1/125 s (\"1/120\", 1 EV faster)"}
var fixedShutterKeys = []string{"1/60", "1/125"}

const fixedShutterTip = "Base shutter of a Fixed frame 0, Record base, the RGB light's auto film-base leveling and trichrome scans.\n" +
	"1/125 s is one stop faster than 1/60 s (the X-T3 has no 1/120 s; 1/125 s is its nearest step).\n" +
	"Saved with the light values in the film-base DB; a preset on Camera and Light > Light restores it."

func fixedShutterIndex(v string) int {
	switch strings.TrimSpace(v) {
	case "1/125", "1/120", "7812":
		return 1
	}
	return 0
}

// fixedShutterIndexOf is the selection for a stored shutter (-1 = none).
func fixedShutterIndexOf(us uint32) int {
	for i, v := range fixedShutterVals {
		if v == us {
			return i
		}
	}
	return -1
}

func (u *HDRUI) fixedShutter() uint32 {
	i := u.cfg.Shutter
	if i < 0 || i >= len(fixedShutterVals) {
		i = 0
	}
	return fixedShutterVals[i]
}

// shutterChanged: the HDR page's Base shutter combo changed.
func (u *HDRUI) shutterChanged() {
	if u.shutCB == nil {
		return
	}
	i := max(0, u.shutCB.CurrentIndex())
	changed := i != u.cfg.Shutter
	u.save()
	if l := u.light; l != nil && l.shutCB != nil && l.shutCB.CurrentIndex() != i {
		l.shutCB.SetCurrentIndex(i)
	}
	if !changed {
		return
	}
	sh := u.fixedShutter()
	u.say("Base shutter: " + camera.ShutterLabel(sh) + fmt.Sprintf(" (PTP %d us)", sh))
	if ho := u.handoff; ho != nil && ho.Shutter != sh {
		u.dropHandoff("base shutter changed to " + camera.ShutterLabel(sh))
	}
	s := u.sess
	if s != nil && s.Locked != nil && u.fixed() && s.Locked.Shutter != sh && !u.busy {
		u.releaseLock("Base shutter changed - fixed frame 0 lock released, meter the base again.")
	}
}

// isoChanged (v0.82): the HDR page's fixed ISO changed - synced to the
// Light page's leveling ISO; a fixed lock or handoff at another ISO is void.
func (u *HDRUI) isoChanged() {
	if u.isoCB == nil {
		return
	}
	i := clampIdx(u.isoCB.CurrentIndex(), len(isoVals))
	changed := i != u.cfg.ISO
	u.save()
	if l := u.light; l != nil && l.isoCB != nil && l.isoCB.CurrentIndex() != i {
		l.isoCB.SetCurrentIndex(i) // fires the Light page's isoChanged (no-op back here)
	}
	if !changed {
		return
	}
	iso := isoVals[i]
	u.say(fmt.Sprintf("Fixed ISO: %d (Light page leveling ISO follows)", iso))
	if ho := u.handoff; ho != nil && ho.ISO != iso {
		u.dropHandoff(fmt.Sprintf("ISO changed to %d", iso))
	}
	s := u.sess
	if s != nil && s.Locked != nil && u.fixed() && s.Locked.ISO != iso && !u.busy {
		u.releaseLock(fmt.Sprintf("ISO changed to %d - fixed frame 0 lock released.", iso))
	}
}

// setISO selects a fixed ISO (index) from the other page or a DB entry.
func (u *HDRUI) setISO(i int) {
	if i < 0 || i >= len(isoVals) {
		return
	}
	if u.isoCB != nil && u.isoCB.CurrentIndex() != i {
		u.isoCB.SetCurrentIndex(i) // fires isoChanged
		return
	}
	if u.isoCB == nil {
		u.cfg.ISO = i
		saveHDRConfig(u.cfg)
	}
}

// setFixedFrame0 selects Frame 0 = Fixed (leveling / stored handoff).
func (u *HDRUI) setFixedFrame0() {
	if u.f0CB != nil && u.f0CB.CurrentIndex() != 1 {
		u.f0CB.SetCurrentIndex(1)
		u.say("Frame 0 set to Fixed (ISO + base shutter) to match the RGB light leveling.")
	}
	u.cfg.Frame0 = 1
	u.save()
}

// releaseLock drops the frame-0 lock (camera worker) with a reason.
func (u *HDRUI) releaseLock(why string) {
	s := u.sess
	if s == nil || s.Locked == nil {
		return
	}
	u.worker.do(func() {
		s.Locked = nil
		u.ui(func() {
			u.clearLocked()
			u.updateButtons()
			u.say(why)
		})
	})
}

func (u *HDRUI) dropHandoff(why string) {
	if u.handoff == nil {
		return
	}
	u.say("Light handoff (" + u.handoff.Source + ") void: " + why + " - meter the base, level or load a preset again.")
	u.handoff = nil
	if u.tri != nil {
		u.tri.updateButtons()
	}
}

// takeHandoff (v0.82): a successful leveling or Apply stored. The HDR page
// takes its ISO and shutter (Frame 0 = Fixed) and locks frame 0 by itself
// with one probe at that exposure under the light as it is now - no Meter
// base press. Without a camera the lock waits until it is connected.
func (u *HDRUI) takeHandoff(ho baseHandoff) {
	for i, v := range isoVals {
		if v == ho.ISO {
			u.setISO(i)
		}
	}
	if i := fixedShutterIndexOf(ho.Shutter); i >= 0 {
		u.setShutter(i)
	}
	u.setFixedFrame0()
	ho.ISO, ho.Shutter = u.fixedISO(), u.fixedShutter()
	ho.pending = true
	u.handoff = &ho
	u.say(fmt.Sprintf("Light handoff from %s: R %d G %d B %d at ISO %d, %s - frame 0 locks automatically (Meter base not needed).",
		ho.Source, ho.Values[0], ho.Values[1], ho.Values[2], ho.ISO, camera.ShutterLabel(ho.Shutter)))
	s := u.sess
	if s != nil && s.Locked != nil && !u.busy {
		s2 := s
		u.worker.do(func() {
			s2.Locked = nil
			u.ui(func() { u.clearLocked(); u.updateButtons() })
		})
		return
	}
	u.updateButtons()
}

// autoLock runs a pending handoff's lock when the camera is free.
func (u *HDRUI) autoLock() {
	ho := u.handoff
	if ho == nil || !ho.pending || u.dev == nil || u.sess == nil || u.busy || u.autoLocking {
		return
	}
	u.autoLocking = true
	u.ui(func() {
		u.autoLocking = false
		if u.handoff != ho || !ho.pending || u.dev == nil || u.busy {
			return
		}
		ho.pending = false
		u.meterSrc(ho)
	})
}

// setShutter selects a base shutter (index) from the other page or a DB entry.
func (u *HDRUI) setShutter(i int) {
	if i < 0 || i >= len(fixedShutterVals) {
		return
	}
	if u.shutCB != nil && u.shutCB.CurrentIndex() != i {
		u.shutCB.SetCurrentIndex(i) // fires shutterChanged
		return
	}
	if u.shutCB == nil {
		u.cfg.Shutter = i
		saveHDRConfig(u.cfg)
	}
}

// ---------- v0.82: the Trichrome page ----------
//
// The HDR scan page is white light only (normal + HDR). Trichrome scans
// (R only, G only, B only with the RGB light) have their own page. They use
// the HDR page's camera, frame-0 lock, output folder, roll and counter, and
// the Light page's light connection.

var triModeItems = []string{"Trichrome R/G/B (one shot per channel)", "Trichrome R/G/B + HDR (bracket per channel)"}
var triModeKeys = []string{"tri", "tri_hdr"}

const triModeTip = "Trichrome: R only, then G only, then B only, one frame 0 per channel; each frame's own colour channel goes into <name>_RGB.tiff.\n" +
	"Trichrome + HDR: the bracket per channel, merged per channel, then combined into <name>_RGB_HDR.tiff.\n" +
	"At scan start the light's state is read (20); each channel is set with 03 at the levelled / stored values;\n" +
	"after the scan (and on Stop or an error) the light is put back exactly as it was before the scan."

// baseHandoff (v0.82) is a leveling or Apply stored result handed to the HDR
// page: frame 0 is locked at its exposure automatically (no Meter base).
type baseHandoff struct {
	ISO     int
	Shutter uint32
	Values  [3]int // the light values it was made with
	Source  string // "RGB light leveling 12:59:39", "stored Ektar100_250"
	pending bool   // the lock probe has not run yet
}

type TriUI struct {
	hdr   *HDRUI
	light *LightUI

	modeCB, brCB                                      *walk.ComboBox
	camLbl, lockLbl, lightLbl, valLbl, outLbl, whyLbl *walk.Label
	capBtn, stopBtn, meterBtn                         *walk.PushButton
	mergeCancel                                       context.CancelFunc // v0.83 Stop during the merge
	sendBtn                                           *walk.PushButton   // v0.82 Send base to Develop
	baseLbl                                           *walk.Label        // v0.82 Density_In film base of the last scan
	pb                                                *walk.ProgressBar
	resTE, logTE                                      *walk.TextEdit

	cancel  func() // non-nil while a scan runs
	running bool
	why     string // last logged disabled reason
	scans   int    // scans this session
	lastTri *hdrscan.TriResult
	queue   []triJob // merges waiting (the next scan may run meanwhile)
	merging bool
}

type triJob struct {
	shot *hdrscan.TriShot
	opts hdrscan.ProcessOptions
}

func (t *TriUI) say(s string) {
	h := t.hdr
	if h.log != nil {
		h.log.Printf("Trichrome: %s", s)
	}
	if t.logTE != nil {
		tx := t.logTE.Text()
		if len(tx) > 40000 {
			t.logTE.SetText(tx[len(tx)-20000:])
		}
		t.logTE.AppendText(time.Now().Format("15:04:05  ") + s + "\r\n")
		t.logTE.ScrollToCaret()
	}
}

func (t *TriUI) mode() int { return clampIdx(t.hdr.cfg.ScanMode, len(triModeItems)) }

func (t *TriUI) bracket() int { return clampIdx(t.hdr.cfg.TriBracket, len(bracketItems)) }

// evs: the frames per channel of the chosen mode and bracket.
func (t *TriUI) evs() []int {
	if t.mode() == 1 {
		return bracketEVs[t.bracket()]
	}
	return []int{0}
}

func (t *TriUI) captureText(evs []int) string {
	if len(evs) == 1 {
		return "Capture trichrome  (R, G, B)"
	}
	return "Capture trichrome HDR  (R, G, B x " + evText(evs) + ")"
}

// scanValues are the light values a scan applies per channel: the leveling /
// Apply stored handoff, else the light's current values.
func (t *TriUI) scanValues() ([3]int, string, bool) {
	if ho := t.hdr.handoff; ho != nil {
		return ho.Values, ho.Source, true
	}
	if lu := t.light; lu != nil && lu.cl != nil {
		if cs := lu.cl.CachedState(); cs != nil {
			return cs.Value, "the light's current values", true
		}
	}
	return [3]int{}, "", false
}

// reason is why Capture is disabled ("" = ready).
func (t *TriUI) reason() string {
	h, lu := t.hdr, t.light
	switch {
	case t.running:
		return "a trichrome scan is running"
	case h.dev == nil || h.sess == nil:
		return "camera not connected (Camera and Light > Camera > Connect)"
	case h.busy && h.handoff != nil && h.handoff.pending:
		return "locking frame 0 from " + h.handoff.Source + " ..."
	case h.busy:
		return "camera busy (metering, capture or leveling)"
	case h.sess.Locked == nil && h.handoff != nil:
		return "locking frame 0 from " + h.handoff.Source + " ..."
	case h.sess.Locked == nil:
		return "frame 0 not locked - level or load a preset on Camera and Light > Light (locks automatically), or Meter base on Camera and Light > Camera"
	case lu == nil || !lu.connected():
		return "RGB light not connected (Camera and Light > Light > Connect)"
	case lu.busy:
		return "RGB light busy (leveling or a light command on Camera and Light > Light)"
	}
	if _, _, ok := t.scanValues(); !ok {
		return "light values unknown (Read state on Camera and Light > Light)"
	}
	return ""
}

func (t *TriUI) updateButtons() {
	if t.capBtn == nil {
		return
	}
	h, lu := t.hdr, t.light
	why := t.reason()
	t.capBtn.SetEnabled(why == "")
	t.stopBtn.SetEnabled(t.cancel != nil || t.mergeCancel != nil)
	t.modeCB.SetEnabled(!t.running)
	t.brCB.SetEnabled(!t.running && t.mode() == 1)
	t.meterBtn.SetEnabled(h.dev != nil && !h.busy)
	t.sendBtn.SetEnabled(t.lastTri != nil && t.lastTri.BaseErr == "" && t.lastTri.BaseN > 0 && h.SendBase != nil)
	t.capBtn.SetText(t.captureText(t.evs()))
	if why != t.why {
		if why == "" {
			t.say("Capture ready.")
		} else {
			t.say("Capture disabled: " + why)
		}
		t.why = why
	}
	if why == "" {
		setStatusWide(t.whyLbl, "READY - press Capture for the next frame.")
	} else {
		setStatusWide(t.whyLbl, "Capture disabled: "+why)
	}
	// status lines
	if h.dev == nil {
		setStatusWide(t.camLbl, "Camera: not connected")
	} else {
		setStatusWide(t.camLbl, "Camera: "+h.camLbl.Text())
	}
	switch {
	case h.sess != nil && h.sess.Locked != nil:
		e := *h.sess.Locked
		src := h.lockSrc
		if src == "" {
			src = "Meter base"
		}
		setStatusWide(t.lockLbl, fmt.Sprintf("Frame 0: LOCKED  ISO %d, %s  (%s)", e.ISO, camera.ShutterLabel(e.Shutter), src))
	case h.handoff != nil:
		setStatusWide(t.lockLbl, fmt.Sprintf("Frame 0: lock pending - ISO %d, %s from %s", h.handoff.ISO, camera.ShutterLabel(h.handoff.Shutter), h.handoff.Source))
	default:
		setStatusWide(t.lockLbl, "Frame 0: not locked")
	}
	if lu == nil || !lu.connected() {
		setStatusWide(t.lightLbl, "Light: not connected")
	} else if cs := lu.cl.CachedState(); cs != nil {
		setStatusWide(t.lightLbl, "Light now: "+cs.String())
	} else {
		setStatusWide(t.lightLbl, "Light: connected")
	}
	if v, src, ok := t.scanValues(); ok {
		setStatusWide(t.valLbl, fmt.Sprintf("Scan values: R %d  G %d  B %d  (%s)", v[0], v[1], v[2], src))
	} else {
		setStatusWide(t.valLbl, "Scan values: -")
	}
	roll := h.cfg.Roll
	if roll == "" {
		roll = "(no roll name)"
	}
	setStatusWide(t.outLbl, fmt.Sprintf("Output: %s, roll %s, frame counter %04d (next %04d), %d trichrome scans this session", h.outDir(), roll, h.cfg.Counter, h.cfg.Counter+1, t.scans))
}

func (t *TriUI) stop() {
	switch {
	case t.cancel != nil:
		t.say("Stop pressed - the shot in progress finishes downloading, then the light is restored to its pre-scan state and this frame's partial RAFs are deleted.")
		t.cancel()
	case t.mergeCancel != nil:
		t.say("Stop pressed - the merge in progress is stopped and this frame's RAFs and partial TIFFs are deleted (earlier frames are kept).")
		t.mergeCancel()
	}
}

// dropShot (v0.83) deletes the RAWs of an interrupted frame.
func dropShot(sh *hdrscan.TriShot) int {
	n := 0
	if sh == nil {
		return 0
	}
	for _, rs := range sh.Raws {
		for _, r := range rs {
			if os.Remove(r) == nil {
				n++
			}
		}
	}
	return n
}

func (t *TriUI) capture() {
	h, lu := t.hdr, t.light
	if why := t.reason(); why != "" {
		t.say("Capture refused: " + why)
		t.updateButtons()
		return
	}
	h.save()
	dir, err := h.rollDir()
	if err != nil {
		walk.MsgBox(h.win(), "Output folder", err.Error(), walk.MsgBoxIconError)
		return
	}
	// mode and bracket are read once here and used for the whole scan
	mode, br := t.mode(), t.bracket()
	evs := t.evs()
	vals, src, _ := t.scanValues()
	settle := time.Duration(lu.cfg.SettleMS) * time.Millisecond
	if lu.settleEd != nil {
		if ms, perr := strconv.Atoi(strings.TrimSpace(lu.settleEd.Text())); perr == nil && ms >= 0 && ms <= 5000 {
			settle = time.Duration(ms) * time.Millisecond
		}
	}
	c := lu.cl
	s := h.sess
	opts := h.processOptions(dir)
	opts.Metadata.LightRGB = &vals
	opts.Metadata.LightModel = "CuteNewDesign RGB Light Air"
	opts.Metadata.Mode = "trichrome"
	opts.Metadata = withScanCamera(opts.Metadata)
	opts.MeteredBase = s.Reading.RGB
	// v0.82: the base is measured in the RGB TIFF over the metered region
	opts.BaseArea = s.Reading.Area
	if !s.Reading.Region.Rect.Empty() {
		reg := s.Reading.Region
		opts.BaseRegion = &reg
	}
	e0 := *s.Locked
	roll, last := h.cfg.Roll, h.cfg.Counter
	sim, _ := s.Dev.(*camera.Sim)
	n := t.scans + 1
	brText := "-"
	if mode == 1 {
		brText = bracketItems[br]
	}
	if ho := h.handoff; ho != nil && (ho.ISO != e0.ISO || ho.Shutter != e0.Shutter) {
		t.say(fmt.Sprintf("WARNING: frame 0 is locked at %s but %s was made at ISO %d, %s", e0, ho.Source, ho.ISO, camera.ShutterLabel(ho.Shutter)))
	}
	t.say(fmt.Sprintf("==== Trichrome scan %d: %s, bracket %s (frames %s, read once at scan start), frame 0 %s, light R %d G %d B %d (%s), settle %v ====",
		n, triModeItems[mode], brText, evText(evs), e0, vals[0], vals[1], vals[2], src, settle))
	h.pauseLiveView()
	h.busy, lu.busy, t.running = true, true, true
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	lu.updateButtons()
	h.updateButtons()
	t.resTE.SetText(fmt.Sprintf("Trichrome scan %d: reading the light (20) ...", n))
	sw := &rgblight.ChannelSwitch{C: c, Values: vals, Log: func(m string) { h.log.Printf("Trichrome: %s", m) }}
	h.worker.do(func() {
		var num int
		var shot *hdrscan.TriShot
		var err error
		var before rgblight.State
		// cleanup always runs (also after a recovered panic): the page is
		// ready for the next frame afterwards
		defer func() {
			cancel()
			var lst *rgblight.State
			if cs := c.CachedState(); cs != nil {
				st := *cs
				lst = &st
			}
			h.ui(func() { t.finish(n, num, shot, err, before, lst, opts, dir) })
		}()
		defer func() {
			if r := recover(); r != nil {
				h.reportPanic("Trichrome (camera)", r, string(debug.Stack()))
				err = fmt.Errorf("internal error: %v", r)
			}
		}()
		before, err = sw.Snapshot()
		if err != nil {
			err = fmt.Errorf("could not read the light's state before the scan (20): %w - nothing captured, light left as it is", err)
			return
		}
		h.log.Printf("Trichrome: pre-scan light state (restored after the scan): %s", before)
		if sim != nil {
			// simulated camera: frames follow the light (a real camera sees it)
			sim.ChannelGain = func() [3]float64 { return simLightGain(c) }
			defer func() { sim.ChannelGain = nil }()
		}
		shot, err = s.CaptureTrichrome(ctx, dir, evs, settle, sw, func(tm time.Time) string {
			b, k := hdrscan.NextShot(dir, roll, tm, last)
			num = k
			return b
		})
	})
}

// finish runs on the UI thread after every scan, whatever happened.
func (t *TriUI) finish(n, num int, shot *hdrscan.TriShot, err error, before rgblight.State, lst *rgblight.State, opts hdrscan.ProcessOptions, dir string) {
	h, lu := t.hdr, t.light
	if num > 0 {
		h.cfg.Counter = num
		h.save()
	}
	h.busy, lu.busy, t.running, t.cancel = false, false, false, nil
	if lst != nil {
		lu.showState(*lst, false)
	}
	h.resumeLiveView()
	restored := ""
	if lst != nil && before.Value != [3]int{} {
		if lst.On == before.On && lst.Value == before.Value {
			restored = " Light back to its pre-scan state (" + lst.String() + ")."
		} else {
			restored = " WARNING: the light is " + lst.String() + ", before the scan it was " + before.String() + "."
		}
	}
	if err != nil {
		msg := fmt.Sprintf("Trichrome scan %d failed: %s", n, err.Error())
		if errors.Is(err, context.Canceled) {
			msg = fmt.Sprintf("Trichrome scan %d stopped", n)
			if k := dropShot(shot); k > 0 {
				msg += fmt.Sprintf(" (%d partial RAF(s) of this frame deleted; earlier frames kept)", k)
			}
		} else if shot != nil && shot.Done > 0 {
			msg += fmt.Sprintf(" (%d of 3 channels captured, RAFs kept)", shot.Done)
		}
		msg += "." + restored
		t.resTE.SetText(msg)
		t.say(msg)
		if errors.Is(err, camera.ErrNotManual) {
			h.notManualMsg(err)
		}
	} else {
		t.scans++
		var names []string
		for ch := 0; ch < 3; ch++ {
			for _, r := range shot.Raws[ch] {
				names = append(names, filepath.Base(r))
			}
		}
		t.say(fmt.Sprintf("Scan %d downloaded: %s to %s.%s Ready for the next frame (counter %04d).", n, strings.Join(names, ", "), dir, restored, h.cfg.Counter+1))
		t.queue = append(t.queue, triJob{shot: shot, opts: opts})
		t.nextMerge()
	}
	lu.updateButtons()
	h.updateButtons()
	t.updateButtons()
}

// nextMerge runs the queued merges one at a time; scanning goes on meanwhile.
func (t *TriUI) nextMerge() {
	if t.merging || len(t.queue) == 0 {
		return
	}
	j := t.queue[0]
	t.queue = t.queue[1:]
	t.merging = true
	t.pb.SetMarqueeMode(true)
	h := t.hdr
	te := t.resTE
	set := func(s string) { te.SetText(strings.ReplaceAll(s, "\n", "\r\n")) }
	o := j.opts
	o.Progress = func(m string) { h.ui(func() { set(m) }) }
	mctx, mcancel := context.WithCancel(context.Background())
	o.Ctx = mctx
	t.mergeCancel = mcancel
	t.updateButtons()
	go func() {
		var res *hdrscan.TriResult
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					h.reportPanic("Trichrome merge", r, string(debug.Stack()))
					err = fmt.Errorf("internal error: %v", r)
				}
			}()
			res, err = hdrscan.ProcessTrichrome(j.shot.Raws, j.shot.Exposures, o)
		}()
		h.ui(func() {
			t.merging = false
			t.mergeCancel = nil
			mcancel()
			t.pb.SetMarqueeMode(false)
			t.pb.SetValue(0)
			if err != nil && errors.Is(err, context.Canceled) {
				m := fmt.Sprintf("Trichrome merge stopped: partial TIFFs removed, %d RAF(s) of this frame deleted; earlier frames kept.", dropShot(j.shot))
				set(m)
				t.say(m)
			} else if err != nil {
				set("Trichrome merge failed: " + err.Error())
				t.say("Trichrome merge failed: " + err.Error())
			} else {
				t.lastTri = res
				camNote := applyCamToBase(res)
				msg := fmt.Sprintf("Saved %s  (trichrome, %d frames per channel, %.1f s)", res.Out, len(j.shot.EVs), res.Elapsed.Seconds())
				for ch := 0; ch < 3; ch++ {
					msg += fmt.Sprintf("\n%s-lit frame: own channel %.4f FS, other channels %.4f / %.4f",
						hdrscan.TriTags[ch], res.Level[ch], res.Leak[ch][(ch+1)%3], res.Leak[ch][(ch+2)%3])
					if r := res.Channel[ch]; r != nil {
						msg += fmt.Sprintf(" (HDR merged from %d frames)", r.Frames)
					}
				}
				if ds := res.Dust; ds.Level != hdrscan.DustOff {
					msg += fmt.Sprintf("\nDust removal (%s): %d spots (%d hairs), %d fibres, %d px changed.", hdrscan.DustName(ds.Level), ds.Spots, ds.Hairs, ds.Fibres, ds.Pixels)
				}
				msg += "\n" + triBaseText(res)
				if camNote != "" {
					msg += "\n" + camNote
				}
				t.showBase(res)
				set(msg)
				t.say(strings.ReplaceAll(msg, "\n", " | "))
			}
			t.updateButtons()
			t.nextMerge()
		})
	}()
}

// simLightGain: what the simulated camera sees of the light's state.
func simLightGain(c *rgblight.Client) [3]float64 {
	g := [3]float64{1, 1, 1}
	if cs := c.CachedState(); cs != nil {
		for k := 0; k < 3; k++ {
			g[k] = 0.002 // stray light
			if cs.On[k] {
				g[k] = float64(cs.Value[k]) / 50
			}
		}
	}
	return g
}

func (t *TriUI) Page() TabPage {
	h := t.hdr
	bold := Font{Family: "Segoe UI", PointSize: 9, Bold: true}
	return TabPage{
		Title:  " Trichrome ",
		Layout: HBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 8},
		Children: []Widget{
			Composite{StretchFactor: 3, Layout: VBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
				h.liveView(2, walk.RGB(110, 110, 110)),
				Label{Text: "Drag on the image to mark the clear film base (orange). Exposure and base come from Camera & Light.", TextColor: walk.RGB(110, 110, 110)},
				TextEdit{AssignTo: &t.logTE, ReadOnly: true, VScroll: true, StretchFactor: 1, MinSize: Size{Height: 120}},
			}},
			Composite{StretchFactor: 2, MinSize: Size{Width: 380}, MaxSize: Size{Width: 470}, Layout: VBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
				Card{
					Title:  "Trichrome scan (R only, G only, B only)",
					Icon:   iconTri,
					Layout: VBox{Spacing: 4, Margins: Margins{Left: 8, Top: 4, Right: 8, Bottom: 6}},
					Children: []Widget{
						Label{AssignTo: &t.camLbl, Text: "Camera: -"},
						Label{AssignTo: &t.lockLbl, Text: "Frame 0: -", Font: bold},
						Label{AssignTo: &t.lightLbl, Text: "Light: -"},
						Label{AssignTo: &t.valLbl, Text: "Scan values: -"},
						Label{AssignTo: &t.outLbl, Text: "Output: -"},
						Composite{Layout: Grid{Columns: 2, MarginsZero: true, Spacing: 4}, Children: []Widget{
							Label{Text: "Mode", MinSize: Size{Width: 130}},
							ComboBox{AssignTo: &t.modeCB, Model: triModeItems, CurrentIndex: t.mode(), ToolTipText: triModeTip, OnCurrentIndexChanged: func() {
								if v := max(0, t.modeCB.CurrentIndex()); v == h.cfg.ScanMode {
									return
								} else {
									h.cfg.ScanMode = v
								}
								h.save()
								t.say("Mode: " + triModeItems[t.mode()])
								t.updateButtons()
							}},
							Label{Text: "Bracket per channel", MinSize: Size{Width: 130}},
							ComboBox{AssignTo: &t.brCB, Model: bracketItems, CurrentIndex: t.bracket(), ToolTipText: "HDR mode only. Read once when Capture is pressed and used for all three channels.", OnCurrentIndexChanged: func() {
								if v := max(0, t.brCB.CurrentIndex()); v == h.cfg.TriBracket {
									return
								} else {
									h.cfg.TriBracket = v
								}
								h.save()
								t.say("Bracket per channel: " + bracketItems[t.bracket()])
								t.updateButtons()
							}},
						}},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
							PushButton{AssignTo: &t.capBtn, Text: "Capture trichrome", Enabled: false, MinSize: Size{Height: 34}, StretchFactor: 3,
								Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}, OnClicked: t.capture},
							PushButton{AssignTo: &t.stopBtn, Text: "Stop", Enabled: false, MinSize: Size{Height: 34}, StretchFactor: 1, OnClicked: t.stop,
								ToolTipText: "Stop the scan in progress (capture or merge): the light is restored to its pre-scan state and the interrupted frame's partial RAFs/TIFFs are deleted. Earlier frames are kept."},
						}},
						Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
							PushButton{AssignTo: &t.meterBtn, Text: "Meter base (manual)", Enabled: false, MinSize: Size{Height: 28}, StretchFactor: 1, OnClicked: func() { h.meter() },
								ToolTipText: "Optional: meter frame 0 by hand (as on Camera and Light > Camera). Not needed after leveling or preset load."},
							PushButton{AssignTo: &t.sendBtn, Text: "Send base to Develop", Enabled: false, MinSize: Size{Height: 28}, StretchFactor: 1, OnClicked: t.sendBase,
								ToolTipText: "Film base R/G/B measured in the last RGB TIFF -> Develop > Density In for preview. TIFF pixels and metadata remain unchanged."},
						}},
						Label{AssignTo: &t.whyLbl, Text: "", Font: bold},
						Label{AssignTo: &t.baseLbl, Text: "Density In: Film base R / G / B = - (measured after each scan)"},
						ProgressBar{AssignTo: &t.pb, MaxSize: Size{Height: 8}},
						TextEdit{AssignTo: &t.resTE, ReadOnly: true, VScroll: true, MinSize: Size{Height: 90},
							Text: "Output: <Save to>\\<roll>\\<name>_RGB.tiff or _RGB_HDR.tiff, RAFs <name>_R_0EV.RAF ... (same folder, roll and counter as HDR scan)."},
					},
				},
				VSpacer{},
			}},
		},
	}
}

func (t *TriUI) AfterCreate() {
	t.say(appVersion + " Trichrome page ready. White-light scans are on HDR Scan > White light scan.")
	t.updateButtons()
}

// isoChanged (v0.82): the Light page's leveling ISO, linked with the HDR
// page's fixed frame-0 ISO.
func (u *LightUI) isoChanged() {
	if u.isoCB == nil {
		return
	}
	i := clampIdx(u.isoCB.CurrentIndex(), len(isoVals))
	if u.cfg.ISO != i {
		u.cfg.ISO = i
		u.saveCfg()
	}
	if h := u.hdr; h != nil && h.isoCB != nil && h.isoCB.CurrentIndex() != i {
		h.isoCB.SetCurrentIndex(i)
	}
}

// triBaseText: the Density In parameters from a trichrome TIFF.
func triBaseText(r *hdrscan.TriResult) string {
	if r.BaseErr != "" || r.BaseN == 0 {
		return "Film base NOT measured in the RGB TIFF: " + map[bool]string{true: r.BaseErr, false: "no samples"}[r.BaseErr != ""]
	}
	rc := r.BaseRegion.Rect
	return fmt.Sprintf("Density In: Film base R %.4f  G %.4f  B %.4f  (linear RGB TIFF / 65535, %d samples of the clear base in (%d,%d)-(%d,%d); orange mask compensation Off, crosstalk strength 0 for trichrome)",
		r.Base[0], r.Base[1], r.Base[2], r.BaseN, rc.Min.X, rc.Min.Y, rc.Max.X, rc.Max.Y)
}

func (t *TriUI) showBase(r *hdrscan.TriResult) {
	if r.BaseErr != "" || r.BaseN == 0 {
		setStatusWide(t.baseLbl, "Film base: not measured - "+r.BaseErr)
		return
	}
	setStatusWide(t.baseLbl, fmt.Sprintf("Density In  Film base  R %.4f   G %.4f   B %.4f   (%s)", r.Base[0], r.Base[1], r.Base[2], filepath.Base(r.Out)))
}

// sendBase hands the last trichrome base to Develop > Density In (the same
// flow as the HDR scan page's Send base to Develop).
func (t *TriUI) sendBase() {
	h, r := t.hdr, t.lastTri
	if r == nil || r.BaseErr != "" || r.BaseN == 0 || h.SendBase == nil {
		return
	}
	b := SentBase{RGB: r.Base, Trichrome: true,
		Source: fmt.Sprintf("trichrome scan %s, %d samples of the clear base", filepath.Base(r.Out), r.BaseN)}
	t.say(fmt.Sprintf("Sent base to Develop: R %.4f G %.4f B %.4f (%s)", b.RGB[0], b.RGB[1], b.RGB[2], b.Source))
	h.SendBase(b)
}
