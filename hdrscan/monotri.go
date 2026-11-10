package hdrscan

import (
	"fmt"
	"time"

	"basetool/scanmeta"
)

// MonoPlane is one 16-bit linear monochrome capture (one light channel).
type MonoPlane struct {
	W, H int
	Pix  []uint16
}

// MonoTriOptions control the monochrome trichrome merge.
type MonoTriOptions struct {
	OutPath   string
	LZW       bool
	CorrectCA bool
	Software  string
	Make      string
	Model     string
	Metadata  scanmeta.Metadata
	Progress  func(string)
}

// MonoTriResult is the written RGB TIFF and the CA report.
type MonoTriResult struct {
	Out      string
	Report   FocusShiftReport
	Elapsed  time.Duration
	Software string
}

// ProcessMonoTrichrome combines three same-size monochrome frames into one
// linear 16-bit RGB TIFF (R from the red-lit frame, G from green, B from
// blue) and optionally corrects chromatic-aberration focus shift, with
// green as the geometric and sharpness reference.
func ProcessMonoTrichrome(r, g, b MonoPlane, o MonoTriOptions) (*MonoTriResult, error) {
	start := time.Now()
	if err := checkMono(r, "R"); err != nil {
		return nil, err
	}
	if err := checkMono(g, "G"); err != nil {
		return nil, err
	}
	if err := checkMono(b, "B"); err != nil {
		return nil, err
	}
	if r.W != g.W || r.H != g.H || b.W != g.W || b.H != g.H {
		return nil, fmt.Errorf("channel sizes differ: R %dx%d G %dx%d B %dx%d", r.W, r.H, g.W, g.H, b.W, b.H)
	}
	w, h := g.W, g.H
	rf := u16ToF64(r.Pix)
	gf := u16ToF64(g.Pix)
	bf := u16ToF64(b.Pix)
	report := FocusShiftReport{Enabled: o.CorrectCA}
	if o.CorrectCA {
		if o.Progress != nil {
			o.Progress("Measuring focus shift against green ...")
		}
		var corr FocusShiftReport
		rf, gf, bf, corr = CorrectMonoCA(rf, gf, bf, w, h)
		report = corr
		if o.Progress != nil {
			o.Progress(report.String())
		}
	}
	n := w * h
	out := make([]uint16, 3*n)
	for i := 0; i < n; i++ {
		out[3*i+0] = f64ToU16(rf[i])
		out[3*i+1] = f64ToU16(gf[i])
		out[3*i+2] = f64ToU16(bf[i])
	}
	sw := o.Software
	if sw == "" {
		sw = "NegativeScan Studio"
	}
	sw += " mono trichrome R/G/B (one light channel per monochrome capture"
	if o.CorrectCA {
		sw += ", CA focus-shift correction, green reference"
	}
	sw += ")"
	meta := o.Metadata.ForCapture()
	meta.Mode = "mono-trichrome"
	meta.Encoding = "linear"
	if report.Enabled {
		if meta.Processing != "" {
			meta.Processing += "; "
		}
		meta.Processing += report.String()
	}
	path := o.OutPath
	if path == "" {
		return nil, fmt.Errorf("output path is empty")
	}
	path = uniqueFile(path)
	if o.Progress != nil {
		o.Progress("Writing " + path + " ...")
	}
	make_ := o.Make
	if make_ == "" {
		make_ = "Hikrobot"
	}
	model := o.Model
	if model == "" {
		model = "MV-CS240-90UM"
	}
	if err := writeScanTIFF(path, w, h, out, o.LZW, make_, model, sw, meta); err != nil {
		return nil, err
	}
	return &MonoTriResult{Out: path, Report: report, Elapsed: time.Since(start), Software: sw}, nil
}

func checkMono(p MonoPlane, name string) error {
	if p.W <= 0 || p.H <= 0 || len(p.Pix) < p.W*p.H {
		return fmt.Errorf("no %s frame", name)
	}
	return nil
}

func u16ToF64(p []uint16) []float64 {
	out := make([]float64, len(p))
	for i, v := range p {
		out[i] = float64(v)
	}
	return out
}

func f64ToU16(v float64) uint16 {
	if v < 0 {
		return 0
	}
	if v > 65535 {
		return 65535
	}
	return uint16(v + 0.5)
}
