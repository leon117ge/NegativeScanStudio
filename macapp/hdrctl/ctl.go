package hdrctl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"basetool/camera"
	"basetool/filmdb"
	"basetool/hdrscan"
	"basetool/rawconv"
	"basetool/scanmeta"

	"basetool/macapp/appstate"
)

// Host is what the controller needs from the UI.
type Host interface {
	// Post runs f on the UI goroutine and redraws (never blocks the caller).
	Post(f func())
	// PickSimScene asks for the RAF the simulated camera photographs.
	PickSimScene(start string, done func(path string, ok bool))
	// Message shows a message box (non-blocking).
	Message(title, text string)
	// Confirm asks OK / Cancel; done runs on the UI goroutine.
	Confirm(title, text string, done func(ok bool))
	// PickFile shows a native open (save=false) or save panel; done runs on
	// the UI goroutine.
	PickFile(title string, save bool, start string, exts []string, done func(path string, ok bool))
	// SetClipboard puts text on the clipboard.
	SetClipboard(text string)
}

// Device is one entry of the camera dropdown.
type Device struct {
	Name string
	Open func(log *camera.Logger) (camera.Device, error)
}

// Ctl is the state of the Camera and White light scan pages.
type Ctl struct {
	Host Host
	Cfg  appstate.HDRConfig
	Log  *camera.Logger
	DB   *filmdb.DB

	// ListDevices lists real cameras (ImageCaptureCore on macOS, nil
	// elsewhere). OpenSim opens the simulated camera (tests replace it
	// with a mock).
	ListDevices func() ([]Device, error)
	OpenSim     func(scene string, log *camera.Logger) (camera.Device, error)

	worker *camWorker
	dev    camera.Device
	sess   *hdrscan.Session
	isSim  bool
	devs   []Device
	DevIdx int

	// view (UI goroutine)
	Frame     image.Image
	FrameSeq  int
	ViewWhat  string
	ROI       hdrscan.Rect
	BaseNorm  hdrscan.Rect
	LvWanted  bool
	lvVisible bool
	lvOn      atomic.Bool
	lvRunning atomic.Bool
	lvDrawing atomic.Bool
	camJob    atomic.Bool
	statBusy  atomic.Bool
	Busy      bool
	Working   bool // merge running (Processing on Windows)
	Progress  bool // the progress bar runs (marquee)

	// readouts
	CamLbl, ExpLbl, BaseLbl                                             string
	CamISO, CamSh, CamF, F0ISO, F0Sh, P2Sh, BaseLv, BaseR, BaseG, BaseB string
	Res                                                                 string
	logLines                                                            []string
	LogSeq                                                              int

	scanCancel context.CancelFunc
	sessFirst  time.Time
	LastCap    *hdrscan.Result
	LastMerge  *hdrscan.Result // Merge files (merge.go)
	MF         MergeFiles
	MRes       string // Merge files result box (mRes)
	MResSeq    int
	Hair       *Hair
	// OpenInDevelop opens an HDR TIFF in Develop (main.go's hook).
	OpenInDevelop func(path string)
	mergeCancel   context.CancelFunc
	camFn         float64

	// Light page link (v0.82 handoff, shared ISO / base shutter / rectangle)
	Light       *Light
	Hik         *hikCams
	handoff     *baseHandoff
	autoLocking bool
	lockSrc     string
	lockLight   *[3]int // light values when frame 0 was locked (nil = unknown)
	// lock is the UI goroutine's copy of sess.Locked. The camera worker
	// writes sess.Locked (hdrscan Session.Meter / LockFixed) while the UI
	// reads the lock state for every redraw, so the UI never reads
	// sess.Locked: the worker publishes a copy through Host.Post.
	lock     *camera.Exposure
	isoPrev  int // Cfg.ISO / Cfg.Shutter as last handled (the combos write Cfg first)
	shutPrev int
	// shared Hikrobot exposure (hikexp_ui.go)
	syncingHikExp, hikExpReady bool
}

// New loads the settings and starts the camera worker.
func New(h Host) *Ctl {
	c := &Ctl{Host: h, Cfg: appstate.LoadHDRConfig(), CamLbl: "Not connected", F0ISO: "not locked",
		Res: "Output: <Save to>/<roll>/<name>_HDR.tiff"}
	c.Log = camera.NewLogger(appstate.ConfigDir())
	c.Log.OnLine = func(s string) { c.Host.Post(func() { c.appendLog(s) }) }
	c.worker = newCamWorker()
	c.worker.onPanic = func(r any, st string) {
		c.Log.Printf("camera job panic: %v\n%s", r, st)
		c.Host.Post(func() { c.Busy = false; c.camJob.Store(false) })
	}
	c.ListDevices = c.platformDevices()
	c.OpenSim = func(scene string, log *camera.Logger) (camera.Device, error) {
		s, err := camera.OpenSim(scene, log)
		if err != nil {
			return nil, err
		}
		// Scene file values = ISO 160, 1/125; start somewhere else so
		// metering has work to do.
		s.SetDials(camera.Exposure{ISO: 160, Shutter: 3906})
		return s, nil
	}
	if db, err := filmdb.Load(filmdb.Path(appstate.ConfigDir())); err == nil {
		c.DB = db
	}
	c.isoPrev, c.shutPrev = c.Cfg.ISO, c.Cfg.Shutter
	c.Hik = newHikCams()
	c.Light = newLight(c)
	c.initMerge()
	c.Hair = &Hair{c: c, Status: hairStatusText, Mask: true}
	return c
}

