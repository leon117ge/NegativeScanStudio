package hdrscan

import (
	"basetool/lightlevel"
	"basetool/rawconv"
	"fmt"
	"math"
	"path/filepath"
	"testing"
)

func TestLevelingControlAndTIFFUseIdenticalSamples(t *testing.T) {
	const w, h = 640, 480
	dir := t.TempDir()
	area := lightlevel.Rect{X0: .173, Y0: .219, X1: .736, Y1: .827}
	var raws [3][]string
	var expected [3]float64
	for c := 0; c < 3; c++ {
		pixels := make([]uint16, w*h*3)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				v := .41 + .12*float64(x)/w + .03*float64(y)/h + .003*float64(c)
				if (x/16+y/16)%23 == 0 {
					v = 1
				}
				if (x/16+y/16)%29 == 0 {
					v = .01
				}
				for k := 0; k < 3; k++ {
					value := v
					if k != c {
						value *= .1
					}
					pixels[3*(y*w+x)+k] = uint16(value * 65535)
				}
			}
		}
		path := filepath.Join(dir, fmt.Sprintf("channel_%d.tiff", c))
		if err := rawconv.WriteTIFF16(path, w, h, pixels, false, "test", "test", "test"); err != nil {
			t.Fatal(err)
		}
		raws[c] = []string{path}
		p, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		rd, err := lightlevel.Measure(LevelingImage(p, c), area)
		p.Free()
		if err != nil {
			t.Fatal(err)
		}
		if rd.BaseClipped() {
			t.Fatal("isolated bright pixels clipped a clean base")
		}
		expected[c] = rd.Level
	}
	res, err := ProcessTrichrome(raws, nil, ProcessOptions{OutDir: dir, MeasureLevelingBase: true, BaseNorm: Rect{area.X0, area.Y0, area.X1, area.Y1}})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseErr != "" || res.BaseN < 4 {
		t.Fatalf("TIFF verification: %s, samples %d", res.BaseErr, res.BaseN)
	}
	for c := 0; c < 3; c++ {
		if math.Abs(res.Base[c]-expected[c]) > 1e-12 {
			t.Fatalf("different pixels or statistic: control %v TIFF %v", expected, res.Base)
		}
	}
}
func TestLevelingTIFFRejectsActuallyClippedBase(t *testing.T) {
	const w, h = 128, 128
	p := &Plane{W: w, H: h, Pix: make([]uint16, w*h*3)}
	for i := range p.Pix {
		p.Pix[i] = 65535
	}
	res := &TriResult{}
	measureTriBase(res, p.Pix, p, ProcessOptions{BaseNorm: Rect{.1, .1, .9, .9}})
	if res.BaseErr == "" {
		t.Fatal("clipped TIFF accepted")
	}
}
