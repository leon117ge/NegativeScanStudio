package hdrctl

// Light is the Camera & Light > Light page (lightpage.go LightUI): RGB Light
// Air control and automatic film-base leveling. The non-UI declarations are
// the Windows ones (copied.go); this file ports the UI-bound methods line by
// line, with walk widgets replaced by fields the Gio page binds to, message
// boxes by Host.Message / Host.Confirm, and mw.Synchronize by Host.Post.
// Serial-port jobs run on the light worker goroutine, captures on the
// camera worker; nothing runs on, or blocks, the main thread.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"basetool/camera"
	"basetool/hikbase"
	"basetool/hikcam"
	"basetool/lightlevel"
	"basetool/rgblight"
	"basetool/scanconfig"
)

// Light is the state of the Light page.
type Light struct {
	hdr *Ctl
	cfg lightConfig
	lw  *camWorker // light jobs (serial port)

	cl          *rgblight.Client
	sim         *rgblight.SimDevice
	port        string
	info        rgblight.Info
	ports       []rgblight.PortInfo
	busy        bool // a light job or the leveling runs (UI goroutine)
	level       bool // leveling runs
	cancel      context.CancelFunc
	lastSuccess *levelingRecord

	lastRes    *lightlevel.Result
	lastExp    camera.Exposure
	lastTgt    float64
	hikPrevGen atomic.Int64
	lastPlan   camPlan
	lastPost   [3]float64 // final base after the matrix
	lastTIFF   bool       // lastPost measured in the trichrome TIFF (else predicted)
	presets    *scanconfig.Store
	inPreset   bool
	saveMu     sync.Mutex

	// OnRouting re-evaluates which live view the page shows (Windows
	// hdr.updateLiveRouting); set by the UI.
	OnRouting func()

	// ---- controls (UI goroutine; the Gio page binds to these) ----
	PortItems    []string
	PortIdx      int
	InfoText     string // infoLbl
	TelText      string // telLbl
	StateText    string // stateLbl
	En           [3]bool
	Val          [3]string // brightness LineEdits
	TolText      string
	IterText     string
	SettleText   string
	HikExpText   string
	CamProfile   bool // "Apply camera profile" checked
	CamProfileOK bool // enabled (Bayer camera)
	CamNote      string
	ResText      string
	FieldSeq     int // bumped when the controller writes the text fields

	PresetItems []string
	PresetIdx   int
	NameText    string
	StockText   string
	NotesText   string
	PresetText  string

	Faults   []string
	logLines []string
	LogSeq   int

	// last leveling captures (not live)
	Comp     image.Image
	Thumbs   [3]image.Image
	ThumbTxt [3]string
	ImgSeq   int
	ViewLbl  string

	// 500T illuminant calculator (castpage.go)
	CastIdx    int
	KelvinText string
	CastOn     bool
	CastText   string
}

func newLight(h *Ctl) *Light {
	l := &Light{hdr: h, PortIdx: 0, PresetIdx: 0}
	l.cfg = loadLightConfig(h.Cfg.ISO)
	l.lw = newCamWorker()
	l.lw.onPanic = func(r any, st string) {
		h.Log.Printf("light job panic: %v\n%s", r, st)
		h.Host.Post(func() { l.busy, l.level = false, false })
	}
	l.PortItems = []string{simLightItem, simNoAckItem}
	pct := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	for c := 0; c < 3; c++ {
		l.Val[c] = strconv.Itoa(l.cfg.Values[c])
	}
	l.TolText, l.IterText, l.SettleText = pct(l.cfg.Tol), strconv.Itoa(l.cfg.MaxIter), strconv.Itoa(l.cfg.SettleMS)
	l.CamProfile, l.CamProfileOK = !l.cfg.NoCam, true
	l.CamNote = camProfileBayerShort
	l.HikExpText = "10000"
	l.InfoText, l.TelText = "Not connected", " "
	l.ViewLbl = "Captures appear after leveling."
	l.PresetItems = []string{presetNone}
	l.CastIdx, l.KelvinText = 0, "5500"
	l.CastText = fmt.Sprintf("Preview: enabled=%v, 500T @ %.0fK.\nAdd density R %+.4f  G %+.4f  B %+.4f.\nPlanck approximation, not measured WB. No light changes.", false, 5500.0, 0.0, 0.0, 0.0)
	return l
}

