// Package convert turns calibrated linear camera RGB into a simulated
// colour-negative matching NegativeScan Studio leveled trichrome scans.
package convert

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"basetool/colorsci"
	"basetool/scanmeta"
)

type Stock struct {
	Name      string
	FileTag   string // compact name for output files
	Source    string
	Gamma     [3]float64 // logistic fallback mid-slopes
}

type Options struct {
	Stock           Stock
	EV              float64
	CamMatrix       colorsci.Mat3
	CamStrength     float64
	ApplyCamProfile bool
	BorderPct       float64
	SoftBorder      bool
	MidDensity      float64
	MaxDensity      float64
	Grain           GrainISO
	GrainStrength   float64 // default 1
	GrainSize       float64 // default 1, relative cloud size
	GrainSeed       int64
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
			{Name: "Vision3 500T 5219", FileTag: "Vision3_500T", Source: "OFX Status M mid-slope; H-1-5219 (logistic fallback — no full D-logH CSV)",
				Gamma: [3]float64{0.4482, 0.5266, 0.5096}},
			{Name: "Vision3 250D 5207", FileTag: "Vision3_250D", Source: "OFX Status M mid-slope; H-1-5207 (logistic fallback — no full D-logH CSV)",
				Gamma: [3]float64{0.48148148, 0.56018519, 0.54166667}},
			{Name: "UltraMax 400", FileTag: "UltraMax400", Source: "Kodak E-7023 p.4 Status M full curve CSV",
				Gamma: [3]float64{0.518590585, 0.536316940, 0.604826092}},
			{Name: "Portra 400", FileTag: "Portra400", Source: "Kodak E-4050 p.4 Status M full curve CSV",
				Gamma: [3]float64{0.555934584, 0.549589137, 0.640031943}},
			{Name: "Gold 200", FileTag: "Gold200", Source: "Kodak E-7022 p.4 Status M full curve CSV",
				Gamma: [3]float64{0.550723702, 0.564449843, 0.610388122}},
			{Name: "Ektar 100", FileTag: "Ektar100", Source: "Kodak E-4046 p.4 Status M full curve CSV",
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
	if o.GrainSeed == 0 {
		o.GrainSeed = 0x4e535347 // 'NSSG'
	}
	if o.GrainStrength <= 0 {
		o.GrainStrength = 1
	}
	if o.GrainSize <= 0 {
		o.GrainSize = 1
	}
}

func densityAboveLogistic(logH, gamma, dMid, dMax float64) float64 {
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

// densityAboveFor maps scene-linear channel value to D-Dmin.
func densityAboveFor(lin float64, c int, o Options, cs *CurveSet) float64 {
	o.normalize()
	exp := math.Pow(2, o.EV)
	h := math.Max(lin*exp, 1e-12)
	rel := math.Log10(h / MidGrey)
	if cs != nil && cs.HasFull {
		return cs.densityAbove(cs.LogHMid+rel, c)
	}
	// Logistic in relative logH (0 = mid grey).
	return densityAboveLogistic(rel, o.Stock.Gamma[c], o.MidDensity, o.MaxDensity)
}

func applyCamMatrix(m colorsci.Mat3, strength float64, v [3]float64) [3]float64 {
	return colorsci.ApplyCam(m, strength, v)
}

func SceneToNeg(v [3]float64, o Options) [3]float64 {
	o.normalize()
	if o.ApplyCamProfile && o.CamStrength > 0 {
		v = colorsci.ApplyCam(o.CamMatrix, o.CamStrength, v)
	}
	cs := CurveFor(o.Stock.Name)
	var out [3]float64
	for c := 0; c < 3; c++ {
		d := densityAboveFor(v[c], c, o, cs)
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
	ProcessPixelsFast(rgb, w, h, o)
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
	ProcessPixelsFast(tmp, w, h, o)
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

// OutputPath is <rafStem>_<FileTag>.tiff with _2, _3... if taken.
func OutputPath(src string, stock Stock) string {
	ext := filepath.Ext(src)
	stem := strings.TrimSuffix(filepath.Base(src), ext)
	dir := filepath.Dir(src)
	tag := stock.FileTag
	if tag == "" {
		tag = strings.ReplaceAll(stock.Name, " ", "")
	}
	base := filepath.Join(dir, stem+"_"+tag+".tiff")
	if _, err := os.Stat(base); err != nil {
		return base
	}
	for n := 2; n < 1000; n++ {
		p := filepath.Join(dir, fmt.Sprintf("%s_%s_%d.tiff", stem, tag, n))
		if _, err := os.Stat(p); err != nil {
			return p
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%s_%d.tiff", stem, tag, 1000))
}

func Metadata(o Options) scanmeta.Metadata {
	b := [3]float64{BaseFS, BaseFS, BaseFS}
	proc := "digital-to-negative; stock=" + o.Stock.Name
	if cs := CurveFor(o.Stock.Name); cs != nil {
		proc += "; curve=StatusM-CSV"
	} else {
		proc += "; curve=logistic-fallback"
	}
	if o.Grain != GrainOff {
		proc += "; grain=" + o.Grain.Label()
		if o.GrainStrength != 1 {
			proc += fmt.Sprintf("; grain_strength=%.2f", o.GrainStrength)
		}
	}
	m := scanmeta.Metadata{
		Mode:          "convert",
		Encoding:      "linear",
		FilmBase:      &b,
		FilmBaseSpace: scanmeta.AfterProfile,
		BaseSource:    "convert simulated levelled base",
		Processing:    proc,
		Roll:          o.Roll,
	}
	if m.Roll == "" {
		m.Roll = o.Stock.Name
	}
	return m
}

func NeutralBaseCode() uint16 { return uint16(BaseFS*65535 + 0.5) }
