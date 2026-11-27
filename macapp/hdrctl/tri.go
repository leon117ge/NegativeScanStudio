package hdrctl

// Tri is the HDR Scan > Trichrome page (tripage.go TriUI): R only, G only,
// B only with the RGB light, one frame 0 (or one bracket) per channel, with
// the Camera page's camera, frame-0 lock, output folder, roll and counter
// and the Light page's light connection. The capture runs on the camera
// worker, the merges on their own goroutine (one at a time, the next scan
// may run meanwhile); results come back through Host.Post. The method
// bodies follow tripage.go line by line; walk widgets are fields the Gio
// page binds to.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"basetool/camera"
	"basetool/hdrscan"
	"basetool/rgblight"
)

// Tri is the state of the Trichrome page. All fields belong to the UI
// goroutine.
type Tri struct {
	hdr *Ctl

	cancel      func() // non-nil while a scan runs
	mergeCancel context.CancelFunc
	running     bool
	why         string   // last logged disabled reason
	scans       int      // scans this session
	queue       []triJob // merges waiting (the next scan may run meanwhile)
	merging     bool
	// Progress runs the marquee progress bar (a merge is running).
	Progress bool

	// status lines (Label texts and their tooltips)
	CamText, LightText, OutText, OutTip string
	LockText, LockTip, ValText, ValTip  string
	WhyText, WhyTip, BaseText           string
	// result box (resTE)
	ResText string
	ResSeq  int

	logLines []string
	LogSeq   int

	// simGain is what the simulated camera sees of the light (tests can
	// replace it); simLightGain on Windows.
	simGain func(c *rgblight.Client) [3]float64
}

type triJob struct {
	shot *hdrscan.TriShot
	opts hdrscan.ProcessOptions
}

func newTri(h *Ctl) *Tri {
	return &Tri{hdr: h, CamText: "Camera: -", LockText: "Frame 0: -", LightText: "Light: -", ValText: "Scan values: -", OutText: "Output: -",
		BaseText: "Last leveling: unavailable", ResText: "Output: <Save to>/<roll>/<name>_RGB.tiff (or _RGB_HDR.tiff)", simGain: simLightGain}
}

// Start is AfterCreate.
func (t *Tri) Start() {
	t.say(appVersion + " Trichrome page ready. White-light scans are on HDR Scan > White light scan.")
	t.Buttons()
}

func (t *Tri) say(s string) {
	if t.hdr.Log != nil {
		t.hdr.Log.Printf("Trichrome: %s", s)
	}
	t.logLines = append(t.logLines, time.Now().Format("15:04:05  ")+s)
	if len(t.logLines) > maxLog {
		t.logLines = t.logLines[len(t.logLines)-maxLog:]
	}
	t.LogSeq++
}

// LogText is the page's log pane.
func (t *Tri) LogText() string { return strings.Join(t.logLines, "\n") }

func (t *Tri) setRes(s string) {
	t.ResText = s
	t.ResSeq++
}

func (t *Tri) mode() int { return clampIdx(t.hdr.Cfg.ScanMode, len(triModeItems)) }

func (t *Tri) bracket() int { return clampIdx(t.hdr.Cfg.TriBracket, len(bracketItems)) }

// ModeItems / ModeTip are the Mode combo's entries and tooltip.
func ModeItems() []string { return triModeItems }
func ModeTip() string     { return triModeTip }

// BracketItems are the Bracket per channel entries.
func BracketItems() []string { return bracketItems }

// LevelMetadataTip is the "Film base metadata" check box tooltip.
func LevelMetadataTip() string { return levelMetadataTip }

// evs: the frames per channel of the chosen mode and bracket.
func (t *Tri) evs() []int {
	if t.mode() == 1 {
		return bracketEVs[t.bracket()]
	}
	return []int{0}
}

func (t *Tri) captureText(evs []int) string {
	if len(evs) == 1 {
		return "Capture trichrome"
	}
	return "Capture trichrome HDR"
}