// Start is AfterCreate: first log line and the device list.
func (c *Ctl) Start() {
	c.showBracket()
	c.say(appstate.AppVersion + " HDR scan ready. Log file: " + c.Log.Path())
	c.RefreshDevices()
	if c.Light != nil {
		c.Light.Start()
	}
}

// Shutdown runs when the window closes: the Light page's planned
// disconnect (lightui.shutdown on Windows).
func (c *Ctl) Shutdown() {
	if c.Light != nil {
		c.Light.Shutdown()
	}
}

const maxLog = 400

func (c *Ctl) appendLog(s string) {
	c.logLines = append(c.logLines, s)
	if len(c.logLines) > maxLog {
		c.logLines = c.logLines[len(c.logLines)-maxLog:]
	}
	c.LogSeq++
}

// LogText is the log pane text.
func (c *Ctl) LogText() string { return strings.Join(c.logLines, "\n") }

func (c *Ctl) say(s string) { c.Log.Printf("%s", s) }

// Save writes hdr_config.txt (the UI changes Cfg first).
func (c *Ctl) Save() {
	c.Cfg.Roll = hdrscan.SanitizeRoll(c.Cfg.Roll)
	appstate.SaveHDRConfig(c.Cfg)
}

// Connected reports whether a camera is open.
func (c *Ctl) Connected() bool { return c.dev != nil }

// Locked reports whether frame 0 is locked.
func (c *Ctl) Locked() bool { return c.dev != nil && c.sess != nil && c.lock != nil }

// lockedCopy copies a session's lock on the goroutine that owns the session
// (the camera worker) so it can be posted to the UI goroutine.
func lockedCopy(s *hdrscan.Session) *camera.Exposure {
	if s == nil || s.Locked == nil {
		return nil
	}
	e := *s.Locked
	return &e
}

// Scanning reports whether Stop is available.
func (c *Ctl) Scanning() bool { return c.scanCancel != nil }

// Buttons is updateButtons: which controls are enabled and their labels.
type Buttons struct {
	ConnText                                 string
	Conn, DevList, Diag, Test, Live, Meter   bool
	LiveText, MeterText, CapText             string
	Capture, Stop, ISO, Shutter, Rec, SaveDB bool
}

func (c *Ctl) Buttons() Buttons {
	conn, locked := c.Connected(), c.Locked()
	b := Buttons{
		ConnText: map[bool]string{true: "Disconnect", false: "Connect"}[conn],
		Conn:     !c.Busy, DevList: !conn && !c.Busy, Diag: !c.Busy, Test: conn && !c.Busy,
		Live: conn && !c.Busy, LiveText: map[bool]string{true: "Stop live view", false: "Start live view"}[c.LvWanted],
		Meter: conn && !c.Busy, MeterText: map[bool]string{true: "Re-meter base", false: "Meter base"}[locked],
		Capture: locked && !c.Busy && !c.Working, Stop: c.scanCancel != nil,
		ISO: c.Cfg.Frame0 == 1, Shutter: !c.Busy,
		CapText: "Capture HDR",
	}
	if len(appstate.EVs(c.Cfg)) <= 1 {
		b.CapText = "Capture"
	}
	if c.handoff != nil && c.handoff.pending {
		c.autoLock() // updateButtons runs a pending light handoff's lock
	}
	return b
}

// DeviceItems are the dropdown entries (real cameras, then the simulator).
func (c *Ctl) DeviceItems() []string {
	var items []string
	for _, d := range c.devs {
		items = append(items, d.Name)
	}
	return append(items, appstate.SimItem)
}

func (c *Ctl) RefreshDevices() {
	list := c.ListDevices
	c.worker.do(func() {
		var devs []Device
		var err error
		if list != nil {
			devs, err = list()
		}
		c.Host.Post(func() {
			c.devs = nil
			switch {
			case list == nil:
			case err != nil:
				c.say("Camera list: " + err.Error())
			case len(devs) == 0:
				c.say("No cameras found. Connect the camera by USB, switch it on and set CONNECTION MODE to USB TETHER SHOOTING FIXED.")
			default:
				c.devs = devs
				for _, d := range devs {
					c.say("Found device: " + d.Name)
				}
			}
			c.DevIdx = 0
		})
	})
}

