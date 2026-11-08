// Package lightlevel levels a film base at fixed camera exposure using measured
// channel responses, bounded secant steps and matrix-aware integer refinement.
package lightlevel

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"time"

	"basetool/colorsci"
)

// Light is the part of the RGB light the leveling uses.
type Light interface {
	// SetAll sets all three channels (command 03) and returns once acknowledged.
	SetAll(on [3]bool, v [3]int) error
}

// Image is one capture reduced to the channel that was lit, as a fraction
// of linear TIFF full scale, on a fixed grid of actual demosaiced pixels.
type Image struct {
	W, H int
	V    []float32
	Clip []bool // sampled pixel is near full scale (diagnostic)
	Path string // file the frame came from ("" = none kept)
}

// Camera shoots one frame at a fixed exposure.
type Camera interface {
	// Name describes the camera and its fixed exposure for the log.
	Name() string
	// Mono reports a monochrome sensor (every pixel measures the lit
	// channel); false = a colour sensor with the matching converted channel.
	Mono() bool
	// Prepare sets / checks the fixed exposure once before the sequence.
	// It must never change the exposure later.
	Prepare() (string, error)
	// Shoot captures a frame with only channel ch lit and returns that
	// channel on the scan converter's linear scale (all pixels on a mono camera).
	Shoot(ctx context.Context, ch int, prefix string) (*Image, error)
}

// Rect is a measure area in fractions of the frame (0..1); empty = centre 50 %.
type Rect struct{ X0, Y0, X1, Y1 float64 }

// Empty reports a zero-size rectangle.
func (r Rect) Empty() bool { return r.X1 <= r.X0 || r.Y1 <= r.Y0 }

// Centre is the default measure area.
var Centre = Rect{0.25, 0.25, 0.75, 0.75}

// Params of a leveling run.
type Params struct {
	Target  float64       // base level, fraction of full scale (0.50 default)
	Tol     float64       // relative tolerance (0.02 = ±2 %)
	MaxIter int           // shooting rounds (default 10)
	Settle  time.Duration // wait after switching the light before the shot (default 300 ms)
	Area    Rect
	Start   [3]int // starting brightness 1..100 per channel

	Matrix     colorsci.Mat3                 // known camera matrix blended by profile strength; zero = identity
	SelectArea func([3]*Image) (Rect, error) // optional one-time auto selection
}

// Defaults fills zero fields.
func (p *Params) Defaults() {
	if p.Target <= 0 {
		p.Target = 0.50
	}
	if p.Tol <= 0 {
		p.Tol = 0.02
	}
	if p.MaxIter <= 0 {
		p.MaxIter = 10
	}
	if p.Settle <= 0 {
		p.Settle = 300 * time.Millisecond
	}
	if p.Area.Empty() {
		p.Area = Centre
	}
	if p.Matrix == (colorsci.Mat3{}) {
		p.Matrix = colorsci.Identity
	}
	for i := range p.Start {
		p.Start[i] = clampV(p.Start[i])
	}
}

// Reading is a measured channel.
type Reading struct {
	Level   float64 // median of the measure area, fraction of full scale
	P90     float64
	P99     float64
	ClipPct float64 // % of samples near full scale (diagnostic only)
	Cells   int
}

// Measure reads the area of an image.
func Measure(im *Image, a Rect) (Reading, error) {
	if im == nil || im.W <= 0 || im.H <= 0 || len(im.V) != im.W*im.H || (im.Clip != nil && len(im.Clip) != len(im.V)) {
		return Reading{}, errors.New("empty image")
	}
	if a.Empty() {
		a = Centre
	}
	x0, x1 := int(a.X0*float64(im.W)), int(math.Ceil(a.X1*float64(im.W)))
	y0, y1 := int(a.Y0*float64(im.H)), int(math.Ceil(a.Y1*float64(im.H)))
	x0, y0 = max(x0, 0), max(y0, 0)
	x1, y1 = min(x1, im.W), min(y1, im.H)
	var v []float64
	clip := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := y*im.W + x
			x := float64(im.V[i])
			if math.IsNaN(x) || math.IsInf(x, 0) {
				continue
			}
			v = append(v, x)
			if im.Clip != nil && im.Clip[i] {
				clip++
			}
		}
	}
	if len(v) < 4 {
		return Reading{}, errors.New("measure area is too small")
	}
	sort.Float64s(v)
	return Reading{Level: v[len(v)/2], P90: v[len(v)*9/10], P99: v[len(v)*99/100], ClipPct: 100 * float64(clip) / float64(len(v)), Cells: len(v)}, nil
}

