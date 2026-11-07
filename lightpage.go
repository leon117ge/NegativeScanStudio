package main

// v0.80: Light page - CuteNewDesign RGB Light Air control and automatic
// film-base leveling (fixed camera exposure, the light's R/G/B brightness
// is adjusted until the clear base sits at the chosen level in each
// channel). Only the documented customer commands are ever sent (see
// package rgblight).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"basetool/camera"
	"basetool/filmdb"
	"basetool/hdrscan"
	"basetool/lightlevel"
	"basetool/rawconv"
	"basetool/rgblight"
	"basetool/scanconfig"
	"basetool/scanmeta"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

const simLightItem = "Simulated light (test, no hardware)"

// simNoAckItem simulates the field light: protocol 1.4 / FW V1.0.5 executes
// 03 but sends no 81 for it, only the new state as event 83 01 (v0.82,
// confirmed from the v0.81 field log). The pure no-reply case is tested in
// rgblight.
const simNoAckItem = "Simulated light - FW V1.0.5 (03: state event, no 81)"

type lightConfig struct {
	UseLastLeveling bool               `json:"use_last_leveling"`
	Leveling        *scanmeta.Leveling `json:"leveling,omitempty"`
	Port            string             `json:"port"`
	Values          [3]int             `json:"values"`
	ISO             int                `json:"iso"`    // index into isoVals
	Target          int                `json:"target"` // index into targetVals
	Tol             float64            `json:"tol_pct"`
	MaxIter         int                `json:"max_iter"`
	SettleMS        int                `json:"settle_ms"`
	Area            lightlevel.Rect    `json:"area"`
	AreaPicked      bool               `json:"area_picked,omitempty"` // only true after an actual drag
	AreaMode        int                `json:"area_mode,omitempty"`   // v0.87: 0 auto, 1 rectangle (same as Camera)
	Stock           string             `json:"stock"`
	AutoLoad        bool               `json:"apply_on_select"`
	NoCam           bool               `json:"no_cam_profile,omitempty"` // v0.85 "Apply camera profile" unticked
}

func lightConfigFile() string { return filepath.Join(configDir(), "rgb_light.json") }

func loadLightConfig(isoIdx int) lightConfig {
	c := lightConfig{UseLastLeveling: true, Values: [3]int{50, 50, 50}, ISO: isoIdx, Target: 0, Tol: 2, MaxIter: 4, SettleMS: 300, Area: lightlevel.Centre, AutoLoad: true}
	if b, err := os.ReadFile(lightConfigFile()); err == nil {
		json.Unmarshal(b, &c)
	}
	for i := range c.Values {
		if c.Values[i] < 1 || c.Values[i] > 100 {
			c.Values[i] = 50
		}
	}
	if c.ISO < 0 || c.ISO >= len(isoVals) {
		c.ISO = isoIdx
	}
	if c.Target < 0 || c.Target >= len(targetVals) {
		c.Target = 0
	}
	if c.Tol <= 0 || c.Tol > 20 {
		c.Tol = 2
	}
	if c.MaxIter < 1 || c.MaxIter > 10 {
		c.MaxIter = 4
	}
	if c.SettleMS < 0 || c.SettleMS > 5000 {
		c.SettleMS = 300
	}
	if c.Area.Empty() {
		c.Area = lightlevel.Centre
	}
	if c.AreaMode != 1 {
		c.AreaMode = 0
	}
	return c
}

// LightUI is the Light page.
type LightUI struct {
	mw  **walk.MainWindow
	hdr *HDRUI
	cfg lightConfig
	lw  *camWorker // light jobs (serial port)

	cl          *rgblight.Client
	sim         *rgblight.SimDevice
	port        string
	info        rgblight.Info
	ports       []rgblight.PortInfo
	busy        bool // a light job or the leveling runs (UI thread)
	level       bool // leveling runs
	cancel      context.CancelFunc
	cast        *castUI
	lastSuccess *levelingRecord

	copyBtn *walk.PushButton
	lastRes *lightlevel.Result
	lastExp camera.Exposure
	lastTgt float64
	// v0.85 camera-profile leveling
	camCB    *walk.CheckBox
	lastPlan camPlan
	lastPost [3]float64 // final base after the matrix
	lastTIFF bool       // lastPost measured in the trichrome TIFF (else predicted)

	// preview
	view     *walk.CustomWidget
	comp     *walk.Bitmap
	thumbs   [3]*walk.Bitmap
	thumbTxt [3]string
	dragging bool
	dragA    lightlevel.Rect
	dragX    float64
	dragY    float64

	portCB, isoCB, targetCB, presetCB, areaCB             *walk.ComboBox
	shutCB                                                *walk.ComboBox // v0.81 base shutter (shared with HDR scan)
	connBtn, applyBtn, readBtn, offBtn                    *walk.PushButton
	startBtn, stopBtn, saveBtn, loadBtn                   *walk.PushButton
	enCB                                                  [3]*walk.CheckBox
	valEd                                                 [3]*walk.LineEdit
	tolEd, iterEd, settleEd                               *walk.LineEdit
	autoCB                                                *walk.CheckBox
	infoLbl, telLbl, stateLbl, resLbl, viewLbl, presetLbl *walk.Label
	nameEd, stockEd, notesEd                              *walk.LineEdit // v0.83 config / preset
	fileSaveBtn, fileLoadBtn, delBtn                      *walk.PushButton
	presets                                               *scanconfig.Store
	inPreset                                              bool
	logTE, faultTE                                        *walk.TextEdit
	saveMu                                                sync.Mutex
}

func (u *LightUI) win() *walk.MainWindow { return *u.mw }

func (u *LightUI) ui(f func()) {
	if mw := u.win(); mw != nil {
		mw.Synchronize(func() {
			defer u.hdr.guard("Light page update")
			f()
		})
	}
}

func (u *LightUI) log(f string, a ...any) {
	s := fmt.Sprintf(f, a...)
	if u.hdr.log != nil {
		u.hdr.log.Printf("Light: %s", s) // HDR log file keeps a record too
	}
	u.ui(func() { appendTE(u.logTE, s) })
}

func appendTE(te *walk.TextEdit, s string) {
	if te == nil {
		return
	}
	t := te.Text()
	if len(t) > 60000 {
		te.SetText(t[len(t)-40000:])
	}
	te.AppendText(time.Now().Format("15:04:05  ") + strings.ReplaceAll(s, "\n", "\r\n") + "\r\n")
	te.ScrollToCaret()
}

func (u *LightUI) saveCfg() {
	u.saveMu.Lock()
	defer u.saveMu.Unlock()
	if b, err := json.MarshalIndent(u.cfg, "", "  "); err == nil {
		os.MkdirAll(configDir(), 0o755)
		os.WriteFile(lightConfigFile(), b, 0o644)
	}
}

// toScanRect converts a Light-page measure box to the Camera page's
// normalized rectangle (same 0..1 coordinates).
func toScanRect(r lightlevel.Rect) hdrscan.Rect {
	return hdrscan.Rect{X0: r.X0, Y0: r.Y0, X1: r.X1, Y1: r.Y1}
}

func toLightRect(r hdrscan.Rect) lightlevel.Rect {
	return lightlevel.Rect{X0: r.X0, Y0: r.Y0, X1: r.X1, Y1: r.Y1}
}

// manualBase is the user-drawn clear-base rectangle when Film base is set
// to Rectangle (drawn here or synced from Camera live view).
func (u *LightUI) manualBase() (lightlevel.Rect, bool) {
	if u.cfg.AreaMode == 1 && u.cfg.AreaPicked && !u.cfg.Area.Empty() {
		return u.cfg.Area, true
	}
	return lightlevel.Rect{}, false
}

