package hdrctl

// Mono is the HDR Scan > Mono trichrome page (monopage.go MonoUI,
// monoopts_ui.go, hikexp_ui.go): R, G and B with a Hikrobot monochrome
// camera while the RGB light switches, merged to a linear 16-bit RGB TIFF.
// On macOS the camera comes through the W7 darwin backend (hikcam loads
// MvCameraControl.framework with dlopen); without the framework only the
// simulator is listed and the SDK line says why. Every camera call
// (enumerate, open, apply, preview, capture, close) runs on the
// hikcam.Controller worker, never on the UI goroutine or the main thread;
// results come back through Host.Post. The method bodies follow
// monopage.go line by line; walk widgets are fields the Gio page binds to
// (walk NumberEdits become text fields read back with their Windows ranges).

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
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
)

// Mono is the state of the Mono trichrome page. Exported fields belong to
// the UI goroutine.
type Mono struct {
	hdr *Ctl
	hik *hikCams

	ctrlOnce sync.Once
	ctrl     hikcam.Controller

	previewOn atomic.Bool
	previewUI atomic.Bool // true while a live frame is being posted to the UI
	running   atomic.Bool
	connBusy  atomic.Bool

	cancelMu sync.Mutex
	cancel   func()
	why      string

	loading, optsLoaded bool // saved capture options: being restored / restored

	// ---- controls (the Gio page binds to these) ----
	DevItems  []string
	DevIdx    int
	PresetIdx int // 0 Custom, 1 Best scan quality
	FmtIdx    int // index into hikcam.PixelFormatChoices
	TrigIdx   int // 0 Free run, 1 Software trigger
	ExpText   string
	GainText  string
	BlackText string
	WText     string
	HText     string
	XText     string
	YText     string
	FPEText   string
	Gamma     bool
	Full      bool
	CA        bool
	Mark      bool
	MeterEach bool
	CapMode   int // 0 single, 1 HDR bracket
	BrIdx     int // index into monoBracketEVs
	FieldSeq  int // bumped when the controller writes the text fields

	// ---- readouts ----
	SDKText, CamText, LightText, OutText, OutTip string
	ValText, ValTip, WhyText, WhyTip             string
	BaseExpText                                  string
	BaseExpOK                                    bool
	CapText                                      string
	RepText                                      string
	RepSeq                                       int
	Progress                                     bool

	// live view
	Frame    image.Image
	FrameSeq int
	ViewWhat string

	logLines []string
	LogSeq   int
}

func newMono(h *Ctl) *Mono {
	m := &Mono{hdr: h, hik: h.Hik}
	m.DevItems = []string{hikcam.OpenSim().Info().Label()}
	m.PresetIdx, m.FmtIdx, m.TrigIdx = 1, hikcam.PixelFormatIndex(hikcam.PixelMono12), 1
	m.ExpText, m.GainText, m.BlackText = "10000", "0.00", strconv.Itoa(hikcam.BestScanBlackLevel)
	m.WText, m.HText, m.XText, m.YText = "6016", "4096", "0", "0"
	m.FPEText = strconv.Itoa(framesPerExpMin)
	m.Full, m.CA, m.Mark = true, true, h.Cfg.CenterMark
	m.CapMode, m.BrIdx = 1, monoBracketDefault
	m.SDKText, m.CamText = "SDK: -", "Camera: not connected"
	m.LightText, m.ValText, m.OutText = "Light: -", "Scan values: -", "Output: -"
	m.BaseExpText = hikbase.StatusUnleveled
	m.CapText = "Capture mono trichrome HDR"
	m.RepText = "Output: <Save to>/<roll>/<name>_RGB.tiff"
	h.Hik.stopOther = func() {
		m.previewOn.Store(false)
		m.worker().StopSync()
	}
	return m
}

// Number fields: the Windows NumberEdit ranges and decimals.
type numSpec struct {
	lo, hi float64
	dec    int
}

