package hdrscan

import (
	"image"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// synthetic negative: grainy flat field, a soft dark blob ("shadow") and a
// thin curly dark fibre.
func hairScene(t *testing.T) (pix []uint16, w, h int, blob func(x, y int) bool, fibreD func(x, y int) float64) {
	w, h = 640, 420
	rng := rand.New(rand.NewSource(7))
	bx, by, br := 470.0, 300.0, 55.0
	blob = func(x, y int) bool { return math.Hypot(float64(x)-bx, float64(y)-by) < br+12 }
	// fibre: y = 150 + 25 sin(x/60), x in [80, 420] (curl radius ~140 px)
	fibreD = func(x, y int) float64 {
		best := 1e9
		for fx := 80.0; fx <= 420; fx += 0.5 {
			fy := 150 + 25*math.Sin(fx/60)
			if d := math.Hypot(float64(x)-fx, float64(y)-fy); d < best {
				best = d
			}
		}
		return best
	}
	// grain: white noise blurred (sigma 3 px) and scaled to 0.08 EV rms; film
	// grain in the user's scan is correlated the same way (lag-1/3/5 px
	// autocorrelation 0.91/0.63/0.41, rms ~0.11 EV incl. structure)
	grain := make([]float64, w*h)
	for i := range grain {
		grain[i] = rng.NormFloat64()
	}
	k := make([]float64, 19)
	for i := range k {
		d := float64(i - 9)
		k[i] = math.Exp(-d * d / 18)
	}
	tmp := make([]float64, w*h)
	for pass := 0; pass < 2; pass++ {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				s := 0.0
				for i, kv := range k {
					xx, yy := x, y
					if pass == 0 {
						xx = min(max(x+i-9, 0), w-1)
					} else {
						yy = min(max(y+i-9, 0), h-1)
					}
					s += kv * grain[yy*w+xx]
				}
				tmp[y*w+x] = s
			}
		}
		grain, tmp = tmp, grain
	}
	ss := 0.0
	for _, g := range grain {
		ss += g * g
	}
	gs := 0.08 / math.Sqrt(ss/float64(w*h))
	pix = make([]uint16, w*h*3)
	base := [3]float64{30000, 22000, 9000}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ev := gs * grain[y*w+x]
			ev -= 0.6 * math.Exp(-math.Pow(math.Hypot(float64(x)-bx, float64(y)-by)/br, 4)) // soft shadow
			if d := fibreD(x, y); d < 8 {
				ev -= 0.30 * math.Exp(-d*d/(2*2.0*2.0))
			}
			for c := 0; c < 3; c++ {
				pix[3*(y*w+x)+c] = uint16(math.Min(65535, base[c]*math.Exp2(ev)))
			}
		}
	}
	return
}

