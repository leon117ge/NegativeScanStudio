package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"basetool/hdrscan"
	"basetool/hikcam"
	"basetool/lightlevel"
	"basetool/scanmeta"
)

const (
	camSrcExisting = 0
	camSrcHik      = 1
	camSrcHikSim   = 2
)

var camSrcItems = []string{
	"Existing camera (Fujifilm)",
	"Hikrobot monochrome",
	"Simulated Hikrobot MV-CS240-90UM",
}

const camProfileMonoNote = "Apply camera profile: for a Bayer camera this aims at the film base AFTER the CameraCal matrix, so the saved reference can stay at 0.5/0.5/0.5. A monochrome camera has no CFA and no CameraCal matrix, so the option is disabled and each raw film-base channel is leveled independently to the 0.5 target."

func (u *LightUI) camSource() int {
	if u.camSrcCB == nil {
		return u.cfg.CamSrc
	}
	return clampIdx(u.camSrcCB.CurrentIndex(), len(camSrcItems))
}

func (u *LightUI) usesMonoCam() bool {
	s := u.camSource()
	return s == camSrcHik || s == camSrcHikSim
}

func (u *LightUI) onCamSourceChanged() {
	if u.camSrcCB == nil || u.inPreset {
		return
	}
	u.cfg.CamSrc = u.camSource()
	u.saveCfg()
	hik := u.usesMonoCam()
	if u.camCB != nil {
		if hik {
			u.camCB.SetChecked(false)
			u.camCB.SetEnabled(false)
		} else {
			u.camCB.SetChecked(!u.cfg.NoCam)
			u.camCB.SetEnabled(true)
		}
	}
	if u.camNoteLbl != nil {
		if hik {
			u.camNoteLbl.SetText("Monochrome camera: camera profile is disabled (no Bayer CFA, no CameraCal matrix). Each raw film-base channel is leveled to the 0.5 target.")
		} else {
			u.camNoteLbl.SetText(camProfileMonoNote)
		}
	}
	if u.isoLbl != nil {
		u.isoLbl.SetVisible(!hik)
	}
	if u.isoRow != nil {
		u.isoRow.SetVisible(!hik)
	}
	if u.hikExpLbl != nil {
		u.hikExpLbl.SetVisible(hik)
	}
	if u.hikExpRow != nil {
		u.hikExpRow.SetVisible(hik)
	}
	if hik && u.hikExpNE != nil {
		u.hikExpNE.SetValue(u.monoScanExposureUS())
	}
	if u.isoCB != nil {
		u.isoCB.SetEnabled(!hik)
	}
	if u.shutCB != nil {
		u.shutCB.SetEnabled(!hik)
	}
	if u.hdr != nil {
		u.hdr.updateLiveRouting()
	}
}

func (u *LightUI) monoScanExposureUS() float64 {
	if u.hdr != nil && u.hdr.mono != nil {
		st := u.hdr.mono.readSettings()
		if st.ExposureUS >= hikcam.CS240ExpMinUS {
			return st.ExposureUS
		}
	}
	return hikcam.BestScanQuality().ExposureUS
}

func (u *LightUI) hikExposureUS() float64 {
	if u.hikExpNE != nil {
		v := u.hikExpNE.Value()
		if v >= hikcam.CS240ExpMinUS {
			return v
		}
	}
	return u.monoScanExposureUS()
}

// hikLevelCamera captures leveling frames with a Hikrobot monochrome
// body (or its simulator) at a fixed exposure. No Bayer matrix applies.
// Leveling always uses BestScanQuality (Mono12, gain 0 dB, software
// trigger, full frame) and a single common ExposureUS for R, G and B.
type hikLevelCamera struct {
	dev                    hikcam.Device
	lights                 func() (on [3]bool, v [3]int) // used only when the device is simulated
	last                   [3]hdrscan.MonoPlane
	wantUS                 float64
	appliedUS, appliedGain float64
	simGain                *[3]float64 // tests only; nil uses the default orange-mask gains
	simSize                int         // tests only; 0 = full frame
}