var (
	specExp   = numSpec{0, 10000000, 0}
	specGain  = numSpec{0, 24, 2}
	specBlack = numSpec{0, 4095, 0}
	specSize  = numSpec{0, 8192, 0}
	specFPE   = numSpec{framesPerExpMin, framesPerExpMax, 0}
)

// value is NumberEdit.Value: the field's number in range (def when the
// text is not a number).
func (s numSpec) value(text string, def float64) float64 {
	t := strings.TrimSpace(text)
	for _, suf := range []string{"us", "dB"} {
		t = strings.TrimSpace(strings.TrimSuffix(t, suf))
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return def
	}
	p := math.Pow(10, float64(s.dec))
	return clampFloat(math.Round(v*p)/p, s.lo, s.hi)
}

func (s numSpec) text(v float64) string {
	return strconv.FormatFloat(clampFloat(v, s.lo, s.hi), 'f', s.dec, 64)
}

func (m *Mono) worker() *hikcam.Controller {
	m.ctrlOnce.Do(func() {
		m.ctrl.OnPanic = func(r any, st string) {
			m.hdr.reportPanic("Hikrobot job", r, st)
		}
	})
	return &m.ctrl
}

func (m *Mono) getCam() hikcam.Device { return m.hik.getCam() }

func (m *Mono) setCancel(fn func()) {
	m.cancelMu.Lock()
	m.cancel = fn
	m.cancelMu.Unlock()
}

func (m *Mono) hasCancel() bool {
	m.cancelMu.Lock()
	defer m.cancelMu.Unlock()
	return m.cancel != nil
}

// Stop is the Stop button.
func (m *Mono) Stop() {
	m.cancelMu.Lock()
	fn := m.cancel
	m.cancelMu.Unlock()
	if fn != nil {
		m.say("Stopping capture after the current step ...")
		fn()
	}
}

func (m *Mono) say(s string) {
	if m.hdr.Log != nil {
		m.hdr.Log.Printf("Mono trichrome: %s", s)
	}
	m.logLines = append(m.logLines, time.Now().Format("15:04:05  ")+s)
	if len(m.logLines) > maxLog {
		m.logLines = m.logLines[len(m.logLines)-maxLog:]
	}
	m.LogSeq++
}

// LogText is the page's log pane.
func (m *Mono) LogText() string { return strings.Join(m.logLines, "\n") }

func (m *Mono) ui(f func()) { m.hdr.Host.Post(f) }

func (m *Mono) setRep(s string) {
	m.RepText = s
	m.RepSeq++
}

// scanLightValues: the leveling / Apply stored handoff, else the light's
// current values.
func (m *Mono) scanLightValues() ([3]int, string, bool) {
	h, lu := m.hdr, m.hdr.Light
	if ho := h.handoff; ho != nil {
		return ho.Values, ho.Source, true
	}
	if lu != nil && lu.cl != nil {
		if cs := lu.cl.CachedState(); cs != nil {
			return cs.Value, "the light's current values", true
		}
	}
	return [3]int{}, "", false
}

func (m *Mono) reason() (short, full string) {
	cam := m.getCam()
	lu := m.hdr.Light
	switch {
	case m.running.Load():
		return "scan in progress", "A mono trichrome scan is running."
	case m.connBusy.Load():
		return "camera busy", "Camera operation in progress."
	case cam == nil:
		return "camera not connected", "Hikrobot camera not connected (Connect above, or use the simulator)."
	case lu == nil || !lu.connected():
		if _, sim := cam.(*hikcam.Sim); sim {
			return "", ""
		}
		return "RGB light not connected", "RGB light not connected (Camera and Light > Light > Connect)."
	case lu.busy:
		return "RGB light busy", "RGB light busy."
	}
	return "", ""
}

// MonoButtons is updateButtons: which controls are enabled.
type MonoButtons struct {
	ConnText                              string
	Conn, DevList, Refresh, Apply         bool
	Capture, Stop, Bracket, Edit, Options bool
}

