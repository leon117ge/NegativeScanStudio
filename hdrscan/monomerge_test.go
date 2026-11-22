package hdrscan

import (
	"math"
	"testing"

	"basetool/scanmeta"
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

func TestMergeMonoBracketSingleFramePassthrough(t *testing.T) {
	p := constPlane(16, 12, 12000)
	got, err := MergeMonoBracket([]MonoPlane{p}, []int{0}, ProcessOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.W != p.W || got.H != p.H || len(got.Pix) != len(p.Pix) {
		t.Fatalf("size %dx%d n=%d", got.W, got.H, len(got.Pix))
	}
	if got.Pix[0] != p.Pix[0] {
		t.Fatalf("pixel %d != %d", got.Pix[0], p.Pix[0])
	}
}

func TestMergeMonoBracketMergesExposurePair(t *testing.T) {
	const w, h = 48, 32
	ev0 := constPlane(w, h, 8000)
	ev2 := constPlane(w, h, 32000) // nominally +2 EV
	o := ProcessOptions{
		Merge:    DefaultOptions(),
		Software: "NegativeScan Studio test",
		Metadata: scanmeta.Metadata{Mode: "test", Encoding: "linear"},
	}
	o.Merge.Align = AlignOff
	got, err := MergeMonoBracket([]MonoPlane{ev0, ev2}, []int{0, 2}, o)
	if err != nil {
		t.Fatal(err)
	}
	if got.W != w || got.H != h || len(got.Pix) != w*h {
		t.Fatalf("merged size %dx%d n=%d", got.W, got.H, len(got.Pix))
	}
	var sum float64
	n := 0
	for _, v := range got.Pix {
		if v > 0 {
			sum += float64(v)
			n++
		}
	}
	if n == 0 {
		t.Fatal("merged plane is black")
	}
	mean := sum / float64(n)
	if mean < 1000 || mean > 60000 {
		t.Fatalf("merged mean %.0f out of a sane linear range", mean)
	}
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

func TestGrayToRGBReplicates(t *testing.T) {
	p := MonoPlane{W: 2, H: 1, Pix: []uint16{100, 200}}
	rgb := GrayToRGB(p)
	if len(rgb) != 6 || rgb[0] != 100 || rgb[1] != 100 || rgb[2] != 100 || rgb[3] != 200 {
		t.Fatalf("%v", rgb)
	}
}

func TestEVScale(t *testing.T) {
	if EVScale(0) != 1 || EVScale(2) != 4 || EVScale(3) != 8 {
		t.Fatalf("EVScale 0/2/3 = %v %v %v", EVScale(0), EVScale(2), EVScale(3))
	}
}

func TestMergeMonoBracketThreeEVSteps(t *testing.T) {
	if EVScale(0) != 1 || EVScale(2) != 4 || EVScale(3) != 8 {
		t.Fatalf("unexpected EVScale: 0=%v 2=%v 3=%v", EVScale(0), EVScale(2), EVScale(3))
	}
	const w, h = 64, 48
	mkFrame := func(scale float64) MonoPlane {
		p := make([]uint16, w*h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				// Half clear base (~8000 at +0EV), half dense negative (~1500 at +0EV)
				base := 8000.0
				if x >= w/2 {
					base = 1500.0
				}
				v := base * scale
				if v > 65535 {
					v = 65535
				}
				p[y*w+x] = uint16(v + 0.5)
			}
		}
		return MonoPlane{W: w, H: h, Pix: p}
	}
	f0 := mkFrame(EVScale(0))
	f2 := mkFrame(EVScale(2))
	f3 := mkFrame(EVScale(3))
	o := ProcessOptions{
		Merge:    DefaultOptions(),
		Software: "NegativeScan Studio test",
		Metadata: scanmeta.Metadata{Mode: "test", Encoding: "linear"},
	}
	o.Merge.Align = AlignOff
	o.Merge.MinSamples = 100
	got, err := MergeMonoBracket([]MonoPlane{f0, f2, f3}, []int{0, 2, 3}, o)
	if err != nil {
		t.Fatal(err)
	}
	if got.W != w || got.H != h || len(got.Pix) != w*h {
		t.Fatalf("merged size %dx%d n=%d", got.W, got.H, len(got.Pix))
	}
	// Clear base side (left) must stay near frame 0's 8000, dense side (right) near 1500.
	left := float64(got.Pix[(h/2)*w+(w/4)])
	right := float64(got.Pix[(h/2)*w+(3*w/4)])
	if math.Abs(left-8000) > 400 {
		t.Fatalf("merged clear-base sample %.0f, want ~8000", left)
	}
	if math.Abs(right-1500) > 200 {
		t.Fatalf("merged dense sample %.0f, want ~1500", right)
	}
}
