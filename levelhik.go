package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
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
	if u.camCB != nil {
		u.camCB.SetEnabled(!u.usesMonoCam())
	}
	if u.camNoteLbl != nil {
		if u.usesMonoCam() {
			u.camNoteLbl.SetText("Monochrome camera: camera profile is disabled (no Bayer CFA, no CameraCal matrix). Each raw film-base channel is leveled to the 0.5 target.")
		} else {
			u.camNoteLbl.SetText(camProfileMonoNote)
		}
	}
	if u.isoCB != nil {
		u.isoCB.SetEnabled(!u.usesMonoCam())
	}
	if u.shutCB != nil {
		u.shutCB.SetEnabled(!u.usesMonoCam())
	}
	if u.hdr != nil {
		u.hdr.updateLiveRouting()
	}
}

// hikLevelCamera captures leveling frames with a Hikrobot monochrome
// body (or its simulator) at a fixed exposure. No Bayer matrix applies.
type hikLevelCamera struct {
	dev    hikcam.Device
	lights func() (on [3]bool, v [3]int) // used only when the device is simulated
	last   [3]hdrscan.MonoPlane
}

func (h *hikLevelCamera) Name() string {
	info := h.dev.Info()
	st := h.dev.Settings()
	kind := "monochrome"
	if info.Simulated {
		kind = "simulated monochrome"
	}
	return fmt.Sprintf("%s (%s), %.0f us, %.2f dB, black %.0f, %s (fixed exposure)",
		info.Label(), kind, st.ExposureUS, st.GainDB, st.BlackLevel, st.PixelFormat)
}

func (h *hikLevelCamera) Mono() bool { return true }

func (h *hikLevelCamera) Prepare() (string, error) {
	st := h.dev.Settings()
	if st.ExposureUS <= 0 {
		st = hikcam.BestScanQuality()
	} else {
		// Keep the current exposure (leveling is fixed-exposure) but
		// refresh the quality nodes so a scan-quality body stays linear.
		q := hikcam.BestScanQuality()
		q.ExposureUS = st.ExposureUS
		q.GainDB = st.GainDB
		st = q
	}
	if err := h.dev.Apply(st); err != nil {
		return "", err
	}
	if err := h.dev.StartGrab(); err != nil {
		return "", err
	}
	got := h.dev.Settings()
	return fmt.Sprintf("%.0f us, %.2f dB, black %.0f, %s", got.ExposureUS, got.GainDB, got.BlackLevel, got.PixelFormat), nil
}

func (h *hikLevelCamera) Shoot(ctx context.Context, ch int, prefix string) (*lightlevel.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fr, err := h.dev.Grab(5 * time.Second)
	if err != nil {
		return nil, err
	}
	pix := append([]uint16(nil), fr.Pix...)
	if h.dev.Info().Simulated && h.lights != nil {
		on, v := h.lights()
		pix = hdrscan.ScaleMonoForSimLight(pix, ch, on, v)
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
		if err := d.StartGrab(); err != nil {
			u.log("Hikrobot preview grab: %v", err)
			return
		}
		for u.hikPrevGen.Load() == gen {
			fr, err := d.Grab(1500 * time.Millisecond)
			if err != nil {
				if u.hikPrevGen.Load() != gen {
					return
				}
				time.Sleep(200 * time.Millisecond)
				continue
			}
			img := previewGray(fr, 1600)
			what := fmt.Sprintf("Hikrobot live  %dx%d  %s  %.0f us", fr.W, fr.H, fr.PixelFormat, fr.ExposureUS)
			u.ui(func() {
				if u.hikPrevGen.Load() != gen || u.hdr == nil {
					return
				}
				u.hdr.setImage(img, what)
			})
			time.Sleep(80 * time.Millisecond)
		}
	}(gen)
}

func (u *LightUI) stopHikPreview() {
	u.hikPrevGen.Add(1)
}

func previewGray(fr *hikcam.Frame, maxEdge int) *image.Gray {
	w, h := fr.W, fr.H
	if w <= 0 || h <= 0 {
		return image.NewGray(image.Rect(0, 0, 1, 1))
	}
	scale := 1
	if max(w, h) > maxEdge && maxEdge > 0 {
		scale = (max(w, h) + maxEdge - 1) / maxEdge
	}
	nw, nh := max(1, w/scale), max(1, h/scale)
	img := image.NewGray(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		for x := 0; x < nw; x++ {
			img.Pix[y*nw+x] = byte(fr.Pix[(y*scale)*w+(x*scale)] >> 8)
		}
	}
	return img
}

func (u *HDRUI) updateLiveRouting() {
	if u != nil && u.liveRoute != nil {
		u.liveRoute()
	}
}
