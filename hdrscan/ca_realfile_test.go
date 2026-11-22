package hdrscan

import (
	"os"
	"runtime"
	"testing"
	"time"

	"basetool/rawconv"
)

// TestCARealFile runs the CA correction on a real mono-trichrome TIFF when
// NSS_CA_FILE is set (and writes the result to NSS_CA_OUT if given).
func TestCARealFile(t *testing.T) {
	in := os.Getenv("NSS_CA_FILE")
	if in == "" {
		t.Skip("NSS_CA_FILE not set")
	}
	p, err := Load(in)
	if err != nil {
		t.Fatal(err)
	}
	w, h := p.W, p.H
	n := w * h
	r := make([]float64, n)
	g := make([]float64, n)
	b := make([]float64, n)
	for i := 0; i < n; i++ {
		r[i] = float64(p.Pix[3*i])
		g[i] = float64(p.Pix[3*i+1])
		b[i] = float64(p.Pix[3*i+2])
	}
	runtime.GC()
	t0 := time.Now()
	rr, gg, bb, rep := CorrectMonoCA(r, g, b, w, h)
	el := time.Since(t0)
	t.Logf("CorrectMonoCA %dx%d took %.2f s on %d CPUs", w, h, el.Seconds(), runtime.NumCPU())
	t.Logf("report: %s", rep.String())
	for _, c := range []struct {
		n string
		c ChannelCA
	}{{"R", rep.R}, {"B", rep.B}} {
		t.Logf("%s model %+v used %d/%d fitRMS %.3f", c.n, c.c.Model, c.c.Used, c.c.Tiles, c.c.FitRMS)
		for _, rc := range c.c.Regions {
			t.Logf("  %s %-12s measured=%v before %.3f after %.3f sharp %.4f", c.n, rc.Name, rc.Measured, rc.Before, rc.After, rc.Sharp)
		}
	}
	if out := os.Getenv("NSS_CA_OUT"); out != "" {
		pix := make([]uint16, 3*n)
		for i := 0; i < n; i++ {
			pix[3*i] = f64ToU16(rr[i])
			pix[3*i+1] = f64ToU16(gg[i])
			pix[3*i+2] = f64ToU16(bb[i])
		}
		if err := rawconv.WriteTIFF16Description(out, w, h, pix, false, "Hikrobot", "MV-CS240-90UM", "NegativeScan Studio CA test", rep.String()); err != nil {
			t.Fatal(err)
		}
	}
}