// Connect connects the selected device (or disconnects).
func (c *Ctl) Connect() {
	if c.dev != nil {
		c.Disconnect()
		return
	}
	i := c.DevIdx
	sim := i < 0 || i >= len(c.devs)
	if sim {
		c.Host.PickSimScene(c.Cfg.SimScene, func(path string, ok bool) {
			if !ok {
				return
			}
			c.Cfg.SimScene = path
			c.Save()
			c.connect(nil, path)
		})
		return
	}
	d := c.devs[i]
	c.connect(&d, "")
}

func (c *Ctl) connect(real *Device, scene string) {
	sim := real == nil
	connISO := hdrscan.FixedISO
	if appstate.Fixed(c.Cfg) {
		connISO = appstate.FixedISO(c.Cfg)
	}
	c.Busy = true
	c.CamLbl = "Connecting ..."
	c.worker.do(func() {
		var d camera.Device
		var err error
		if sim {
			d, err = c.OpenSim(scene, c.Log)
		} else {
			c.Log.Printf("Opening %s", real.Name)
			d, err = real.Open(c.Log)
		}
		var sess *hdrscan.Session
		var notManual error
		if err == nil {
			sess = &hdrscan.Session{Dev: d, Log: c.Log, ProbeDir: filepath.Join(os.TempDir(), "NegativeScanStudio", "probe"), Dials: c.dialPrompt}
			if ierr := sess.EnsureConnectISO(connISO); errors.Is(ierr, camera.ErrNotManual) {
				notManual = ierr
				c.Log.Printf("%v", ierr)
			} else if ierr != nil {
				c.Log.Printf("Could not set ISO %d on connect: %v - set ISO %d on the camera (ISO dial, or A with ISO DIAL SETTING (A) = COMMAND)", connISO, ierr, connISO)
			}
		}
		var e camera.Exposure
		var eerr error
		if err == nil {
			e, eerr = d.Exposure()
		}
		c.Host.Post(func() {
			c.Busy = false
			if err != nil {
				c.CamLbl = "Not connected"
				c.Host.Message("Connect camera", "Could not connect:\n"+err.Error()+"\n\nCheck: camera on, USB cable direct to the Mac, CONNECTION MODE = USB TETHER SHOOTING FIXED, and no other tether program (X Acquire, Lightroom, Capture One) running.\nUse Camera diagnostics for a detailed log.")
				return
			}
			c.dev, c.isSim, c.sess = d, sim, sess
			info := d.Info()
			cam := fmt.Sprintf("%s %s", info.Manufacturer, info.Model)
			if info.Driver != "" && !strings.Contains(cam, info.Driver) {
				cam += "  (" + info.Driver + ")"
			}
			c.CamLbl = "Connected: " + cam
			if eerr != nil {
				c.ExpLbl = "Exposure: could not read (" + eerr.Error() + ")"
			} else {
				c.ExpLbl = "Not metered yet  (camera " + e.String() + ")"
			}
			c.BaseLbl = "Press Meter base. The base is found automatically (rebate strips, gaps between frames, sprocket area), or drag a rectangle over it."
			c.clearLocked()
			c.LvWanted = true // live view starts by itself after connecting
			c.startLiveView()
			c.startStatusPoll(d)
			if notManual != nil {
				c.notManualMsg(notManual)
			}
		})
	})
}

func (c *Ctl) Disconnect() {
	c.lvOn.Store(false)
	c.LvWanted = false
	c.clearLocked()
	c.CamISO, c.CamSh, c.CamF = "", "", ""
	d := c.dev
	c.dev, c.sess = nil, nil
	c.CamLbl, c.ExpLbl, c.BaseLbl = "Not connected", "", ""
	if d != nil {
		c.worker.do(func() {
			d.StopLiveView()
			d.Close()
		})
	}
}

func (c *Ctl) notManualMsg(err error) {
	c.ExpLbl = "Set the camera to M (shutter dial T)"
	c.Host.Message("Set the camera to M",
		"Set the camera to M (shutter dial T).\n\n"+err.Error()+
			"\n\nOn the X-T3: turn the shutter speed dial to T (and the aperture ring off A, or use a manual lens), "+
			"then press Meter base again. The program then sets ISO 160 and the shutter over USB.")
}

// dialPrompt runs on the camera worker and waits for the user's answer;
// the dialog itself is on the UI goroutine, so the main thread is free.
func (c *Ctl) dialPrompt(e camera.Exposure, why string) bool {
	ch := make(chan bool, 1)
	c.Host.Post(func() {
		c.Host.Confirm("Set the exposure on the camera",
			fmt.Sprintf("The camera does not accept exposure changes over USB, so set it by hand (%s):\n\n    ISO:      %d\n    Shutter:  %s\n\nLeave the aperture where it is. Press OK when the camera shows these values.",
				why, e.ISO, camera.ShutterLabel(e.Shutter)), func(ok bool) { ch <- ok })
	})
	return <-ch
}

