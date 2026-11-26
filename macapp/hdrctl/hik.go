package hdrctl

// hikCams owns the Hikrobot monochrome camera for the Light page (leveling
// camera and its preview), as MonoUI does on Windows (monopage.go,
// levelhik.go ensureHik). The Mono trichrome page takes it over when it is
// ported. Every camera call runs on a goroutine, never on the main thread.

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
	levelSim *hikcam.Sim   // the simulator used for leveling
	sdkMsg   string

	prevGen  atomic.Int64
	prevDone chan struct{} // closed when the running preview loop has stopped
}

func newHikCams() *hikCams { return &hikCams{} }

func (h *hikCams) getCam() hikcam.Device {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cam
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
	list := hikcam.Enumerate()
	h.mu.Lock()
	h.sdkMsg = list.Message()
	h.mu.Unlock()
	if list.Err != nil {
		return nil, list.Err
	}
	if len(list.Cameras) == 0 {
		return nil, hikcam.ErrNoCamera
	}
	d, err := hikcam.OpenReal(list.Cameras[0])
	if err != nil {
		return nil, err
	}
	_ = d.Apply(hikcam.BestScanQuality())
	h.setCamIfNil(d)
	return d, nil
}

// stopPreview ends the preview loop and waits until it has released the
// camera (the leveling then owns it). Not on the UI goroutine.
func (h *hikCams) stopPreviewSync() {
	h.prevGen.Add(1)
	h.mu.Lock()
	done := h.prevDone
	h.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}
}

// startPreview runs the Light page's Hikrobot / simulator preview (Windows
// LightUI.startHikPreview): live settings (2x binning), grab, PreviewGray,
// posted to the shared live view. It stops when gen changes.
func (h *hikCams) startPreview(wantSim bool, logf func(string, ...any), show func(img image.Image, what string, done func()), alive func() bool) {
	gen := h.prevGen.Add(1)
	go func() {
		h.stopWait(gen)
		if h.prevGen.Load() != gen || !alive() {
			return
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

// stopWait waits for an older preview loop to finish before a new one starts.
func (h *hikCams) stopWait(gen int64) {
	h.mu.Lock()
	done := h.prevDone
	h.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}
}