// Settings is the persisted rgb_light.json state (combos bind to its fields).
func (l *Light) Settings() *lightConfig { return &l.cfg }

func (l *Light) ui(f func()) { l.hdr.Host.Post(f) }

func (l *Light) log(f string, a ...any) {
	s := fmt.Sprintf(f, a...)
	if l.hdr.Log != nil {
		l.hdr.Log.Printf("Light: %s", s) // HDR log file keeps a record too
	}
	l.ui(func() { l.appendLog(s) })
}

func (l *Light) appendLog(s string) {
	l.logLines = append(l.logLines, time.Now().Format("15:04:05  ")+s)
	if len(l.logLines) > maxLog {
		l.logLines = l.logLines[len(l.logLines)-maxLog:]
	}
	l.LogSeq++
}

// LogText is the page's log pane.
func (l *Light) LogText() string { return strings.Join(l.logLines, "\n") }

// FaultText is the "Faults reported by the light" pane.
func (l *Light) FaultText() string { return strings.Join(l.Faults, "\n") }

func (l *Light) msg(title, text string) { l.hdr.Host.Message(title, text) }

func (l *Light) saveCfg() {
	l.saveMu.Lock()
	defer l.saveMu.Unlock()
	if b, err := json.MarshalIndent(l.cfg, "", "  "); err == nil {
		os.MkdirAll(configDir(), 0o755)
		os.WriteFile(lightConfigFile(), b, 0o644)
	}
}

// manualBase is the user-drawn clear-base rectangle when Film base is set
// to Rectangle (drawn here or synced from Camera live view).
func (l *Light) manualBase() (lightlevel.Rect, bool) {
	if l.cfg.AreaMode == 1 && l.cfg.AreaPicked && !l.cfg.Area.Empty() {
		return l.cfg.Area, true
	}
	return lightlevel.Rect{}, false
}

// ManualBase is manualBase for the page's drawing.
func (l *Light) ManualBase() (lightlevel.Rect, bool) { return l.manualBase() }

// publishRect stores the pick on the Light page and on Camera, so the
// leveling TIFF check, the RAW measure box and the frame-0 handoff lock
// all use the same small patch.
func (l *Light) publishRect(r lightlevel.Rect) {
	if r.Empty() {
		return
	}
	l.cfg.Area, l.cfg.AreaMode, l.cfg.AreaPicked = r, 1, true
	if h := l.hdr; h != nil {
		h.ROI = toScanRect(r)
		h.Cfg.AreaMode = 1
		h.Save()
	}
	l.saveCfg()
}

// AreaModeChanged: the Film base combo changed Settings().AreaMode.
func (l *Light) AreaModeChanged() {
	m := l.cfg.AreaMode
	if m < 0 {
		return
	}
	l.cfg.AreaMode = m & 1
	if l.cfg.AreaMode == 1 {
		if h := l.hdr; h != nil && !h.ROI.Empty() {
			l.cfg.Area = toLightRect(h.ROI)
			l.cfg.AreaPicked = true
		}
	}
	if h := l.hdr; h != nil {
		h.Cfg.AreaMode = l.cfg.AreaMode
		h.Save()
	}
	l.saveCfg()
}

func (l *Light) connected() bool { return l.cl != nil && !l.cl.Closed() }

// Connected reports an open light connection.
func (l *Light) Connected() bool { return l.connected() }

// Leveling reports a running leveling.
func (l *Light) Leveling() bool { return l.level }

// LightButtons is updateButtons / updatePresetButtons.
type LightButtons struct {
	ConnText                                         string
	Conn, Ports, Apply, Read, Off, Start, Stop, Copy bool
	PresetLoad, PresetDel, PresetSave, PresetFile    bool
}

func (l *Light) Buttons() LightButtons {
	conn := l.connected()
	idle := conn && !l.busy
	b := LightButtons{ConnText: "Connect", Conn: !l.busy || !conn, Ports: !conn,
		Apply: idle, Read: idle, Off: idle, Start: idle, Stop: l.level,
		Copy: !l.busy && l.lastSuccess != nil}
	if conn {
		b.ConnText = "Disconnect"
	}
	has := l.curPreset() != nil
	b.PresetLoad, b.PresetDel = !l.busy && has, !l.busy && has
	b.PresetSave, b.PresetFile = !l.busy, !l.busy
	return b
}