// BaseClipped requires a broadly saturated base, not isolated clipped photosites.
// The near-full-scale sample percentage remains diagnostic only.
func (r Reading) BaseClipped() bool { return r.Level >= .98 && r.P90 >= .995 }

// Step is one shot.
type Step struct {
	Round   int
	Channel int
	Value   int
	Reading Reading
	OK      bool // within tolerance
	Next    int  // brightness for the next round
	Note    string
}

func (s Step) String() string {
	t := fmt.Sprintf("round %d %s at %d: base %.4f FS", s.Round, "RGB"[s.Channel:s.Channel+1], s.Value, s.Reading.Level)
	if s.Reading.ClipPct > 0 {
		t += fmt.Sprintf(" (%.1f%% near-FS samples)", s.Reading.ClipPct)
	}
	if s.OK && s.Note == "" {
		t += " - within tolerance"
	} else if !s.OK && s.Next != s.Value {
		t += fmt.Sprintf(" -> %d", s.Next)
	}
	if s.Note != "" {
		t += " - " + s.Note
	}
	return t
}

// Result of a run.
type Result struct {
	Values    [3]int     // final brightness
	Readings  [3]float64 // last measured base per channel
	Done      [3]bool    // final after-matrix channels within the requested tolerance
	Limited   [3]bool    // would need more than 100
	Needed    [3]float64 // proportional estimate only, never a measured code
	Boundary  [3]float64 // actual reading at the unreachable bound
	TooBright [3]bool    // still above target at brightness 1
	Images    [3]*Image
	Rounds    int
	Steps     []Step
	Camera    string
	Post      [3]float64
	Verified  bool // a fresh full R/G/B capture, never cached prediction
	Captured  [3]bool
	Reason    string
	Area      Rect
}

// Converged reports all three channels within tolerance.
func (r Result) Converged() bool { return r.Verified && r.Done[0] && r.Done[1] && r.Done[2] }

// Problems lists what the user must act on.
func (r Result) Problems() []string {
	var p []string
	if r.Converged() {
		return nil
	}
	if r.Reason != "" {
		p = append(p, r.Reason)
	}
	for c := 0; c < 3; c++ {
		n := "RGB"[c : c+1]
		switch {
		case r.Limited[c]:
			p = append(p, fmt.Sprintf("%s too dark at brightness 100 (base %.5f FS); increase camera exposure", n, r.Boundary[c]))
		case r.TooBright[c]:
			p = append(p, fmt.Sprintf("%s too bright at brightness 1 (base %.5f FS); reduce camera exposure", n, r.Boundary[c]))
		case !r.Captured[c]:
			p = append(p, n+" has no measured frame")
		case !r.Done[c]:
			p = append(p, fmt.Sprintf("%s after-profile base %.5f FS at %d (not within tolerance)", n, r.Post[c], r.Values[c]))
		}
	}
	return p
}

func clampV(v int) int {
	if v < 1 {
		return 1
	}
	if v > 100 {
		return 100
	}
	return v
}

// Composite builds a colour preview from the three channel captures, each
// scaled so the target base level shows as mid grey (a levelled base is
// neutral).
func Composite(im [3]*Image, target float64) image.Image {
	w, h := 0, 0
	for _, m := range im {
		if m != nil {
			w, h = max(w, m.W), max(h, m.H)
		}
	}
	if w == 0 {
		return nil
	}
	if target <= 0 {
		target = 0.5
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	g := 0.5 / target
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var c [3]uint8
			for k, m := range im {
				if m == nil || x >= m.W || y >= m.H {
					continue
				}
				c[k] = toByte(float64(m.V[y*m.W+x]) * g)
			}
			out.SetRGBA(x, y, color.RGBA{c[0], c[1], c[2], 255})
		}
	}
	return out
}

// Gray renders one channel capture, scaled like Composite.
func Gray(m *Image, target float64) image.Image {
	if m == nil {
		return nil
	}
	if target <= 0 {
		target = 0.5
	}
	out := image.NewRGBA(image.Rect(0, 0, m.W, m.H))
	for i, v := range m.V {
		b := toByte(float64(v) * 0.5 / target)
		if m.Clip != nil && m.Clip[i] {
			out.Pix[4*i], out.Pix[4*i+1], out.Pix[4*i+2], out.Pix[4*i+3] = 255, 0, 255, 255
			continue
		}
		out.Pix[4*i], out.Pix[4*i+1], out.Pix[4*i+2], out.Pix[4*i+3] = b, b, b, 255
	}
	return out
}

// toByte maps linear 0..1 to display sRGB-ish (gamma 1/2.2).
func toByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(math.Pow(v, 1/2.2)*255 + 0.5)
}