func (c *Ctl) Diagnostics() {
	c.pauseLiveView()
	c.Busy = true
	d := c.dev
	c.worker.do(func() {
		c.Log.Printf("==== %s camera diagnostics, %s/%s ====", appstate.AppVersion, runtime.GOOS, runtime.GOARCH)
		if d != nil {
			if err := d.Diagnostics(); err != nil {
				c.Log.Printf("Diagnostics error: %v", err)
			}
		} else {
			c.Log.Printf("(Connect the camera first for the full property dump.)")
		}
		c.Log.Printf("==== end ====")
		c.Host.Post(func() {
			c.Busy = false
			c.resumeLiveView()
			c.Host.Message("Camera diagnostics", "Written to:\n"+c.Log.Path())
		})
	})
}

// ---------- live view ----------

func (c *Ctl) setImage(img image.Image, what string) {
	c.Frame, c.ViewWhat = img, what
	c.FrameSeq++
}

func (c *Ctl) ToggleLiveView() {
	if c.LvWanted {
		c.LvWanted = false
		c.lvOn.Store(false)
		return
	}
	c.LvWanted = true
	c.startLiveView()
}

// SetLiveVisible is setLivePage: false stops polling (page without a view).
func (c *Ctl) SetLiveVisible(on bool) {
	if c.lvVisible == on {
		return
	}
	c.lvVisible = on
	if !on {
		c.lvOn.Store(false)
		return
	}
	if c.LvWanted {
		c.startLiveView()
	}
}

func (c *Ctl) pauseLiveView() {
	c.camJob.Store(true)
	c.lvOn.Store(false)
}

func (c *Ctl) resumeLiveView() {
	c.camJob.Store(false)
	if c.LvWanted && c.dev != nil {
		c.startLiveView()
	}
}

// LiveRunning reports whether the live-view loop is alive (tests).
func (c *Ctl) LiveRunning() bool { return c.lvRunning.Load() }

func (c *Ctl) startLiveView() {
	if c.dev == nil || !c.LvWanted || !c.lvVisible || c.camJob.Load() {
		return
	}
	if c.lvRunning.Load() {
		if !c.lvOn.Load() {
			time.AfterFunc(150*time.Millisecond, func() { c.Host.Post(c.startLiveView) })
		}
		return
	}
	c.lvOn.Store(true)
	c.lvRunning.Store(true)
	d := c.dev
	go func() {
		defer c.lvRunning.Store(false)
		const minGap = 66 * time.Millisecond // <= 15 fps
		fails, frames := 0, 0
		var fps float64
		tWin := time.Now()
		for c.lvOn.Load() {
			t0 := time.Now()
			done := make(chan struct{})
			var jpg []byte
			var err error
			c.worker.do(func() {
				if c.lvOn.Load() {
					jpg, err = d.LiveView()
				}
				close(done)
			})
			<-done
			if err != nil {
				fails++
				if fails >= 5 {
					c.say("Live view stopped: " + err.Error())
					c.lvOn.Store(false)
					c.Host.Post(func() { c.LvWanted = false })
					break
				}
				time.Sleep(300 * time.Millisecond)
				continue
			}
			fails = 0
			if jpg != nil && c.lvOn.Load() {
				frames++
				if el := time.Since(tWin); el >= time.Second {
					fps = float64(frames) / el.Seconds()
					frames, tWin = 0, time.Now()
				}
				if c.lvDrawing.CompareAndSwap(false, true) {
					// decode off the UI goroutine; the UI uploads it as a GPU texture
					if img, e := jpeg.Decode(bytes.NewReader(jpg)); e == nil {
						lbl := "Live view  (measuring fps)"
						if fps > 0 {
							lbl = fmt.Sprintf("Live view  %.1f fps", fps)
						}
						c.Host.Post(func() {
							defer c.lvDrawing.Store(false)
							if c.lvOn.Load() && c.dev == d {
								c.setImage(img, lbl)
							}
						})
					} else {
						c.lvDrawing.Store(false)
					}
				}
			}
			if gap := time.Since(t0); gap < minGap {
				time.Sleep(minGap - gap)
			}
		}
		c.worker.do(func() { d.StopLiveView() })
		c.Host.Post(func() {
			if !c.lvOn.Load() && strings.HasPrefix(c.ViewWhat, "Live view") {
				c.ViewWhat = "Live view paused"
			}
		})
	}()
}

func (c *Ctl) startStatusPoll(d camera.Device) {
	sr, ok := d.(camera.StatusReader)
	if !ok {
		return
	}
	go func() {
		for {
			alive := make(chan bool, 1)
			c.Host.Post(func() { alive <- c.dev == d })
			if !<-alive {
				return
			}
			if !c.camJob.Load() && c.statBusy.CompareAndSwap(false, true) {
				c.worker.do(func() {
					defer c.statBusy.Store(false)
					if c.camJob.Load() {
						return
					}
					e, fn, err := sr.Status()
					if err != nil {
						return
					}
					c.Host.Post(func() { c.showCamera(e, fn) })
				})
			}
			time.Sleep(1200 * time.Millisecond)
		}
	}()
}