// RefreshPorts lists the serial ports (on the light worker: on macOS the
// list comes from IOKit / ioreg) and selects the light.
func (l *Light) RefreshPorts() {
	l.lw.do(func() {
		ps, err := rgblight.ListPorts()
		l.ui(func() { l.setPorts(ps, err) })
	})
}

func (l *Light) setPorts(ps []rgblight.PortInfo, err error) {
	if err != nil {
		l.log("Serial port list: %v", err)
	}
	l.ports = ps
	items := []string{}
	sel := -1
	for i, p := range ps {
		items = append(items, p.Label())
		if strings.EqualFold(p.Name, l.cfg.Port) {
			sel = i
		}
	}
	items = append(items, simLightItem, simNoAckItem)
	if sel < 0 {
		sel = 0 // first port; ListPorts sorts the light (VID 0483 / PID 5740) first
		switch {
		case l.cfg.Port == "SIM-NOACK":
			sel = len(items) - 1
		case len(ps) == 0 || l.cfg.Port == "SIM":
			sel = len(items) - 2
		}
	}
	l.PortItems, l.PortIdx = items, sel
	n := 0
	for _, p := range ps {
		if p.Light {
			n++
		}
	}
	switch {
	case n == 1:
		l.InfoText = "RGB Light Air found on " + ps[0].Name + " (USB 0483:5740). Press Connect."
	case n > 1:
		l.InfoText = fmt.Sprintf("%d lights found - choose the port.", n)
	case len(ps) > 0:
		l.InfoText = "No port with USB ID 0483:5740 - choose the COM port by hand, or check the cable."
	default:
		l.InfoText = "No serial ports. Plug the light in (USB-C data cable) and press Refresh."
	}
}

// ToggleConnect is the Connect / Disconnect button.
func (l *Light) ToggleConnect() {
	if l.connected() {
		l.disconnect()
		return
	}
	i := l.PortIdx
	sim := i < 0 || i >= len(l.ports)
	noAck := sim && i == len(l.ports)+1
	name := "SIM"
	if noAck {
		name = "SIM-NOACK"
	}
	if !sim {
		name = l.ports[i].Name
	}
	l.cfg.Port = name
	l.saveCfg()
	l.busy = true
	l.lw.do(func() {
		var rw interface {
			Read([]byte) (int, error)
			Write([]byte) (int, error)
			Close() error
		}
		var sd *rgblight.SimDevice
		if sim {
			sd = rgblight.NewSimDevice()
			if noAck {
				sd.Protocol, sd.Firmware = "1.4", "V1.0.5"
				sd.OddAck = rgblight.AckEventOnly
			}
			sd.Start()
			rw = sd
		} else {
			p, err := rgblight.OpenPort(name)
			if err != nil {
				l.ui(func() {
					l.busy = false
					l.log("Cannot open %s: %v", name, err)
					l.msg("RGB light", "Cannot open "+name+":\n\n"+err.Error()+"\n\nClose other programs that use the light (CuteNew software, serial monitors) and try again.")
				})
				return
			}
			rw = p
		}
		c := rgblight.NewClient(rw)
		l.hook(c)
		c.Start()
		info, err := c.Info()
		var st rgblight.State
		if err == nil {
			st, err = c.ReadState()
		}
		if err != nil {
			c.Close()
			l.ui(func() {
				l.busy = false
				l.log("Connect %s failed: %v", name, err)
				l.msg("RGB light", "The light on "+name+" did not answer:\n\n"+err.Error()+"\n\nCheck the port and that the light is powered.")
			})
			return
		}
		l.ui(func() {
			l.cl, l.sim, l.port, l.info = c, sd, name, info
			l.busy = false
			t := "Connected: " + name + "\n" + info.String()
			if info.Protocol != "1.3" {
				t += "\nNOTE: this program follows protocol 1.3; the light reports " + info.Protocol + "."
			}
			if !strings.Contains(strings.ToUpper(info.Model), "CUTENEW") {
				t += "\nNOTE: unexpected product name."
			}
			l.InfoText = t
			l.log("Connected to %s: %s", name, info)
			l.showState(st, true)
			if l.PresetIdx > 0 && l.PresetIdx < len(l.PresetItems) {
				l.log("Preset %s is applied only when you choose it or press Load preset.", l.PresetItems[l.PresetIdx])
			}
		})
	})
}