// scanValues are the light values a scan applies per channel: the leveling /
// Apply stored handoff, else the light's current values.
func (t *Tri) scanValues() ([3]int, string, bool) {
	if ho := t.hdr.handoff; ho != nil {
		return ho.Values, ho.Source, true
	}
	if lu := t.hdr.Light; lu != nil && lu.cl != nil {
		if cs := lu.cl.CachedState(); cs != nil {
			return cs.Value, "the light's current values", true
		}
	}
	return [3]int{}, "", false
}

// reason is why Capture is disabled ("" = ready).
func (t *Tri) reason() (short, full string) {
	h, lu := t.hdr, t.hdr.Light
	switch {
	case t.running:
		return "scan in progress", "A trichrome scan is running."
	case h.dev == nil || h.sess == nil:
		return "camera not connected", "Camera not connected (Camera and Light > Camera > Connect)."
	case h.Busy && h.handoff != nil && h.handoff.pending:
		return "locking frame 0 ...", "Locking frame 0 from " + h.handoff.Source + " ..."
	case h.Busy:
		return "camera busy", "Camera busy (metering, capture or leveling)."
	case h.lock == nil && h.handoff != nil:
		return "locking frame 0 ...", "Locking frame 0 from " + h.handoff.Source + " ..."
	case h.lock == nil:
		return "frame 0 not locked", "Frame 0 not locked - level or load a preset on Camera and Light > Light, or Meter base."
	case lu == nil || !lu.connected():
		return "RGB light not connected", "RGB light not connected (Camera and Light > Light > Connect)."
	case lu.busy:
		return "RGB light busy", "RGB light busy (leveling or a light command)."
	}
	if _, _, ok := t.scanValues(); !ok {
		return "light values unknown", "Light values unknown (Read state on Camera and Light > Light)."
	}
	return "", ""
}

// TriButtons is updateButtons: which controls are enabled and the
// Capture label.
type TriButtons struct {
	Capture, Stop, Mode, Bracket, Meter, LevelMeta bool
	CapText                                        string
}

// Buttons is updateButtons: the enabled controls, and it refreshes the
// status lines and logs a change of the Capture-disabled reason.
func (t *Tri) Buttons() TriButtons {
	h, lu := t.hdr, t.hdr.Light
	h.Buttons() // runs a pending light handoff's lock, as HDRUI.updateButtons does
	why, whyFull := t.reason()
	v, _, _ := t.scanValues()
	b := TriButtons{
		Capture:   why == "",
		Stop:      t.cancel != nil || t.mergeCancel != nil,
		Mode:      !t.running,
		Bracket:   !t.running && t.mode() == 1,
		Meter:     h.dev != nil && !h.Busy,
		LevelMeta: !t.running && !t.merging,
		CapText:   t.captureText(t.evs()),
	}
	t.BaseText = h.lastLevelingText(v)
	if why != t.why {
		if why == "" {
			t.say("Capture ready.")
		} else {
			t.say("Capture disabled: " + whyFull)
		}
		t.why = why
	}
	if why == "" {
		t.WhyText, t.WhyTip = "Ready. Press Capture for the next frame.", ""
	} else {
		t.WhyText, t.WhyTip = "Capture disabled: "+why, "Capture disabled: "+whyFull
	}
	// status lines
	if h.dev == nil {
		t.CamText = "Camera: not connected"
	} else {
		t.CamText = "Camera: " + h.CamLbl
	}
	switch {
	case h.sess != nil && h.lock != nil:
		e := *h.lock
		src := h.lockSrc
		if src == "" {
			src = "Meter base"
		}
		t.LockText = fmt.Sprintf("Frame 0: LOCKED  ISO %d, %s", e.ISO, camera.ShutterLabel(e.Shutter))
		t.LockTip = fmt.Sprintf("Frame 0: LOCKED  ISO %d, %s (%s)", e.ISO, camera.ShutterLabel(e.Shutter), src)
	case h.handoff != nil:
		t.LockText = fmt.Sprintf("Frame 0: lock pending  ISO %d, %s", h.handoff.ISO, camera.ShutterLabel(h.handoff.Shutter))
		t.LockTip = fmt.Sprintf("Frame 0: lock pending - ISO %d, %s from %s", h.handoff.ISO, camera.ShutterLabel(h.handoff.Shutter), h.handoff.Source)
	default:
		t.LockText, t.LockTip = "Frame 0: not locked", ""
	}
	if lu == nil || !lu.connected() {
		t.LightText = "Light: not connected"
	} else if cs := lu.cl.CachedState(); cs != nil {
		t.LightText = "Light now: " + cs.String()
	} else {
		t.LightText = "Light: connected"
	}
	if v, src, ok := t.scanValues(); ok {
		t.ValText = fmt.Sprintf("Scan values: R %d  G %d  B %d", v[0], v[1], v[2])
		t.ValTip = fmt.Sprintf("Scan values: R %d  G %d  B %d (%s)", v[0], v[1], v[2], src)
	} else {
		t.ValText, t.ValTip = "Scan values: -", ""
	}
	roll := h.Cfg.Roll
	if roll == "" {
		roll = "none"
	}
	t.OutText = fmt.Sprintf("Output: %s  (roll: %s, next #%04d)", h.outDir(), roll, h.Cfg.Counter+1)
	t.OutTip = fmt.Sprintf("Output: %s, roll %s, frame counter %04d (next %04d), %d trichrome scans this session", h.outDir(), roll, h.Cfg.Counter, h.Cfg.Counter+1, t.scans)
	return b
}