func (c *Ctl) showCamera(e camera.Exposure, fn float64) {
	if c.dev == nil {
		return
	}
	c.camFn = fn
	c.CamISO = camera.ISOLabel(e.ISO)
	c.CamSh = camera.ShutterLabel(e.Shutter)
	if fn > 0 {
		c.CamF = fmt.Sprintf("f/%.1f", fn)
	} else {
		c.CamF = "f/-- (lens)"
	}
}

func (c *Ctl) clearLocked() {
	c.lock = nil
	c.lockSrc, c.lockLight = "", nil
	c.F0ISO, c.F0Sh, c.P2Sh, c.BaseLv, c.BaseR, c.BaseG, c.BaseB = "not locked", "", "", "", "", "", ""
}

// ---------- base rectangle ----------

// SetROI stores a rectangle dragged on the image (Rectangle mode).
func (c *Ctl) SetROI(r hdrscan.Rect) {
	if r.Empty() {
		return
	}
	c.ROI = r
	c.Cfg.AreaMode = 1
	c.Save()
	if l := c.Light; l != nil {
		l.cfg.Area = toLightRect(r)
		l.cfg.AreaMode = 1
		l.cfg.AreaPicked = true
		l.saveCfg()
	}
	c.BaseLbl = "Base rectangle set. Press Meter base."
}

// ClearROI is Reset to Auto.
func (c *Ctl) ClearROI() {
	if c.Busy || (c.Light != nil && c.Light.level) {
		return
	}
	c.ROI = hdrscan.Rect{}
	c.Cfg.AreaMode = 0
	c.Save()
	if l := c.Light; l != nil {
		l.cfg.Area, l.cfg.AreaMode, l.cfg.AreaPicked = toLightRect(hdrscan.Rect{}), 0, false
		l.saveCfg()
	}
}

// ---------- meter / capture ----------

// Meter is the Meter base button (a manual lock supersedes a light
// handoff's label, the handoff's values stay the trichrome scan values).
func (c *Ctl) Meter() { c.meterSrc(nil) }

// meterSrc meters / locks frame 0; ho != nil is a leveling / Apply stored
// handoff (fixed lock at its exposure, no dialogs).
func (c *Ctl) meterSrc(ho *baseHandoff) {
	if c.dev == nil || c.Busy {
		return
	}
	if ho == nil && c.Cfg.AreaMode == 1 && c.ROI.Empty() {
		c.Host.Message("Meter base", "Base area is set to \"Rectangle\": drag a rectangle over the clear film base on the image first (start live view to see the frame), or choose Auto.")
		return
	}
	c.pauseLiveView()
	c.Busy = true
	s := c.sess
	s.Target = appstate.Target(c.Cfg)
	s.Slowest = appstate.Slowest(c.Cfg)
	if c.Cfg.AreaMode == 1 {
		s.ROI = c.ROI
	} else {
		s.ROI = hdrscan.Rect{}
	}
	fixed, iso := appstate.Fixed(c.Cfg), appstate.FixedISO(c.Cfg)
	src := ""
	if ho != nil {
		fixed, iso, src = true, ho.ISO, "from "+ho.Source
		c.Log.Printf("==== Frame 0 lock from the light handoff (%s): fixed ISO %d, %s - no Meter base needed ====", ho.Source, ho.ISO, camera.ShutterLabel(ho.Shutter))
	}
	var lockLight *[3]int
	if l := c.Light; l != nil && l.cl != nil {
		if cs := l.cl.CachedState(); cs != nil {
			v := cs.Value
			lockLight = &v
		}
	}
	s.Fixed = appstate.FixedShutter(c.Cfg)
	if ho != nil {
		s.Fixed = ho.Shutter
	}
	if fixed {
		c.ExpLbl = fmt.Sprintf("Frame 0 fixed: ISO %d, %s ...", iso, camera.ShutterLabel(s.Fixed))
	} else {
		c.ExpLbl = "Metering ..."
	}
	c.LastCap = nil
	c.lock = nil // Session.Meter / LockFixed drop the lock first
	c.worker.do(func() {
		var e camera.Exposure
		var r hdrscan.Reading
		var err error
		var warns []string
		if fixed {
			e, r, warns, err = s.LockFixed(iso)
			for _, w := range warns {
				c.Log.Printf("Frame 0 fixed: WARNING: %s", w)
			}
		} else {
			e, r, err = s.Meter()
		}
		var st camera.Exposure
		var fn float64
		var sterr error = errors.New("no status")
		if sr, ok := s.Dev.(camera.StatusReader); ok {
			st, fn, sterr = sr.Status()
		}
		var prev image.Image
		if s.LastProbe != nil {
			prev = hdrscan.Preview(s.LastProbe)
		}
		lock := lockedCopy(s)
		c.Host.Post(func() {
			c.Busy = false
			if c.sess == s {
				c.lock = lock
			}
			if sterr == nil {
				c.showCamera(st, fn)
			}
			if prev != nil {
				c.setImage(prev, "Last metering probe (RAW) - live view resumes in 4 s")
			}
			if r.Max > 0 {
				c.BaseNorm = r.Norm
				c.showBase(r)
			}
			time.AfterFunc(4*time.Second, func() { c.Host.Post(c.resumeLiveView) })
			if err != nil {
				c.clearLocked()
				c.ExpLbl = "NOT LOCKED"
				c.BaseLbl = "Metering failed: " + err.Error()
				c.say("Metering failed: " + err.Error())
				if ho != nil {
					c.say("Frame 0 lock from " + ho.Source + " failed - press Meter base, or level / load a preset again.")
				}
				if errors.Is(err, camera.ErrNotManual) {
					c.notManualMsg(err)
				}
				return
			}
			c.BaseNorm = r.Norm
			c.lockSrc, c.lockLight = src, lockLight
			mode := "set over USB"
			if s.DialMode {
				mode = "set on the camera dials"
			}
			if src != "" {
				mode += ", " + src + " - ready to scan, no Meter base needed"
				c.say(fmt.Sprintf("Frame 0 LOCKED from %s: ISO %d, %s, base %.2f FS, R/G/B %.3f / %.3f / %.3f - ready to scan.",
					ho.Source, e.ISO, camera.ShutterLabel(e.Shutter), r.Max, r.RGB[0], r.RGB[1], r.RGB[2]))
			}
			if fixed {
				c.ExpLbl = fmt.Sprintf("LOCKED  (fixed ISO %d, %s, %s)", e.ISO, camera.ShutterLabel(e.Shutter), mode)
				extra := ""
				for _, w := range warns {
					extra += "\nWARNING: " + w
				}
				extra += "\nFilm stock check: not on macOS yet"
				c.BaseLbl += extra
				if len(warns) > 0 && ho == nil {
					c.Host.Message("Frame 0", strings.TrimPrefix(extra, "\n"))
				}
			} else {
				c.ExpLbl = fmt.Sprintf("LOCKED  (%s, ISO stays 160)", mode)
			}
			c.F0ISO = camera.ISOLabel(e.ISO)
			c.F0Sh = camera.ShutterLabel(e.Shutter)
			c.showBracket()
		})
	})
}

