// Package lightlevel levels the film base with the RGB light: the camera
// exposure stays fixed and only the light's R, G and B brightness change.
// For each channel only that LED is lit, one frame is shot and the clear
// film base is measured in the matching camera channel; the brightness is
// scaled by target / measured and the round is repeated until every
// channel is within tolerance.
//
// The camera is behind the Camera interface (Fujifilm X-T3 over USB today;
// a monochrome industrial camera such as a Hikrobot MVS body can implement
// it later), the light behind Light (rgblight.Client).
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
)

// Light is the part of the RGB light the leveling uses.
type Light interface {
	// SetAll sets all three channels (command 03) and returns once acknowledged.
	SetAll(on [3]bool, v [3]int) error
}

// Image is one capture reduced to the channel that was lit, as a fraction
// of the camera's raw full scale (black subtracted), on a coarse grid.
type Image struct {
	W, H int
	V    []float32
	Clip []bool // cell contains a clipped photosite
	Path string // file the frame came from ("" = none kept)
}

// Camera shoots one frame at a fixed exposure.
type Camera interface {
	// Name describes the camera and its fixed exposure for the log.
	Name() string
	// Mono reports a monochrome sensor (every pixel measures the lit
	// channel); false = colour filter array (only the matching CFA colour
	// is used).
	Mono() bool
	// Prepare sets / checks the fixed exposure once before the sequence.
	// It must never change the exposure later.
	Prepare() (string, error)
	// Shoot captures a frame with only channel ch lit and returns that
	// channel (CFA colour ch on a Bayer/X-Trans camera, all pixels on a
	// mono camera).
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
	MaxIter int           // shooting rounds (default 4)
	Settle  time.Duration // wait after switching the light before the shot (default 300 ms)
	Area    Rect
	Start   [3]int // starting brightness 1..100 per channel
	// Targets (v0.85), when > 0, override Target per channel: used for a
	// camera-profile pre-matrix base and the TIFF-box correction.
	Targets [3]float64
}

// T is the target of channel c.
func (p Params) T(c int) float64 {
	if p.Targets[c] > 0 {
		return p.Targets[c]
	}
	return p.Target
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
		p.MaxIter = 4
	}
	if p.Settle <= 0 {
		p.Settle = 300 * time.Millisecond
	}
	if p.Area.Empty() {
		p.Area = Centre
	}
	for i := range p.Start {
		p.Start[i] = clampV(p.Start[i])
	}
}

// Reading is a measured channel.
type Reading struct {
	Level   float64 // median of the measure area, fraction of full scale
	P99     float64
	ClipPct float64 // % of cells with clipped photosites
	Cells   int
}

// Measure reads the area of an image.
func Measure(im *Image, a Rect) (Reading, error) {
	if im == nil || im.W == 0 || im.H == 0 {
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
			v = append(v, float64(im.V[i]))
			if im.Clip != nil && im.Clip[i] {
				clip++
			}
		}
	}
	if len(v) < 4 {
		return Reading{}, errors.New("measure area is too small")
	}
	sort.Float64s(v)
	return Reading{Level: v[len(v)/2], P99: v[len(v)*99/100], ClipPct: 100 * float64(clip) / float64(len(v)), Cells: len(v)}, nil
}

// ClipLimit: more than this % of clipped cells makes a reading unusable.
const ClipLimit = 1.0

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
		t += fmt.Sprintf(" (%.1f%% clipped)", s.Reading.ClipPct)
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
	Done      [3]bool    // within tolerance (or the closest 1-step value)
	Limited   [3]bool    // would need more than 100
	Needed    [3]float64 // brightness the last reading asks for (unclamped)
	TooBright [3]bool    // still above target at brightness 1
	Images    [3]*Image
	Rounds    int
	Steps     []Step
	Camera    string
}

// Converged reports all three channels within tolerance.
func (r Result) Converged() bool { return r.Done[0] && r.Done[1] && r.Done[2] }