// Buttons is updateButtons: the enabled controls; it also refreshes the
// status lines and logs a change of the Capture-disabled reason.
func (m *Mono) Buttons() MonoButtons {
	cam := m.getCam()
	running := m.running.Load()
	connBusy := m.connBusy.Load()
	why, whyFull := m.reason()
	b := MonoButtons{
		ConnText: map[bool]string{true: "Disconnect", false: "Connect"}[cam != nil],
		Conn:     !running && !connBusy,
		DevList:  cam == nil && !running && !connBusy,
		Refresh:  cam == nil && !running && !connBusy,
		Apply:    cam != nil && !running && !connBusy,
		Capture:  why == "" && !running,
		Stop:     m.hasCancel(),
		Bracket:  !running && m.CapMode == 1,
		Edit:     true,
		Options:  true,
	}
	m.SDKText = "SDK: " + m.hik.getSDKMsg()
	if cam == nil {
		m.CamText = "Camera: not connected"
	} else {
		m.CamText = "Camera: " + cam.Info().Label()
	}
	lu := m.hdr.Light
	if lu == nil || !lu.connected() {
		m.LightText = "Light: not connected"
	} else if cs := lu.cl.CachedState(); cs != nil {
		m.LightText = "Light now: " + cs.String()
	} else {
		m.LightText = "Light: connected"
	}
	if v, src, ok := m.scanLightValues(); ok {
		m.ValText = fmt.Sprintf("Scan values: R %d  G %d  B %d", v[0], v[1], v[2])
		m.ValTip = fmt.Sprintf("Scan values: R %d  G %d  B %d (%s)", v[0], v[1], v[2], src)
	} else {
		m.ValText, m.ValTip = "Scan values: -", ""
	}
	roll := m.hdr.Cfg.Roll
	if roll == "" {
		roll = "none"
	}
	m.OutText = fmt.Sprintf("Output: %s  (roll: %s)", m.hdr.outDir(), roll)
	m.OutTip = fmt.Sprintf("Output: %s, roll %s (same folder as White light scan)", m.hdr.outDir(), roll)
	if why != m.why {
		if why == "" {
			m.say("Capture ready.")
		} else {
			m.say("Capture disabled: " + whyFull)
		}
		m.why = why
	}
	if why == "" {
		m.WhyText, m.WhyTip = m.readyStatusText(), m.readyStatusTooltip()
	} else {
		m.WhyText, m.WhyTip = "Capture disabled: "+why, "Capture disabled: "+whyFull
	}
	m.updateBaseExposureLabel()
	return b
}

// Start is AfterCreate. The SDK status (a dlopen of
// MvCameraControl.framework) is read on the camera worker.
func (m *Mono) Start() {
	m.fillBestScanUI()
	m.hdr.applySharedHikFromConfig()
	m.loadCaptureOptions()
	m.updateCaptureLabel()
	m.updateBaseExposureLabel()
	m.say(appVersion + " Mono trichrome page ready. Best scan quality is selected; Capture uses the shared exposure for R, G and B unless Auto-meter each channel is checked.")
	m.RefreshDevices()
}

// RefreshDevices is the Refresh button: the simulator plus the cameras MVS
// lists. Without MvCameraControl.framework the list is the simulator only
// and the SDK line says why.
func (m *Mono) RefreshDevices() {
	h := m.hik
	m.worker().Do(func() {
		res := h.enumerate()
		h.setSDK(res.Message(), res.Cameras)
		items := []string{hikcam.OpenSim().Info().Label()}
		for _, d := range res.Cameras {
			items = append(items, d.Label())
		}
		m.ui(func() {
			cur := m.DevIdx
			m.DevItems = items
			if cur < 0 || cur >= len(items) {
				m.DevIdx = 0
			}
			if res.Err != nil {
				m.say(res.Message())
			} else if len(res.Cameras) == 0 {
				m.say(res.Message() + ". The simulator is always available.")
			} else {
				m.say(fmt.Sprintf("%s.", res.Message()))
			}
		})
	})
}

