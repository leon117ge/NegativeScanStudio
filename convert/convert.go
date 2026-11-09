// Package convert turns calibrated linear camera RGB into a simulated
// colour-negative that matches NegativeScan Studio leveled trichrome scans:
// film base at 0.5/0.5/0.5 linear FS, image = 0.5 * 10^(-(D-Dmin)), optional
// rebate border, nss.scan/1 metadata values. No orange-mask cast.
package convert

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"

	"basetool/colorsci"
	"basetool/scanmeta"
)

// Stock is one film model available in Convert.
type Stock struct {
	Name   string
	Source string
	Gamma  [3]float64
}

// Options control one conversion.
type Options struct {
	Stock           Stock
	EV              float64
	CamMatrix       colorsci.Mat3
	CamStrength     float64
	ApplyCamProfile bool
	BorderPct       float64 // rebate each side as fraction of short edge
	SoftBorder      bool
	MidDensity      float64 // D-Dmin at 18% grey (default 0.75)
	MaxDensity      float64 // asymptotic D-Dmin
	Software        string
	Roll            string
	Make            string
	Model           string
	LZW             bool
}

const (
	MidGrey = 0.18
	BaseFS  = 0.5
)

var (
	stockOnce sync.Once
	stockList []Stock
)

func Stocks() []Stock {
	stockOnce.Do(func() {
		stockList = []Stock{
			{Name: "Vision3 500T 5219", Source: "OFX Vision3 Status M gamma; Kodak H-1-5219",
				Gamma: [3]float64{0.4482, 0.5266, 0.5096}},
			{Name: "Vision3 250D 5207", Source: "OFX 250D gamma; H-1-5219 family",
				Gamma: [3]float64{0.48148148, 0.56018519, 0.54166667}},
			{Name: "UltraMax 400", Source: "OFX still_gammas from Kodak E-7023 polylines",
				Gamma: [3]float64{0.518590585, 0.536316940, 0.604826092}},
			{Name: "Portra 400", Source: "OFX still_gammas from Kodak E-4050 polylines",
				Gamma: [3]float64{0.555934584, 0.549589137, 0.640031943}},
			{Name: "Gold 200", Source: "OFX still_gammas from Kodak E-7022 polylines",
				Gamma: [3]float64{0.550723702, 0.564449843, 0.610388122}},
			{Name: "Ektar 100", Source: "OFX still_gammas from Kodak E-4046 polylines",
				Gamma: [3]float64{0.580572786, 0.579564388, 0.663740443}},
		}
	})
	return stockList
}

func StockByName(name string) (Stock, error) {
	for _, s := range Stocks() {
		if s.Name == name {
			return s, nil
		}
	}
	return Stock{}, fmt.Errorf("unknown film stock %q", name)
}

func (o *Options) normalize() {
	if o.MidDensity <= 0 {
		o.MidDensity = 0.75
	}
	if o.MaxDensity <= o.MidDensity {
		o.MaxDensity = math.Max(2*o.MidDensity, 1.5)
	}
	if o.BorderPct < 0 {
		o.BorderPct = 0
	}
	if o.BorderPct > 0.2 {
		o.BorderPct = 0.2
	}
}

func densityAbove(logH, gamma, dMid, dMax float64) float64 {
	if gamma < 1e-6 {
		gamma = 1e-6
	}
	if dMax <= dMid {
		dMax = 2 * dMid
	}
	k := 4 * gamma / dMax
	x0 := math.Log(dMax/dMid-1) / k
	d := dMax / (1 + math.Exp(-k*(logH-x0)))
	if d < 0 {
		return 0
	}
	return d
}

// SceneToNeg maps one calibrated linear RGB pixel to leveled negative FS.
func SceneToNeg(v [3]float64, o Options) [3]float64 {
	o.normalize()
	exp := math.Pow(2, o.EV)
	if o.ApplyCamProfile && o.CamStrength > 0 {
		v = colorsci.ApplyCam(o.CamMatrix, o.CamStrength, v)
	}
	var out [3]float64
	for c := 0; c < 3; c++ {
		h := math.Max(v[c]*exp, 1e-12)
		d := densityAbove(math.Log10(h/MidGrey), o.Stock.Gamma[c], o.MidDensity, o.MaxDensity)
		t := BaseFS * math.Pow(10, -d)
		if t < 0 {
			t = 0
		}
		if t > 1 {
			t = 1
		}
		out[c] = t
	}
	return out
}

func ProcessPixels(rgb []float64, w, h int, o Options) {
	o.normalize()
	for i := 0; i+2 < len(rgb); i += 3 {
		v := SceneToNeg([3]float64{rgb[i], rgb[i+1], rgb[i+2]}, o)
		rgb[i], rgb[i+1], rgb[i+2] = v[0], v[1], v[2]
	}
	applyBorder(rgb, w, h, o)
}

func applyBorder(rgb []float64, w, h int, o Options) {
	if o.BorderPct <= 0 || w < 8 || h < 8 {
		return
	}
	short := w
	if h < w {
		short = h
	}
	bw := int(math.Round(float64(short) * o.BorderPct))
	if bw < 1 {
		bw = 1
	}
	feather := 0
	if o.SoftBorder {
		feather = bw / 3
		if feather < 1 {
			feather = 1
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := x
			if w-1-x < dx {
				dx = w - 1 - x
			}
			dy := y
			if h-1-y < dy {
				dy = h - 1 - y
			}
			dEdge := dx
			if dy < dEdge {
				dEdge = dy
			}
			var a float64
			if dEdge >= bw {
				a = 1
			} else if feather == 0 {
				a = 0
			} else if dEdge < bw-feather {
				a = 0
			} else {
				a = float64(dEdge-(bw-feather)) / float64(feather)
			}
			if a >= 1 {
				continue
			}
			i := (y*w + x) * 3
			for c := 0; c < 3; c++ {
				rgb[i+c] = rgb[i+c]*a + BaseFS*(1-a)
			}
		}
	}
}

func Process16(pix []uint16, w, h int, o Options) []uint16 {
	n := len(pix)
	tmp := make([]float64, n)
	for i, v := range pix {
		tmp[i] = float64(v) / 65535
	}
	ProcessPixels(tmp, w, h, o)
	out := make([]uint16, n)
	for i, v := range tmp {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		out[i] = uint16(v*65535 + 0.5)
	}
	return out
}

// RGBPath matches trichrome naming: <stem>_RGB.tiff
func RGBPath(src string) string {
	ext := filepath.Ext(src)
	base := strings.TrimSuffix(src, ext)
	base = strings.TrimSuffix(base, "_RGB_HDR")
	base = strings.TrimSuffix(base, "_RGB")
	base = strings.TrimSuffix(base, "_NEG")
	return base + "_RGB.tiff"
}

// Metadata builds nss.scan/1 with film_base 0.5/0.5/0.5 after_profile.
func Metadata(o Options) scanmeta.Metadata {
	b := [3]float64{BaseFS, BaseFS, BaseFS}
	m := scanmeta.Metadata{
		Mode:          "convert",
		Encoding:      "linear",
		FilmBase:      &b,
		FilmBaseSpace: scanmeta.AfterProfile,
		BaseSource:    "convert simulated levelled base",
		Processing:    "digital-to-negative; stock=" + o.Stock.Name,
		Roll:          o.Roll,
	}
	if m.Roll == "" {
		m.Roll = o.Stock.Name
	}
	return m
}

func NeutralBaseCode() uint16 { return uint16(BaseFS*65535 + 0.5) }
