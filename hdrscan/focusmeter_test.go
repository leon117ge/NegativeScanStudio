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