// ToggleConnect is the Connect / Disconnect button.
func (m *Mono) ToggleConnect() {
	if m.running.Load() || m.connBusy.Load() {
		return
	}
	if cam := m.getCam(); cam != nil {
		m.connBusy.Store(true)
		m.stopPreviewAsync()
		m.hik.setCam(nil)
		m.worker().Do(func() {
			m.worker().StopSync()
			_ = cam.Close()
			m.ui(func() {
				m.connBusy.Store(false)
				m.say("Disconnected.")
			})
		})
		return
	}
	idx := m.DevIdx
	devs := m.hik.getDevices()
	var realInfo *hikcam.Info
	if idx > 0 {
		realIdx := idx - 1
		if realIdx < 0 || realIdx >= len(devs) {
			m.say("Select a camera first.")
			return
		}
		info := devs[realIdx]
		realInfo = &info
	}
	st := m.readSettings()
	m.connBusy.Store(true)
	m.stopPreviewAsync()
	h := m.hik
	m.worker().Do(func() {
		m.worker().StopSync()
		var dev hikcam.Device
		var err error
		if realInfo == nil {
			dev = hikcam.OpenSim()
		} else {
			dev, err = h.openReal(*realInfo)
		}
		if err != nil {
			m.ui(func() {
				m.connBusy.Store(false)
				m.say("Connect failed: " + err.Error())
				m.hdr.Host.Message("Hikrobot camera", err.Error())
			})
			return
		}
		applyErr := dev.Apply(st)
		h.setCam(dev)
		m.ui(func() {
			m.connBusy.Store(false)
			m.say("Opened " + dev.Info().Label())
			if applyErr != nil {
				m.say("Apply settings: " + applyErr.Error())
			}
			m.StartPreview()
		})
	})
}

func (m *Mono) expValue() float64 { return specExp.value(m.ExpText, 10000) }

func (m *Mono) readSettings() hikcam.Settings {
	st := hikcam.DefaultSettings()
	st.ExposureUS = m.expValue()
	st.GainDB = specGain.value(m.GainText, 0)
	st.BlackLevel = specBlack.value(m.BlackText, float64(hikcam.BestScanBlackLevel))
	if i := m.FmtIdx; i >= 0 && i < len(hikcam.PixelFormatChoices) {
		st.PixelFormat = hikcam.PixelFormatChoices[i]
	}
	if m.PresetIdx == 1 {
		st.QualityPreset = true
	}
	st.GammaEnable = m.Gamma
	st.FullFrame = m.Full
	st.Width = int(specSize.value(m.WText, 6016))
	st.Height = int(specSize.value(m.HText, 4096))
	st.OffsetX = int(specSize.value(m.XText, 0))
	st.OffsetY = int(specSize.value(m.YText, 0))
	st.SoftwareTrigger = m.TrigIdx == 1
	return st.Clamp()
}

// setExpField is expNE.SetValue (the shared exposure moved).
func (m *Mono) setExpField(us float64) {
	if t := specExp.text(us); t != m.ExpText {
		m.ExpText = t
		m.FieldSeq++
	}
}

func (m *Mono) setGainField(db float64) {
	if t := specGain.text(db); t != m.GainText {
		m.GainText = t
		m.FieldSeq++
	}
}

// ExposureEdited: the Exposure field was edited (OnValueChanged).
func (m *Mono) ExposureEdited() {
	v := specExp.value(m.ExpText, m.hdr.sharedHikExposure())
	m.ExpText = specExp.text(v)
	m.FieldSeq++
	m.hdr.setSharedHikExposure(v, "user")
}

// FieldEdited normalises a number field after editing (NumberEdit shows
// its value in range with its decimals).
func (m *Mono) FieldEdited() {
	m.GainText = specGain.text(specGain.value(m.GainText, 0))
	m.BlackText = specBlack.text(specBlack.value(m.BlackText, float64(hikcam.BestScanBlackLevel)))
	m.WText = specSize.text(specSize.value(m.WText, 6016))
	m.HText = specSize.text(specSize.value(m.HText, 4096))
	m.XText = specSize.text(specSize.value(m.XText, 0))
	m.YText = specSize.text(specSize.value(m.YText, 0))
	m.FieldSeq++
}