// hook wires the client's unsolicited reports to the page.
func (l *Light) hook(c *rgblight.Client) {
	c.OnLog = func(s string) { l.log("%s", s) }
	// v0.81: full serial trace, file only (too much for the page's log box)
	c.OnRaw = func(s string) {
		if l.hdr.Log != nil {
			l.hdr.Log.Printf("Light RAW: %s", s)
		}
	}
	c.OnTelemetry = func(t rgblight.Telemetry) {
		l.ui(func() {
			if l.cl != c {
				return
			}
			capt := "idle"
			if t.Capturing {
				capt = "CAPTURING"
			}
			l.TelText = fmt.Sprintf("Temperature %.1f °C    Supply %.2f V    %s", t.TempC, t.Volts, capt)
		})
	}
	c.OnState = func(s rgblight.State) {
		l.ui(func() {
			if l.cl == c {
				l.showState(s, false)
			}
		})
	}
	c.OnProgress = func(p rgblight.Progress) { l.log("Light %s", p) }
	c.OnFault = func(f rgblight.Fault) {
		l.ui(func() {
			l.Faults = append(l.Faults, time.Now().Format("15:04:05  ")+f.String()+" (the active field is shown raw: the protocol does not say whether it means raised or cleared)")
			l.log("FAULT: %s", f)
		})
	}
	c.OnClosed = func(err error) {
		l.ui(func() {
			if l.cl != c {
				return
			}
			l.log("Connection closed: %v", err)
			l.cl, l.sim = nil, nil
			l.InfoText = "Not connected (" + err.Error() + ")"
			l.TelText = " "
		})
	}
}

func (l *Light) disconnect() {
	c := l.cl
	if c == nil {
		return
	}
	l.busy = true
	l.lw.do(func() {
		err := c.SafeClose(30 * time.Second)
		l.ui(func() {
			l.busy = false
			if l.cl == c {
				l.cl, l.sim = nil, nil
			}
			if err != nil {
				l.log("Disconnected - WARNING: %v", err)
				l.InfoText = "Disconnected. The light may still be on: " + err.Error()
			} else {
				l.log("All channels switched off (confirmed by state read) and disconnected.")
				l.InfoText = "Disconnected (light switched off)."
			}
			l.TelText = " "
		})
	})
}

// Shutdown is called when the window closes: the same planned disconnect,
// with a short idle wait.
func (l *Light) Shutdown() {
	l.stopHikPreview()
	if c := l.cl; c != nil && !c.Closed() {
		if l.cancel != nil {
			l.cancel()
		}
		if err := c.SafeClose(5 * time.Second); err != nil && l.hdr.Log != nil {
			l.hdr.Log.Printf("Light: on exit: %v", err)
		}
	}
}

func (l *Light) showState(s rgblight.State, toEdits bool) {
	t := s.String()
	if s.Focus < 0 {
		t += " (legacy state report without focus)"
	}
	l.StateText = "Light reports: " + t
	if toEdits {
		for c := 0; c < 3; c++ {
			l.En[c] = s.On[c]
			if s.Value[c] >= 1 && s.Value[c] <= 100 {
				l.Val[c] = strconv.Itoa(s.Value[c])
			}
		}
		l.FieldSeq++
	}
}

// edits reads the channel controls; values must be 1..100.
func (l *Light) edits() (on [3]bool, v [3]int, err error) {
	for c := 0; c < 3; c++ {
		on[c] = l.En[c]
		n, e := strconv.Atoi(strings.TrimSpace(l.Val[c]))
		if e != nil || n < 1 || n > 100 {
			return on, v, fmt.Errorf("%s brightness must be 1..100 (a channel is switched off with its checkbox, not with 0)", rgblight.ChannelNames[c])
		}
		v[c] = n
	}
	return
}

// lightJob runs f on the light worker with the buttons disabled.
func (l *Light) lightJob(what string, f func(c *rgblight.Client) (rgblight.State, error)) {
	l.lightJobThen(what, f, nil)
}

