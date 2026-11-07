package colorsci

import (
	"math"
	"math/rand"
	"testing"
)

func near(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

func TestCineonConventions(t *testing.T) {
	base := [3]float64{29769, 33936, 10160}
	d := DensityAboveBase(base, base)
	for c := 0; c < 3; c++ {
		if !near(CineonCode(d[c]), 95, 1e-9) {
			t.Fatalf("base must encode to 95, got %v", CineonCode(d[c]))
		}
	}
	// 0.002 D per code: a 10x denser patch (D=1) is 500 codes above base.
	if !near(CineonCode(1), 595, 1e-9) {
		t.Fatal("D=1 should be code 595")
	}
	// One stop of exposure on a gamma 0.6 negative = 0.6*log10(2)/0.002 ≈ 90 codes (SMPTE).
	if !near(0.6*math.Log10(2)/CineonStep, 90.3, 0.1) {
		t.Fatal("stop size")
	}
	// Standard log2lin: 95->0, 685->1, 470 (18% grey card) -> ~0.18.
	if !near(CineonToLinear(95), 0, 1e-12) || !near(CineonToLinear(685), 1, 1e-12) {
		t.Fatal("log2lin end points")
	}
	if g := CineonToLinear(470); !near(g, 0.18, 0.01) {
		t.Fatalf("18%% grey -> %v", g)
	}
	// Same curve as FilmScan_Positive_Out.dctl Cineon output: 685 + 300*log10(lin*0.98921+0.01079).
	for _, lin := range []float64{0, 0.02, 0.18, 0.9, 1, 4} {
		dctl := 685 + 300*math.Log10(lin*0.98921+0.01079)
		if !near(LinearToCineon(lin), dctl, 0.15) { // DCTL rounds the offset to 0.01079
			t.Fatalf("lin %v: %v vs dctl %v", lin, LinearToCineon(lin), dctl)
		}
		if !near(CineonToLinear(LinearToCineon(lin)), lin, 1e-9) {
			t.Fatal("log/lin round trip")
		}
	}
	// Clamp and 16-bit storage: Resolve reads a 16-bit TIFF as v/65535, which must equal code/1023.
	if CineonCode(-1) != 0 || CineonCode(5) != 1023 {
		t.Fatal("clamp")
	}
	for _, code := range []float64{0, 95, 470, 685, 1023} {
		v := CineonTo16(code)
		if !near(float64(v)/65535, code/1023, 0.5/65535) || !near(CineonFrom16(v), code, 0.01) {
			t.Fatalf("16-bit storage of %v", code)
		}
	}
}

func TestApplyDefaultIsIdentical(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	pix := make([]uint16, 3*5000)
	for i := range pix {
		pix[i] = uint16(r.Intn(65536))
	}
	out, err := Apply(pix, Options{Base: [3]float64{30000, 34000, 10000}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range pix {
		if out[i] != pix[i] {
			t.Fatal("default options must not change the image")
		}
	}
	// Strength > 0 with identity matrix: unchanged within rounding.
	out, _ = Apply(pix, Options{Base: [3]float64{30000, 34000, 10000}, Strength: 1})
	for i := range pix {
		if d := int(out[i]) - int(pix[i]); d < -1 || d > 1 {
			t.Fatalf("identity crosstalk changed %d -> %d", pix[i], out[i])
		}
	}
	if _, err := Apply(pix, Options{}); err != ErrBadBase {
		t.Fatal("want ErrBadBase")
	}
}

func TestMaskCompensation(t *testing.T) {
	base := [3]float64{29769, 33936, 10160}
	o := Options{Base: base, MaskComp: true}
	b := o.Pixel(base)
	if !near(b[0], b[1], 1e-6) || !near(b[1], b[2], 1e-6) || !near(b[1], 33936, 1e-6) {
		t.Fatalf("base not neutral after mask compensation: %v", b)
	}
	// A patch with 0.3 D above base in every channel stays neutral.
	p := TransmittanceFromDensity([3]float64{0.3, 0.3, 0.3}, base)
	q := o.Pixel(p)
	if !near(q[0], q[2], 1e-6) {
		t.Fatal("neutral patch")
	}
	// Cineon with mask comp: base in all channels at 95.
	c := Options{Base: base, Encoding: EncCineon, MaskComp: true}.Pixel(base)
	if !near(c[0], 95, 1e-9) || !near(c[2], 95, 1e-9) {
		t.Fatal("cineon base")
	}
	// v0.72 Cineon without mask comp: brightest base channel (G) at 95, the
	// others keep the mask: 95 + log10(ref/base)/0.002.
	c = Options{Base: base, Encoding: EncCineon}.Pixel(base)
	if !near(c[1], 95, 1e-9) || !near(c[0], 95+math.Log10(33936.0/29769)/0.002, 1e-9) || !near(c[2], 95+math.Log10(33936.0/10160)/0.002, 1e-9) {
		t.Fatalf("cineon log negative base: %v", c)
	}
	// Apply path agrees with Pixel.
	pix := []uint16{29769, 33936, 10160, 3000, 4000, 1000}
	out, _ := Apply(pix, o)
	if out[0] != 33936 || out[1] != 33936 || out[2] != 33936 {
		t.Fatalf("Apply mask comp: %v", out[:3])
	}
}

func TestMatrixAlgebra(t *testing.T) {
	m := Mat3{{1, 0.5, 0.06}, {0.27, 1, 0.35}, {0.13, 0.24, 1}}
	inv, err := m.Inverse()
	if err != nil || !m.Mul(inv).IsIdentity(1e-12) {
		t.Fatal("inverse")
	}
	if !m.Lerp(0).IsIdentity(0) || m.Lerp(1) != m {
		t.Fatal("lerp ends")
	}
	h := m.Lerp(0.5)
	if !near(h[0][1], 0.25, 1e-12) || !near(h[0][0], 1, 1e-12) {
		t.Fatal("lerp half")
	}
	n := m.NormalizeRows()
	g := n.Apply([3]float64{0.4, 0.4, 0.4})
	if !near(g[0], 0.4, 1e-12) || !near(g[2], 0.4, 1e-12) {
		t.Fatal("normalized rows keep neutrals")
	}
	if _, err := (Mat3{}).Inverse(); err != ErrSingular {
		t.Fatal("singular")
	}
}

func TestDefaultCrosstalk(t *testing.T) {
	f := SpectralForward()
	// Sanity of the spectral model: each channel reads its own dye most.
	for k := 0; k < 3; k++ {
		for i := 0; i < 3; i++ {
			if i != k && !(f[k][i] < f[k][k]) {
				t.Fatalf("forward not diagonal dominant: %v", f)
			}
		}
	}
	// Camera R (assumed 600 nm peak) sees a lot of magenta; G sees yellow.
	if f[0][1] < 0.3 || f[1][2] < 0.2 {
		t.Fatalf("expected visible crosstalk, got %v", f)
	}
	d := DefaultCrosstalk()
	g := d.Apply([3]float64{0.5, 0.5, 0.5})
	for c := 0; c < 3; c++ {
		if !near(g[c], 0.5, 1e-9) {
			t.Fatal("default must keep neutrals")
		}
	}
	// It must undo the mask-aware forward model's mixing (up to row scale).
	ma := MaskAware(f)
	x := [3]float64{0.8, 0.2, 0.4}
	y := d.Apply(ma.Apply(x))
	inv, _ := ma.Inverse()
	for r := 0; r < 3; r++ {
		s := inv[r][0] + inv[r][1] + inv[r][2]
		if !near(y[r], x[r]/s, 1e-9) {
			t.Fatal("default != row-normalised inverse")
		}
	}
	t.Logf("forward %.3f\nmask-aware correction %.3f\nunmasked correction %.3f", f, d, UnmaskedCrosstalk())
}

// Synthetic ColorChecker: true patch log-exposures go through a known
// crosstalk mix + per-channel gamma + offset + noise; the solver must recover
// the inverse.
func TestSolveCrosstalkSynthetic(t *testing.T) {
	lin := make([][3]float64, 24)
	for i, p := range ColorChecker24sRGB {
		for c := 0; c < 3; c++ {
			lin[i][c] = SRGBToLinear(p[c] / 255)
		}
	}
	ref := ReferenceLogExposure(lin)
	mix := Mat3{{0.6, 0.30, 0.04}, {0.15, 0.62, 0.20}, {0.06, 0.12, 0.70}} // gamma ≈0.6 + crosstalk
	off := [3]float64{0.9, 1.0, 1.1}
	r := rand.New(rand.NewSource(7))
	for _, noise := range []float64{0, 0.004} {
		meas := make([][3]float64, 24)
		for i := range ref {
			m := mix.Apply(ref[i])
			for c := 0; c < 3; c++ {
				meas[i][c] = m[c] + off[c] + noise*r.NormFloat64()
			}
		}
		cal, err := SolveCrosstalk(meas, ref, 0)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := mix.Inverse()
		tol := 1e-9
		if noise > 0 {
			tol = 0.12
		}
		for a := 0; a < 3; a++ {
			for b := 0; b < 3; b++ {
				if !near(cal.Matrix[a][b], want[a][b], tol) {
					t.Fatalf("noise %v: got %.4f want %.4f", noise, cal.Matrix, want)
				}
			}
		}
		// Ridge keeps it close and stable.
		cr, _ := SolveCrosstalk(meas, ref, 0.01)
		for a := 0; a < 3; a++ {
			for b := 0; b < 3; b++ {
				if !near(cr.Matrix[a][b], want[a][b], 0.35) {
					t.Fatalf("ridge fit too far: %.3f", cr.Matrix)
				}
			}
		}
		t.Logf("noise %.3f: matrix %.3f rms %.4f", noise, cal.Matrix, cal.RMS)
	}
	if _, err := SolveCrosstalk(ref[:5], ref[:5], 0); err != ErrTooFewPatches {
		t.Fatal("too few")
	}
	grey := make([][3]float64, 10)
	if _, err := SolveCrosstalk(grey, grey, 0); err == nil {
		t.Fatal("degenerate must fail")
	}
}

// v0.72: a Cineon file without mask comp is the log of the linear
// negative: decoding gives the input back (within 16-bit code rounding).
func TestCineonLogNegativeRoundTrip(t *testing.T) {
	base := [3]float64{29769, 33936, 10160}
	o := Options{Base: base, Encoding: EncCineon}
	ref := CineonRef(base)
	var pix []uint16
	for v := 200; v < 65536; v += 97 {
		pix = append(pix, uint16(v), uint16(v), uint16(v))
	}
	out, err := Apply(pix, o)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range pix {
		d := float64(v)
		if 95+(-math.Log10(d/ref))/0.002 > 1023 || 95+(-math.Log10(d/ref))/0.002 < 0 {
			continue // clipped codes
		}
		back := DecodeCineonNegative(out[i], ref)
		// one 16-bit step is 1023/65535 code = 3.1e-5 D: relative 3.6e-5 at half a step
		if math.Abs(back-d)/d > 4e-5 {
			t.Fatalf("value %d -> code %d -> %.3f", v, out[i], back)
		}
	}
	// Not inverted: a denser (darker) negative value gets a higher code.
	if !(out[0] > out[len(out)-1]) {
		t.Fatal("code order")
	}
}