// publishRect stores the pick on the Light page and on Camera, so the
// leveling TIFF check, the RAW measure box and the frame-0 handoff lock
// all use the same small patch.
func (u *LightUI) publishRect(r lightlevel.Rect) {
	if r.Empty() {
		return
	}
	u.cfg.Area, u.cfg.AreaMode, u.cfg.AreaPicked = r, 1, true
	if h := u.hdr; h != nil {
		h.roi = toScanRect(r)
		h.cfg.AreaMode = 1
		if h.areaCB != nil && h.areaCB.CurrentIndex() != 1 {
			h.areaCB.SetCurrentIndex(1)
		}
		h.save()
		h.invalidateViews()
	}
	if u.areaCB != nil && u.areaCB.CurrentIndex() != 1 {
		u.areaCB.SetCurrentIndex(1)
	}
	u.saveCfg()
}

func (u *LightUI) areaModeChanged() {
	if u.areaCB == nil {
		return
	}
	m := u.areaCB.CurrentIndex()
	if m < 0 {
		return
	}
	u.cfg.AreaMode = m & 1
	if u.cfg.AreaMode == 1 {
		if h := u.hdr; h != nil && !h.roi.Empty() {
			u.cfg.Area = toLightRect(h.roi)
			u.cfg.AreaPicked = true
		}
	}
	if h := u.hdr; h != nil {
		h.cfg.AreaMode = u.cfg.AreaMode
		if h.areaCB != nil && h.areaCB.CurrentIndex() != u.cfg.AreaMode {
			h.areaCB.SetCurrentIndex(u.cfg.AreaMode)
		}
		h.save()
		h.invalidateViews()
	}
	u.saveCfg()
	if u.view != nil {
		u.view.Invalidate()
	}
}

func (u *LightUI) connected() bool { return u.cl != nil && !u.cl.Closed() }

func (u *LightUI) updateButtons() {
	conn := u.connected()
	idle := conn && !u.busy
	if u.connBtn == nil {
		return
	}
	if conn {
		u.connBtn.SetText("Disconnect")
	} else {
		u.connBtn.SetText("Connect")
	}
	u.connBtn.SetEnabled(!u.busy || !conn)
	u.portCB.SetEnabled(!conn)
	for _, b := range []*walk.PushButton{u.applyBtn, u.readBtn, u.offBtn, u.startBtn} {
		b.SetEnabled(idle)
	}
	u.stopBtn.SetEnabled(u.level)

	if u.copyBtn != nil {
		u.copyBtn.SetEnabled(!u.busy && u.lastSuccess != nil)
	}
	u.updatePresetButtons()
}

func (u *LightUI) refreshPorts() {
	ps, err := rgblight.ListPorts()
	if err != nil {
		u.log("Serial port list: %v", err)
	}
	u.ports = ps
	items := []string{}
	sel := -1
	for i, p := range ps {
		items = append(items, p.Label())
		if strings.EqualFold(p.Name, u.cfg.Port) {
			sel = i
		}
	}
	items = append(items, simLightItem, simNoAckItem)
	if sel < 0 {
		sel = 0 // first port; ListPorts sorts the light (VID 0483 / PID 5740) first
		switch {
		case u.cfg.Port == "SIM-NOACK":
			sel = len(items) - 1
		case len(ps) == 0 || u.cfg.Port == "SIM":
			sel = len(items) - 2
		}
	}
	u.portCB.SetModel(items)
	u.portCB.SetCurrentIndex(sel)
	n := 0
	for _, p := range ps {
		if p.Light {
			n++
		}
	}
	switch {
	case n == 1:
		setLightStatusN(3, u.infoLbl, "RGB Light Air found on "+ps[0].Name+" (USB 0483:5740). Press Connect.")
	case n > 1:
		setLightStatusN(3, u.infoLbl, fmt.Sprintf("%d lights found - choose the port.", n))
	case len(ps) > 0:
		setLightStatusN(3, u.infoLbl, "No port with USB ID 0483:5740 - choose the COM port by hand, or check the cable.")
	default:
		setLightStatusN(3, u.infoLbl, "No serial ports. Plug the light in (USB-C data cable) and press Refresh.")
	}
}

func (u *LightUI) toggleConnect() {
	if u.connected() {
		u.disconnect()
		return
	}
	i := u.portCB.CurrentIndex()
	sim := i < 0 || i >= len(u.ports)
	noAck := sim && i == len(u.ports)+1
	name := "SIM"
	if noAck {
		name = "SIM-NOACK"
	}
	if !sim {
		name = u.ports[i].Name
	}
	u.cfg.Port = name
	u.saveCfg()
	u.busy = true
	u.updateButtons()
	u.lw.do(func() {
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
				u.ui(func() {
					u.busy = false
					u.updateButtons()
					u.log("Cannot open %s: %v", name, err)
					walk.MsgBox(u.win(), "RGB light", "Cannot open "+name+":\n\n"+err.Error()+"\n\nClose other programs that use the light (CuteNew software, serial monitors) and try again.", walk.MsgBoxIconWarning)
				})
				return
			}
			rw = p
		}
		c := rgblight.NewClient(rw)
		u.hook(c)
		c.Start()
		info, err := c.Info()
		var st rgblight.State
		if err == nil {
			st, err = c.ReadState()
		}
		if err != nil {
			c.Close()
			u.ui(func() {
				u.busy = false
				u.updateButtons()
				u.log("Connect %s failed: %v", name, err)
				walk.MsgBox(u.win(), "RGB light", "The light on "+name+" did not answer:\n\n"+err.Error()+"\n\nCheck the port and that the light is powered.", walk.MsgBoxIconWarning)
			})
			return
		}
		u.ui(func() {
			u.cl, u.sim, u.port, u.info = c, sd, name, info
			u.busy = false
			t := "Connected: " + name + "\n" + info.String()
			if info.Protocol != "1.3" {
				t += "\nNOTE: this program follows protocol 1.3; the light reports " + info.Protocol + "."
			}
			if !strings.Contains(strings.ToUpper(info.Model), "CUTENEW") {
				t += "\nNOTE: unexpected product name."
			}
			setLightStatusN(3, u.infoLbl, t)
			u.log("Connected to %s: %s", name, info)
			u.showState(st, true)
			u.updateButtons()
			if u.presetCB != nil && u.presetCB.CurrentIndex() > 0 {
				u.log("Preset %s is applied only when you choose it or press Load preset.", u.presetCB.Text())
			}
		})
	})
}

// hook wires the client's unsolicited reports to the page.
func (u *LightUI) hook(c *rgblight.Client) {
	c.OnLog = func(s string) { u.log("%s", s) }
	// v0.81: full serial trace, file only (too much for the page's log box)
	c.OnRaw = func(s string) {
		if u.hdr.log != nil {
			u.hdr.log.Printf("Light RAW: %s", s)
		}
	}
	c.OnTelemetry = func(t rgblight.Telemetry) {
		u.ui(func() {
			if u.cl != c {
				return
			}
			capt := "idle"
			if t.Capturing {
				capt = "CAPTURING"
			}
			u.telLbl.SetText(fmt.Sprintf("Temperature %.1f °C    Supply %.2f V    %s", t.TempC, t.Volts, capt))
		})
	}
	c.OnState = func(s rgblight.State) {
		u.ui(func() {
			if u.cl == c {
				u.showState(s, false)
			}
		})
	}
	c.OnProgress = func(p rgblight.Progress) { u.log("Light %s", p) }
	c.OnFault = func(f rgblight.Fault) {
		u.ui(func() {
			appendTE(u.faultTE, f.String()+" (the active field is shown raw: the protocol does not say whether it means raised or cleared)")
			u.log("FAULT: %s", f)
		})
	}
	c.OnClosed = func(err error) {
		u.ui(func() {
			if u.cl != c {
				return
			}
			u.log("Connection closed: %v", err)
			u.cl, u.sim = nil, nil
			setLightStatusN(3, u.infoLbl, "Not connected ("+err.Error()+")")
			u.telLbl.SetText(" ")
			redrawLightRight()
			u.updateButtons()
		})
	}
}