func (m *Mono) usingBestScan() bool { return m.PresetIdx == 1 }

func (m *Mono) fillBestScanUI() {
	st := hikcam.BestScanQuality()
	m.FmtIdx = hikcam.PixelFormatIndex(st.PixelFormat)
	us := st.ExposureUS
	if m.hdr.Cfg.HikExposureUS >= hikcam.CS240ExpMinUS {
		us = hikbase.ClampExposureUS(m.hdr.Cfg.HikExposureUS)
	}
	m.ExpText = specExp.text(us)
	m.GainText = specGain.text(st.GainDB)
	m.BlackText = specBlack.text(st.BlackLevel)
	m.Gamma, m.Full, m.TrigIdx = false, true, 1
	m.WText, m.HText = specSize.text(float64(hikcam.CS240Width)), specSize.text(float64(hikcam.CS240Height))
	m.XText, m.YText = "0", "0"
	m.FieldSeq++
}

// PresetChanged: the Preset combo changed PresetIdx.
func (m *Mono) PresetChanged() {
	if m.PresetIdx == 1 {
		m.fillBestScanUI()
		m.say("Preset: Best scan quality (MV-CS240-90UM). R, G and B use the shared exposure (gain 0 dB) unless Auto-meter each channel is checked.")
	} else {
		m.say("Preset: Custom camera settings.")
	}
	m.updateCaptureLabel()
}

func (m *Mono) captureEVs() []int {
	if m.CapMode == 1 {
		br := clampIdx(m.BrIdx, len(monoBracketEVs))
		return append([]int(nil), monoBracketEVs[br]...)
	}
	return []int{0}
}

func (m *Mono) framesPerExposure() int {
	return int(math.Round(clampFloat(specFPE.value(m.FPEText, framesPerExpMin), framesPerExpMin, framesPerExpMax)))
}

// captureOptions reads the capture options from the page. The noise model
// is always the MV-CS240-90UM sensor model (hdrscan.DefaultMonoNoise).
func (m *Mono) captureOptions() monocap.Options {
	return monocap.Options{FramesPerExposure: m.framesPerExposure(), Noise: hdrscan.DefaultMonoNoise()}
}

// loadCaptureOptions restores mode, bracket and frames per exposure.
func (m *Mono) loadCaptureOptions() {
	c := m.hdr.Cfg
	m.loading = true
	defer func() { m.loading, m.optsLoaded = false, true }()
	if c.HikHDR {
		m.CapMode = 1
	} else {
		m.CapMode = 0
	}
	m.BrIdx = clampIdx(c.HikBracket, len(monoBracketEVs))
	m.FPEText = specFPE.text(float64(max(framesPerExpMin, c.HikFramesPerExp)))
	m.FieldSeq++
}

func (m *Mono) saveCaptureOptions() {
	if m.loading || !m.optsLoaded {
		return // never overwrite the config before it was restored
	}
	m.hdr.Cfg.HikHDR = m.CapMode == 1
	m.hdr.Cfg.HikBracket = clampIdx(m.BrIdx, len(monoBracketEVs))
	m.hdr.Cfg.HikFramesPerExp = m.framesPerExposure()
	m.hdr.Save()
}

func (m *Mono) readyStatusText() string {
	evs := m.captureEVs()
	if m.hdr.hikMeterEach() {
		return "Ready. Auto-meter each channel is on."
	}
	baseUS := m.hdr.sharedHikExposure()
	return fmt.Sprintf("Ready. Capture about %.1f s (exposure + readout, excludes light settle).",
		monocap.EstimateCapture(baseUS, evs, m.framesPerExposure()).Seconds())
}

