package colorsci

import (
	"fmt"
	"math"
	"os"
	"testing"

	"golang.org/x/image/tiff"
)

func TestKeepNeutral(t *testing.T) {
	m := Mat3{{1.5223, -0.5778, 0.0555}, {0, 1.2547, -0.2547}, {0, 0, 1}}
	k := KeepNeutral(m, 0.8072, 1.1045)
	g := [3]float64{0.8072, 1, 1.1045}
	for i := 0; i < 3; i++ {
		v := k[i][0]*g[0] + k[i][1]*g[1] + k[i][2]*g[2]
		if math.Abs(v-g[i]) > 1e-12 {
			t.Fatalf("row %d: %v want %v", i, v, g[i])
		}
	}
}

func TestNeutralAxisSynthetic(t *testing.T) {
	var d [][3]float64
	for i := 0; i < 5000; i++ {
		x := 0.06 + 1.2*float64(i)/5000
		d = append(d, [3]float64{0.83 * x, x, 1.12 * x})
		w := 0.3 * math.Sin(float64(i)) // off-axis colour
		d = append(d, [3]float64{(0.83 + w) * x, x, (1.12 - w) * x})
	}
	ax, _, err := NeutralAxis(d)
	if err != nil || math.Abs(ax[0]-0.83) > 1e-9 || math.Abs(ax[1]-1.12) > 1e-9 {
		t.Fatalf("axis %v err %v", ax, err)
	}
}

// NEUTRAL_TIFF=user_hdr.tiff NEUTRAL_BASE=r,g,b: compare with neutral_est.py.
func TestNeutralAxisTIFF(t *testing.T) {
	p := os.Getenv("NEUTRAL_TIFF")
	if p == "" {
		t.Skip("NEUTRAL_TIFF not set")
	}
	var base [3]float64
	for i, s := range splitComma(os.Getenv("NEUTRAL_BASE")) {
		base[i] = s
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	img, err := tiff.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	W, H := b.Dx(), b.Dy()
	mx, my := int(float64(W)*0.04), int(float64(H)*0.04)
	step := 1
	for ((W-2*mx)/step)*((H-2*my)/step) > 700000 {
		step++
	}
	var d [][3]float64
	for y := my; y < H-my; y += step {
		for x := mx; x < W-mx; x += step {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			v := [3]float64{float64(r) / 65535, float64(g) / 65535, float64(bl) / 65535}
			d = append(d, [3]float64{math.Log10(base[0] / math.Max(v[0], 1e-6)), math.Log10(base[1] / math.Max(v[1], 1e-6)), math.Log10(base[2] / math.Max(v[2], 1e-6))})
		}
	}
	ax, n, err := NeutralAxis(d)
	t.Logf("axis R/G %.6f B/G %.6f n %d step %d err %v", ax[0], ax[1], n, step, err)
}

func splitComma(s string) []float64 {
	var out []float64
	cur := ""
	for _, c := range s + "," {
		if c == ',' {
			var v float64
			if _, err := fmt.Sscan(cur, &v); err == nil {
				out = append(out, v)
			}
			cur = ""
			continue
		}
		cur += string(c)
	}
	return out
}
