package hdrscan

import (
	"math/rand"
	"testing"
)

// synthetic probe: textured image, a 4-cell orange rebate strip at the top,
// clipped sprocket holes inside the strip and a bright neutral (unclipped)
// leak patch that must be rejected.
func TestFindBaseThinStripWithHolesAndLeak(t *testing.T) {
	bw, bh := 200, 130
	rgb := make([]float32, bw*bh*3)
	clip := make([]bool, bw*bh)
	rnd := rand.New(rand.NewSource(1))
	set := func(x, y int, r, g, b float32) {
		i := y*bw + x
		rgb[3*i], rgb[3*i+1], rgb[3*i+2] = r, g, b
	}
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			v := 0.1 + 0.3*rnd.Float32()
			set(x, y, v, v*0.8, v*0.3)
		}
	}
	for y := 2; y < 7; y++ { // rebate strip
		for x := 0; x < bw; x++ {
			set(x, y, 0.70, 0.52, 0.16)
		}
	}
	for hx := 10; hx < bw; hx += 30 { // sprocket holes: clipped
		for y := 3; y < 6; y++ {
			for x := hx; x < hx+5; x++ {
				set(x, y, 1, 1, 1)
				clip[y*bw+x] = true
			}
		}
	}
	for y := 60; y < 80; y++ { // neutral bright leak, unclipped
		for x := 150; x < 180; x++ {
			set(x, y, 0.80, 0.82, 0.78)
		}
	}
	fb, ok := findBase(bw, bh, rgb, clip)
	if !ok {
		t.Fatal("no base found")
	}
	t.Logf("%+v", fb)
	if fb.Y0 < 2 || fb.Y1 > 7 {
		t.Fatalf("picked rows %d-%d, want inside the rebate strip 2-7", fb.Y0, fb.Y1)
	}
	if !fb.Orange {
		t.Fatal("expected orange base")
	}
}

func TestFindBaseNoneFallsBack(t *testing.T) {
	bw, bh := 100, 80
	rgb := make([]float32, bw*bh*3)
	clip := make([]bool, bw*bh)
	rnd := rand.New(rand.NewSource(2))
	for i := 0; i < bw*bh; i++ {
		v := 0.1 + 0.5*rnd.Float32()
		rgb[3*i], rgb[3*i+1], rgb[3*i+2] = v, v*0.8, v*0.3
	}
	if _, ok := findBase(bw, bh, rgb, clip); ok {
		t.Fatal("found a base in pure texture")
	}
}