func (m *Mono) readyStatusTooltip() string {
	evs := m.captureEVs()
	if m.hdr.hikMeterEach() {
		return fmt.Sprintf("Ready. Auto-meter each channel is on (each channel meters separately from %.0f us). Mode: %s.",
			m.hdr.sharedHikExposure(), evText(evs))
	}
	baseUS := m.hdr.sharedHikExposure()
	return fmt.Sprintf("Ready. Shared exposure %.0f us, 0 dB (%s). Estimate %.1f s covers exposure + readout only; RGB light settling, HDR merge, CA correction and TIFF write add extra time.",
		baseUS, evText(evs), monocap.EstimateCapture(baseUS, evs, m.framesPerExposure()).Seconds())
}

// updateCaptureLabel saves the capture options and sets the Capture label
// (Mode, HDR bracket and Frames per exposure call it).
func (m *Mono) updateCaptureLabel() {
	m.saveCaptureOptions()
	if len(m.captureEVs()) > 1 {
		m.CapText = "Capture mono trichrome HDR"
	} else {
		m.CapText = "Capture mono trichrome"
	}
}

// OptionsChanged: Mode, HDR bracket or Frames per exposure changed.
func (m *Mono) OptionsChanged() {
	m.FPEText = specFPE.text(float64(m.framesPerExposure()))
	m.FieldSeq++
	m.updateCaptureLabel()
}

// MeterEachChanged: the Auto-meter each channel check box was toggled.
func (m *Mono) MeterEachChanged() {
	m.hdr.Cfg.HikMeterEach = m.MeterEach
	m.hdr.Save()
	m.updateCaptureLabel()
}

// MarkChanged is setCenterMark: the display-only centre / flatness overlay.
func (m *Mono) MarkChanged() {
	m.hdr.Cfg.CenterMark = m.Mark
	m.hdr.Save()
}

func (m *Mono) updateBaseExposureLabel() {
	us := m.hdr.sharedHikExposure()
	snap := m.hdr.hikSnapshot()
	rgb := m.hdr.currentLightRGB()
	m.BaseExpText = hikbase.Status(snap, us, rgb)
	m.BaseExpOK = hikbase.IsLeveled(snap, us, rgb)
}

func (m *Mono) stopPreviewAsync() {
	m.previewOn.Store(false)
	m.worker().Bump()
}

// StopPreview ends the live view (another page is shown).
func (m *Mono) StopPreview() { m.stopPreviewAsync() }

// StartPreview starts the Hikrobot live view (the page is shown or the
// camera was connected).
func (m *Mono) StartPreview() {
	cam := m.getCam()
	if cam == nil || m.running.Load() || m.connBusy.Load() {
		return
	}
	st := m.readSettings()
	gen := m.worker().Bump()
	h := m.hik
	m.worker().Do(func() {
		m.worker().StopSyncIfNeeded()
		h.stopPreviewSync() // the Light page's preview of the same camera
		if m.worker().Gen() != gen || m.running.Load() || m.connBusy.Load() {
			return
		}
		cur := m.getCam()
		if cur == nil || cur != cam {
			return
		}
		summary, err := applyLiveSettings(cur, st, false)
		if err != nil {
			msg := "Live settings: " + err.Error()
			m.ui(func() { m.say(msg) })
		} else if summary != "" {
			m.ui(func() { m.say(summary) })
		}
		if err := cur.StartGrab(); err != nil {
			msg := "Live grab: " + err.Error()
			m.ui(func() { m.say(msg) })
			return
		}
		m.previewOn.Store(true)
		if !m.worker().StartLoop(gen, func(valid func() bool) {
			defer func() {
				m.previewOn.Store(false)
				_ = cur.StopGrab()
			}()
			misses := 0
			for valid() && m.previewOn.Load() && m.getCam() == cur {
				fr, err := cur.Grab(hikcam.GrabTimeout(cur.Settings().ExposureUS))
				if err != nil {
					if !valid() || !m.previewOn.Load() {
						break
					}
					misses++
					if msg, log := liveGrabMessage(err, misses); log {
						mm := msg
						m.ui(func() { m.say(mm) })
					}
					time.Sleep(50 * time.Millisecond)
					continue
				}
				misses = 0
				if m.previewUI.CompareAndSwap(false, true) {
					img := hikcam.PreviewGray(fr.Pix, fr.W, fr.H, hikcam.LivePreviewMaxEdge)
					what := fmt.Sprintf("Live  %dx%d  %s  %.0f us", fr.W, fr.H, fr.PixelFormat, fr.ExposureUS)
					m.ui(func() {
						defer m.previewUI.Store(false)
						if m.getCam() != cur || !valid() || !m.previewOn.Load() {
							return
						}
						m.Frame, m.ViewWhat = img, what
						m.FrameSeq++
					})
				}
				if cur.Info().Simulated {
					time.Sleep(40 * time.Millisecond)
				}
			}
		}) {
			m.previewOn.Store(false)
			_ = cur.StopGrab()
		}
	})
}

