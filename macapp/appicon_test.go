//go:build darwin || (linux && gio)

package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// The header icon comes from the Windows icon in the source tree (no copy
// in this module): the 256 px PNG entry of ico06/app.ico.
func TestAppIconFromWindowsIco(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "ico06", "app.ico"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := icoLargestPNG(b)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(d))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 256 || cfg.Height != 256 {
		t.Fatalf("largest entry %dx%d, want 256x256", cfg.Width, cfg.Height)
	}
	img := loadAppIcon()
	if img == nil {
		t.Fatal("loadAppIcon found no icon in the source tree")
	}
	if s := img.Bounds().Size(); s.X != 256 || s.Y != 256 {
		t.Fatalf("header icon %v", s)
	}
	if _, err := icoLargestPNG([]byte("not an icon at all")); err == nil {
		t.Fatal("garbage accepted as .ico")
	}
}