func (u *LightUI) disconnect() {
	c := u.cl
	if c == nil {
		return
	}
	u.busy = true
	u.updateButtons()
	u.lw.do(func() {
		err := c.SafeClose(30 * time.Second)
		u.ui(func() {
			u.busy = false
			if u.cl == c {
				u.cl, u.sim = nil, nil
			}
			if err != nil {
				u.log("Disconnected - WARNING: %v", err)
				setLightStatusN(3, u.infoLbl, "Disconnected. The light may still be on: "+err.Error())
			} else {
				u.log("All channels switched off (confirmed by state read) and disconnected.")
				setLightStatusN(3, u.infoLbl, "Disconnected (light switched off).")
			}
			u.telLbl.SetText(" ")
			redrawLightRight()
			u.updateButtons()
		})
	})
}

// shutdown is called when the window closes: the same planned disconnect,
// with a short idle wait.
func (u *LightUI) shutdown() {
	if t := u.hdr.tri; t != nil && t.cancel != nil {
		t.cancel() // a running trichrome scan stops; the light is switched off below
	}
	if c := u.cl; c != nil && !c.Closed() {
		if u.cancel != nil {
			u.cancel()
		}
		if err := c.SafeClose(5 * time.Second); err != nil && u.hdr.log != nil {
			u.hdr.log.Printf("Light: on exit: %v", err)
		}
	}
}

func (u *LightUI) showState(s rgblight.State, toEdits bool) {
	t := s.String()
	if s.Focus < 0 {
		t += " (legacy state report without focus)"
	}
	setLightStatusN(2, u.stateLbl, "Light reports: "+t)
	if toEdits {
		for c := 0; c < 3; c++ {
			u.enCB[c].SetChecked(s.On[c])
			if s.Value[c] >= 1 && s.Value[c] <= 100 {
				u.valEd[c].SetText(strconv.Itoa(s.Value[c]))
			}
		}
	}
}

// edits reads the channel controls; values must be 1..100.
func (u *LightUI) edits() (on [3]bool, v [3]int, err error) {
	for c := 0; c < 3; c++ {
		on[c] = u.enCB[c].Checked()
		n, e := strconv.Atoi(strings.TrimSpace(u.valEd[c].Text()))
		if e != nil || n < 1 || n > 100 {
			return on, v, fmt.Errorf("%s brightness must be 1..100 (a channel is switched off with its checkbox, not with 0)", rgblight.ChannelNames[c])
		}
		v[c] = n
	}
	return
}

// lightJob runs f on the light worker with the buttons disabled.
func (u *LightUI) lightJob(what string, f func(c *rgblight.Client) (rgblight.State, error)) {
	u.lightJobThen(what, f, nil)
}

// lightJobThen is lightJob with a UI callback after success.
func (u *LightUI) lightJobThen(what string, f func(c *rgblight.Client) (rgblight.State, error), then func(rgblight.State)) {
	c := u.cl
	if c == nil || u.busy {
		return
	}
	u.busy = true
	u.updateButtons()
	u.lw.do(func() {
		st, err := f(c)
		u.ui(func() {
			u.busy = false
			u.updateButtons()
			if err != nil {
				u.log("%s: %v", what, err)
				walk.MsgBox(u.win(), "RGB light", what+":\n\n"+err.Error(), walk.MsgBoxIconWarning)
				return
			}
			u.log("%s: %s", what, st)
			u.showState(st, true)
			if then != nil {
				then(st)
			}
			if t := u.hdr.tri; t != nil {
				t.updateButtons()
			}
		})
	})
}

