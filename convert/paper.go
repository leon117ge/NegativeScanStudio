package convert

import (
	"fmt"
	"math"
	"os"
	"strings"
)

// Still-paper viewing transform.
//
// Input is Cineon Film Log as produced by the OFX plugin Positive Out
// (ofx/src/dctl_math.hpp out_mode 2). Output is Rec.709 display with
// a 1/2.4 encoding, the same contract as the embedded Kodak 2383 cubes.
//
// This is a parametric approximation of viewing a still-camera colour
// negative printed onto RA-4 colour paper (Kodak Endura / Portra Endura
// class). Public paper datasheets (e.g. Kodak E-4021 ENDURA Premier)
// show reflection characteristic curves with a mid-slope above 1 and a
// soft shoulder, and a dye set intended for flesh tones and foliage
// rather than the projector-balanced 2383 print-film look. The numbers
// below are our model, not digitised paper-chart readings and not a
// third-party profile.
//
// Greys stay grey: no white-point rotation is applied, so a neutral
// Cineon input produces a neutral display value.

const (
	paperContrast = 1.50
	paperChroma   = 1.12
	paperPivot    = MidGrey
)

// CineonDecode inverts the plugin's Cineon encode (out_mode 2).
func CineonDecode(code float64) float64 {
	x := math.Pow(10, (code*1023-685)/300)
	lin := (x - 0.01079) / 0.98921
	if lin < 0 {
		return 0
	}
	return lin
}

// CineonEncode matches the plugin Positive Out Cineon curve.
func CineonEncode(lin float64) float64 {
	if lin < 0 {
		lin = 0
	}
	v := (685 + 300*math.Log10(lin*0.98921+0.01079)) / 1023
	if v < 0 {
		return 0
	}
	return v
}

func rec709Gamma24(c float64) float64 {
	if c <= 0 {
		return 0
	}
	if c >= 1 {
		return 1
	}
	return math.Pow(c, 1.0/2.4)
}

func paperTone(lin float64) float64 {
	if lin <= 0 {
		return 0
	}
	y := paperPivot * math.Pow(lin/paperPivot, paperContrast)
	// Soft shoulder above ~1.15 (relative to pivot-scaled value).
	const hi = 1.15 * paperPivot
	if y > hi {
		t := (y - hi) / (0.85 * paperPivot)
		y = hi + 0.85*paperPivot*math.Tanh(t)
	}
	return y
}

// ApplyStillPaper maps one Cineon-encoded RGB triple to display Rec.709
// (gamma 2.4) using the still-paper model.
func ApplyStillPaper(r, g, b float64) (float64, float64, float64) {
	lr, lg, lb := CineonDecode(r), CineonDecode(g), CineonDecode(b)
	tr, tg, tb := paperTone(lr), paperTone(lg), paperTone(lb)
	mean := (tr + tg + tb) / 3
	tr = mean + paperChroma*(tr-mean)
	tg = mean + paperChroma*(tg-mean)
	tb = mean + paperChroma*(tb-mean)
	if tr < 0 {
		tr = 0
	}
	if tg < 0 {
		tg = 0
	}
	if tb < 0 {
		tb = 0
	}
	return rec709Gamma24(tr), rec709Gamma24(tg), rec709Gamma24(tb)
}

// WriteStillPaperCube writes NSS_StillPaper_v1 as a 3D .cube (size 33 or 65).
func WriteStillPaperCube(path string, size int) error {
	if size != 33 && size != 65 {
		size = 33
	}
	var b strings.Builder
	b.Grow(size * size * size * 32)
	b.WriteString("# NSS_StillPaper_v1\n")
	b.WriteString("# Still-camera colour-negative paper viewing LUT.\n")
	b.WriteString("# Input:  Cineon Film Log (OFX Positive Out, out_mode 2).\n")
	b.WriteString("# Output: Rec.709 display, gamma 2.4.\n")
	b.WriteString("# Parametric RA-4 paper approximation (Endura / Portra Endura class):\n")
	b.WriteString("# system contrast 1.50 about mid-grey, chroma 1.12, soft shoulder.\n")
	b.WriteString("# Neutrals stay neutral. Not copied from any third-party profile.\n")
	b.WriteString("# Public Kodak paper datasheets inform the qualitative shape only.\n")
	b.WriteString("TITLE \"NSS_StillPaper_v1\"\n")
	b.WriteString(fmt.Sprintf("LUT_3D_SIZE %d\n", size))
	b.WriteString("LUT_3D_INPUT_RANGE 0.0 1.0\n\n")
	den := float64(size - 1)
	for ib := 0; ib < size; ib++ {
		bb := float64(ib) / den
		for ig := 0; ig < size; ig++ {
			gg := float64(ig) / den
			for ir := 0; ir < size; ir++ {
				rr := float64(ir) / den
				or, og, ob := ApplyStillPaper(rr, gg, bb)
				fmt.Fprintf(&b, "%.6f %.6f %.6f\n", or, og, ob)
			}
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}