func (h *hikLevelCamera) Name() string {
	info := h.dev.Info()
	us, gain := h.GetExposure()
	st := h.dev.Settings()
	kind := "monochrome"
	if info.Simulated {
		kind = "simulated monochrome"
	}
	if us <= 0 {
		us, gain = st.ExposureUS, st.GainDB
	}
	return fmt.Sprintf("%s (%s), %.0f us, %.2f dB, black %.0f, %s (fixed exposure)",
		info.Label(), kind, us, gain, st.BlackLevel, st.PixelFormat)
}

func (h *hikLevelCamera) Mono() bool { return true }

func (h *hikLevelCamera) GetExposure() (float64, float64) {
	if h.appliedUS > 0 {
		return h.appliedUS, h.appliedGain
	}
	if h.wantUS > 0 {
		return h.wantUS, 0
	}
	st := h.dev.Settings()
	return st.ExposureUS, st.GainDB
}

func (h *hikLevelCamera) scanSettings(us float64) hikcam.Settings {
	st := hikcam.BestScanQuality()
	if us > 0 {
		st.ExposureUS = us
	} else if h.wantUS > 0 {
		st.ExposureUS = h.wantUS
	}
	st.GainDB = 0
	if h.simSize > 0 {
		st.FullFrame = false
		st.Width, st.Height = h.simSize, h.simSize
		st.OffsetX, st.OffsetY = 0, 0
	}
	return st.Clamp()
}

func (h *hikLevelCamera) applyScan(us float64) error {
	if sim, ok := h.dev.(*hikcam.Sim); ok {
		sim.FlatField = true
	}
	st := h.scanSettings(us)
	if err := h.dev.Apply(st); err != nil {
		return err
	}
	if err := h.dev.StartGrab(); err != nil {
		return err
	}
	got := h.dev.Settings()
	h.appliedUS, h.appliedGain = got.ExposureUS, got.GainDB
	h.wantUS = h.appliedUS
	return nil
}

func (h *hikLevelCamera) SetExposure(us, _ float64) error {
	return h.applyScan(us)
}

func (h *hikLevelCamera) Prepare() (string, error) {
	// Always start from the intended scan exposure (the UI / Mono Trichrome
	// field), never leftover live-view Mono8 settings.
	if err := h.applyScan(h.wantUS); err != nil {
		return "", err
	}
	got := h.dev.Settings()
	return fmt.Sprintf("%.0f us, %.2f dB, black %.0f, %s", got.ExposureUS, got.GainDB, got.BlackLevel, got.PixelFormat), nil
}

func (h *hikLevelCamera) Shoot(ctx context.Context, ch int, prefix string) (*lightlevel.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	timeout := hikcam.GrabTimeout(h.appliedUS)
	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}
	fr, err := h.dev.Grab(timeout)
	if err != nil {
		return nil, err
	}
	pix := append([]uint16(nil), fr.Pix...)
	if h.dev.Info().Simulated && h.lights != nil {
		on, v := h.lights()
		if h.simGain != nil {
			pix = hdrscan.ScaleMonoForSimLightGain(pix, ch, on, v, *h.simGain)
		} else {
			pix = hdrscan.ScaleMonoForSimLight(pix, ch, on, v)
		}
	}
	plane := hdrscan.MonoPlane{W: fr.W, H: fr.H, Pix: pix}
	h.last[ch] = plane
	im := hdrscan.LevelingImageMono(pix, fr.W, fr.H)
	im.Path = prefix
	return im, nil
}

func hikTiffBase(ctx context.Context, hk *hikLevelCamera, dir string, box hdrscan.Rect) ([3]float64, int, error) {
	if err := ctx.Err(); err != nil {
		return [3]float64{}, 0, err
	}
	for c, p := range hk.last {
		if p.Pix == nil {
			return [3]float64{}, 0, fmt.Errorf("no last %c mono frame", "RGB"[c])
		}
	}
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp("", "nss-hik-tiff-*")
		if err != nil {
			return [3]float64{}, 0, err
		}
		defer os.RemoveAll(dir)
	}
	path := filepath.Join(dir, "leveling_mono_check.tiff")
	r, err := hdrscan.ProcessMonoTrichrome(hk.last[0], hk.last[1], hk.last[2], hdrscan.MonoTriOptions{
		OutPath:             path,
		CorrectCA:           false,
		Software:            appVersion + " leveling check",
		MeasureLevelingBase: true,
		BaseNorm:            box,
		Metadata:            scanmeta.Metadata{Mode: "mono-trichrome", Encoding: "linear"},
	})
	if path != "" {
		_ = os.Remove(path)
	}
	if err != nil {
		return [3]float64{}, 0, err
	}
	if r.BaseErr != "" {
		return r.Base, r.BaseN, errors.New(r.BaseErr)
	}
	return r.Base, r.BaseN, nil
}