func (u *LightUI) apply() {
	on, v, err := u.edits()
	if err != nil {
		walk.MsgBox(u.win(), "RGB light", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	u.cfg.Values = v
	u.saveCfg()
	u.hdrLightChanged(v)
	u.lightJob("Set channels (03, confirmed with 20)", func(c *rgblight.Client) (rgblight.State, error) {
		return setAllBusyOnce(c, on, v)
	})
}

func (u *LightUI) allOff() {
	_, v, err := u.edits()
	if err != nil {
		v = [3]int{50, 50, 50}
		if s := u.cl.CachedState(); s != nil {
			v = s.Value
		}
	}
	u.lightJob("All off (03, confirmed with 20)", func(c *rgblight.Client) (rgblight.State, error) {
		return setAllBusyOnce(c, [3]bool{}, v)
	})
}

// setAllBusyOnce sends 03 and confirms with 20. A BUSY answer means the
// light rejected the command, so after waiting for idle it is sent once
// more; any other failure is reported, never resent.
func setAllBusyOnce(c *rgblight.Client, on [3]bool, v [3]int) (rgblight.State, error) {
	st, err := c.SetAllConfirmed(on, v)
	if errors.Is(err, rgblight.ErrBusy) {
		if werr := c.WaitIdle(60 * time.Second); werr != nil {
			return st, fmt.Errorf("light busy: %w", werr)
		}
		st, err = c.SetAllConfirmed(on, v)
	}
	return st, err
}

// busyOnceLight adapts the client for the leveling (same BUSY rule).
type busyOnceLight struct{ c *rgblight.Client }

func (l busyOnceLight) SetAll(on [3]bool, v [3]int) error {
	err := l.c.SetAll(on, v)
	if errors.Is(err, rgblight.ErrBusy) {
		if werr := l.c.WaitIdle(60 * time.Second); werr != nil {
			return fmt.Errorf("light busy: %w", werr)
		}
		err = l.c.SetAll(on, v)
	}
	return err
}

// hdrLightChanged: new light values v on the Light page. v0.82: only a
// change of the brightness values voids the frame-0 lock and the leveling
// handoff (switching channels on/off - or a trichrome scan - does not).
func (u *LightUI) hdrLightChanged(v [3]int) {
	h := u.hdr
	if ho := h.handoff; ho != nil && ho.Values != v {
		h.dropHandoff(fmt.Sprintf("light values changed to R %d G %d B %d", v[0], v[1], v[2]))
	}
	if h.sess == nil || h.sess.Locked == nil || h.busy {
		return
	}
	if h.lockLight != nil && *h.lockLight == v {
		h.log.Printf("Light set on the Light page with the values frame 0 was locked with (R %d G %d B %d) - lock kept.", v[0], v[1], v[2])
		return
	}
	h.releaseLock(fmt.Sprintf("Light values changed on the Light sub-page (R %d G %d B %d) - frame 0 lock released, meter the base again (or load a preset).", v[0], v[1], v[2]))
}

// ---------- leveling ----------

// xt3Camera shoots the leveling frames with the tethered X-T3 at the HDR
// Fixed frame-0 exposure (chosen ISO and base shutter, 1/60 s or 1/125 s).
type xt3Camera struct {
	sess *hdrscan.Session
	iso  int
	dir  string
	exp  camera.Exposure
	last [3]string
}

func (x *xt3Camera) Name() string {
	return fmt.Sprintf("Fujifilm X-T3 (USB tether), ISO %d, %s", x.iso, camera.ShutterLabel(x.sess.FixedExposure(x.iso).Shutter))
}
func (x *xt3Camera) Mono() bool { return false }
func (x *xt3Camera) Prepare() (string, error) {
	e, err := x.sess.SetFixed(x.iso, "RGB light leveling: fixed exposure (same as HDR Fixed frame 0)")
	if err != nil {
		return "", err
	}
	x.exp = e
	return e.String(), nil
}
func (x *xt3Camera) Shoot(ctx context.Context, ch int, prefix string) (*lightlevel.Image, error) {
	p, err := x.sess.ShootFixed(x.exp, x.dir, prefix)
	if err != nil {
		return nil, err
	}
	f, err := rawconv.ReadRawFrame(p)
	if err != nil {
		return nil, fmt.Errorf("%s is not a readable RAW (set RAW or RAW+JPEG): %w", filepath.Base(p), err)
	}
	w, h, v, cl := f.ChannelPlane(ch, 16)
	if x.last[ch] != "" && x.last[ch] != p {
		os.Remove(x.last[ch]) // keep only the latest frame per channel
	}
	x.last[ch] = p
	return &lightlevel.Image{W: w, H: h, V: v, Clip: cl, Path: p}, nil
}

func (u *LightUI) params() (lightlevel.Params, error) {
	var p lightlevel.Params
	_, v, err := u.edits()
	if err != nil {
		return p, err
	}
	tol, err1 := strconv.ParseFloat(strings.TrimSpace(u.tolEd.Text()), 64)
	it, err2 := strconv.Atoi(strings.TrimSpace(u.iterEd.Text()))
	st, err3 := strconv.Atoi(strings.TrimSpace(u.settleEd.Text()))
	if err1 != nil || tol <= 0 || tol > 20 {
		return p, errors.New("tolerance must be 0.1 .. 20 %")
	}
	if err2 != nil || it < 1 || it > 10 {
		return p, errors.New("max. iterations must be 1 .. 10")
	}
	if err3 != nil || st < 0 || st > 5000 {
		return p, errors.New("settle time must be 0 .. 5000 ms")
	}
	ti := u.targetCB.CurrentIndex()
	if ti < 0 || ti >= len(targetVals) {
		ti = 0
	}
	u.cfg.Values, u.cfg.Tol, u.cfg.MaxIter, u.cfg.SettleMS, u.cfg.Target, u.cfg.ISO = v, tol, it, st, ti, u.isoCB.CurrentIndex()
	if u.camCB != nil {
		u.cfg.NoCam = !u.camCB.Checked()
	}
	u.saveCfg()
	pr := lightlevel.Params{Target: targetVals[ti], Tol: tol / 100, MaxIter: it, Settle: time.Duration(st) * time.Millisecond, Area: u.cfg.Area, Start: v}
	pl, err := camPlanFor(pr.Target, !u.cfg.NoCam)
	if err != nil {
		return p, err
	}
	if pl.on {
		pr.Targets = pl.pre
	}
	u.lastPlan = pl
	return pr, nil
}

func (u *LightUI) startLevel() {
	if !u.connected() || u.busy {
		return
	}
	p, err := u.params()
	if err != nil {
		walk.MsgBox(u.win(), "Auto film-base leveling", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	var baseNorm hdrscan.Rect
	if box, ok := u.manualBase(); ok {
		p.Area = box
		baseNorm = toScanRect(box)
		u.publishRect(box)
	} else {
		p.Area = lightlevel.Centre
		if u.cfg.AreaMode == 1 {
			walk.MsgBox(u.win(), "Auto film-base leveling", "Film base is set to Rectangle: drag a rectangle over a small patch of clear film base on the preview (or on Camera live view) first, or choose Auto.", walk.MsgBoxIconInformation)
			return
		}
	}
	c := u.cl
	h := u.hdr
	restore := [3]bool{true, true, true}
	if s := c.CachedState(); s != nil {
		restore = s.On
		if restore == [3]bool{} {
			restore = [3]bool{true, true, true} // levelled light for scanning: all on
		}
	}
	var cam lightlevel.Camera
	worker := u.lw
	tmpDir := ""
	iso := isoVals[clampIdx(u.isoCB.CurrentIndex(), len(isoVals))]
	realCam := false
	if u.sim != nil {
		sd := u.sim
		syn := lightlevel.NewSynth(sd.Lights)
		syn.Exp = camera.Exposure{ISO: iso, Shutter: h.fixedShutter()}.String()
		cam = syn
	} else {
		if h.dev == nil || h.isSim || h.sess == nil {
			walk.MsgBox(u.win(), "Auto film-base leveling", "Connect the X-T3 on Camera and Light > Camera first (USB tether, M mode).\n\nThe simulated HDR camera cannot see the light.", walk.MsgBoxIconInformation)
			return
		}
		if h.busy {
			walk.MsgBox(u.win(), "Auto film-base leveling", "The camera is busy (metering, scan or leveling).", walk.MsgBoxIconInformation)
			return
		}
		h.sess.Fixed = h.fixedShutter()
		// v0.83: leveling RAWs are temp files - never in the scan folder
		td, terr := os.MkdirTemp("", "nss_leveling_*")
		if terr != nil {
			walk.MsgBox(u.win(), "Auto film-base leveling", "Cannot create a temp folder for the leveling frames:\n\n"+terr.Error(), walk.MsgBoxIconWarning)
			return
		}
		tmpDir = td
		cam = &xt3Camera{sess: h.sess, iso: iso, dir: td}
		worker, realCam = h.worker, true
		h.pauseLiveView()
		h.busy = true
		h.updateButtons()
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.lastSuccess = nil
	u.cfg.Leveling = nil
	u.saveCfg()
	u.busy, u.level = true, true
	u.lastTgt = p.Target
	u.updateButtons()
	setLightStatusN(3, u.resLbl, "Leveling ...")
	u.log("==== Auto film-base leveling: target %.2f FS, tolerance ±%.1f%%, max %d rounds, settle %v, start R %d G %d B %d ====",
		p.Target, p.Tol*100, p.MaxIter, p.Settle, p.Start[0], p.Start[1], p.Start[2])
	u.log("Camera: %s", cam.Name())
	if !baseNorm.Empty() {
		u.log("Film base: manual rectangle %.0f-%.0f%% x %.0f-%.0f%% (RAW measure and TIFF check; not the auto cluster).",
			baseNorm.X0*100, baseNorm.X1*100, baseNorm.Y0*100, baseNorm.Y1*100)
	} else {
		u.log("Film base: auto-detect in the trichrome TIFF (rebate / gaps / sprocket area). RAW measure uses the centre 50%% box. Choose Rectangle and drag a small patch if that cluster is too large.")
	}
	pl := u.lastPlan
	if pl.on {
		u.log("Apply camera profile: %s - target is the base AFTER the matrix (%.2f/%.2f/%.2f), i.e. %s before it.", pl.label(), pl.want, pl.want, pl.want, f3(pl.pre))
	} else {
		u.log("Apply camera profile: off (or no profile enabled) - target %.2f in the plain trichrome TIFF.", pl.want)
	}
	leak := h.leak()
	sess := h.sess
	fixedSh := h.fixedShutter()
	worker.do(func() {
		defer cancel()
		if tmpDir != "" {
			defer func() {
				n := countFiles(tmpDir)
				if rerr := os.RemoveAll(tmpDir); rerr != nil {
					u.log("Leveling temp folder %s not fully removed: %v", tmpDir, rerr)
				} else {
					u.log("Leveling temp frames deleted (%d files in %s) - nothing kept in the scan folder.", n, tmpDir)
				}
			}()
		}
		res, err := lightlevel.Run(ctx, busyOnceLight{c}, cam, p, restore, func(s string) { u.log("%s", s) })
		// v0.85: check in the real trichrome TIFF (same converter and
		// clear-base box as Density_In's auto base), one corrective pass
		post, inTIFF := pl.post(res.Readings), false
		if x, ok := cam.(*xt3Camera); ok && err == nil && res.Converged() {
			for pass := 0; pass < 2; pass++ {
				u.log("TIFF check %d: converting the last R/G/B leveling RAWs with the trichrome scan pipeline ...", pass+1)
				base, n, verr := tiffBase(ctx, x.last, tmpDir, leak, baseNorm)
				if verr != nil {
					u.log("TIFF check not possible: %v - the result below is the leveling-box reading.", verr)
					break
				}
				post, inTIFF = pl.post(base), true
				u.log("TIFF check %d: film base in the trichrome TIFF %s (%d samples); after %s: %s; worst %.1f%% from %.2f.",
					pass+1, f3(base), n, pl.label(), f3(post), 100*pl.worstDev(post), pl.want)
				if pl.worstDev(post) <= p.Tol || pass == 1 {
					break
				}
				p2 := p
				p2.Start = res.Values
				for ch := 0; ch < 3; ch++ {
					p2.Targets[ch] = res.Readings[ch] * pl.pre[ch] / base[ch]
				}
				u.log("Correcting: the leveling box reads %s where the TIFF base box reads %s - new box targets %s.", f3(res.Readings), f3(base), f3(p2.Targets))
				r2, e2 := lightlevel.Run(ctx, busyOnceLight{c}, cam, p2, restore, func(s string) { u.log("%s", s) })
				res, err = r2, e2
				post, inTIFF = pl.post(res.Readings), false
				if err != nil || !res.Converged() {
					break
				}
			}
		}
		if err == nil && res.Converged() && inTIFF && pl.worstDev(post) > p.Tol {
			err = fmt.Errorf("final TIFF base is still %.2f%% from target (tolerance %.2f%%); no measured base accepted", 100*pl.worstDev(post), 100*p.Tol)
		}
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		st, serr := c.ReadState()
		if err == nil && res.Converged() {
			if serr != nil {
				err = fmt.Errorf("cannot confirm final light state: %w", serr)
			} else if st.Value != res.Values {
				err = fmt.Errorf("final light state does not match measured levels: got %v, measured %v", st.Value, res.Values)
			}
		}
		var record *levelingRecord
		if err == nil && res.Converged() {
			mode, source := "Auto", "RAW estimate (TIFF check unavailable)"
			if !baseNorm.Empty() {
				mode = "Rectangle"
			}
			if inTIFF {
				source = "TIFF-checked"
			}
			if !realCam {
				source = "Simulated measurement"
			}
			record = &levelingRecord{Time: time.Now(), Version: appVersion, Mode: mode, Box: baseNorm,
				Values: res.Values, Base: post, Profile: pl.label(), Source: source, Measured: inTIFF || !realCam}
			if e := appendLevelingLog(filepath.Join(configDir(), "leveling_log.csv"), *record); e != nil {
				u.log("WARNING: leveling_log.csv could not be appended: %v", e)
			}
		}
		var exp camera.Exposure
		if x, ok := cam.(*xt3Camera); ok {
			exp = x.exp
		} else {
			exp = camera.Exposure{ISO: iso, Shutter: fixedSh}
		}
		released := false
		if sess != nil && sess.Locked != nil && (realCam || err == nil) {
			sess.Locked, released = nil, true // the light changed: frame 0 is locked again (handoff) or metered by hand
		}
		u.ui(func() {
			u.busy, u.level, u.cancel = false, false, nil
			if released {
				h.clearLocked()
			}
			if realCam {
				h.busy = false
				h.resumeLiveView()
			}
			if serr == nil {
				u.showState(st, true)
			}
			u.lastPost, u.lastTIFF = post, inTIFF
			u.lastSuccess = record
			if record != nil {
				u.cfg.Leveling = &scanmeta.Leveling{Base: post, Values: res.Values, At: record.Time.Format(time.RFC3339)}
				u.cfg.Values = res.Values
			}
			u.saveCfg()
			u.showResult(res, err, exp)
			if err == nil && res.Converged() {
				// v0.82: hand the levelled light + exposure to the frame-0 lock
				h.takeHandoff(baseHandoff{ISO: exp.ISO, Shutter: exp.Shutter, Values: res.Values, Source: "RGB light leveling " + time.Now().Format("15:04:05")})
			} else if released {
				h.say("Light levelled on the Light page - frame 0 lock released, meter the base again.")
			}
			h.updateButtons()
			u.updateButtons()
		})
	})
}

func clampIdx(i, n int) int {
	if i < 0 || i >= n {
		return 0
	}
	return i
}

func (u *LightUI) showResult(res lightlevel.Result, err error, exp camera.Exposure) {
	u.setImages(res.Images, u.lastTgt, res)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			u.log("Leveling stopped.")
			setLightStatusN(3, u.resLbl, fmt.Sprintf("Stopped. Light left at R %d G %d B %d.", res.Values[0], res.Values[1], res.Values[2]))
		} else {
			u.log("Leveling FAILED: %v", err)
			setLightStatusN(3, u.resLbl, "Failed: "+err.Error())
			if errors.Is(err, camera.ErrNotManual) {
				u.hdr.notManualMsg(err)
			} else {
				walk.MsgBox(u.win(), "Auto film-base leveling", "Leveling failed:\n\n"+err.Error(), walk.MsgBoxIconWarning)
			}
		}
		return
	}
	r := res
	u.lastRes, u.lastExp = &r, exp
	pl := u.lastPlan
	src := "predicted"
	if u.lastTIFF {
		src = "TIFF-checked"
	}
	prof := "no profile"
	if pl.on {
		prof = "after profile"
	}
	t := fmt.Sprintf("R %d G %d B %d -> base %.5f/%.5f/%.5f linear FS (%s, %s); box %.3f/%.3f/%.3f FS, %d rounds, %s",
		res.Values[0], res.Values[1], res.Values[2], u.lastPost[0], u.lastPost[1], u.lastPost[2], prof, src,
		res.Readings[0], res.Readings[1], res.Readings[2], res.Rounds, exp)
	pr := res.Problems()
	if len(pr) == 0 {
		t = "LEVELLED: " + t
	} else {
		t = "NOT LEVELLED: " + t + "\n" + strings.Join(pr, "\n")
	}
	setLightStatusN(3, u.resLbl, t)
	u.log("Result: %s (%s)", t, pl.label())
	if res.Converged() && u.lastSuccess != nil && !u.lastSuccess.Measured {
		u.log("TIFF verification is unavailable: the saved after-profile leveling uses the RAW estimate. Check the conversion log and level again if needed.")
	}
	u.updateButtons()
	limited := res.Limited[0] || res.Limited[1] || res.Limited[2]
	if limited || res.TooBright[0] || res.TooBright[1] || res.TooBright[2] {
		msg := strings.Join(pr, "\n") + "\n\nThe camera exposure was NOT changed (it stays at " + exp.String() + ")."
		if limited {
			msg += "\n\nTo reach the target: choose a higher ISO in the leveling settings (then use the same ISO for HDR Fixed frame 0), open the aperture, or choose a lower base level. You can also keep this result and save it as it is."
		} else {
			msg += "\n\nThe light is too strong for this exposure even at brightness 1: choose a lower ISO, stop the lens down, or a higher base level."
		}
		walk.MsgBox(u.win(), "Auto film-base leveling", msg, walk.MsgBoxIconWarning)
	}
}

// ---------- film-base DB ----------

func (u *LightUI) db() *filmdb.DB {
	if u.hdr.db == nil {
		u.hdr.loadDB()
	}
	return u.hdr.db
}

// ---------- preview ----------

func (u *LightUI) setImages(im [3]*lightlevel.Image, target float64, res lightlevel.Result) {
	if c := lightlevel.Composite(im, target); c != nil {
		u.setBmp(&u.comp, c)
	}
	for k := 0; k < 3; k++ {
		if g := lightlevel.Gray(im[k], target); g != nil {
			u.setBmp(&u.thumbs[k], g)
			u.thumbTxt[k] = fmt.Sprintf("%s only, brightness %d: base %.3f", rgblight.ChannelNames[k], valueShot(res, k), res.Readings[k])
		}
	}
	if u.comp != nil {
		u.viewLbl.SetText("Top: the three captures combined (target = mid grey).\nMagenta = clipped. Drag to move the measure area.")
		u.viewLbl.SetToolTipText("Composite of the three single-channel captures; the target level is shown as mid grey.")
	}
	u.view.Invalidate()
}

// valueShot is the brightness of the channel's last capture.
func valueShot(r lightlevel.Result, ch int) int {
	v := r.Values[ch]
	for _, s := range r.Steps {
		if s.Channel == ch {
			v = s.Value
		}
	}
	return v
}

func (u *LightUI) setBmp(dst **walk.Bitmap, img image.Image) {
	b, err := walk.NewBitmapFromImage(img)
	if err != nil {
		return
	}
	if *dst != nil {
		(*dst).Dispose()
	}
	*dst = b
}

func lightFit(b *walk.Bitmap, box walk.Rectangle) walk.Rectangle {
	if b == nil {
		return box
	}
	s := b.Size()
	if s.Width == 0 || s.Height == 0 {
		return box
	}
	sc := float64(box.Width) / float64(s.Width)
	if f := float64(box.Height) / float64(s.Height); f < sc {
		sc = f
	}
	w, h := int(float64(s.Width)*sc), int(float64(s.Height)*sc)
	return walk.Rectangle{X: box.X + (box.Width-w)/2, Y: box.Y + (box.Height-h)/2, Width: w, Height: h}
}

// layout: composite on top, three channel captures below.
func (u *LightUI) boxes() (comp walk.Rectangle, th [3]walk.Rectangle) {
	cb := u.view.ClientBoundsPixels()
	d := dpiOf(u.view)
	gap, lab := px(6, d), px(16, d)
	topH := (cb.Height - gap) * 64 / 100
	comp = walk.Rectangle{X: cb.X, Y: cb.Y, Width: cb.Width, Height: topH}
	tw := (cb.Width - 2*gap) / 3
	for k := 0; k < 3; k++ {
		th[k] = walk.Rectangle{X: cb.X + k*(tw+gap), Y: cb.Y + topH + gap, Width: tw, Height: cb.Height - topH - gap - lab}
	}
	return
}

func (u *LightUI) paint(c *walk.Canvas, _ walk.Rectangle) error {
	cb := u.view.ClientBoundsPixels()
	bg, _ := walk.NewSolidColorBrush(walk.RGB(22, 22, 25))
	if bg != nil {
		defer bg.Dispose()
		c.FillRectanglePixels(bg, cb)
	}
	f, _ := walk.NewFont("Segoe UI", 9, 0)
	if f != nil {
		defer f.Dispose()
	}
	cr, th := u.boxes()
	area := u.cfg.Area
	showArea := false
	if u.dragging {
		area, showArea = u.dragA, true
	} else if box, ok := u.manualBase(); ok {
		area, showArea = box, true
	}
	pen := func(col walk.Color) *walk.GeometricPen {
		br, _ := walk.NewSolidColorBrush(col)
		if br == nil {
			return nil
		}
		p, _ := walk.NewGeometricPen(walk.PenSolid, 2, br)
		return p
	}
	orange := pen(walk.RGB(255, 170, 40))
	if orange != nil {
		defer orange.Dispose()
	}
	drawArea := func(ir walk.Rectangle) {
		if orange == nil || !showArea || area.Empty() {
			return
		}
		c.DrawRectanglePixels(orange, walk.Rectangle{X: ir.X + int(area.X0*float64(ir.Width)), Y: ir.Y + int(area.Y0*float64(ir.Height)),
			Width: int((area.X1 - area.X0) * float64(ir.Width)), Height: int((area.Y1 - area.Y0) * float64(ir.Height))})
	}
	if u.comp == nil {
		if f != nil {
			msg := "No leveling capture yet.\r\nConnect the light (and the X-T3 on Camera and Light > Camera), lay a piece of clear film base (unexposed, developed) on the light, then press Start leveling.\r\nAuto finds a large rebate/gap cluster in the TIFF. Choose Rectangle and drag a small orange box over clear base (same as white-light on Camera)."
			c.DrawTextPixels(msg, f, walk.RGB(128, 128, 136), cr, walk.TextCenter|walk.TextVCenter|walk.TextWordbreak)
		}
		frame := walk.Rectangle{X: cr.X + cr.Width/6, Y: cr.Y + cr.Height/8, Width: cr.Width * 2 / 3, Height: cr.Height * 3 / 4}
		drawArea(frame)
	} else {
		ir := lightFit(u.comp, cr)
		c.DrawImageStretchedPixels(u.comp, ir)
		drawArea(ir)
	}
	cols := [3]walk.Color{walk.RGB(220, 80, 80), walk.RGB(80, 200, 100), walk.RGB(90, 130, 235)}
	for k := 0; k < 3; k++ {
		r := th[k]
		if u.thumbs[k] != nil {
			ir := lightFit(u.thumbs[k], r)
			c.DrawImageStretchedPixels(u.thumbs[k], ir)
			drawArea(ir)
		} else if p := pen(walk.RGB(60, 60, 66)); p != nil {
			c.DrawRectanglePixels(p, r)
			p.Dispose()
		}
		if f != nil {
			t := u.thumbTxt[k]
			if t == "" {
				t = rgblight.ChannelNames[k] + " only"
			}
			c.DrawTextPixels(t, f, cols[k], walk.Rectangle{X: r.X, Y: r.Y + r.Height + 1, Width: r.Width, Height: px(16, dpiOf(u.view))}, walk.TextCenter|walk.TextSingleLine|walk.TextEndEllipsis)
		}
	}
	return nil
}

func (u *LightUI) compImageRect() walk.Rectangle {
	cr, _ := u.boxes()
	if u.comp == nil {
		return walk.Rectangle{X: cr.X + cr.Width/6, Y: cr.Y + cr.Height/8, Width: cr.Width * 2 / 3, Height: cr.Height * 3 / 4}
	}
	return lightFit(u.comp, cr)
}

func (u *LightUI) norm(x, y int) (float64, float64) {
	ir := u.compImageRect()
	cl := func(v float64) float64 { return min(max(v, 0), 1) }
	return cl(float64(x-ir.X) / float64(max(ir.Width, 1))), cl(float64(y-ir.Y) / float64(max(ir.Height, 1)))
}

func (u *LightUI) mouseDown(x, y int, b walk.MouseButton) {
	if b != walk.LeftButton || u.level || u.comp == nil {
		return
	}
	u.dragging = true
	u.dragX, u.dragY = u.norm(x, y)
	u.dragA = lightlevel.Rect{X0: u.dragX, Y0: u.dragY, X1: u.dragX, Y1: u.dragY}
}

func (u *LightUI) mouseMove(x, y int, b walk.MouseButton) {
	if !u.dragging {
		return
	}
	fx, fy := u.norm(x, y)
	u.dragA = lightlevel.Rect{X0: min(u.dragX, fx), Y0: min(u.dragY, fy), X1: max(u.dragX, fx), Y1: max(u.dragY, fy)}
	u.view.Invalidate()
}

func (u *LightUI) mouseUp(x, y int, b walk.MouseButton) {
	if !u.dragging {
		return
	}
	u.dragging = false
	if u.dragA.X1-u.dragA.X0 > 0.03 && u.dragA.Y1-u.dragA.Y0 > 0.03 {
		u.publishRect(u.dragA)
		u.log("Film base rectangle set to %.0f-%.0f%% x %.0f-%.0f%% of the frame (used for leveling measure and TIFF check).", u.dragA.X0*100, u.dragA.X1*100, u.dragA.Y0*100, u.dragA.Y1*100)
	}
	u.view.Invalidate()
}

// ---------- page ----------

func (u *LightUI) Page() TabPage {
	u.cfg = loadLightConfig(u.hdr.cfg.ISO)
	u.cast = &castUI{owner: u}
	u.lw = newCamWorker()
	u.lw.onPanic = func(r any, st string) { u.hdr.reportPanic("light job", r, st) }
	bold := Font{Family: "Segoe UI", PointSize: 9, Bold: true}
	big := Font{Family: "Consolas", PointSize: 12, Bold: true}
	grey := walk.RGB(110, 110, 110)
	chCol := [3]walk.Color{walk.RGB(220, 80, 80), walk.RGB(80, 200, 100), walk.RGB(90, 130, 235)}
	var chWidgets []Widget
	for c := 0; c < 3; c++ {
		c := c
		chWidgets = append(chWidgets,
			CheckBox{AssignTo: &u.enCB[c], Text: rgblight.ChannelNames[c] + " on", Font: bold},
			LineEdit{AssignTo: &u.valEd[c], Text: strconv.Itoa(u.cfg.Values[c]), Font: big, TextColor: chCol[c], MaxLength: 3, MinSize: Size{Width: 56}, MaxSize: Size{Width: 56},
				ToolTipText: "Brightness 1..100. Switch a channel off with its checkbox (brightness 0 is not allowed)."},
		)
	}
	pct := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	return TabPage{
		Title:  " Light ",
		Layout: HBox{Margins: Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 8},
		Children: []Widget{
			Composite{
				StretchFactor: 3,
				Layout:        VBox{MarginsZero: true, Spacing: 6},
				Children: []Widget{
					Label{Text: "Live view: drag an orange box over clear film base."},
					u.hdr.liveView(3, grey),
					Card{Title: "Last leveling captures (not live)", Collapsed: true,
						Layout: VBox{MarginsZero: true, Spacing: 3}, Children: []Widget{
							CustomWidget{AssignTo: &u.view, Paint: u.paint, PaintMode: PaintBuffered, InvalidatesOnResize: true,
								MinSize:     Size{Width: 360, Height: 180},
								OnMouseDown: u.mouseDown, OnMouseMove: u.mouseMove, OnMouseUp: u.mouseUp},
							Label{AssignTo: &u.viewLbl, Text: "Captures appear after leveling.", TextColor: grey},
						}},
					u.cast.page(),
					TextEdit{AssignTo: &u.logTE, ReadOnly: true, VScroll: true, StretchFactor: 1, MinSize: Size{Height: 90}},
				},
			},
			Composite{
				AssignTo:      &lightRight,
				StretchFactor: 2,
				MinSize:       Size{Width: 480},
				MaxSize:       Size{Width: 520},
				Layout:        VBox{MarginsZero: true, Spacing: 3},
				Children: []Widget{
					Card{
						Title:  "RGB Light Air",
						Icon:   iconLight,
						Layout: VBox{Spacing: 3, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 4}},
						Children: []Widget{
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								ComboBox{AssignTo: &u.portCB, Model: []string{simLightItem, simNoAckItem}, CurrentIndex: 0, StretchFactor: 1,
									ToolTipText: "Serial ports. The light (USB 0483:5740) is found automatically and listed first; any COM port can be chosen by hand."},
								PushButton{Text: "Refresh", MaxSize: Size{Width: 70}, OnClicked: u.refreshPorts},
								PushButton{AssignTo: &u.connBtn, Text: "Connect", MinSize: Size{Width: 90}, OnClicked: u.toggleConnect,
									ToolTipText: "Connect reads the device info (21) and the state (20).\nDisconnect waits until the light is idle, switches R, G and B off and confirms it."},
							}},
							Label{AssignTo: &u.infoLbl, Text: "Not connected", Font: bold, MinSize: Size{Height: 48}},
							Label{AssignTo: &u.telLbl, Text: " ", TextColor: walk.RGB(30, 110, 60), Font: bold, MinSize: Size{Height: 18}},
						},
					},
					Card{
						Title:  "Channels",
						Icon:   iconWB,
						Layout: VBox{Spacing: 3, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 4}},
						Children: []Widget{
							Composite{Layout: Grid{Columns: 7, MarginsZero: true, Spacing: 6}, Children: append(chWidgets, HSpacer{})},
							Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
								PushButton{AssignTo: &u.applyBtn, Text: "Apply", Enabled: false, Font: bold, OnClicked: u.apply,
									ToolTipText: "Send the three channels (command 03) and confirm with a state read (20)."},
								PushButton{AssignTo: &u.readBtn, Text: "Read state", Enabled: false, OnClicked: func() {
									u.lightJob("Read state (20)", func(c *rgblight.Client) (rgblight.State, error) { return c.ReadState() })
								}},
								PushButton{AssignTo: &u.offBtn, Text: "All off", Enabled: false, OnClicked: u.allOff},
								HSpacer{},
							}},
							Label{AssignTo: &u.stateLbl, Text: "Light reports: -", MinSize: Size{Width: 440, Height: 44}},
						},
					},
					Card{
						Title:  "Auto film-base leveling",
						Icon:   iconMeter,
						Layout: Grid{Columns: 2, Spacing: 3, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 4}},
						Children: []Widget{
							Label{Text: "ISO", MinSize: Size{Width: 96}},
							Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
								ComboBox{AssignTo: &u.isoCB, Model: isoItems, CurrentIndex: u.cfg.ISO, MaxSize: Size{Width: 80}, OnCurrentIndexChanged: u.isoChanged,
									ToolTipText: "Leveling ISO - linked with the Camera page fixed frame-0 ISO (v0.82)."},
								Label{Text: "Shutter", MinSize: Size{Width: 50}, TextAlignment: AlignFar},
								ComboBox{AssignTo: &u.shutCB, Model: fixedShutterItems, CurrentIndex: u.hdr.cfg.Shutter, MinSize: Size{Width: 190},
									ToolTipText:           fixedShutterTip + "\nThe same setting as HDR scan's Base shutter (fixed frame 0).",
									OnCurrentIndexChanged: func() { u.hdr.setShutter(u.shutCB.CurrentIndex()) }},
								HSpacer{},
							}},
							Label{Text: "Base level", MinSize: Size{Width: 96}},
							ComboBox{AssignTo: &u.targetCB, Model: targetItems, CurrentIndex: u.cfg.Target,
								ToolTipText: "Level of the clear base in each channel (fraction of raw full scale) - the same choices as HDR scan's Base level."},
							Label{Text: "Tolerance", MinSize: Size{Width: 96}},
							Composite{Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
								Label{Text: "±"},
								LineEdit{AssignTo: &u.tolEd, Text: pct(u.cfg.Tol), MinSize: Size{Width: 44}, MaxSize: Size{Width: 44}},
								Label{Text: "%"},
								Label{Text: "Max rounds", MinSize: Size{Width: 88}, TextAlignment: AlignFar},
								LineEdit{AssignTo: &u.iterEd, Text: strconv.Itoa(u.cfg.MaxIter), MinSize: Size{Width: 36}, MaxSize: Size{Width: 36}},
								Label{Text: "Settle", MinSize: Size{Width: 56}, TextAlignment: AlignFar},
								LineEdit{AssignTo: &u.settleEd, Text: strconv.Itoa(u.cfg.SettleMS), MinSize: Size{Width: 50}, MaxSize: Size{Width: 50}},
								Label{Text: "ms"},
								HSpacer{},
							}},
							Label{Text: "Film base", MinSize: Size{Width: 96}},
							ComboBox{AssignTo: &u.areaCB, Model: []string{"Auto: find the base (rebate, gaps, sprocket area)", "Rectangle drawn on the image"}, CurrentIndex: u.cfg.AreaMode, OnCurrentIndexChanged: u.areaModeChanged,
								ToolTipText: "Auto measures the detected rebate / gap / sprocket cluster in the trichrome TIFF - that area can be large.\nRectangle: drag a small patch of clear film base on the preview (or on Camera live view), the same orange box as white-light metering.\nLeveling then reads only that patch for the RAW measure and the TIFF check."},
							Label{Text: "Camera profile", MinSize: Size{Width: 96}},
							CheckBox{AssignTo: &u.camCB, Text: "Apply camera profile (base after the matrix)", Checked: !u.cfg.NoCam,
								ToolTipText: "v0.85: with a camera profile enabled (Chart calibration), leveling aims at the film base AFTER the CameraCal matrix,\nso the film-base reference can stay at 0.5/0.5/0.5. After leveling the last R/G/B RAWs go through the real trichrome\nTIFF converter and clear-base measurement; if that base is off by more than the tolerance, one corrective pass runs."},
							Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
								PushButton{AssignTo: &u.startBtn, Text: "Start leveling", Enabled: false, Font: bold, MinSize: Size{Height: 26}, OnClicked: u.startLevel,
									ToolTipText: "For R, G and B in turn: only that channel on, wait the settle time, shoot one RAW at the fixed exposure and measure the base\nin that colour channel; brightness = current x target / measured, repeated until all three are within tolerance.\nFilm base: Auto (large detected cluster) or a rectangle you drag on the preview / Camera live view."},
								PushButton{AssignTo: &u.stopBtn, Text: "Stop", Enabled: false, OnClicked: func() {
									if u.cancel != nil {
										u.cancel()
									}
								}},
								HSpacer{},
							}},
							Label{AssignTo: &u.resLbl, ColumnSpan: 2, Text: "Clear film base on the light; exposure fixed, only the light changes.", MinSize: Size{Width: 440, Height: 76}},
							Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
								PushButton{AssignTo: &u.copyBtn, Text: "Copy", Enabled: false, OnClicked: u.copyLeveling},
								HSpacer{},
							}},
						},
					},
					u.presetGroup(),
					Card{
						Title:     "Faults reported by the light",
						Collapsed: true,
						Layout:    VBox{Spacing: 3, Margins: Margins{Left: 8, Top: 2, Right: 8, Bottom: 4}},
						Children: []Widget{
							TextEdit{AssignTo: &u.faultTE, ReadOnly: true, VScroll: true, MinSize: Size{Height: 50}},
						},
					},
					VSpacer{},
				},
			},
		},
	}
}

