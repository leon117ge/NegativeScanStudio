//go:build darwin || (linux && gio)

package main

import (
	"image"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"basetool/cropper"
	"basetool/rawconv"

	"basetool/macapp/appstate"
)

// testBracket writes a frame-0 and a +2 EV 16-bit TIFF of one synthetic
// negative (clear base strip, dust specks, a thin fibre).
func testBracket(t *testing.T, dir string) []string {
	t.Helper()
	w, h := 960, 640
	rng := rand.New(rand.NewSource(3))
	rad := make([]float64, w*h*3)
	mask := [3]float64{1, 0.72, 0.38}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			l := 0.015 + 0.25*(0.5+0.5*math.Sin(float64(x)/61)*math.Cos(float64(y)/47))
			if x < w/12 {
				l = 0.62
			}
			if math.Hypot(float64(x-500), float64(y-300)) < 3 || math.Hypot(float64(x-700), float64(y-180)) < 2.5 {
				l *= 0.3
			}
			if fy := 420 + 25*math.Sin(float64(x)/60); x > 250 && x < 650 && math.Abs(float64(y)-fy) < 1.3 {
				l *= 0.55
			}
			for c := 0; c < 3; c++ {
				rad[3*(y*w+x)+c] = l * mask[c] * (1 + 0.01*rng.NormFloat64())
			}
		}
	}
	var out []string
	for f, n := range []string{"Roll_0007_0EV.tif", "Roll_0007_2EV.tif"} {
		pix := make([]uint16, len(rad))
		for i, v := range rad {
			pix[i] = uint16(math.Min(65535, math.Round(v*math.Exp2(float64(2*f))*65535)))
		}
		p := filepath.Join(dir, n)
		if err := rawconv.WriteTIFF16(p, w, h, pix, true, "Synthetic", "Bracket", "test"); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// TestCropMergePages lays out the four Crop & Merge pages at 1280x800
// with real work done on each (crop detection and export, a merge with
// dust removal, Open in Develop, a hair removal, batch rows); with
// NSS_SHOTS=<dir> the frames are saved.
func TestCropMergePages(t *testing.T) {
	isolate(t)
	w, err := headless.NewWindow(1280, 800)
	if err != nil {
		t.Skip("no headless GPU:", err)
	}
	defer w.Release()
	u := newUI(newTheme(), nil)
	s := &shooter{t: t, w: w, u: u, sz: image.Pt(1280, 800), dir: os.Getenv("NSS_SHOTS")}
	if s.dir != "" {
		os.MkdirAll(s.dir, 0o755)
	}
	u.hdr.Start()
	d := t.TempDir()
	open := func(sub int) {
		u.nav.SetModule(2)
		u.nav.SetSub(sub)
		u.setNav()
	}

	// Crop
	open(0)
	if u.nav.Page() != appstate.PageCrop {
		t.Fatal(u.nav.Page())
	}
	s.save("01-crop-idle.png")
	var scans []string
	for i, spec := range []cropper.SynthSpec{
		{W: 2400, H: 1600, Tilt: 0.8, FrameW: 2112, FrameH: 1408, Rebate: [3]float64{0.94, 0.92, 0.88}, RebateW: 50, Holder: [3]float64{0.03, 0.03, 0.035}, Sprockets: true, Bits: 16, Seed: 1},
		{W: 2400, H: 1600, Tilt: -1.4, FrameW: 2160, FrameH: 1440, Rebate: [3]float64{0.03, 0.03, 0.035}, Holder: [3]float64{0.03, 0.03, 0.035}, Bits: 8, Seed: 2},
	} {
		im, _ := cropper.Synth(spec)
		n := []string{"scan_01.tif", "scan_02.png"}[i]
		im.Format = cropper.FormatOf(n)
		p := filepath.Join(d, n)
		if err := cropper.Save(p, im, cropper.SaveOptions{LZW: true}); err != nil {
			t.Fatal(err)
		}
		scans = append(scans, p)
	}
	// Add files goes through the native panel (multiple selection)
	u.pick = func(_ string, k pickKind, _ string, _ []string, done func(string, bool)) {
		if k != pickOpenMulti {
			t.Errorf("Add files used panel kind %d", k)
		}
		done(strings.Join(scans, "\n"), true)
	}
	u.crop.AddFilesDialog()
	s.pumpFor("crop detection", 60*time.Second, func() bool {
		return u.crop.Rows() == 2 && u.crop.Cell(0, 4) == "Ready" && u.crop.Cell(1, 4) == "Ready" && u.crop.Preview().Ready
	})
	s.save("02-crop-loaded.png")
	if u.cp.imgKey == "" {
		t.Fatal("crop preview not painted")
	}
	u.crop.Export(true)
	s.pumpFor("crop export", 60*time.Second, func() bool { return !u.crop.Exporting() })
	if !strings.HasPrefix(u.crop.StatusLbl, "Export finished: 2 written, 0 failed.") {
		t.Fatal(u.crop.StatusLbl)
	}
	s.save("03-crop-exported.png")

	// Merge files
	open(1)
	s.save("04-merge-idle.png")
	fr := testBracket(t, d)
	u.pick = func(_ string, k pickKind, _ string, _ []string, done func(string, bool)) { done(fr[0], true) }
	u.hdr.BrowseMerge(0)
	u.pick = func(_ string, k pickKind, _ string, _ []string, done func(string, bool)) { done(fr[1], true) }
	u.hdr.BrowseMerge(1)
	u.hdr.MF.Dust = 1
	s.frame()
	if u.mp.eds[0].Text() != fr[0] || u.mp.eds[1].Text() != fr[1] {
		t.Fatal("frame fields not filled from Browse...")
	}
	u.hdr.RunMerge()
	s.save("05-merge-running.png")
	s.pumpFor("merge", 120*time.Second, func() bool { return !u.hdr.MergeBusy() })
	if !strings.HasPrefix(u.hdr.MRes, "Saved ") || !u.hdr.CanOpenMerge() {
		t.Fatal(u.hdr.MRes)
	}
	s.save("06-merge-done.png")

	// Open in Develop: Convert is not on macOS yet, so the page switches to
	// Convert and says so (known gap)
	u.hdr.OpenMergeInDevelop()
	if u.nav.Page() != appstate.PageConvert || u.develop != u.hdr.LastMerge.HDR || len(u.dialogs) != 1 {
		t.Fatalf("open in develop: page %v develop %q", u.nav.Page(), u.develop)
	}
	s.save("07-open-in-develop.png")
	u.dialogs = nil

	// Remove hairs on the merged HDR
	open(2)
	s.save("08-hairs-idle.png")
	u.hdr.Hair.LastHDR()
	s.pumpFor("hair load", 30*time.Second, func() bool { return u.hdr.Hair.Loaded() && !u.hdr.Hair.Busy() })
	s.save("09-hairs-loaded.png")
	if u.hap.ir.Empty() {
		t.Fatal("hair preview not laid out")
	}
	u.hdr.Hair.Box(image.Rect(230, 380, 670, 460), u.hap.ir.Dx(), u.hap.ir.Dy())
	s.pumpFor("hair removal", 30*time.Second, func() bool { return !u.hdr.Hair.Busy() })
	if len(u.hdr.Hair.Edits()) != 1 {
		t.Fatal(u.hdr.Hair.Status)
	}
	s.save("10-hairs-removed.png")

	// Batch RAW to TIFF
	open(3)
	s.save("11-batch-idle.png")
	bad := filepath.Join(d, "DSCF0001.RAF")
	os.WriteFile(bad, []byte("not a raw file"), 0644)
	files := []string{bad}
	for _, p := range filepath.SplitList(os.Getenv("NSS_TEST_RAF")) {
		if _, err := os.Stat(p); err == nil {
			files = append(files, p)
			break
		}
	}
	u.raw.AddFiles(files)
	u.raw.Convert()
	s.save("12-batch-converting.png")
	s.pumpFor("batch", 180*time.Second, func() bool { return !u.raw.Running() && u.raw.ConvOn })
	if !strings.HasPrefix(u.raw.Info, "Finished. ") {
		t.Fatal(u.raw.Info)
	}
	s.save("13-batch-done.png")
	u.raw.Shutdown()
	u.hdr.Shutdown()
}
