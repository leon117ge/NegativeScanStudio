package colorsci

// ColorChecker sampling for the app's "Calibrate crosstalk from chart"
// (added in basetool v0.66; not part of the ./testdata/colorsci prototype).

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// ChartCorners are the centres of the four corner patches in image pixels,
// in chart order: patch 1 (dark skin), patch 6 (bluish green), patch 24
// (black), patch 19 (white). Patches in between lie on the bilinear grid.
type ChartCorners [4][2]float64

// PatchCentre returns the centre of patch i (0..23, row-major 6×4).
func (c ChartCorners) PatchCentre(i int) (x, y float64) {
	col, row := i%6, i/6
	u, v := float64(col)/5, float64(row)/3
	for k := 0; k < 2; k++ {
		top := c[0][k]*(1-u) + c[1][k]*u
		bot := c[3][k]*(1-u) + c[2][k]*u
		if k == 0 {
			x = top*(1-v) + bot*v
		} else {
			y = top*(1-v) + bot*v
		}
	}
	return
}

// PatchHalf is the half size of the sampled square: frac of the smaller
// patch pitch (frac 0.25 = the central half of each patch).
func (c ChartCorners) PatchHalf(frac float64) float64 {
	d := func(a, b [2]float64) float64 { return math.Hypot(a[0]-b[0], a[1]-b[1]) }
	px := math.Min(d(c[0], c[1]), d(c[3], c[2])) / 5
	py := math.Min(d(c[0], c[3]), d(c[1], c[2])) / 3
	return frac * math.Min(px, py)
}

// ChartSample is one patch reading.
type ChartSample struct {
	Mean    [3]float64 // linear, input scale
	N       int
	Clipped bool // a channel at/near full scale or zero
}

// SampleChart reads the 24 patches (trimmed mean of a square around each
// centre, the brightest and darkest 10% per channel dropped).
func SampleChart(pix []uint16, w, h int, c ChartCorners, frac float64) [24]ChartSample {
	var out [24]ChartSample
	r := c.PatchHalf(frac)
	for i := 0; i < 24; i++ {
		cx, cy := c.PatchCentre(i)
		x0, x1 := max(0, int(cx-r)), min(w, int(cx+r)+1)
		y0, y1 := max(0, int(cy-r)), min(h, int(cy+r)+1)
		var vals [3][]float64
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				j := (y*w + x) * 3
				for k := 0; k < 3; k++ {
					vals[k] = append(vals[k], float64(pix[j+k]))
				}
			}
		}
		s := ChartSample{N: len(vals[0])}
		for k := 0; k < 3; k++ {
			v := vals[k]
			if len(v) == 0 {
				s.Clipped = true
				continue
			}
			sort.Float64s(v)
			a, b := len(v)/10, len(v)-len(v)/10
			sum := 0.0
			for _, x := range v[a:b] {
				sum += x
			}
			s.Mean[k] = sum / float64(b-a)
			if v[b-1] >= 65000 || s.Mean[k] < 2 {
				s.Clipped = true
			}
		}
		out[i] = s
	}
	return out
}

// ChartResult is a calibration from one chart.
type ChartResult struct {
	Calibration
	Used    int      // patches used
	Skipped []int    // 1-based patch numbers left out (clipped)
	Warn    []string // plausibility warnings
}

// CalibrateChart solves the crosstalk matrix from sampled patches. base is
// the film base in the same scale (any positive value works: the fit is
// centred, so a base error is absorbed by the offset).
func CalibrateChart(s [24]ChartSample, base [3]float64, ridge float64) (ChartResult, error) {
	var res ChartResult
	if base[0] <= 0 || base[1] <= 0 || base[2] <= 0 {
		return res, ErrBadBase
	}
	var meas, lin [][3]float64
	for i, p := range s {
		if p.Clipped || p.N == 0 {
			res.Skipped = append(res.Skipped, i+1)
			continue
		}
		meas = append(meas, DensityAboveBase(p.Mean, base))
		var l [3]float64
		for k := 0; k < 3; k++ {
			l[k] = SRGBToLinear(ColorChecker24sRGB[i][k] / 255)
		}
		lin = append(lin, l)
	}
	res.Used = len(meas)
	cal, err := SolveCrosstalk(meas, ReferenceLogExposure(lin), ridge)
	if err != nil {
		return res, err
	}
	res.Calibration = cal
	// A negative gets denser with exposure: the diagonal must be positive.
	for r := 0; r < 3; r++ {
		if cal.Matrix[r][r] <= 0 {
			res.Warn = append(res.Warn, fmt.Sprintf("channel %c gets lighter with exposure - is this a positive, or are the corners in the wrong order?", "RGB"[r]))
		}
	}
	n := cal.Neutral
	big := 0.0
	for r := 0; r < 3; r++ {
		for k := 0; k < 3; k++ {
			if r != k {
				big = math.Max(big, math.Abs(n[r][k]))
			}
		}
	}
	if big > 1.5 {
		res.Warn = append(res.Warn, fmt.Sprintf("very strong off-diagonal terms (up to %.2f) - check the corner order and that the whole chart is in the frame", big))
	}
	if inv, err := n.Inverse(); err != nil || math.IsNaN(inv[0][0]) {
		res.Warn = append(res.Warn, "the matrix is nearly singular")
	}
	if r := math.Max(cal.RMS[0], math.Max(cal.RMS[1], cal.RMS[2])); r > 0.12 {
		res.Warn = append(res.Warn, fmt.Sprintf("poor fit (residual up to %.2f log exposure) - check the corners, glare on the chart, or the light", r))
	}
	if len(res.Skipped) > 6 {
		res.Warn = append(res.Warn, fmt.Sprintf("%d patches clipped or empty", len(res.Skipped)))
	}
	return res, nil
}

// ErrNoChart is returned when no corners were set.
var ErrNoChart = errors.New("colorsci: chart corners not set")
