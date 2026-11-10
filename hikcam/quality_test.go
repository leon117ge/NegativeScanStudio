package hikcam

import (
	"strings"
	"testing"
)

func TestBestScanQualityValues(t *testing.T) {
	st := BestScanQuality()
	if st.PixelFormat != PixelMono12 {
		t.Fatalf("PixelFormat %s, want Mono12", st.PixelFormat)
	}
	if st.GainDB != 0 {
		t.Fatalf("GainDB %v, want 0", st.GainDB)
	}
	if st.BlackLevel != BestScanBlackLevel {
		t.Fatalf("BlackLevel %v, want %v", st.BlackLevel, BestScanBlackLevel)
	}
	if st.GammaEnable {
		t.Fatal("gamma should be off (linear)")
	}
	if !st.FullFrame {
		t.Fatal("full frame")
	}
	if !st.SoftwareTrigger {
		t.Fatal("software trigger")
	}
	if !st.QualityPreset {
		t.Fatal("QualityPreset")
	}
	if st.ExposureUS != 10_000 {
		t.Fatalf("starting ExposureUS %v, want 10000", st.ExposureUS)
	}
	if st.Width != CS240Width || st.Height != CS240Height {
		t.Fatalf("frame %dx%d, want %dx%d", st.Width, st.Height, CS240Width, CS240Height)
	}
	cl := st.Clamp()
	if cl.BlackLevel != BestScanBlackLevel || cl.GainDB != 0 || cl.ExposureUS != 10_000 {
		t.Fatalf("Clamp changed preset values: %+v", cl)
	}
}

func TestBestScanExplainEnglish(t *testing.T) {
	s := BestScanExplain()
	for _, w := range []string{"Mono12", "0 dB", "black level 32", "gamma off", "software trigger", "automatically"} {
		if !strings.Contains(s, w) {
			t.Fatalf("explain missing %q: %s", w, s)
		}
	}
}

func TestPixelFormatChoicesOrder(t *testing.T) {
	if PixelFormatIndex(PixelMono12) != 1 {
		t.Fatalf("Mono12 index %d", PixelFormatIndex(PixelMono12))
	}
	if PixelFormatChoices[1] != PixelMono12 {
		t.Fatal("UI slot 1 should be Mono12")
	}
	if PixelFormat(1) == PixelMono12 {
		t.Fatal("iota accidentally matches UI index")
	}
}

func TestSimAcceptsBestScanQuality(t *testing.T) {
	s := OpenSim()
	if err := s.Apply(BestScanQuality()); err != nil {
		t.Fatal(err)
	}
	got := s.Settings()
	if got.PixelFormat != PixelMono12 || got.GainDB != 0 || got.BlackLevel != BestScanBlackLevel {
		t.Fatalf("sim settings %+v", got)
	}
	if !got.SoftwareTrigger || !got.QualityPreset || got.GammaEnable {
		t.Fatalf("sim flags %+v", got)
	}
	if err := s.StartGrab(); err != nil {
		t.Fatal(err)
	}
	fr, err := s.Grab(0)
	if err != nil {
		t.Fatal(err)
	}
	if fr.W < 16 || fr.H < 16 || len(fr.Pix) < fr.W*fr.H {
		t.Fatalf("bad frame %dx%d n=%d", fr.W, fr.H, len(fr.Pix))
	}
	_ = s.Close()
}
