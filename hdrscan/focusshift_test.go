package hdrscan

import (
	"math"
	"math/rand"
	"strings"
	"testing"
)

// texture is a band-limited analytic pattern, so a warped copy can be
// rendered exactly instead of being interpolated.
type texture struct {
	fx, fy, ph, amp []float64
}

func newTexture(seed int64) texture {
	rng := rand.New(rand.NewSource(seed))
	var t texture
	for i := 0; i < 60; i++ {
		f := 0.015 + 0.16*rng.Float64()
		a := 2 * math.Pi * rng.Float64()
		t.fx = append(t.fx, f*math.Cos(a))
		t.fy = append(t.fy, f*math.Sin(a))
		t.ph = append(t.ph, 2*math.Pi*rng.Float64())
		t.amp = append(t.amp, 0.5+rng.Float64())
	}
	return t
}

func (t texture) at(x, y float64) float64 {
	var s float64
	for i := range t.fx {
		s += t.amp[i] * math.Cos(2*math.Pi*(t.fx[i]*x+t.fy[i]*y)+t.ph[i])
	}
	return 20000 + 900*s
}

// render draws the texture as seen by a channel whose features sit at
// p + m.Disp(p) relative to green (zero model = green itself).
func (t texture) render(w, h int, m RadialCA, gain float64, noise float64, seed int64) []float64 {
	rng := rand.New(rand.NewSource(seed))
	out := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := m.Disp(float64(x), float64(y))
			out[y*w+x] = gain*t.at(float64(x)-dx, float64(y)-dy) + noise*rng.NormFloat64()
		}
	}
	return out
}

func testModel(w, h int, tx, ty, a1, a3 float64) RadialCA {
	return RadialCA{TX: tx, TY: ty, AX1: a1, AX3: a3, AY1: a1, AY3: a3, CX: float64(w-1) / 2, CY: float64(h-1) / 2,
		RN: math.Hypot(float64(w-1)/2, float64(h-1)/2)}
}

func cornerRMS(a, b []float64, w, h, n int) float64 {
	var s float64
	c := 0
	for _, o := range [][2]int{{8, 8}, {w - 8 - n, 8}, {8, h - 8 - n}, {w - 8 - n, h - 8 - n}} {
		for y := o[1]; y < o[1]+n; y++ {
			for x := o[0]; x < o[0]+n; x++ {
				d := a[y*w+x] - b[y*w+x]
				s += d * d
				c++
			}
		}
	}
	return math.Sqrt(s / float64(c))
}

func TestRadialCARecovery(t *testing.T) {
	const w, h = 1024, 768
	tex := newTexture(1)
	g := tex.render(w, h, RadialCA{}, 1, 30, 11)
	want := testModel(w, h, 0.3, -0.2, -1.1, -0.6)
	b := tex.render(w, h, want, 0.8, 30, 12)
	gCopy := append([]float64(nil), g...)
	_, gg, bb, rep := CorrectMonoCA(g, g, b, w, h)
	for i := range gCopy {
		if gg[i] != gCopy[i] {
			t.Fatal("green changed")
		}
	}
	m := rep.B.Model
	t.Logf("report: %s", rep.String())
	if !rep.B.Applied {
		t.Fatalf("B not applied: %s", rep.B.Reason)
	}
	if math.Abs(m.TX-want.TX) > 0.06 || math.Abs(m.TY-want.TY) > 0.06 {
		t.Errorf("translation %.3f,%.3f want %.3f,%.3f", m.TX, m.TY, want.TX, want.TY)
	}
	if math.Abs((m.AX1+m.AX3)-(want.AX1+want.AX3)) > 0.1 || math.Abs((m.AY1+m.AY3)-(want.AY1+want.AY3)) > 0.1 {
		t.Errorf("corner radial x %.3f y %.3f want %.3f", m.AX1+m.AX3, m.AY1+m.AY3, want.AX1+want.AX3)
	}
	if math.Abs(rep.B.MaxCorner-want.MaxCorner(w, h)) > 0.1 {
		t.Errorf("max corner %.3f want %.3f", rep.B.MaxCorner, want.MaxCorner(w, h))
	}
	// Corner registration: the corrected plane should match green (after gain).
	ref := make([]float64, len(g))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ref[y*w+x] = 0.8 * tex.at(float64(x), float64(y))
		}
	}
	before := cornerRMS(b, ref, w, h, 96)
	after := cornerRMS(bb, ref, w, h, 96)
	if after > before*0.35 {
		t.Errorf("corner RMS vs truth %.1f -> %.1f, expected a large drop", before, after)
	}
	for _, rc := range rep.B.Regions {
		if rc.Sharp < caSharpGuard {
			t.Errorf("%s sharpness %.3f", rc.Name, rc.Sharp)
		}
	}
}