// ModeChanged: the Mode combo changed Cfg.ScanMode.
func (t *Tri) ModeChanged() {
	t.hdr.Save()
	t.say("Mode: " + triModeItems[t.mode()])
}

// BracketChanged: the Bracket per channel combo changed Cfg.TriBracket.
func (t *Tri) BracketChanged() {
	t.hdr.Save()
	t.say("Bracket per channel: " + bracketItems[t.bracket()])
}

// Stop is the Stop button: the scan or the merge in progress.
func (t *Tri) Stop() {
	switch {
	case t.cancel != nil:
		t.say("Stop pressed - the shot in progress finishes downloading, then the light is restored to its pre-scan state and this frame's partial RAFs are deleted.")
		t.cancel()
	case t.mergeCancel != nil:
		t.say("Stop pressed - the merge in progress is stopped and this frame's RAFs and partial TIFFs are deleted (earlier frames are kept).")
		t.mergeCancel()
	}
}

// settle is the Light page's settle time (its field, else the saved value).
func (t *Tri) settle() time.Duration {
	lu := t.hdr.Light
	settle := time.Duration(lu.cfg.SettleMS) * time.Millisecond
	if ms, perr := strconv.Atoi(strings.TrimSpace(lu.SettleText)); perr == nil && ms >= 0 && ms <= 5000 {
		settle = time.Duration(ms) * time.Millisecond
	}
	return settle
}

// Capture is Capture trichrome: R, G and B on the camera worker, then the
// merge in the background.
func (t *Tri) Capture() {
	h, lu := t.hdr, t.hdr.Light
	if why, whyFull := t.reason(); why != "" {
		t.say("Capture refused: " + whyFull)
		return
	}
	h.Save()
	dir, err := h.rollDir()
	if err != nil {
		h.Host.Message("Output folder", err.Error())
		return
	}
	// mode and bracket are read once here and used for the whole scan
	mode, br := t.mode(), t.bracket()
	evs := t.evs()
	vals, src, _ := t.scanValues()
	settle := t.settle()
	c := lu.cl
	s := h.sess
	opts := h.processOptions(dir)
	opts.Metadata = h.scanMetadata() // Windows processOptions writes scanMetadata
	opts.Metadata.LightRGB = &vals
	opts.Metadata.LightModel = "CuteNewDesign RGB Light Air"
	opts.Metadata.Mode = "trichrome"
	opts.Metadata = withScanCamera(opts.Metadata).WithLeveling(h.metadataLeveling(), vals)
	opts.MeteredBase = s.Reading.RGB
	// v0.82: the base is measured in the RGB TIFF over the metered region
	opts.BaseArea = s.Reading.Area
	if !s.Reading.Region.Rect.Empty() {
		reg := s.Reading.Region
		opts.BaseRegion = &reg
	}
	e0 := *h.lock
	roll, last := h.Cfg.Roll, h.Cfg.Counter
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
	h.Busy, lu.busy, t.running = true, true, true
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.setRes(fmt.Sprintf("Trichrome scan %d: reading the light (20) ...", n))
	sw := &rgblight.ChannelSwitch{C: c, Values: vals, Log: func(m string) { h.Log.Printf("Trichrome: %s", m) }}
	gain := t.simGain
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
			h.Host.Post(func() { t.finish(n, num, shot, err, before, lst, opts, dir) })
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
		h.Log.Printf("Trichrome: pre-scan light state (restored after the scan): %s", before)
		if sim != nil {
			// simulated camera: frames follow the light (a real camera sees it)
			sim.ChannelGain = func() [3]float64 { return gain(c) }
			defer func() { sim.ChannelGain = nil }()
		}
		shot, err = s.CaptureTrichrome(ctx, dir, evs, settle, sw, func(tm time.Time) string {
			b, k := hdrscan.NextShot(dir, roll, tm, last)
			num = k
			return b
		})
	})
}

