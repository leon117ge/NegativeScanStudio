package main

import (
	"image"
	"testing"

	"basetool/hdrscan"
)

func TestFitRect(t *testing.T) {
	if r := fitRect(6000, 4000, 900, 900); r != image.Rect(0, 150, 900, 750) {
		t.Error(r)
	}
	if r := fitRect(100, 400, 400, 400); r != image.Rect(150, 0, 250, 400) {
		t.Error(r)
	}
	if !fitRect(0, 10, 10, 10).Empty() {
		t.Error("empty area")
	}
}

func TestHairPreviewMask(t *testing.T) {
	w, h := 40, 20
	pix := make([]uint16, w*h*3)
	for i := range pix {
		pix[i] = 30000
	}
	lut := hairLUT(pix, w, h)
	e := &hdrscan.HairEdit{Idx: []int32{int32(5*w + 10)}}
	img := hairPreview(pix, w, h, image.Rect(0, 0, w, h), w, h, lut, []*hdrscan.HairEdit{e}, true)
	if c := img.RGBAAt(10, 5); c.R != 255 || c.G != 40 {
		t.Errorf("mask pixel %v", c)
	}
	if c := img.RGBAAt(11, 5); c.R != c.G || c.R != 255 {
		t.Errorf("base pixel %v (brightest = 255 grey)", c)
	}
	img = hairPreview(pix, w, h, image.Rect(0, 0, w, h), w, h, lut, []*hdrscan.HairEdit{e}, false)
	if c := img.RGBAAt(10, 5); c.G != 255 {
		t.Errorf("mask off: %v", c)
	}
	// zoomed viewport: image (20,10) lands at the view origin
	img = hairPreview(pix, w, h, image.Rect(10, 5, 20, 10), 20, 10, lut, []*hdrscan.HairEdit{e}, true)
	if c := img.RGBAAt(0, 0); c.R != 255 || c.G != 40 {
		t.Errorf("zoomed mask %v", c)
	}
}
