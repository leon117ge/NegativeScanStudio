package hdrscan

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"basetool/lightlevel"
)

// LevelingImageMono downsamples a 16-bit monochrome plane onto the same
// lattice LevelingImage uses for Bayer frames, so the leveling solver and
// the TIFF check share one statistic.
func LevelingImageMono(pix []uint16, w, h int) *lightlevel.Image {
	if w <= 0 || h <= 0 || len(pix) < w*h {
		return &lightlevel.Image{}
	}
	step := max(1, min(16, min(w, h)/8))
	ww, hh := w/step, h/step
	im := &lightlevel.Image{W: ww, H: hh, V: make([]float32, ww*hh), Clip: make([]bool, ww*hh)}
	for y := 0; y < hh; y++ {
		for x := 0; x < ww; x++ {
			v := pix[(y*step+step/2)*w+(x*step+step/2)]
			im.V[y*ww+x] = float32(v) / 65535
			im.Clip[y*ww+x] = v >= 65207
		}
	}
	return im
}

// ScaleMonoForSimLight scales a simulated monochrome frame by the current
// RGB light code so a software camera can drive the v0.92 leveling solver.
// Real cameras must not call this: the physical light already changes the
// sensor values. The LED curve and per-channel gains match lightlevel.Synth
// (an orange-mask film base: blue needs the most light).
func ScaleMonoForSimLight(pix []uint16, ch int, on [3]bool, v [3]int) []uint16 {
	if ch < 0 || ch > 2 {
		ch = 0
	}
	gain := [3]float64{1.35, 0.85, 0.55}[ch]
	scale := 0.0
	if on[ch] {
		x := float64(v[ch]) / 100
		if x < 0 {
			x = 0
		}
		if x > 1 {
			x = 1
		}
		scale = gain * (0.85*x + 0.15*x*x)
	}
	out := make([]uint16, len(pix))
	for i, p := range pix {
		n := float64(p) * scale
		if n < 0 {
			n = 0
		}
		if n > 65535 {
			n = 65535
		}
		out[i] = uint16(n + 0.5)
	}
	return out
}

// GrayToRGB replicates a monochrome plane as R=G=B so ProcessN / Load can
// reuse the existing HDR merge path.
func GrayToRGB(p MonoPlane) []uint16 {
	out := make([]uint16, 3*p.W*p.H)
	for i := 0; i < p.W*p.H && i < len(p.Pix); i++ {
		v := p.Pix[i]
		out[3*i+0], out[3*i+1], out[3*i+2] = v, v, v
	}
	return out
}

// MergeMonoBracket merges a per-channel exposure bracket the same way
// white-light and trichrome HDR do: write each mono frame as a 16-bit RGB
// TIFF (gray replicated) and call ProcessN with the bracket exposure
// ratios. The merged HDR is read back as one linear MonoPlane.
func MergeMonoBracket(frames []MonoPlane, evs []int, o ProcessOptions) (MonoPlane, error) {
	if len(frames) == 0 {
		return MonoPlane{}, fmt.Errorf("no bracket frames")
	}
	if len(frames) == 1 {
		return frames[0], nil
	}
	for i, fr := range frames {
		if err := checkMono(fr, fmt.Sprintf("bracket %d", i)); err != nil {
			return MonoPlane{}, err
		}
		if fr.W != frames[0].W || fr.H != frames[0].H {
			return MonoPlane{}, fmt.Errorf("bracket sizes differ: 0 is %dx%d, %d is %dx%d", frames[0].W, frames[0].H, i, fr.W, fr.H)
		}
	}
	work, err := os.MkdirTemp("", "nss-mono-hdr-*")
	if err != nil {
		return MonoPlane{}, err
	}
	defer os.RemoveAll(work)
	keepDir := ""
	if o.KeepTIFF && o.OutDir != "" {
		keepDir = o.OutDir
	}
	paths := make([]string, len(frames))
	exps := make([]float64, len(frames))
	meta := o.Metadata
	meta.Mode = "mono-bracket-component"
	meta.Encoding = "linear"
	for i, fr := range frames {
		ev := 0
		if i < len(evs) {
			ev = evs[i]
		}
		name := fmt.Sprintf("mono_br_%d_%s.tiff", i, StepTag(ev))
		path := filepath.Join(work, name)
		if keepDir != "" {
			path = uniqueFile(filepath.Join(keepDir, name))
		}
		sw := o.Software
		if sw == "" {
			sw = "NegativeScan Studio"
		}
		if err := writeScanTIFF(path, fr.W, fr.H, GrayToRGB(fr), false, "Hikrobot", "mono", sw+" mono HDR bracket frame", meta); err != nil {
			return MonoPlane{}, err
		}
		paths[i] = path
		exps[i] = math.Pow(2, float64(ev))
	}
	co := o
	co.OutDir = work
	co.KeepTIFF = false
	co.DeleteRAW = false
	co.Merge.Exposure = exps
	co.Metadata = meta
	if o.Progress != nil {
		o.Progress(fmt.Sprintf("Merging mono HDR bracket (%d frames) ...", len(frames)))
	}
	res, err := ProcessN(paths, co)
	if err != nil {
		return MonoPlane{}, err
	}
	p, err := Load(res.HDR)
	if err != nil {
		return MonoPlane{}, err
	}
	defer p.Free()
	n := p.W * p.H
	gray := make([]uint16, n)
	for i := 0; i < n; i++ {
		gray[i] = p.Pix[3*i+1]
	}
	return MonoPlane{W: p.W, H: p.H, Pix: gray}, nil
}

// measurePackedBase reads the film-base median of a packed RGB plane using
// the same LevelingImage + Measure path as trichrome TIFF verification.
func measurePackedBase(pix []uint16, w, h int, box Rect) (base [3]float64, n int, errStr string) {
	if w <= 0 || h <= 0 || len(pix) < 3*w*h {
		return base, 0, "no image"
	}
	area := lightlevel.Centre
	if !box.Empty() {
		area = lightlevel.Rect{X0: box.X0, Y0: box.Y0, X1: box.X1, Y1: box.Y1}
	}
	combined := &Plane{W: w, H: h, Pix: pix}
	for c := 0; c < 3; c++ {
		rd, err := lightlevel.Measure(LevelingImage(combined, c), area)
		if err != nil {
			return base, 0, err.Error()
		}
		if rd.BaseClipped() {
			return base, rd.Cells, fmt.Sprintf("%c film base is clipped in the calibration TIFF; reduce exposure", "RGB"[c])
		}
		base[c], n = rd.Level, rd.Cells
	}
	return base, n, ""
}

// EVScale is 2^ev, used to set the mono camera's exposure for a bracket step.
func EVScale(ev int) float64 {
	return math.Pow(2, float64(ev))
}