// Problems lists what the user must act on.
func (r Result) Problems() []string {
	var p []string
	for c := 0; c < 3; c++ {
		n := "RGB"[c : c+1]
		switch {
		case r.Limited[c]:
			p = append(p, fmt.Sprintf("%s would need brightness %.0f (max 100): base %.3f FS at 100", n, r.Needed[c], r.Readings[c]))
		case r.TooBright[c]:
			p = append(p, fmt.Sprintf("%s is still above target at brightness 1 (base %.3f FS)", n, r.Readings[c]))
		case !r.Done[c]:
			p = append(p, fmt.Sprintf("%s not within tolerance after %d rounds (base %.3f FS at %d)", n, r.Rounds, r.Readings[c], r.Values[c]))
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

// Run levels the three channels. progress (may be nil) gets each step and
// status line. The light is left with the final values and the enables in
// restore (normally the state before the run).
func Run(ctx context.Context, l Light, cam Camera, p Params, restore [3]bool, progress func(string)) (Result, error) {
	p.Defaults()
	say := func(f string, a ...any) {
		if progress != nil {
			progress(fmt.Sprintf(f, a...))
		}
	}
	res := Result{Values: p.Start, Camera: cam.Name()}
	exp, err := cam.Prepare()
	if err != nil {
		return res, fmt.Errorf("camera: %w", err)
	}
	say("Fixed camera exposure: %s (not changed during leveling)", exp)
	// the light is restored to the final values with the original enables,
	// also after an error or cancel
	defer func() {
		if err := l.SetAll(restore, res.Values); err != nil {
			say("Could not restore the light: %v", err)
		}
	}()
	for round := 1; round <= p.MaxIter; round++ {
		res.Rounds = round
		todo := false
		for c := 0; c < 3; c++ {
			if res.Done[c] || res.Limited[c] || res.TooBright[c] {
				continue
			}
			todo = true
			if err := ctx.Err(); err != nil {
				return res, err
			}
			var on [3]bool
			on[c] = true
			if err := l.SetAll(on, res.Values); err != nil {
				return res, fmt.Errorf("light: %w", err)
			}
			select {
			case <-ctx.Done():
				return res, ctx.Err()
			case <-time.After(p.Settle):
			}
			im, err := cam.Shoot(ctx, c, fmt.Sprintf("light_%c%d_", "RGB"[c], round))
			if err != nil {
				return res, fmt.Errorf("%c capture: %w", "RGB"[c], err)
			}
			res.Images[c] = im
			rd, err := Measure(im, p.Area)
			if err != nil {
				return res, fmt.Errorf("%c: %w", "RGB"[c], err)
			}
			st := Step{Round: round, Channel: c, Value: res.Values[c], Reading: rd}
			res.Readings[c] = rd.Level
			cur := float64(res.Values[c])
			switch {
			case rd.ClipPct > ClipLimit:
				st.Next = clampV(res.Values[c] / 2)
				st.Note = "clipped, halving"
				res.Needed[c] = cur / 2
			case rd.Level < 0.002:
				st.Next = clampV(res.Values[c] * 4)
				st.Note = "almost no signal"
				res.Needed[c] = cur * 4
			default:
				res.Needed[c] = cur * p.T(c) / rd.Level
				st.Next = clampV(int(math.Round(res.Needed[c])))
				switch {
				case math.Abs(rd.Level/p.T(c)-1) <= p.Tol:
					st.OK, st.Next = true, res.Values[c]
				case res.Values[c] == 100 && res.Needed[c] > 100:
					res.Limited[c] = true
					st.Note = "needs more than 100"
				case res.Values[c] == 1 && res.Needed[c] < 1:
					res.TooBright[c] = true
					st.Note = "too bright even at 1"
				case st.Next == res.Values[c]:
					st.OK = true
					st.Note = "closest brightness step"
				}
			}
			res.Done[c] = st.OK
			res.Values[c] = st.Next
			res.Steps = append(res.Steps, st)
			say("%s", st)
		}
		if !todo {
			res.Rounds = round - 1
			break
		}
		if res.Converged() {
			break
		}
	}
	return res, nil
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
