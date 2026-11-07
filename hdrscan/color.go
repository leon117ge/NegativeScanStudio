package hdrscan

// Optional colour processing (v0.66): Cineon log encoding, orange-mask
// compensation and dye-crosstalk correction from package colorsci. With
// every option off nothing here runs and the HDR is written exactly as
// before. Since v0.73 the result is written into <name>_HDR.tiff itself
// (one file per frame); the Software tag says what was applied.

import (
	"fmt"
	"strings"

	"basetool/colorsci"
)

// ColorOptions select the extra colour file. The zero value is off.
type ColorOptions struct {
	Cineon     bool    // Cineon log codes of the negative instead of linear
	MaskComp   bool    // neutral base (v0.72: independent of Cineon; in Cineon every base channel at 95)
	Strength   float64 // crosstalk strength 0 (off), 0.5, 1
	Matrix     colorsci.Mat3
	MatrixName string // e.g. "default (spectral, mask-aware)" or "Portra 400 (chart)"
	// StockBase (fraction of full scale, module 1 units) is used when
	// UseStock is set or frame 0 has no measurable base; zero = none.
	StockBase [3]float64
	StockName string
	UseStock  bool
}

// Enabled reports whether colour processing is applied to the HDR.
func (c ColorOptions) Enabled() bool { return c.Cineon || c.MaskComp || c.Strength != 0 }

// Describe is the Software-tag / report text.
func (c ColorOptions) Describe(base string) string {
	var p []string
	switch {
	case c.Cineon && c.MaskComp: // same text as v0.66-v0.71
		p = append(p, "encoding Cineon log (base 95, 0.002 D/code)")
	case c.Cineon:
		p = append(p, "encoding Cineon log negative (not inverted, 0.002 D/code, brightest base channel at 95, linear = ref*10^(-(code-95)*0.002))")
	default:
		p = append(p, "encoding linear")
	}
	if c.MaskComp {
		p = append(p, "mask comp on")
	} else {
		p = append(p, "mask comp off")
	}
	if c.Strength != 0 {
		m := c.Matrix
		if m == (colorsci.Mat3{}) {
			m = colorsci.Identity
		}
		p = append(p, fmt.Sprintf("crosstalk %.0f%% matrix %s [%.4f %.4f %.4f; %.4f %.4f %.4f; %.4f %.4f %.4f]",
			100*c.Strength, c.MatrixName, m[0][0], m[0][1], m[0][2], m[1][0], m[1][1], m[1][2], m[2][0], m[2][1], m[2][2]))
	} else {
		p = append(p, "crosstalk off")
	}
	if base != "" {
		p = append(p, "base "+base)
	}
	return strings.Join(p, ", ")
}

// colorBase picks the film base in TIFF units (0..65535): the frame-0
// measurement (module 1 statistic on the merged HDR) or the DB stock.
func colorBase(c ColorOptions, res *Result) ([3]float64, string, error) {
	f0 := res.Base1N > 0 && res.Base1Err == "" && res.Base1[0] > 0 && res.Base1[1] > 0 && res.Base1[2] > 0
	sb := c.StockBase
	st := sb[0] > 0 && sb[1] > 0 && sb[2] > 0
	fmtB := func(v [3]float64) string { return fmt.Sprintf("R %.4f G %.4f B %.4f FS", v[0], v[1], v[2]) }
	switch {
	case c.UseStock && st:
		return [3]float64{sb[0] * 65535, sb[1] * 65535, sb[2] * 65535}, "film stock " + c.StockName + " " + fmtB(sb), nil
	case f0:
		b := res.Base1
		note := "frame 0 " + fmtB(b)
		if c.UseStock {
			note += " (no film stock selected)"
		}
		return [3]float64{b[0] * 65535, b[1] * 65535, b[2] * 65535}, note, nil
	case st:
		return [3]float64{sb[0] * 65535, sb[1] * 65535, sb[2] * 65535}, "film stock " + c.StockName + " " + fmtB(sb) + " (frame 0 has no measurable base)", nil
	}
	return [3]float64{}, "", fmt.Errorf("no film base: frame 0 has no measurable clear base and no film stock is selected")
}

// applyColor applies the colour options to the final linear HDR plane
// (pix, not modified) and returns the processed plane and the tag text.
func applyColor(c ColorOptions, res *Result, pix []uint16, say func(string, ...any)) ([]uint16, string, error) {
	base, bnote, err := colorBase(c, res)
	if err != nil {
		return nil, "", err
	}
	o := colorsci.Options{Base: base, MaskComp: c.MaskComp, Crosstalk: c.Matrix, Strength: c.Strength}
	if c.Cineon {
		o.Encoding = colorsci.EncCineon
	}
	say("Applying %s ...", c.Short())
	out, err := colorsci.Apply(pix, o)
	if err != nil {
		return nil, "", err
	}
	desc := c.Describe(bnote)
	if c.Cineon && !c.MaskComp {
		desc += fmt.Sprintf(", Cineon ref %.6f FS", colorsci.CineonRef(base)/65535)
	}
	return out, desc, nil
}

// Short names the selected processing for progress lines, e.g.
// "Cineon log + crosstalk".
func (c ColorOptions) Short() string {
	var p []string
	if c.Cineon {
		p = append(p, "Cineon log")
	}
	if c.MaskComp {
		p = append(p, "mask comp")
	}
	if c.Strength != 0 {
		p = append(p, "crosstalk")
	}
	return strings.Join(p, " + ")
}