// lightJobThen is lightJob with a UI callback after success.
func (l *Light) lightJobThen(what string, f func(c *rgblight.Client) (rgblight.State, error), then func(rgblight.State)) {
	c := l.cl
	if c == nil || l.busy {
		return
	}
	l.busy = true
	l.lw.do(func() {
		st, err := f(c)
		l.ui(func() {
			l.busy = false
			if err != nil {
				l.log("%s: %v", what, err)
				l.msg("RGB light", what+":\n\n"+err.Error())
				return
			}
			l.log("%s: %s", what, st)
			l.showState(st, true)
			if then != nil {
				then(st)
			}
		})
	})
}

// Apply sends the channel controls.
func (l *Light) Apply() {
	on, v, err := l.edits()
	if err != nil {
		l.msg("RGB light", err.Error())
		return
	}
	l.cfg.Values = v
	l.saveCfg()
	l.hdrLightChanged(v)
	l.lightJob("Set channels (03, confirmed with 20)", func(c *rgblight.Client) (rgblight.State, error) {
		return setAllBusyOnce(c, on, v)
	})
}

// ReadState is the Read state button.
func (l *Light) ReadState() {
	l.lightJob("Read state (20)", func(c *rgblight.Client) (rgblight.State, error) { return c.ReadState() })
}

// AllOff is the All off button.
func (l *Light) AllOff() {
	_, v, err := l.edits()
	if err != nil {
		v = [3]int{50, 50, 50}
		if l.cl != nil {
			if s := l.cl.CachedState(); s != nil {
				v = s.Value
			}
		}
	}
	l.lightJob("All off (03, confirmed with 20)", func(c *rgblight.Client) (rgblight.State, error) {
		return setAllBusyOnce(c, [3]bool{}, v)
	})
}

// hdrLightChanged: new light values v on the Light page. v0.82: only a
// change of the brightness values voids the frame-0 lock and the leveling
// handoff (switching channels on/off - or a trichrome scan - does not).
func (l *Light) hdrLightChanged(v [3]int) {
	h := l.hdr
	if ho := h.handoff; ho != nil && ho.Values != v {
		h.dropHandoff(fmt.Sprintf("light values changed to R %d G %d B %d", v[0], v[1], v[2]))
	}
	if h.sess == nil || h.lock == nil || h.Busy {
		return
	}
	if h.lockLight != nil && *h.lockLight == v {
		h.Log.Printf("Light set on the Light page with the values frame 0 was locked with (R %d G %d B %d) - lock kept.", v[0], v[1], v[2])
		return
	}
	h.releaseLock(fmt.Sprintf("Light values changed on the Light sub-page (R %d G %d B %d) - frame 0 lock released, meter the base again (or load a preset).", v[0], v[1], v[2]))
}

// ISOChanged (v0.82): the Light page's leveling ISO changed Settings().ISO,
// linked with the Camera page's fixed frame-0 ISO.
func (l *Light) ISOChanged() {
	i := clampIdx(l.cfg.ISO, len(isoVals))
	l.cfg.ISO = i
	l.saveCfg()
	if h := l.hdr; h != nil && h.Cfg.ISO != i {
		h.Cfg.ISO = i
		h.ISOChanged()
	}
}

// restoreExposure (v0.81) selects the ISO and base shutter the stored light
// setting was levelled at (entries before v0.81 without a known shutter
// leave the selection alone).
func (l *Light) restoreExposure(isoV int, shutter uint32) {
	var parts []string
	if i := fixedShutterIndexOf(shutter); i >= 0 {
		l.hdr.setShutter(i)
		parts = append(parts, "shutter "+camera.ShutterLabel(shutter))
	} else if shutter != 0 {
		parts = append(parts, fmt.Sprintf("stored shutter %s is not a base shutter choice - selection kept", camera.ShutterLabel(shutter)))
	}
	for i, v := range isoVals {
		if v == isoV && isoV != 0 {
			l.cfg.ISO = i
			l.saveCfg()
			l.hdr.setISO(i) // v0.82: and the HDR page's fixed ISO
			parts = append(parts, fmt.Sprintf("ISO %d", v))
		}
	}
	if len(parts) > 0 {
		l.log("Preset exposure set: %s (camera side follows)", strings.Join(parts, ", "))
	}
}