// AfterCreate fills the port list and stocks and switches the light off on exit.
func (u *LightUI) AfterCreate() {
	if box, ok := u.manualBase(); ok {
		u.publishRect(box)
	}
	u.refreshPorts()
	u.initPresets()
	for c := 0; c < 3; c++ {
		u.enCB[c].SetChecked(true)
	}
	setLightStatusN(2, u.stateLbl, "Light reports: -")
	setLightStatusN(3, u.resLbl, "Clear film base on the light; exposure fixed, only the light changes.")
	u.updateButtons()
	if mw := u.win(); mw != nil {
		mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) { u.shutdown() })
	}
	u.log("%s Light page ready. Only the documented customer commands are sent to the light.", appVersion)
}

// setLightStatusN is setStatus with a narrower wrap for the Light panel. The
// text is padded to at least n lines so the panel keeps its layout when the
// message changes (no re-layout, no half-painted group titles).
func setLightStatusN(n int, l *walk.Label, s string) {
	if l == nil {
		return
	}
	t := wrapLines(s, 50)
	for k := strings.Count(t, "\n") + 1; k < n; k++ {
		t += "\n "
	}
	l.SetText(t)
	l.SetToolTipText(s)
	redrawLightRight()
}

// lightRight is the Light page's right-hand column. Its group boxes are
// owner-drawn by the dark theme; when a status label changes height the
// boxes move, so the whole column is repainted after the new layout.
var lightRight *walk.Composite

