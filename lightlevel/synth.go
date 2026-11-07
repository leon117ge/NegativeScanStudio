package lightlevel

import (
	"context"
	"math"
	"math/rand"
)

// Synth is a simulated camera looking at a clear film base on the light:
// the lit channel's level is Gain[c] * f(brightness) with a mildly
// non-linear LED response, light fall-off towards the corners and noise.
// Lights returns the light's current brightness (the simulated light).
type Synth struct {
	Gain   [3]float64 // base level at brightness 100
	W, H   int
	Lights func() (on [3]bool, v [3]int)
	Shots  int
	Exp    string // Prepare's exposure text (empty = ISO 160, 1/60 s)
	rnd    *rand.Rand
}

// NewSynth returns a typical orange-mask base: blue needs the most light.
func NewSynth(lights func() ([3]bool, [3]int)) *Synth {
	return &Synth{Gain: [3]float64{1.35, 0.85, 0.55}, W: 120, H: 80, Lights: lights, rnd: rand.New(rand.NewSource(1))}
}

func (s *Synth) Name() string { return "Simulated camera (follows the simulated light)" }
func (s *Synth) Mono() bool   { return false }
func (s *Synth) Prepare() (string, error) {
	if s.Exp != "" {
		return s.Exp + " (simulated)", nil
	}
	return "ISO 160, 1/60 s (simulated)", nil
}

// Shoot renders channel ch.
func (s *Synth) Shoot(ctx context.Context, ch int, prefix string) (*Image, error) {
	s.Shots++
	on, v := s.Lights()
	lv := 0.0
	if on[ch] {
		x := float64(v[ch]) / 100
		lv = s.Gain[ch] * (0.85*x + 0.15*x*x) // LED drive is not exactly linear
	}
	im := &Image{W: s.W, H: s.H, V: make([]float32, s.W*s.H), Clip: make([]bool, s.W*s.H)}
	for y := 0; y < s.H; y++ {
		for x := 0; x < s.W; x++ {
			dx, dy := float64(x)/float64(s.W)-0.5, float64(y)/float64(s.H)-0.5
			fall := 1 - 0.25*(dx*dx+dy*dy)*4
			val := lv*fall + 0.002*s.rnd.NormFloat64()
			val = math.Max(val, 0)
			i := y*s.W + x
			if val >= 1 {
				val, im.Clip[i] = 1, true
			}
			im.V[i] = float32(val)
		}
	}
	return im, nil
}
