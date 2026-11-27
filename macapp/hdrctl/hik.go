package hdrctl

// hikCams owns the Hikrobot monochrome camera shared by the Mono trichrome
// page and the Light page (leveling camera and its preview), as MonoUI does
// on Windows (monopage.go camMu/cam/devices/sdkMsg/levelSim, levelhik.go
// ensureHik). Every camera call runs on a goroutine, never on the main
// thread.

import (
	"fmt"
	"image"
	"sync"
	"sync/atomic"
	"time"

	"basetool/hikcam"
)

type hikCams struct {
	mu       sync.Mutex
	cam      hikcam.Device // the real (or Mono-page) camera
	devices  []hikcam.Info // real cameras from the last refresh
	levelSim *hikcam.Sim   // the simulator used for leveling
	sdkMsg   string

	// The MVS entry points (tests replace them to simulate a machine
	// without MvCameraControl.framework or with a camera attached).
	enumerate func() hikcam.ListResult
	openReal  func(hikcam.Info) (hikcam.Device, error)
	sdkStatus func() string

	// stopOther stops the Mono trichrome preview loop before the Light
	// page's preview or leveling uses the camera (Windows runs both on
	// MonoUI's worker). Not on the UI goroutine.
	stopOther func()

	prevGen  atomic.Int64
	prevDone chan struct{} // closed when the running preview loop has stopped
}

func newHikCams() *hikCams {
	return &hikCams{enumerate: hikcam.Enumerate, openReal: hikcam.OpenReal, sdkStatus: hikcam.SDKStatus}
}

func (h *hikCams) getCam() hikcam.Device {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cam
}

func (h *hikCams) setCam(d hikcam.Device) {
	h.mu.Lock()
	h.cam = d
	h.mu.Unlock()
}

func (h *hikCams) getSDKMsg() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sdkMsg
}

func (h *hikCams) setSDK(msg string, devs []hikcam.Info) {
	h.mu.Lock()
	h.sdkMsg = msg
	if devs != nil {
		h.devices = append([]hikcam.Info(nil), devs...)
	} else {
		h.devices = nil
	}
	h.mu.Unlock()
}

func (h *hikCams) getDevices() []hikcam.Info {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hikcam.Info(nil), h.devices...)
}

func (h *hikCams) setCamIfNil(d hikcam.Device) {
	h.mu.Lock()
	if h.cam == nil {
		h.cam = d
	}
	h.mu.Unlock()
}

// ensureHik is MonoUI.ensureHik: the simulator for leveling, or the first
// real camera MVS lists. Runs on a worker goroutine.
func (h *hikCams) ensureHik(wantSim bool) (hikcam.Device, error) {
	if wantSim {
		if cur := h.getCam(); cur != nil && cur.Info().Simulated {
			return cur, nil
		}
		h.mu.Lock()
		if h.levelSim == nil {
			h.levelSim = hikcam.OpenSim()
			_ = h.levelSim.Apply(hikcam.BestScanQuality())
		}
		sim := h.levelSim
		h.mu.Unlock()
		return sim, nil
	}
	if cur := h.getCam(); cur != nil && !cur.Info().Simulated {
		return cur, nil
	}
	list := h.enumerate()
	h.setSDK(list.Message(), list.Cameras)
	if list.Err != nil {
		return nil, list.Err
	}
	if len(list.Cameras) == 0 {
		return nil, hikcam.ErrNoCamera
	}
	d, err := h.openReal(list.Cameras[0])
	if err != nil {
		return nil, err
	}
	_ = d.Apply(hikcam.BestScanQuality())
	h.setCamIfNil(d)
	return d, nil
}

// hikStopTimeout is how long a caller waits for the Light page's preview
// loop to release the camera.
var hikStopTimeout = 10 * time.Second

// errPreviewStuck is returned when the preview loop did not release the
// camera in time: the caller must not use the camera alongside it.
var errPreviewStuck = fmt.Errorf("the Hikrobot live preview did not stop within %s (the camera may be hung): reconnect it on HDR Scan > Mono trichrome", hikStopTimeout)

// stopPreviewSync ends the preview loop and waits until it has released the
// camera (the leveling then owns it). It returns errPreviewStuck when the
// loop is still running after hikStopTimeout. Not on the UI goroutine.
func (h *hikCams) stopPreviewSync() error {
	h.prevGen.Add(1)
	return h.waitPreviewDone()
}

// previewRunning reports whether a preview loop is running (it holds the
// camera until it closes prevDone).
func (h *hikCams) previewRunning() bool {
	h.mu.Lock()
	done := h.prevDone
	h.mu.Unlock()
	if done == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}

// waitPreviewDone waits for the running preview loop (if any) to finish.
func (h *hikCams) waitPreviewDone() error {
	h.mu.Lock()
	done := h.prevDone
	h.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-time.After(hikStopTimeout):
		return errPreviewStuck
	}
}

// startPreview runs the Light page's Hikrobot / simulator preview (Windows
// LightUI.startHikPreview): live settings (2x binning), grab, PreviewGray,
// posted to the shared live view. It stops when gen changes.
func (h *hikCams) startPreview(wantSim bool, logf func(string, ...any), show func(img image.Image, what string, done func()), alive func() bool) {
	gen := h.prevGen.Add(1)
	go func() {
		if err := h.waitPreviewDone(); err != nil {
			// Do not start a second loop on a camera the old one still holds.
			logf("Hikrobot preview not started: %v", err)
			return
		}
		if h.prevGen.Load() != gen || !alive() {
			return
		}
		if h.stopOther != nil {
			h.stopOther()
		}
		done := make(chan struct{})
		h.mu.Lock()
		h.prevDone = done
		h.mu.Unlock()
		defer close(done)
		d, err := h.ensureHik(wantSim)
		if err != nil {
			logf("Hikrobot preview: %v", err)
			return
		}
		if h.prevGen.Load() != gen {
			return
		}
		if sim, ok := d.(*hikcam.Sim); ok {
			sim.FlatField = false
		}
		if err := d.Apply(hikcam.LiveViewSettings(d.Settings(), 2)); err != nil {
			logf("Hikrobot live settings: %v", err)
		} else {
			logf("Live view: %s", d.Settings().LiveSummary())
		}
		if err := d.StartGrab(); err != nil {
			logf("Hikrobot preview grab: %v", err)
			return
		}
		defer func() { _ = d.StopGrab() }()
		var posting atomic.Bool
		misses := 0
		for h.prevGen.Load() == gen && alive() {
			fr, err := d.Grab(hikcam.GrabTimeout(d.Settings().ExposureUS))
			if err != nil {
				if h.prevGen.Load() != gen {
					return
				}
				misses++
				if msg, log := liveGrabMessage(err, misses); log {
					logf("%s", msg)
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}
			misses = 0
			if posting.CompareAndSwap(false, true) {
				img := hikcam.PreviewGray(fr.Pix, fr.W, fr.H, hikcam.LivePreviewMaxEdge)
				what := fmt.Sprintf("Hikrobot live  %dx%d  %s  %.0f us", fr.W, fr.H, fr.PixelFormat, fr.ExposureUS)
				show(img, what, func() { posting.Store(false) })
			}
			if d.Info().Simulated {
				time.Sleep(40 * time.Millisecond)
			}
		}
	}()
}
