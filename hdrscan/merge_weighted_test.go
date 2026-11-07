package hdrscan

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// truthRamp is synth's frame 0 without noise.
func truthRamp(w, h int, lo, hi float64) []float64 {
	col := [3]float64{1, 0.7, 0.25}
	t := make([]float64, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := lo * math.Pow(hi/lo, float64(x)/float64(w-1)) * 65535
			for c := 0; c < 3; c++ {
				t[(y*w+x)*3+c] = v * col[c]
			}
		}
	}
	return t
}

// colErr returns the RMS relative error and the largest |mean relative
// error| of any 8-column strip (a seam or band shows up there), over
// columns [x0, x1).
func colErr(out []uint16, truth []float64, w, h, x0, x1 int) (rms, band float64) {
	var s float64
	n := 0
	for xs := x0; xs+8 <= x1; xs += 8 {
		var m float64
		k := 0
		for y := 0; y < h; y++ {
			for x := xs; x < xs+8; x++ {
				for c := 0; c < 3; c++ {
					i := (y*w+x)*3 + c
					e := (float64(out[i]) - truth[i]) / truth[i]
					s += e * e
					m += e
					n++
					k++
				}
			}
		}
		band = math.Max(band, math.Abs(m/float64(k)))
	}
	return math.Sqrt(s / float64(n)), band
}

func TestWeightedRGB(t *testing.T) {
	const w, h = 1600, 300
	tr := truthRamp(w, h, 0.045, 0.979)
	fr := synth(w, h, 0.045, 0.979, []float64{1, 4.05, 16.2}, 11)
	res := map[int][2]float64{}
	for _, m := range []int{MergeClassic, MergeWeighted} {
		o := DefaultOptions()
		o.Exposure = []float64{1, 4, 16}
		o.BaseLevel = -1
		o.Method = m
		out, sts, err := MergeN(fr, o)
		if err != nil {
			t.Fatal(err)
		}
		for _, st := range sts {
			t.Logf("%s: %s", MethodName(m), st)
		}
		// Dark half: both brighter frames contribute there.
		rms, band := colErr(out, tr, w, h, 0, w/2)
		res[m] = [2]float64{rms, band}
		t.Logf("%s: dark half rms %.3f%%, worst strip bias %.3f%%", MethodName(m), 100*rms, 100*band)
	}
	if res[MergeWeighted][0] > res[MergeClassic][0] {
		t.Errorf("weighted noise %.4f > classic %.4f", res[MergeWeighted][0], res[MergeClassic][0])
	}
	if res[MergeWeighted][1] > 0.003 {
		t.Errorf("weighted strip bias %.3f%% (banding)", 100*res[MergeWeighted][1])
	}
}

func TestWeightedKeepsBase(t *testing.T) {
	const w, h = 800, 200
	fr := synth(w, h, 0.045, 0.6, []float64{1, 4.05}, 3)
	o := DefaultOptions()
	o.Exposure = []float64{1, 4}
	o.Method = MergeWeighted
	L := 0.5 * 65535
	o.BaseLevel = L
	out, _, err := MergeN(fr, o)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < w*h; i++ {
		f0 := fr[0].Pix[3*i : 3*i+3]
		if float64(max3(f0[0], f0[1], f0[2])) >= weightedProtectHi*L {
			if out[3*i] != f0[0] || out[3*i+1] != f0[1] || out[3*i+2] != f0[2] {
				t.Fatalf("pixel %d near the base level differs from frame 0", i)
			}
		}
	}
}

// TestWeightedRaw runs the sensor-mosaic path on synthetic RAF brackets
// (cmd/synth) when HDR_SYNTH_DIR points at them.
func TestWeightedRaw(t *testing.T) {
	dir := os.Getenv("HDR_SYNTH_DIR")
	if dir == "" {
		t.Skip("HDR_SYNTH_DIR not set")
	}
	var fr []*Plane
	for _, n := range []string{"0EV", "+2EV"} {
		p, err := Load(filepath.Join(dir, "SIM_0001_"+n+".RAF"))
		if err != nil {
			t.Fatal(err)
		}
		defer p.Free()
		fr = append(fr, p)
	}
	o := DefaultOptions()
	o.Exposure = []float64{1, 4}
	o.Method = MergeWeighted
	out, sts, err := MergeN(fr, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(sts[0])
	if len(out) != len(fr[0].Pix) {
		t.Fatal("size")
	}
}
