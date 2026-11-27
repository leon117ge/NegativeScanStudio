//go:build darwin || (linux && gio)

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"basetool/cropper"
	"basetool/macapp/appstate"
)

// "--crop <files or folders>" opens Crop & Merge > Crop with those files
// added, as Windows' main.go does; the other switches only pick the page.
func TestArgsCropAddsFiles(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	for i, n := range []string{"a.tif", "b.png"} {
		im, _ := cropper.Synth(cropper.SynthSpec{W: 600, H: 400, FrameW: 520, FrameH: 340, Rebate: [3]float64{0.9, 0.9, 0.9}, RebateW: 12, Holder: [3]float64{0.03, 0.03, 0.03}, Bits: 8, Seed: int64(i + 1)})
		im.Format = cropper.FormatOf(n)
		if err := cropper.Save(filepath.Join(d, n), im, cropper.SaveOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(d, "notes.txt"), []byte("x"), 0o644)
	single := filepath.Join(t.TempDir(), "c.tif")
	im, _ := cropper.Synth(cropper.SynthSpec{W: 600, H: 400, FrameW: 520, FrameH: 340, Bits: 8, Seed: 3})
	im.Format = cropper.FormatOf(single)
	if err := cropper.Save(single, im, cropper.SaveOptions{}); err != nil {
		t.Fatal(err)
	}

	u := newUI(newTheme(), nil)
	s := &shooter{t: t, u: u}
	u.applyArgs([]string{"--crop", d, single})
	if u.nav.Page() != appstate.PageCrop {
		t.Fatalf("page %v", u.nav.Page())
	}
	s.pumpFor("crop files", 30*time.Second, func() bool { return u.crop.CountLbl == "3 file(s)" })

	for _, arg := range []string{"--mono", "--batch"} {
		u := newUI(newTheme(), nil)
		u.applyArgs([]string{arg, single})
		if u.crop.CountLbl != "0 file(s)" {
			t.Fatalf("%s added files: %q", arg, u.crop.CountLbl)
		}
	}
}