func (c *Ctl) showBase(r hdrscan.Reading) {
	c.BaseLv = fmt.Sprintf("%.2f FS", r.Max)
	c.BaseR = fmt.Sprintf("%.3f", r.RGB[0])
	c.BaseG = fmt.Sprintf("%.3f", r.RGB[1])
	c.BaseB = fmt.Sprintf("%.3f", r.RGB[2])
	t := "Base region (green box), your rectangle"
	if r.Auto {
		t = "Base region (green box), auto:\n" + r.Where
		if i := strings.Index(t, ", median of"); i > 0 {
			t = t[:i]
		}
	}
	if r.Cells > 0 && r.Lo > 0 {
		t += fmt.Sprintf("\nMedian over the whole base; %s min/max %.3f/%.3f (%.2f EV)",
			[]string{"R", "G", "B"}[r.Channel], r.Lo, r.Hi, math.Log2(r.Hi/r.Lo))
	} else {
		t += fmt.Sprintf("\n%dx%d px at x %d, y %d", r.Area.Dx(), r.Area.Dy(), r.Area.Min.X, r.Area.Min.Y)
	}
	if r.Warn != "" {
		t += "\nWARNING: " + r.Warn
	}
	c.BaseLbl = t
}

func (c *Ctl) showBracket() {
	if !c.Locked() {
		c.P2Sh = ""
		return
	}
	c.P2Sh = appstate.BrighterText(*c.lock, appstate.EVs(c.Cfg))
}

// BracketChanged updates the Brighter readout after the Bracket choice.
func (c *Ctl) BracketChanged() { c.Save(); c.showBracket() }

func (c *Ctl) outDir() string {
	d := strings.TrimSpace(c.Cfg.OutDir)
	if d == "" {
		d = appstate.DefaultOutDir()
		c.Cfg.OutDir = d
	}
	return d
}

