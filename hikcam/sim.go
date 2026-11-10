package hikcam

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// Sim is a software stand-in for MV-CS240-90UM used when the SDK or the
// camera is missing, and by unit tests.
type Sim struct {
	mu     sync.Mutex
	info   Info
	set    Settings
	grab   bool
	frame  int
	closed bool
	// LightChannel, if set, tints the simulated frame (0=R, 1=G, 2=B)
	// so a trichrome capture of the simulator is not three identical planes.
	LightChannel func() int
}

// OpenSim returns a simulated CS240. It never fails.
func OpenSim() *Sim {
	return &Sim{
		info: Info{
			Model:     CS240Model,
			Serial:    "SIM",
			Vendor:    "Hikrobot",
			Interface: "Simulated",
			Simulated: true,
		},
		set: DefaultSettings(),
	}
}

func (s *Sim) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

func (s *Sim) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.set
}

func (s *Sim) Apply(st Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrNotOpen
	}
	s.set = st.Clamp()
	if s.set.FullFrame {
		s.set.Width, s.set.Height = 960, 654
		s.set.OffsetX, s.set.OffsetY = 0, 0
	}
	return nil
}

func (s *Sim) StartGrab() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrNotOpen
	}
	s.grab = true
	return nil
}

func (s *Sim) StopGrab() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grab = false
	return nil
}

func (s *Sim) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.grab = false
	return nil
}

func (s *Sim) Grab(timeout time.Duration) (*Frame, error) {
	_ = timeout
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrNotOpen
	}
	if !s.grab {
		return nil, ErrNotGrab
	}
	w, h := s.set.Width, s.set.Height
	if w < 16 {
		w = 960
	}
	if h < 16 {
		h = 654
	}
	ch := -1
	if s.LightChannel != nil {
		ch = s.LightChannel()
	}
	s.frame++
	pix := renderSimFrame(w, h, s.set, s.frame, ch)
	return &Frame{
		W:           w,
		H:           h,
		Pix:         pix,
		PixelFormat: "Mono12",
		ExposureUS:  s.set.ExposureUS,
		GainDB:      s.set.GainDB,
	}, nil
}

// renderSimFrame draws a grid plus fine grain so a focus meter has edges
// to score. Exposure and gain scale the mean level. LightChannel tints
// the pattern so R/G/B captures differ slightly (wavelength stand-in).
func renderSimFrame(w, h int, st Settings, n, ch int) []uint16 {
	exp := st.ExposureUS / 10_000 // 1.0 at the default 10 ms
	if exp < 0.05 {
		exp = 0.05
	}
	if exp > 8 {
		exp = 8
	}
	gain := math.Pow(10, st.GainDB/20)
	level := exp * gain
	pix := make([]uint16, w*h)
	cx, cy := float64(w)/2, float64(h)/2
	// A slight per-channel scale/shift mimics CA so the merge report is non-zero
	// when the simulator is captured under different lights.
	sc, dx, dy := 1.0, 0.0, 0.0
	switch ch {
	case 0:
		sc, dx, dy = 1.004, 1.6, -0.8
	case 2:
		sc, dx, dy = 0.996, -1.1, 1.3
	}
	seed := uint32(n*9973 + 17)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx := (float64(x)-cx)/sc + cx - dx
			fy := (float64(y)-cy)/sc + cy - dy
			g := gridPattern(fx, fy, w, h)
			// fine grain
			seed = seed*1664525 + 1013904223
			gr := float64(seed>>24)/255*0.08 - 0.04
			v := (g + gr) * level
			if v < 0 {
				v = 0
			}
			if v > 1 {
				v = 1
			}
			pix[y*w+x] = uint16(v * 65535)
		}
	}
	return pix
}

func gridPattern(x, y float64, w, h int) float64 {
	// radial spoke + square grid: plenty of edges at many orientations
	cx, cy := float64(w)/2, float64(h)/2
	dx, dy := x-cx, y-cy
	ang := math.Atan2(dy, dx)
	rad := math.Hypot(dx, dy)
	spoke := 0.5 + 0.5*math.Cos(ang*16)
	gx := math.Abs(math.Mod(x, 32) - 16)
	gy := math.Abs(math.Mod(y, 32) - 16)
	line := 0.0
	if gx < 1.2 || gy < 1.2 {
		line = 1
	}
	disk := 0.0
	if int(rad)%40 < 2 {
		disk = 1
	}
	bg := 0.35 + 0.15*math.Sin(x/40)*math.Cos(y/35)
	return clamp01(0.25*spoke + 0.45*line + 0.15*disk + 0.15*bg)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func (s *Sim) String() string { return fmt.Sprintf("sim %s", s.info.Model) }
