package cropper

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestSynthDetect(t *testing.T) {
	white := [3]float64{0.94, 0.92, 0.88}
	black := [3]float64{0.03, 0.03, 0.035}
	cases := []SynthSpec{
		{W: 1800, H: 1200, Tilt: 0.7, FrameW: 1584, FrameH: 1056, Rebate: white, RebateW: 40, Holder: black, Bits: 16, Seed: 1},
		{W: 1800, H: 1200, Tilt: -1.8, FrameW: 1620, FrameH: 1080, Rebate: black, Holder: black, Bits: 16, Seed: 2},
		{W: 1200, H: 1800, Tilt: 0.7, FrameW: 1056, FrameH: 1584, Rebate: white, Holder: white, Bits: 8, Seed: 3},
		{W: 1200, H: 1800, Tilt: -1.8, FrameW: 1068, FrameH: 1602, Rebate: black, Holder: black, Bits: 16, Seed: 4},
	}
	for _, c := range cases {
		im, inside := Synth(c)
		p := MakeProxy(im, ProxySide)
		det := Detect(p)
		if !det.OK || math.Abs(det.Angle-c.Tilt) > 0.05 {
			t.Fatalf("tilt %.2f: got %.3f ok=%v (%s)", c.Tilt, det.Angle, det.OK, det.Message)
		}
		r, _ := AutoRect(im.W, im.H, det.Angle, &det, p.Scale, AspectAuto, 0.01)
		x0, y0, w, h := IntRect(r, im.W, im.H)
		wantLand := c.FrameW > c.FrameH
		if (w > h) != wantLand || math.Abs(float64(w)/float64(h)-map[bool]float64{true: 1.5, false: 2.0 / 3}[wantLand]) > 0.01 {
			t.Fatalf("aspect %dx%d", w, h)
		}
		for _, q := range [][2]float64{{0, 0}, {float64(w), 0}, {0, float64(h)}, {float64(w), float64(h)}} {
			sx, sy := RotInv(det.Angle, float64(im.W), float64(im.H), float64(x0)+q[0], float64(y0)+q[1])
			if inside(sx, sy) < 0 {
				t.Fatalf("crop corner outside picture (tilt %.2f)", c.Tilt)
			}
		}
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	for _, bits := range []int{8, 16} {
		for _, f := range []string{"a.tif", "a.png", "a.jpg"} {
			if bits == 16 && f == "a.jpg" {
				continue
			}
			im, _ := Synth(SynthSpec{W: 300, H: 200, FrameW: 260, FrameH: 170, Rebate: [3]float64{1, 1, 1}, Bits: bits, Seed: 9})
			im.Format = FormatOf(f)
			for _, lzw := range []bool{false, true} {
				path := filepath.Join(dir, f)
				if err := Save(path, im, SaveOptions{LZW: lzw, JPEGQuality: 100}); err != nil {
					t.Fatal(err)
				}
				back, err := Load(path)
				if err != nil {
					t.Fatal(f, err)
				}
				if back.W != im.W || back.H != im.H || back.Bits != bits {
					t.Fatalf("%s %d: got %dx%d %d-bit", f, bits, back.W, back.H, back.Bits)
				}
				if im.Format != "jpeg" {
					for i := range im.Pix {
						want := im.Pix[i]
						if bits == 8 {
							want = uint16(to8(want)) * 257
						}
						if back.Pix[i] != want {
							t.Fatalf("%s %d-bit lzw=%v: pixel %d %d != %d", f, bits, lzw, i, back.Pix[i], want)
						}
					}
				}
				os.Remove(path)
			}
		}
	}
}

func TestApplyZeroIsCopy(t *testing.T) {
	im, _ := Synth(SynthSpec{W: 120, H: 80, FrameW: 100, FrameH: 66, Bits: 16, Seed: 3})
	out := Apply(im, 0, Rect{10, 5, 110, 71}, InterpLanczos)
	for y := 0; y < out.H; y++ {
		for x := 0; x < out.W*3; x++ {
			if out.Pix[y*out.W*3+x] != im.Pix[(y+5)*im.W*3+30+x] {
				t.Fatal("not a copy")
			}
		}
	}
}