// finish runs on the UI goroutine after every scan, whatever happened.
func (t *Tri) finish(n, num int, shot *hdrscan.TriShot, err error, before rgblight.State, lst *rgblight.State, opts hdrscan.ProcessOptions, dir string) {
	h, lu := t.hdr, t.hdr.Light
	if num > 0 {
		h.Cfg.Counter = num
		h.Save()
	}
	h.Busy, lu.busy, t.running, t.cancel = false, false, false, nil
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
		t.setRes(msg)
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
		t.say(fmt.Sprintf("Scan %d downloaded: %s to %s.%s Ready for the next frame (counter %04d).", n, strings.Join(names, ", "), dir, restored, h.Cfg.Counter+1))
		t.queue = append(t.queue, triJob{shot: shot, opts: opts})
		t.nextMerge()
	}
}

// nextMerge runs the queued merges one at a time; scanning goes on meanwhile.
func (t *Tri) nextMerge() {
	if t.merging || len(t.queue) == 0 {
		return
	}
	j := t.queue[0]
	t.queue = t.queue[1:]
	t.merging = true
	t.Progress = true
	h := t.hdr
	o := j.opts
	o.Progress = func(m string) { h.Host.Post(func() { t.setRes(m) }) }
	mctx, mcancel := context.WithCancel(context.Background())
	o.Ctx = mctx
	t.mergeCancel = mcancel
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
		h.Host.Post(func() {
			t.merging = false
			t.mergeCancel = nil
			mcancel()
			t.Progress = false
			if err != nil && errors.Is(err, context.Canceled) {
				m := fmt.Sprintf("Trichrome merge stopped: partial TIFFs removed, %d RAF(s) of this frame deleted; earlier frames kept.", dropShot(j.shot))
				t.setRes(m)
				t.say(m)
			} else if err != nil {
				t.setRes("Trichrome merge failed: " + err.Error())
				t.say("Trichrome merge failed: " + err.Error())
			} else {
				lastTriOut = res.Out
				t.setRes(triSavedText(res, j))
				t.say(strings.ReplaceAll(t.ResText, "\n", " | "))
			}
			t.nextMerge()
		})
	}()
}

// triSavedText is the result box text after a good merge.
func triSavedText(res *hdrscan.TriResult, j triJob) string {
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
	msg += "\n" + levelMetadataSummary(j.opts.Metadata)
	return msg
}

// Meter is the page's Meter base button (the Camera page's Meter base).
func (t *Tri) Meter() { t.hdr.Meter() }

// SetUseLastLeveling is the page's Film base metadata check box.
func (t *Tri) SetUseLastLeveling(on bool) { t.hdr.SetUseLastLeveling(on) }

// Running reports a scan in progress (tests).
func (t *Tri) Running() bool { return t.running }

// Merging reports a merge in progress or queued (tests).
func (t *Tri) Merging() bool { return t.merging || len(t.queue) > 0 }
