package hdrscan

import (
	"encoding/json"
	"os"
	"runtime"
	"testing"
	"time"

	"basetool/rawconv"
	"basetool/scanmeta"
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

// TestMovementRealFile runs the R/G/B movement check on a real
// mono-trichrome TIFF when NSS_MOVE_FILE is set.
func TestMovementRealFile(t *testing.T) {
	in := os.Getenv("NSS_MOVE_FILE")
	if in == "" {
		t.Skip("NSS_MOVE_FILE not set")
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
	t0 := time.Now()
	m := MeasureMovement(r, g, b, w, h)
	t.Logf("%s (%.2f s, tiles R %d B %d)", m.String(), time.Since(t0).Seconds(), m.Tiles[0], m.Tiles[1])
}

// TestMonoTriRealFile re-runs the v1.10 mono-trichrome writer (CA on,
// movement check) on the R/G/B planes of a real scan (NSS_TRI_FILE) and
// writes NSS_TRI_OUT, keeping the original film base / light metadata.
func TestMonoTriRealFile(t *testing.T) {
	in, out := os.Getenv("NSS_TRI_FILE"), os.Getenv("NSS_TRI_OUT")
	if in == "" || out == "" {
		t.Skip("NSS_TRI_FILE / NSS_TRI_OUT not set")
	}
	ti, err := ReadTIFFInfo(in)
	if err != nil {
		t.Fatal(err)
	}
	var meta scanmeta.Metadata
	if err := json.Unmarshal([]byte(ti.Description), &meta); err != nil {
		t.Fatal(err)
	}
	meta.Processing = "" // a v1.08 CA-on scan carried the CA report here
	p, err := Load(in)
	if err != nil {
		t.Fatal(err)
	}
	n := p.W * p.H
	var pl [3]MonoPlane
	for c := 0; c < 3; c++ {
		pix := make([]uint16, n)
		for i := 0; i < n; i++ {
			pix[i] = p.Pix[3*i+c]
		}
		pl[c] = MonoPlane{W: p.W, H: p.H, Pix: pix}
	}
	p.Free()
	_ = os.Remove(out)
	res, err := ProcessMonoTrichrome(pl[0], pl[1], pl[2], MonoTriOptions{OutPath: out, LZW: true, CorrectCA: true, CheckMovement: true,
		Software: "NegativeScan Studio v1.10 real-file test", Make: ti.Make, Model: ti.Model, Metadata: meta})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", res.Movement.String())
	t.Logf("%s", res.Report.String())
	t.Logf("wrote %s in %.1f s", res.Out, res.Elapsed.Seconds())
}