// PreviewRunning reports a running live view (tests).
func (m *Mono) PreviewRunning() bool { return m.previewOn.Load() }

// Connected reports an open camera.
func (m *Mono) Connected() bool { return m.getCam() != nil }

// Running reports a capture in progress.
func (m *Mono) Running() bool { return m.running.Load() }

// ConnBusy reports a connect, disconnect or apply in progress.
func (m *Mono) ConnBusy() bool { return m.connBusy.Load() }

// ApplySettings is the Apply camera settings button.
func (m *Mono) ApplySettings() {
	if m.usingBestScan() {
		m.fillBestScanUI()
	}
	cam := m.getCam()
	if cam == nil || m.running.Load() || m.connBusy.Load() {
		return
	}
	st := m.readSettings()
	live := m.previewOn.Load()
	m.connBusy.Store(true)
	m.stopPreviewAsync()
	m.worker().Do(func() {
		m.worker().StopSync()
		err := cam.Apply(st)
		got := cam.Settings()
		m.ui(func() {
			m.connBusy.Store(false)
			if err != nil {
				m.say("Apply failed: " + err.Error())
			} else {
				m.say(fmt.Sprintf("Applied: %.0f us, %.2f dB, black %.0f, %s, trigger=%v, %dx%d @ %d,%d",
					got.ExposureUS, got.GainDB, got.BlackLevel, got.PixelFormat, got.SoftwareTrigger,
					got.Width, got.Height, got.OffsetX, got.OffsetY))
			}
			if live {
				m.StartPreview()
			}
		})
	})
}

