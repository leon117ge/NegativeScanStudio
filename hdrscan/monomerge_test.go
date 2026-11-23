package hdrscan

import (
	"math"
	"testing"
)

func constPlane(w, h int, v uint16) MonoPlane {
	p := make([]uint16, w*h)
	for i := range p {
		p[i] = v
	}
	// a darker square so merge / alignment have structure
	for y := h / 4; y < 3*h/4; y++ {
		for x := w / 4; x < 3*w/4; x++ {
			p[y*w+x] = v / 2
		}
	}
	return MonoPlane{W: w, H: h, Pix: p}
}

func TestScaleMonoForSimLightRespondsToCode(t *testing.T) {
	src := make([]uint16, 16)
	for i := range src {
		src[i] = 20000
	}
	lo := ScaleMonoForSimLight(src, 1, [3]bool{false, true, false}, [3]int{50, 20, 50})
	hi := ScaleMonoForSimLight(src, 1, [3]bool{false, true, false}, [3]int{50, 80, 50})
	off := ScaleMonoForSimLight(src, 1, [3]bool{true, false, true}, [3]int{80, 80, 80})
	if lo[0] >= hi[0] {
		t.Fatalf("higher light code should raise the sim frame: %d vs %d", lo[0], hi[0])
	}
	if off[0] != 0 {
		t.Fatalf("unlit channel should be 0, got %d", off[0])
	}
}

func TestLevelingImageMonoMedian(t *testing.T) {
	const w, h = 64, 48
	pix := make([]uint16, w*h)
	for i := range pix {
		pix[i] = 32768
	}
	im := LevelingImageMono(pix, w, h)
	if im.W == 0 || im.H == 0 {
		t.Fatal("empty image")
	}
	var sum float64
	for _, v := range im.V {
		sum += float64(v)
	}
	mean := sum / float64(len(im.V))
	if math.Abs(mean-0.5) > 0.01 {
		t.Fatalf("mean %v, want ~0.5", mean)
	}
}

func TestGrayToRGBReplicatesLegacy(t *testing.T) {
	p := MonoPlane{W: 2, H: 1, Pix: []uint16{100, 200}}
	rgb := grayToRGB(p)
	if len(rgb) != 6 || rgb[0] != 100 || rgb[1] != 100 || rgb[2] != 100 || rgb[3] != 200 {
		t.Fatalf("%v", rgb)
	}
}

func TestEVScale(t *testing.T) {
	if EVScale(0) != 1 || EVScale(2) != 4 || EVScale(3) != 8 {
		t.Fatalf("EVScale 0/2/3 = %v %v %v", EVScale(0), EVScale(2), EVScale(3))
	}
}
