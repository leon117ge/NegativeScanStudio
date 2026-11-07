package hdrscan

import (
	"image"
	"testing"

	"basetool/basestat"
)

func TestResolveTriBasePrefersNorm(t *testing.T) {
	bounds := image.Rect(0, 0, 1000, 800)
	o := ProcessOptions{
		BaseNorm:   Rect{0.1, 0.2, 0.3, 0.4},
		BaseArea:   image.Rect(0, 0, 900, 700),
		BaseRegion: &basestat.Region{Rect: image.Rect(5, 5, 800, 600)},
	}
	reg, src := resolveTriBase(bounds, o, func() (image.Rectangle, bool) {
		t.Fatal("auto FindBase must not run when a rectangle is set")
		return image.Rectangle{}, false
	})
	if src != "rectangle" {
		t.Fatalf("src=%q", src)
	}
	want := image.Rect(100, 160, 300, 320)
	if reg.Rect != want {
		t.Fatalf("rect=%v want %v", reg.Rect, want)
	}
}

func TestResolveTriBaseAutoWhenEmpty(t *testing.T) {
	bounds := image.Rect(0, 0, 1000, 800)
	auto := image.Rect(10, 20, 400, 500)
	reg, src := resolveTriBase(bounds, ProcessOptions{}, func() (image.Rectangle, bool) {
		return auto, true
	})
	if src != "auto" {
		t.Fatalf("src=%q", src)
	}
	if reg.Rect != auto {
		t.Fatalf("rect=%v want %v", reg.Rect, auto)
	}
}

func TestResolveTriBaseMeteredBeforeAuto(t *testing.T) {
	bounds := image.Rect(0, 0, 1000, 800)
	area := image.Rect(50, 60, 150, 180)
	reg, src := resolveTriBase(bounds, ProcessOptions{BaseArea: area}, func() (image.Rectangle, bool) {
		t.Fatal("auto should not run when BaseArea is set")
		return image.Rectangle{}, false
	})
	if src != "metered" || reg.Rect != area {
		t.Fatalf("src=%q rect=%v", src, reg.Rect)
	}
}

func TestPixelRect(t *testing.T) {
	r := Rect{0.25, 0.25, 0.75, 0.75}.PixelRect(200, 100)
	if r != (image.Rect(50, 25, 150, 75)) {
		t.Fatalf("centre 50%%: %v", r)
	}
	if r2 := (Rect{}).PixelRect(200, 100); !r2.Empty() {
		t.Fatal("empty rect should stay empty")
	}
}