func (c *Ctl) TestShot() {
	if c.dev == nil || c.Busy {
		return
	}
	dir := c.outDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.Host.Message("Test shot", err.Error())
		return
	}
	c.pauseLiveView()
	c.Busy = true
	d := c.dev
	c.worker.do(func() {
		c.Log.Printf("==== Test shot (%s) ====", appstate.AppVersion)
		if e, err := d.Exposure(); err == nil {
			c.Log.Printf("Test shot: camera reports %s", e)
		}
		t0 := time.Now()
		path, err := d.Capture(dir, "testshot_")
		var prev image.Image
		var r hdrscan.Reading
		var merr error
		if err == nil {
			c.Log.Printf("Test shot: downloaded %s in %.1f s", path, time.Since(t0).Seconds())
			if f, rerr := rawconv.ReadRawFrame(path); rerr != nil {
				c.Log.Printf("Test shot: not a readable RAW (%v) - set the camera to RAW or RAW+JPEG", rerr)
			} else {
				prev = hdrscan.Preview(f)
				r, merr = hdrscan.MeasureBase(f, hdrscan.Rect{})
				if merr != nil {
					c.Log.Printf("Test shot: base detection: %v", merr)
				} else {
					c.Log.Printf("Test shot: %s", r)
				}
			}
		} else {
			c.Log.Printf("Test shot FAILED: %v", err)
		}
		c.Log.Printf("==== end of test shot ====")
		c.Host.Post(func() {
			c.Busy = false
			if prev != nil {
				c.setImage(prev, "Test shot (RAW) - live view resumes in 4 s")
				if merr == nil {
					c.BaseNorm = r.Norm
					c.showBase(r)
				}
			}
			time.AfterFunc(4*time.Second, func() { c.Host.Post(c.resumeLiveView) })
			if err != nil {
				c.Host.Message("Test shot", "The test shot failed:\n\n"+err.Error()+"\n\nThe full sequence is in "+c.Log.Path())
			}
		})
	})
}

func (c *Ctl) rollDir() (string, error) {
	if c.sessFirst.IsZero() {
		c.sessFirst = time.Now()
	}
	return hdrscan.RollDir(c.outDir(), c.Cfg.Roll, c.sessFirst)
}

func (c *Ctl) processOptions(dir string) hdrscan.ProcessOptions {
	mo := hdrscan.DefaultOptions()
	mo.Align = appstate.AlignMode(c.Cfg)
	mo.Method = appstate.MergeMode(c.Cfg)
	md := scanmeta.Metadata{Roll: c.Cfg.Roll, Mode: "white-light", Encoding: "linear"}
	return hdrscan.ProcessOptions{Merge: mo, OutDir: dir, KeepTIFF: c.Cfg.KeepTIFF, DeleteRAW: c.Cfg.DeleteRAW, LZW: c.Cfg.LZW,
		Software: appstate.AppVersion, Metadata: md, LeakPct: 20, Dust: c.Cfg.Dust}
}

// Capture is Capture HDR: bracket, download, then merge in the background.
func (c *Ctl) Capture() {
	if c.dev == nil || c.Busy || !c.Locked() {
		return
	}
	c.Save()
	dir, err := c.rollDir()
	if err != nil {
		c.Host.Message("Output folder", err.Error())
		return
	}
	c.pauseLiveView()
	c.Busy = true
	s := c.sess
	opts := c.processOptions(dir)
	opts.BaseArea = s.Reading.Area
	if !s.Reading.Region.Rect.Empty() {
		reg := s.Reading.Region
		opts.BaseRegion = &reg
	}
	evs := appstate.EVs(c.Cfg)
	opts.MeteredBase = s.Reading.RGB
	e0 := *c.lock
	roll, last := c.Cfg.Roll, c.Cfg.Counter
	c.Res = fmt.Sprintf("Capturing %s ...", appstate.EVText(evs))
	ctx, cancel := context.WithCancel(context.Background())
	c.scanCancel = cancel
	opts.Ctx = ctx
	c.worker.do(func() {
		var num int
		shot, err := s.CaptureBracketEVCtx(ctx, dir, evs, func(t time.Time) string {
			b, n := hdrscan.NextShot(dir, roll, t, last)
			num = n
			return b
		})
		var raws []string
		if shot != nil {
			raws = shot.Raws
			opts.Merge.Exposure = shot.Exposures
			for i, r := range raws {
				if iso, sh, xerr := rawconv.ReadExposure(r); xerr == nil {
					c.Log.Printf("RAF EXIF %s: ISO %.0f, %.5f s", filepath.Base(r), iso, sh)
				}
				if i == 0 {
					if m := hdrscan.EXIFCheck(r, e0); m != "" {
						c.Log.Printf("Frame 0 %s", m)
					}
				}
			}
		}
		c.Host.Post(func() {
			if num > 0 {
				c.Cfg.Counter = num
				c.Save()
			}
			c.Busy = false
			c.resumeLiveView()
			if err != nil && errors.Is(err, context.Canceled) {
				c.scanStopped(raws, "capture")
				return
			}
			if err != nil {
				c.endScan()
				c.Res = "Capture failed: " + err.Error()
				c.say(c.Res)
				if errors.Is(err, camera.ErrNotManual) {
					c.notManualMsg(err)
				}
				return
			}
			var names []string
			for _, r := range raws {
				names = append(names, filepath.Base(r))
			}
			c.say("Downloaded " + strings.Join(names, ", ") + " to " + dir)
			c.process(raws, opts, false)
		})
	})
}