// ---------- leveling camera (levelhik.go) ----------

func (l *Light) camSource() int { return clampIdx(l.cfg.CamSrc, len(camSrcItems)) }

// UsesMonoCam reports a Hikrobot (or simulated Hikrobot) leveling camera.
func (l *Light) UsesMonoCam() bool {
	s := l.camSource()
	return s == camSrcHik || s == camSrcHikSim
}

// CamSrcItems are the Leveling camera choices.
func CamSrcItems() []string { return camSrcItems }

// CamSourceChanged: the Leveling camera combo changed Settings().CamSrc.
func (l *Light) CamSourceChanged() {
	if l.inPreset {
		return
	}
	l.cfg.CamSrc = l.camSource()
	l.saveCfg()
	hik := l.UsesMonoCam()
	if hik {
		l.CamProfile, l.CamProfileOK = false, false
		l.CamNote = camProfileMonoShort
		l.setHikField(l.hdr.sharedHikExposure())
	} else {
		l.CamProfile, l.CamProfileOK = !l.cfg.NoCam, true
		l.CamNote = camProfileBayerShort
	}
	if l.OnRouting != nil {
		l.OnRouting()
	}
}

// CamProfileTip is the long camera-profile note (tooltip).
func CamProfileTip() string { return camProfileMonoNote }

func (l *Light) setHikField(us float64) {
	l.HikExpText = strconv.FormatFloat(us, 'f', 0, 64)
	l.FieldSeq++
}

// HikExposureEdited: the Exposure field was edited (OnValueChanged).
func (l *Light) HikExposureEdited() {
	v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l.HikExpText), "us")), 64)
	if err != nil || v < 0 || v > 10000000 {
		l.setHikField(l.hdr.sharedHikExposure())
		return
	}
	l.hdr.setSharedHikExposure(v, "user")
}

func (l *Light) monoScanExposureUS() float64 {
	if m := l.hdr.Mono; m != nil {
		st := m.readSettings()
		if st.ExposureUS >= hikcam.CS240ExpMinUS {
			return st.ExposureUS
		}
	}
	return hikcam.BestScanQuality().ExposureUS
}

func (l *Light) hikExposureUS() float64 {
	if l.hdr.Cfg.HikExposureUS >= hikcam.CS240ExpMinUS {
		return hikbase.ClampExposureUS(l.hdr.Cfg.HikExposureUS)
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(l.HikExpText), 64); err == nil && v >= hikcam.CS240ExpMinUS {
		return v
	}
	return l.monoScanExposureUS()
}

func (l *Light) hikLights() func() ([3]bool, [3]int) {
	sim, cl := l.sim, l.cl
	return func() ([3]bool, [3]int) {
		if sim != nil {
			return sim.Lights()
		}
		if cl != nil {
			if cs := cl.CachedState(); cs != nil {
				return cs.On, cs.Value
			}
		}
		return [3]bool{true, true, true}, [3]int{50, 50, 50}
	}
}

// StartHikPreview shows the Hikrobot (or simulator) preview in the Light
// page's live view while a monochrome leveling camera is selected.
func (l *Light) StartHikPreview() {
	h := l.hdr
	if l.busy || !l.UsesMonoCam() || (h.Mono != nil && h.Mono.running.Load()) {
		return
	}
	if h.Mono != nil {
		h.Mono.stopPreviewAsync()
	}
	gen := l.hikPrevGen.Add(1)
	alive := func() bool { return l.hikPrevGen.Load() == gen }
	h.Hik.startPreview(l.camSource() == camSrcHikSim, l.log, func(img image.Image, what string, done func()) {
		h.Host.Post(func() {
			defer done()
			if alive() {
				h.setImage(img, what)
			}
		})
	}, alive)
}

// StopHikPreview ends the Hikrobot preview.
func (l *Light) StopHikPreview() { l.stopHikPreview() }

func (l *Light) stopHikPreview() {
	l.hikPrevGen.Add(1)
	l.hdr.Hik.prevGen.Add(1)
	if m := l.hdr.Mono; m != nil {
		m.stopPreviewAsync()
	}
}
