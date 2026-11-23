package hdrscan

import (
	"fmt"
	"math"

	"basetool/lightlevel"
)

// LevelingImageMono downsamples a 16-bit monochrome plane onto the same
// lattice LevelingImage uses for Bayer frames, so the leveling solver and
// the TIFF check share one statistic.
func LevelingImageMono(pix []uint16, w, h int) *lightlevel.Image {
	return levelingImageMono(pix, w, h, 0)
}

func levelingImageMono(pix []uint16, w, h int, black16 float64) *lightlevel.Image {
	if w <= 0 || h <= 0 || len(pix) < w*h {
		return &lightlevel.Image{}
	}
	step := max(1, min(16, min(w, h)/8))
	ww, hh := w/step, h/step
	im := &lightlevel.Image{W: ww, H: hh, V: make([]float32, ww*hh), Clip: make([]bool, ww*hh)}
	for y := 0; y < hh; y++ {
		for x := 0; x < ww; x++ {
			v := pix[(y*step+step/2)*w+(x*step+step/2)]
			im.V[y*ww+x] = float32(math.Max(0, float64(v)-black16) / 65535)
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
	return ScaleMonoForSimLightGain(pix, ch, on, v, [3]float64{1.35, 0.85, 0.55})
}

// ScaleMonoForSimLightGain is ScaleMonoForSimLight with explicit per-channel
// gains (tests use a dim-green set; the default orange-mask gains stay in
// ScaleMonoForSimLight).
func ScaleMonoForSimLightGain(pix []uint16, ch int, on [3]bool, v [3]int, gain [3]float64) []uint16 {
	return ScaleMonoForSimLightBlack(pix, 0, ch, on, v, gain)
}

// ScaleMonoForSimLightBlack scales only the signal above the BlackLevel
// pedestal (black16, 16-bit units), as a real sensor does: the pedestal
// does not change with the light.
func ScaleMonoForSimLightBlack(pix []uint16, black16 float64, ch int, on [3]bool, v [3]int, gain [3]float64) []uint16 {
	if ch < 0 || ch > 2 {
		ch = 0
	}
	g := gain[ch]
	scale := 0.0
	if on[ch] {
		x := float64(v[ch]) / 100
		if x < 0 {
			x = 0
		}
		if x > 1 {
			x = 1
		}
		scale = g * (0.85*x + 0.15*x*x)
	}
	out := make([]uint16, len(pix))
	for i, p := range pix {
		n := (float64(p)-black16)*scale + black16
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