// process merges in the background (the camera stays usable). merge is
// the Merge files page (te == u.mRes on Windows): its text box gets the
// progress and the result, and the result is the last merge.
func (c *Ctl) process(srcs []string, o hdrscan.ProcessOptions, merge bool) {
	c.Working, c.Progress = true, true
	set := func(s string) {
		if merge {
			c.MRes = s
			c.MResSeq++
		} else {
			c.Res = s
		}
	}
	o.Progress = func(m string) { c.Host.Post(func() { set(m) }) }
	go func() {
		res, err := hdrscan.ProcessN(srcs, o)
		c.Host.Post(func() {
			c.Working, c.Progress = false, false
			if merge {
				c.mergeCancel = nil
			}
			if err != nil && o.Ctx != nil && errors.Is(err, context.Canceled) {
				if merge {
					// Merge files never deletes its sources (Stop is macOS only).
					set("Merge stopped. Partial output removed; the source files are not changed.")
					c.say("Merge files: " + c.MRes)
					return
				}
				c.scanStopped(srcs, "merge")
				return
			}
			if o.Ctx != nil && !merge {
				c.endScan()
			}
			if err != nil {
				set("Merge failed: " + err.Error())
				c.say("Merge failed: " + err.Error())
				return
			}
			msg := resultText(res, o.Metadata)
			if merge {
				c.LastMerge = res
			} else {
				c.LastCap = res
			}
			set(msg)
			c.say(strings.ReplaceAll(msg, "\n", " | "))
		})
	}()
}

// resultText is the result message of process() on Windows (the film-stock
// compare of a capture is not on macOS yet).
func resultText(res *hdrscan.Result, md scanmeta.Metadata) string {
	msg := fmt.Sprintf("Saved %s  (%d frames, %.1f s)", res.HDR, res.Frames, res.Elapsed.Seconds())
	msg += "\n" + levelMetadataSummary(md)
	for _, st := range res.StatsN {
		if st.Dropped {
			msg += fmt.Sprintf("\nWARNING: the %s frame could not be used - this is a %d-frame HDR.", st.Label, res.Frames)
		} else if st.Warning != "" {
			msg += fmt.Sprintf("\nNote: %s ratio taken from the exposure times (the frame is too clipped to measure).", st.Label)
		}
	}
	for _, st := range res.StatsN {
		msg += "\n" + st.String()
	}
	if res.OrderNote != "" {
		msg += "\nNOTE: " + res.OrderNote + "."
	}
	msg += "\nExposure times: " + res.ExposureSrc
	msg += "\n" + res.BaseCheck()
	if res.LightNote != "" {
		msg += "\nWARNING: " + res.LightNote
	}
	if ds := res.DustStats; ds.Level != hdrscan.DustOff {
		msg += fmt.Sprintf("\nDust removal (%s) applied in %s: %d spots (%d hairs), %d fibres, %d px changed, %d left alone, %.1f s.",
			hdrscan.DustName(ds.Level), filepath.Base(res.HDR), ds.Spots, ds.Hairs, ds.Fibres, ds.Pixels, ds.Rejected, ds.Elapsed.Seconds())
	}
	if len(res.TIFFs) > 0 {
		msg += "\nFrame TIFFs kept: " + strings.Join(res.TIFFs, ", ")
	}
	return msg
}

// Stop cancels the scan in progress.
func (c *Ctl) Stop() {
	if c.scanCancel == nil {
		return
	}
	c.say("Stop pressed - the shot in progress finishes downloading, then this frame's partial RAFs/TIFFs are deleted (earlier frames are kept).")
	c.scanCancel()
}

func (c *Ctl) endScan() {
	if c.scanCancel != nil {
		c.scanCancel()
	}
	c.scanCancel = nil
}

func (c *Ctl) scanStopped(raws []string, phase string) {
	c.endScan()
	n := 0
	for _, r := range raws {
		if os.Remove(r) == nil {
			n++
		}
	}
	c.Res = fmt.Sprintf("Scan stopped during the %s: %d RAF(s) of this frame deleted, partial TIFFs removed; earlier frames kept.", phase, n)
	c.say(c.Res)
}

// StockItems are the film-stock dropdown entries ("None" first).
func (c *Ctl) StockItems() []string {
	items := []string{"None"}
	if c.DB != nil {
		items = append(items, c.DB.Names()...)
	}
	return items
}

// StockIndex is the selected stock's index in StockItems.
func (c *Ctl) StockIndex() int {
	for i, n := range c.StockItems() {
		if i > 0 && strings.EqualFold(n, c.Cfg.Stock) {
			return i
		}
	}
	return 0
}

// SetStock selects stock i of StockItems.
func (c *Ctl) SetStock(i int) {
	items := c.StockItems()
	c.Cfg.Stock = ""
	if i > 0 && i < len(items) {
		c.Cfg.Stock = items[i]
	}
	c.Save()
}
