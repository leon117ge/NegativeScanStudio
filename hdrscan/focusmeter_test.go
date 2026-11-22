package hdrscan

import "testing"

func TestMeasureFocusPrefersSharp(t *testing.T) {
	const w, h = 64, 64
	sharp := synthPattern(w, h)
	blur := blurSeparable(sharp, w, h, 1.6)
	a := MeasureFocus(sharp, w, h)
	b := MeasureFocus(blur, w, h)
	if a.Combined <= b.Combined {
		t.Fatalf("sharp %v vs blur %v", a, b)
	}
	if a.Laplacian <= b.Laplacian {
		t.Fatalf("laplacian did not drop: %.4f vs %.4f", a.Laplacian, b.Laplacian)
	}
}

func TestPeakHold(t *testing.T) {
	var p PeakHold
	p.Update(1)
	p.Update(3)
	p.Update(2)
	if p.Peak != 3 {
		t.Fatalf("peak %v", p.Peak)
	}
	p.Reset()
	if p.Have {
		t.Fatal("reset")
	}
}

func TestCropGray(t *testing.T) {
	p := []float64{
		0, 1, 2,
		3, 4, 5,
		6, 7, 8,
	}
	c, cw, ch := CropGray(p, 3, 3, 1, 1, 3, 3)
	if cw != 2 || ch != 2 || c[0] != 4 || c[3] != 8 {
		t.Fatalf("got %v %dx%d", c, cw, ch)
	}
}

func TestCropGray16ToFloat64(t *testing.T) {
	p := []uint16{
		0, 65535, 0,
		32768, 16384, 8192,
	}
	c, cw, ch := CropGray16ToFloat64(p, 3, 2, 1, 0, 3, 2)
	if cw != 2 || ch != 2 {
		t.Fatalf("size %dx%d", cw, ch)
	}
	if c[0] != 1 || c[2] < 0.24 || c[2] > 0.26 {
		t.Fatalf("vals %v", c)
	}
}