func TestRemoveHairSynthetic(t *testing.T) {
	pix, w, h, blob, fibreD := hairScene(t)
	orig := append([]uint16(nil), pix...)
	sel := image.Rect(60, 100, 560, 262) // fibre + the top of the shadow
	e, err := RemoveHair(pix, w, h, sel, DefaultHairOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Log(e)
	for i := range pix {
		if pix[i] != orig[i] {
			t.Fatal("RemoveHair modified the input")
		}
	}
	e.Apply(pix)
	onFibre, changedFibre, inBlob, far := 0, 0, 0, 0
	for i, gi := range e.Idx {
		_ = i
		x, y := int(gi)%w, int(gi)/w
		if !image.Pt(x, y).In(e.Rect) {
			t.Fatalf("pixel (%d,%d) changed outside the selection", x, y)
		}
		if blob(x, y) {
			inBlob++
		}
		// most changes within 14 px of the fibre; dark grain lines touching
		// it get healed too (with grain texture, so visually harmless) -
		// measured 21% on this scene, bound at 25%
		if fibreD(x, y) > 14 {
			far++
		}
	}
	for y := sel.Min.Y; y < sel.Max.Y; y++ {
		for x := 80; x <= 420; x++ {
			if fibreD(x, y) < 1 {
				onFibre++
				if pix[3*(y*w+x)+1] != orig[3*(y*w+x)+1] {
					changedFibre++
				}
			}
		}
	}
	t.Logf("%d of %d changed px more than 14 px from the fibre", far, len(e.Idx))
	if float64(far) > 0.25*float64(len(e.Idx)) {
		t.Errorf("%d of %d changed px more than 14 px from the fibre", far, len(e.Idx))
	}
	if inBlob != 0 {
		t.Errorf("%d shadow pixels changed", inBlob)
	}
	if float64(changedFibre) < 0.9*float64(onFibre) {
		t.Errorf("fibre core changed in %d of %d px", changedFibre, onFibre)
	}
	if e.DepthPre < 0.15 || e.DepthPost > 0.3*e.DepthPre {
		t.Errorf("depth %.3f -> %.3f EV", e.DepthPre, e.DepthPost)
	}
	e.Undo(pix)
	for i := range pix {
		if pix[i] != orig[i] {
			t.Fatal("Undo did not restore the image")
		}
	}
}

func TestRemoveHairNothing(t *testing.T) {
	pix, w, h, _, _ := hairScene(t)
	// grain only (top-left corner has no fibre)
	if _, err := RemoveHair(pix, w, h, image.Rect(0, 300, 200, 420), DefaultHairOptions()); err == nil {
		t.Error("grain-only selection should report no hair")
	}
	if _, err := RemoveHair(pix, w, h, image.Rect(10, 10, 20, 20), DefaultHairOptions()); err == nil {
		t.Error("tiny selection accepted")
	}
	o := DefaultHairOptions()
	o.MaxSide = 100
	if _, err := RemoveHair(pix, w, h, image.Rect(0, 0, 300, 300), o); err == nil {
		t.Error("oversized selection accepted")
	}
}

func TestHairSavePath(t *testing.T) {
	d := t.TempDir()
	p, in := HairSavePath(filepath.Join(d, "Roll_0001_HDR.tiff"))
	if !in || p != filepath.Join(d, "Roll_0001_HDR.tiff") {
		t.Errorf("HDR output: %s %v", p, in)
	}
	if _, in := HairSavePath(filepath.Join(d, "Roll_0001_HDR_2.tif")); !in {
		t.Error("numbered HDR not in place")
	}
	src := filepath.Join(d, "scan.tiff")
	os.WriteFile(src, []byte("x"), 0o644)
	p, in = HairSavePath(src)
	if in || p != filepath.Join(d, "scan_HDR.tiff") {
		t.Errorf("source TIFF: %s %v", p, in)
	}
	os.WriteFile(p, []byte("x"), 0o644)
	if p2, _ := HairSavePath(src); p2 != filepath.Join(d, "scan_HDR_2.tiff") {
		t.Errorf("existing name reused: %s", p2)
	}
	if s := HairSoftware("NSS v0.77 HDR, hair removal (1 selection, x)", "NSS v0.77", 3); s != "NSS v0.77 HDR, hair removal (3 selections, NSS v0.77)" {
		t.Error(s)
	}
}

func TestSaveHairTIFFRoundTrip(t *testing.T) {
	pix, w, h, _, _ := hairScene(t)
	d := t.TempDir()
	p := filepath.Join(d, "a_HDR.tiff")
	for _, lzw := range []bool{false, true} {
		ti := TIFFInfo{Make: "FUJIFILM", Model: "X-T3", Software: "NSS v0.77 HDR", LZW: lzw}
		if err := SaveHairTIFF(p, w, h, pix, ti); err != nil {
			t.Fatal(err)
		}
		got, err := ReadTIFFInfo(p)
		if err != nil || got != ti {
			t.Fatalf("tags %+v err %v", got, err)
		}
		pl, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		for i := range pix {
			if pl.Pix[i] != pix[i] {
				t.Fatal("pixels differ after save")
			}
		}
		if _, err := os.Stat(p + ".hairtmp"); err == nil {
			t.Error("temp file left")
		}
	}
}