func redrawLightRight() {
	c := lightRight
	if c == nil {
		return
	}
	c.Synchronize(func() {
		win.RedrawWindow(c.Handle(), nil, 0, win.RDW_INVALIDATE|win.RDW_ERASE|win.RDW_FRAME|win.RDW_ALLCHILDREN)
	})
}

// restoreExposure (v0.81) selects the ISO and base shutter the stored light
// setting was levelled at (entries before v0.81 without a known shutter
// leave the selection alone).
func (u *LightUI) restoreExposure(isoV int, shutter uint32) {
	l := struct {
		ISO     int
		Shutter uint32
	}{isoV, shutter}
	var parts []string
	if i := fixedShutterIndexOf(l.Shutter); i >= 0 {
		u.hdr.setShutter(i)
		if u.shutCB != nil && u.shutCB.CurrentIndex() != i {
			u.shutCB.SetCurrentIndex(i)
		}
		parts = append(parts, "shutter "+camera.ShutterLabel(l.Shutter))
	} else if l.Shutter != 0 {
		parts = append(parts, fmt.Sprintf("stored shutter %s is not a base shutter choice - selection kept", camera.ShutterLabel(l.Shutter)))
	}
	for i, v := range isoVals {
		if v == l.ISO && l.ISO != 0 {
			if u.isoCB != nil && u.isoCB.CurrentIndex() != i {
				u.isoCB.SetCurrentIndex(i)
			}
			u.cfg.ISO = i
			u.saveCfg()
			u.hdr.setISO(i) // v0.82: and the HDR page's fixed ISO
			parts = append(parts, fmt.Sprintf("ISO %d", v))
		}
	}
	if len(parts) > 0 {
		u.log("Preset exposure set: %s (camera side follows)", strings.Join(parts, ", "))
	}
}

// countFiles counts the regular files directly in dir (v0.83 temp cleanup log).
func countFiles(dir string) int {
	es, _ := os.ReadDir(dir)
	n := 0
	for _, e := range es {
		if !e.IsDir() {
			n++
		}
	}
	return n
}
