package hdrscan

import (
	"math"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

// fullBase builds a probe the size of an X-T3 frame at bin 8 (750x500
// cells) filled with clear orange base under the user's light (R .40 G .50
// B .20): vignetting/light falloff of evEdge EV in the corners, an X-Trans
// phase pattern (24 px period = 3 cells), PDAF rows, random noise, dust and a
// few hot (clipped) pixels.
func fullBase(seed int64, evEdge float64) (int, int, []float32, []bool) {
	bw, bh := 750, 500
	rgb := make([]float32, bw*bh*3)
	clip := make([]bool, bw*bh)
	rnd := rand.New(rand.NewSource(seed))
	base := [3]float64{0.40, 0.50, 0.20}
	var phase [3][3][3]float64
	for a := 0; a < 3; a++ {
		for b := 0; b < 3; b++ {
			for c := 0; c < 3; c++ {
				phase[a][b][c] = 1 + 0.04*(rnd.Float64()-0.5)*2
			}
		}
	}
	cx, cy := float64(bw)/2, float64(bh)/2
	rmax := math.Hypot(cx, cy)
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			r := math.Hypot(float64(x)-cx, float64(y)-cy) / rmax
			v := math.Pow(2, -evEdge*r*r)
			i := y*bw + x
			for c := 0; c < 3; c++ {
				f := base[c] * v * phase[x%3][y%3][c] * (1 + 0.015*rnd.NormFloat64())
				if y%12 == 5 && c == 1 {
					f *= 0.98 // PDAF row
				}
				rgb[3*i+c] = float32(f)
			}
		}
	}
	for k := 0; k < 40; k++ { // dust
		dx, dy := rnd.Intn(bw-4), rnd.Intn(bh-4)
		for y := dy; y < dy+3; y++ {
			for x := dx; x < dx+3; x++ {
				i := y*bw + x
				for c := 0; c < 3; c++ {
					rgb[3*i+c] *= 0.5
				}
			}
		}
	}
	for k := 0; k < 10; k++ { // hot pixels
		i := rnd.Intn(bw * bh)
		clip[i] = true
		rgb[3*i] = 1
	}
	return bw, bh, rgb, clip
}

func clusterMedian(rgb []float32, cells []int, c int) float64 {
	v := make([]float64, len(cells))
	for k, i := range cells {
		v[k] = float64(rgb[3*i+c])
	}
	sort.Float64s(v)
	return v[len(v)/2]
}

func TestFindBaseFullFrame(t *testing.T) {
	for _, ev := range []float64{0, 0.5, 1.0} {
		bw, bh, rgb, clip := fullBase(7, ev)
		fb, ok := findBase(bw, bh, rgb, clip)
		if !ok {
			t.Fatalf("falloff %.1f EV: no base found in a frame of pure base", ev)
		}
		g := clusterMedian(rgb, fb.Cells, 1)
		t.Logf("falloff %.1f EV: %s; coverage %.1f%%, G median %.3f, rect %d,%d-%d,%d", ev, fb.Where, 100*fb.Coverage, g, fb.X0, fb.Y0, fb.X1, fb.Y1)
		if !strings.HasPrefix(fb.Where, "large clear base area") {
			t.Fatalf("falloff %.1f EV: region type %q", ev, fb.Where)
		}
		want := 0.8 // within 0.4 EV of the brightest base: everything up to 0.5 EV falloff
		if ev > 0.5 {
			want = 0.5 // 1 EV corners: the brighter centre is metered
		}
		if fb.Coverage < want {
			t.Fatalf("falloff %.1f EV: only %.0f%% of the frame recognised as base", ev, 100*fb.Coverage)
		}
		if !fb.Orange {
			t.Fatal("not orange")
		}
		if g > 0.51 || g < 0.5*math.Pow(2, -ev) {
			t.Fatalf("median G %.3f out of range", g)
		}
	}
}

// Base on the left 30% of the frame, a dense textured image on the rest.
func TestFindBasePartialLargeArea(t *testing.T) {
	bw, bh, rgb, clip := fullBase(8, 0.3)
	rnd := rand.New(rand.NewSource(9))
	for y := 0; y < bh; y++ {
		for x := bw * 3 / 10; x < bw; x++ {
			i := y*bw + x
			v := float32(0.05 + 0.25*rnd.Float64())
			rgb[3*i], rgb[3*i+1], rgb[3*i+2] = v*0.8, v, v*0.4
		}
	}
	fb, ok := findBase(bw, bh, rgb, clip)
	if !ok {
		t.Fatal("no base")
	}
	t.Logf("%s; coverage %.1f%%", fb.Where, 100*fb.Coverage)
	if fb.BX1 > bw*3/10+1 || fb.Coverage < 0.2 || !strings.HasPrefix(fb.Where, "large clear base area") {
		t.Fatalf("wrong region: x %d-%d, %s", fb.BX0, fb.BX1, fb.Where)
	}
}

// A full frame of base with a bright neutral light leak patch: the leak must
// not be chosen.
func TestFindBaseFullFrameWithLeak(t *testing.T) {
	bw, bh, rgb, clip := fullBase(10, 0.5)
	for y := 100; y < 200; y++ {
		for x := 100; x < 250; x++ {
			i := y*bw + x
			rgb[3*i], rgb[3*i+1], rgb[3*i+2] = 0.70, 0.72, 0.66
		}
	}
	fb, ok := findBase(bw, bh, rgb, clip)
	if !ok {
		t.Fatal("no base")
	}
	t.Logf("%s", fb.Where)
	if g := clusterMedian(rgb, fb.Cells, 1); g > 0.55 || !fb.Orange {
		t.Fatalf("picked the leak: G %.3f", g)
	}
}