func TestCAIdentityOnNoCA(t *testing.T) {
	const w, h = 1024, 768
	tex := newTexture(2)
	g := tex.render(w, h, RadialCA{}, 1, 30, 21)
	r := tex.render(w, h, RadialCA{}, 0.7, 30, 22)
	b := tex.render(w, h, RadialCA{}, 1.2, 30, 23)
	rr, _, bb, rep := CorrectMonoCA(r, g, b, w, h)
	t.Logf("report: %s", rep.String())
	for i := range r {
		if rr[i] != r[i] || bb[i] != b[i] {
			t.Fatalf("pixel %d changed with no CA", i)
		}
	}
	if rep.R.Applied || rep.B.Applied {
		t.Fatal("no-CA input should not be resampled")
	}
	if rep.R.MaxCorner > caBlendLo || rep.B.MaxCorner > caBlendLo {
		t.Errorf("no-CA fit too large: R %.3f B %.3f", rep.R.MaxCorner, rep.B.MaxCorner)
	}
}

func TestCANoSharpeningAndUntouchedCentre(t *testing.T) {
	const w, h = 1024, 768
	tex := newTexture(3)
	g := tex.render(w, h, RadialCA{}, 1, 60, 31)
	// Pure radial (no translation): the centre must stay bit-identical.
	want := testModel(w, h, 0, 0, -1.4, -0.4)
	b := tex.render(w, h, want, 1, 60, 32)
	_, _, bb, rep := CorrectMonoCA(g, g, b, w, h)
	if !rep.B.Applied {
		t.Fatalf("B not applied: %s", rep.B.Reason)
	}
	cx, cy := w/2, h/2
	for y := cy - 20; y < cy+20; y++ {
		for x := cx - 20; x < cx+20; x++ {
			if bb[y*w+x] != b[y*w+x] {
				t.Fatalf("centre pixel %d,%d changed", x, y)
			}
		}
	}
	// No sharpening: gradient energy may only stay or drop slightly.
	tb := tenengrad(b, w, h)
	ta := tenengrad(bb, w, h)
	if ta > tb*1.005 {
		t.Errorf("correction sharpened: Tenengrad %.1f -> %.1f", tb, ta)
	}
	if ta < tb*0.97 {
		t.Errorf("correction blurred too much: Tenengrad %.1f -> %.1f", tb, ta)
	}
}

func TestSincIntegerShiftIsExact(t *testing.T) {
	ksOnce.Do(ksInit)
	const w, h = 32, 24
	src := newTexture(4).render(w, h, RadialCA{}, 1, 0, 0)
	for y := 4; y < h-4; y++ {
		for x := 4; x < w-4; x++ {
			v := sampleSinc(src, w, h, float64(x+1), float64(y-1))
			if math.Abs(v-src[(y-1)*w+x+1]) > 1e-9 {
				t.Fatalf("integer sample off at %d,%d: %v vs %v", x, y, v, src[(y-1)*w+x+1])
			}
		}
	}
}

func TestFocusShiftReportStrings(t *testing.T) {
	if s := (FocusShiftReport{}).String(); s != "CA correction off" {
		t.Fatal(s)
	}
	rep := FocusShiftReport{Enabled: true}
	rep.R = ChannelCA{Calculated: true, Applied: true, Model: testModel(100, 100, 0.1, 0, -1, -0.5), MaxCorner: 1.6, Tiles: 77, Used: 70}
	rep.B.Reason = "only 3 of 77 tiles had enough texture"
	s := rep.String()
	for _, want := range []string{"windowed-sinc", "no sharpening", "R applied", "max corner 1.60 px", "B identity"} {
		if !strings.Contains(s, want) {
			t.Errorf("report %q missing %q", s, want)
		}
	}
	for _, r := range s {
		if r > 127 {
			t.Fatalf("non-ASCII in report: %q", s)
		}
	}
}

// synthPattern is a high-contrast grid plus spokes, easy to register.
func synthPattern(w, h int) []float64 {
	p := make([]float64, w*h)
	cx, cy := float64(w)/2, float64(h)/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := 0.35
			if x%12 < 2 || y%12 < 2 {
				v = 0.95
			}
			dx, dy := float64(x)-cx, float64(y)-cy
			if math.Hypot(dx, dy) < 8 {
				v = 0.1
			}
			if (x+y)%18 < 2 {
				v = 0.15
			}
			// a few filled squares for scale cues
			if x > w/3 && x < w/3+10 && y > h/3 && y < h/3+10 {
				v = 0.05
			}
			p[y*w+x] = v
		}
	}
	return p
}