// monoResult is what the capture job hands back to the UI goroutine.
type monoResult struct {
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

// Capture is Capture mono trichrome: apply the settings, R, G and B on the
// camera worker (monocap.Capture), then the CPU merge
// (hdrscan.ProcessMonoTrichrome) on the same worker.
func (m *Mono) Capture() {
	if why, whyFull := m.reason(); why != "" {
		m.say("Capture refused: " + whyFull)
		return
	}
	h, lu := m.hdr, m.hdr.Light
	h.Save()
	dir, err := h.rollDir()
	if err != nil {
		h.Host.Message("Output folder", err.Error())
		return
	}
	vals, src, ok := m.scanLightValues()
	if !ok {
		vals = [3]int{50, 50, 50}
		src = "default 50 (light values unknown)"
	}
	settle := 300 * time.Millisecond
	if lu != nil {
		if ms, perr := strconv.Atoi(strings.TrimSpace(lu.SettleText)); perr == nil && ms >= 0 && ms <= 5000 {
			settle = time.Duration(ms) * time.Millisecond
		}
	}
	correct := m.CA
	meterEach := h.hikMeterEach()
	baseUS := h.sharedHikExposure()
	snap := h.hikSnapshot()
	if !meterEach {
		m.setExpField(baseUS)
		m.setGainField(0)
	}
	evs := m.captureEVs()
	cam := m.getCam()
	if cam == nil {
		return
	}
	st := m.readSettings()
	var sw *rgblight.ChannelSwitch
	if lu != nil && lu.connected() {
		sw = &rgblight.ChannelSwitch{C: lu.cl, Values: vals, Log: func(s string) { h.Log.Printf("Mono trichrome: %s", s) }}
	}
	var light monocap.Light
	if sw != nil {
		light = sw
	} else if sim, ok := cam.(*hikcam.Sim); ok {
		light = monocap.SimLight{S: sim}
	}
	opt := m.captureOptions()
	box := h.ROI
	roll, last := h.Cfg.Roll, h.Cfg.Counter
	// scanMetadata, metadataLeveling and the shared-exposure metadata are
	// read here on the UI goroutine (Windows reads the last one on the
	// camera worker); the fields they set are the same.
	scanMeta := h.withHikExposureMeta(h.scanMetadata(), baseUS, meterEach)
	levelMeta := h.metadataLeveling()

	ctx, cancel := context.WithCancel(context.Background())
	m.setCancel(cancel)
	m.running.Store(true)
	m.stopPreviewAsync()

	m.say(fmt.Sprintf("==== Mono trichrome: light R %d G %d B %d (%s), settle %v + stability check, CA correction %v, bracket %v, %d frame(s) per exposure, noise model %.0f e- / %.2f e- (MV-CS240-90UM datasheet) ====",
		vals[0], vals[1], vals[2], src, settle, correct, evs, opt.FramesPerExposure, opt.Noise.FullWellE, opt.Noise.ReadE))
	m.say(hikbase.CaptureLog(meterEach, baseUS, snap, vals))
	if len(evs) > 1 {
		m.setRep(fmt.Sprintf("Capturing R, G, B HDR (%s) ...", evText(evs)))
	} else {
		m.setRep("Capturing R, G, B (single frame per channel) ...")
	}
	m.Progress = true

	m.worker().Do(func() {
		m.worker().StopSync()
		var res monoResult
		defer func() {
			m.ui(func() { m.captureDone(res, evs) })
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
		m.ui(func() {
			m.say(fmt.Sprintf("Applied: %.0f us, %.2f dB, black %.0f, %s, trigger=%v, %dx%d @ %d,%d",
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
		opt.Log = func(s string) { m.ui(func() { m.say(s) }) }
		opt.Status = func(s string) { m.ui(func() { m.setRep(s) }) }
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
		m.ui(func() {
			h.Cfg.Counter = num
			h.Save()
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
			Progress:      func(s string) { m.ui(func() { m.say(s) }) },
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

// captureDone runs on the UI goroutine after every capture.
func (m *Mono) captureDone(res monoResult, evs []int) {
	m.setCancel(nil)
	m.running.Store(false)
	m.Progress = false
	switch {
	case res.applyErr != nil:
		m.say("Apply settings: " + res.applyErr.Error())
		m.setRep("Apply settings failed: " + res.applyErr.Error())
		m.hdr.Host.Message("Mono trichrome", "Could not apply camera settings:\n\n"+res.applyErr.Error())
	case errors.Is(res.err, context.Canceled):
		msg := "Mono trichrome stopped."
		m.say(msg)
		m.setRep(msg)
	case res.err != nil:
		msg := "Mono trichrome failed: " + res.err.Error()
		m.say(msg)
		m.setRep(msg)
	default:
		modeTag := "Single"
		if len(evs) > 1 {
			modeTag = "HDR " + evText(evs)
		}
		var warn string
		if res.move.Warn {
			warn += res.move.String() + "\n"
		}
		for _, n := range res.notes {
			warn += n + "\n"
		}
		msg := fmt.Sprintf("%sSaved %s  (%s, capture %.1f s, total %.1f s)\n%s\n%s\n%s", warn, res.path, modeTag, res.capS, res.ms, res.move.String(), res.hdr, res.report.String())
		m.say(strings.ReplaceAll(msg, "\n", " | "))
		m.setRep(msg)
	}
	m.StartPreview()
}
