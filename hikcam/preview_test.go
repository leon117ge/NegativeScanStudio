package hikcam

import (
	"testing"
	"time"
)

func TestLiveViewSettings(t *testing.T) {
	scan := BestScanQuality()
	live := LiveViewSettings(scan, 2)
	if live.SoftwareTrigger {
		t.Fatal("live view must be free-run (TriggerMode Off)")
	}
	if live.PixelFormat != PixelMono8 {
		t.Fatalf("live PixelFormat %s, want Mono8", live.PixelFormat)
	}
	if live.QualityPreset {
		t.Fatal("live view should not re-apply the quality preset")
	}
	if live.Binning != 2 {
		t.Fatalf("binning %d, want 2", live.Binning)
	}
	if live.ExposureUS != scan.ExposureUS || live.GainDB != scan.GainDB || live.BlackLevel != scan.BlackLevel {
		t.Fatalf("exposure/gain/black changed: %+v", live)
	}
	if !live.FullFrame || live.GammaEnable {
		t.Fatalf("geometry/gamma %+v", live)
	}
	full := LiveViewSettings(scan, 1)
	if full.Binning != 1 || full.SoftwareTrigger || full.PixelFormat != PixelMono8 {
		t.Fatalf("full-res live %+v", full)
	}
	if scan.SoftwareTrigger != true || scan.PixelFormat != PixelMono12 || scan.Binning != 0 {
		t.Fatal("LiveViewSettings must not mutate the scan settings")
	}
}

func TestSimLiveThenScan(t *testing.T) {
	s := OpenSim()
	defer s.Close()
	if err := s.Apply(BestScanQuality()); err != nil {
		t.Fatal(err)
	}
	scanW, scanH := s.Settings().Width, s.Settings().Height
	if err := s.Apply(LiveViewSettings(BestScanQuality(), 2)); err != nil {
		t.Fatal(err)
	}
	live := s.Settings()
	if live.SoftwareTrigger || live.PixelFormat != PixelMono8 || live.Binning != 2 {
		t.Fatalf("live %+v", live)
	}
	if live.Width != scanW/2 || live.Height != scanH/2 {
		t.Fatalf("binned size %dx%d, want %dx%d", live.Width, live.Height, scanW/2, scanH/2)
	}
	if err := s.StartGrab(); err != nil {
		t.Fatal(err)
	}
	fr, err := s.Grab(0)
	if err != nil {
		t.Fatal(err)
	}
	if fr.W != live.Width || fr.H != live.Height {
		t.Fatalf("live frame %dx%d", fr.W, fr.H)
	}
	_ = s.StopGrab()
	if err := s.Apply(BestScanQuality()); err != nil {
		t.Fatal(err)
	}
	got := s.Settings()
	if !got.SoftwareTrigger || got.PixelFormat != PixelMono12 || got.Binning != 0 {
		t.Fatalf("scan restore %+v", got)
	}
	if got.Width != scanW || got.Height != scanH {
		t.Fatalf("scan size %dx%d, want %dx%d", got.Width, got.Height, scanW, scanH)
	}
}

func TestPreviewGraySize(t *testing.T) {
	w, h := CS240Width, CS240Height
	pix := make([]uint16, w*h)
	pix[0] = 0x8000
	// Downsample step is 4; the last sampled source pixel is (1503*4, 1023*4).
	pix[1023*4*w+1503*4] = 0xFF00
	img := PreviewGray(pix, w, h, LivePreviewMaxEdge)
	if img.Bounds().Dx() != 1504 || img.Bounds().Dy() != 1024 {
		t.Fatalf("preview %dx%d, want 1504x1024", img.Bounds().Dx(), img.Bounds().Dy())
	}
	if img.Pix[0] != 0x80 {
		t.Fatalf("first pixel %d", img.Pix[0])
	}
	if img.Pix[1023*1504+1503] != 0xFF {
		t.Fatalf("corner pixel %d", img.Pix[1023*1504+1503])
	}
	full := PreviewGray(pix, w, h, 0)
	if full.Bounds().Dx() != w || full.Bounds().Dy() != h {
		t.Fatalf("no-scale %dx%d", full.Bounds().Dx(), full.Bounds().Dy())
	}
}

func TestPreviewGray8AndCrop(t *testing.T) {
	src := []byte{
		10, 20, 30, 40,
		50, 60, 70, 80,
		90, 100, 110, 120,
		130, 140, 150, 160,
	}
	img := PreviewGray8(src, 4, 4, 2)
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 2 {
		t.Fatalf("size %v", img.Bounds())
	}
	if img.Pix[0] != 10 || img.Pix[1] != 30 || img.Pix[2] != 90 || img.Pix[3] != 110 {
		t.Fatalf("pix %v", img.Pix)
	}
	pix := []uint16{0x1000, 0x2000, 0x3000, 0x4000, 0x5000, 0x6000}
	crop := CropGray8(pix, 3, 2, 1, 0, 3, 2)
	if crop.Bounds().Dx() != 2 || crop.Bounds().Dy() != 2 {
		t.Fatalf("crop size %v", crop.Bounds())
	}
	if crop.Pix[0] != 0x20 || crop.Pix[1] != 0x30 || crop.Pix[2] != 0x50 || crop.Pix[3] != 0x60 {
		t.Fatalf("crop pix %v", crop.Pix)
	}
}

// TestDisplayPipelineTiming reports the per-frame CPU cost of the old
// full-resolution conversion versus the downsampled live-view path at
// the CS240's 6016x4096 size.
func TestDisplayPipelineTiming(t *testing.T) {
	w, h := CS240Width, CS240Height
	mono12 := make([]byte, w*h*2)
	for i := 0; i < w*h; i++ {
		v := uint16(i * 17)
		mono12[2*i] = byte(v)
		mono12[2*i+1] = byte(v >> 8)
	}
	mono8 := make([]byte, w*h)
	for i := range mono8 {
		mono8[i] = byte(i)
	}

	before := timeAvg(t, 3, func() {
		pix, err := unpackMono(mono12, w, h, gvspMono12)
		if err != nil {
			t.Fatal(err)
		}
		_ = fullGray16To8(pix, w, h)
	})
	after := timeAvg(t, 8, func() {
		_ = PreviewGray8(mono8, w, h, LivePreviewMaxEdge)
	})
	t.Logf("before (Mono12 unpack + full %dx%d Gray): %.2f ms/frame", w, h, millis(before))
	t.Logf("after  (Mono8 downsample to max %d):      %.2f ms/frame", LivePreviewMaxEdge, millis(after))
	if after > 30*time.Millisecond {
		t.Fatalf("downsampled path too slow: %.2f ms/frame", millis(after))
	}
	if after >= before {
		t.Logf("note: after was not faster on this run (before %.2f ms, after %.2f ms)", millis(before), millis(after))
	}
}

func timeAvg(t *testing.T, n int, fn func()) time.Duration {
	t.Helper()
	fn() // warmup / allocate
	t0 := time.Now()
	for i := 0; i < n; i++ {
		fn()
	}
	return time.Since(t0) / time.Duration(n)
}

func millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