func (u *MonoUI) ensureHik(wantSim bool) (hikcam.Device, error) {
	if wantSim {
		if u.cam != nil && u.cam.Info().Simulated {
			return u.cam, nil
		}
		if u.levelSim == nil {
			u.levelSim = hikcam.OpenSim()
			_ = u.levelSim.Apply(hikcam.BestScanQuality())
		}
		return u.levelSim, nil
	}
	if u.cam != nil && !u.cam.Info().Simulated {
		return u.cam, nil
	}
	list := hikcam.Enumerate()
	u.sdkMsg = list.Message()
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
	u.devices = list.Cameras
	if u.cam == nil {
		u.cam = d
	}
	return d, nil
}

func (u *LightUI) hikLights() func() ([3]bool, [3]int) {
	return func() ([3]bool, [3]int) {
		if u.sim != nil {
			return u.sim.Lights()
		}
		if u.cl != nil {
			if cs := u.cl.CachedState(); cs != nil {
				return cs.On, cs.Value
			}
		}
		return [3]bool{true, true, true}, [3]int{50, 50, 50}
	}
}

func (u *LightUI) startHikPreview() {
	if u.hdr == nil || u.hdr.mono == nil {
		return
	}
	gen := u.hikPrevGen.Add(1)
	wantSim := u.camSource() == camSrcHikSim
	go func(gen int64) {
		d, err := u.hdr.mono.ensureHik(wantSim)
		if err != nil {
			u.log("Hikrobot preview: %v", err)
			return
		}
		if sim, ok := d.(*hikcam.Sim); ok {
			sim.FlatField = false
		}
		if err := d.Apply(hikcam.LiveViewSettings(d.Settings(), 2)); err != nil {
			u.log("Hikrobot live settings: %v", err)
		} else {
			u.log("Live view: %s", d.Settings().LiveSummary())
		}
		if err := d.StartGrab(); err != nil {
			u.log("Hikrobot preview grab: %v", err)
			return
		}
		var posting atomic.Bool
		misses := 0
		for u.hikPrevGen.Load() == gen {
			fr, err := d.Grab(hikcam.GrabTimeout(d.Settings().ExposureUS))
			if err != nil {
				if u.hikPrevGen.Load() != gen {
					return
				}
				misses++
				if msg, log := liveGrabMessage(err, misses); log {
					u.log("%s", msg)
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}
			misses = 0
			img := hikcam.PreviewGray(fr.Pix, fr.W, fr.H, hikcam.LivePreviewMaxEdge)
			what := fmt.Sprintf("Hikrobot live  %dx%d  %s  %.0f us", fr.W, fr.H, fr.PixelFormat, fr.ExposureUS)
			if posting.CompareAndSwap(false, true) {
				u.ui(func() {
					defer posting.Store(false)
					if u.hikPrevGen.Load() != gen || u.hdr == nil {
						return
					}
					u.hdr.setImage(img, what)
				})
			}
			if d.Info().Simulated {
				time.Sleep(40 * time.Millisecond)
			}
		}
	}(gen)
}

func (u *LightUI) stopHikPreview() {
	u.hikPrevGen.Add(1)
}

func previewGray(fr *hikcam.Frame, maxEdge int) *image.Gray {
	return hikcam.PreviewGray(fr.Pix, fr.W, fr.H, maxEdge)
}

func (u *HDRUI) updateLiveRouting() {
	if u != nil && u.liveRoute != nil {
		u.liveRoute()
	}
}
