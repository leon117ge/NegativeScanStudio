package hdrscan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"basetool/scanmeta"
)

func TestProcessMonoTrichromeWritesLinearTIFF(t *testing.T) {
	const w, h = 32, 24
	mk := func(v uint16) MonoPlane {
		p := make([]uint16, w*h)
		for i := range p {
			p[i] = v
		}
		// a bright square so channels are distinguishable
		for y := 8; y < 16; y++ {
			for x := 8; x < 16; x++ {
				p[y*w+x] = 40000
			}
		}
		return MonoPlane{W: w, H: h, Pix: p}
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "frame_RGB.tiff")
	base := [3]float64{0.50, 0.50, 0.50}
	rgb := [3]int{40, 50, 45}
	meta := scanmeta.Metadata{
		Mode:          "placeholder",
		Encoding:      "linear",
		LightModel:    "CuteNewDesign RGB Light Air",
		LightRGB:      &rgb,
		FilmBase:      &base,
		FilmBaseSpace: scanmeta.AfterProfile,
		BaseSource:    scanmeta.LevelingSource,
	}
	res, err := ProcessMonoTrichrome(mk(8000), mk(9000), mk(7000), MonoTriOptions{
		OutPath:   out,
		LZW:       true,
		CorrectCA: true,
		Software:  "NegativeScan Studio test",
		Make:      "Hikrobot",
		Model:     "MV-CS240-90UM",
		Metadata:  meta,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Out == "" {
		t.Fatal("empty path")
	}
	st, err := os.Stat(res.Out)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() < 100 {
		t.Fatalf("tiny file %d", st.Size())
	}
	if !res.Report.Enabled {
		t.Fatal("expected CA report")
	}
	if !strings.Contains(res.Software, "mono trichrome") {
		t.Fatalf("software %q", res.Software)
	}
}

func TestProcessMonoTrichromeRejectsSizeMismatch(t *testing.T) {
	a := MonoPlane{W: 8, H: 8, Pix: make([]uint16, 64)}
	b := MonoPlane{W: 4, H: 8, Pix: make([]uint16, 32)}
	_, err := ProcessMonoTrichrome(a, a, b, MonoTriOptions{OutPath: "x.tiff"})
	if err == nil {
		t.Fatal("expected size error")
	}
}

func TestProcessMonoTrichromeRejectsEmpty(t *testing.T) {
	_, err := ProcessMonoTrichrome(MonoPlane{}, MonoPlane{}, MonoPlane{}, MonoTriOptions{OutPath: "x.tiff"})
	if err == nil {
		t.Fatal("expected empty error")
	}
}

func TestProcessMonoTrichromeMeasuresLevelingBase(t *testing.T) {
	const w, h = 48, 32
	mk := func(v uint16) MonoPlane {
		p := make([]uint16, w*h)
		for i := range p {
			p[i] = v
		}
		return MonoPlane{W: w, H: h, Pix: p}
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "check_RGB.tiff")
	res, err := ProcessMonoTrichrome(mk(32768), mk(32768), mk(32768), MonoTriOptions{
		OutPath:             out,
		MeasureLevelingBase: true,
		BaseNorm:            Rect{X0: 0.25, Y0: 0.25, X1: 0.75, Y1: 0.75},
		Metadata:            scanmeta.Metadata{Mode: "mono-trichrome", Encoding: "linear"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseErr != "" {
		t.Fatal(res.BaseErr)
	}
	if res.BaseN == 0 {
		t.Fatal("no samples")
	}
	for c, v := range res.Base {
		if v < 0.49 || v > 0.51 {
			t.Fatalf("base[%d]=%v, want ~0.50", c, v)
		}
	}
}
