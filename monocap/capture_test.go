package monocap

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"basetool/hdrscan"
	"basetool/hikcam"
	"basetool/scanmeta"
)

func openSim(t *testing.T) *hikcam.Sim {
	sim := hikcam.OpenSim()
	if err := sim.Apply(hikcam.BestScanQuality()); err != nil {
		t.Fatal(err)
	}
	if err := sim.StartGrab(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sim.StopGrab(); _ = sim.Close() })
	return sim
}

func TestSettleState(t *testing.T) {
	s := SettleState{Tol: 0.003}
	if s.Add(10000) {
		t.Fatal("first frame cannot be stable")
	}
	if s.Add(12000) {
		t.Fatal("20% change reported stable")
	}
	if s.Add(12500) {
		t.Fatal("4% change reported stable")
	}
	if !s.Add(12520) {
		t.Fatalf("0.16%% change not stable (delta %v)", s.Delta)
	}
}

// slowLight is a simulated light that ramps up over several grabs.
type rampCam struct {
	*hikcam.Sim
	n int
}

func (r *rampCam) Grab(d time.Duration) (*hikcam.Frame, error) {
	fr, err := r.Sim.Grab(d)
	if err != nil {
		return fr, err
	}
	r.n++
	k := math.Min(1, 0.4+0.15*float64(r.n))
	for i, v := range fr.Pix {
		fr.Pix[i] = uint16(fr.Black16 + (float64(v)-fr.Black16)*k)
	}
	return fr, nil
}

func TestWaitSettledWaitsForRamp(t *testing.T) {
	sim := openSim(t)
	sim.SetFlatField(true)
	rc := &rampCam{Sim: sim}
	o := Options{Cam: rc, SettleTol: 0.003, SettleTimeout: 2 * time.Second}
	note, err := o.waitSettled(1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(note, "stable") || rc.n < 5 {
		t.Fatalf("note %q after %d grabs", note, rc.n)
	}
}

// End to end on the simulator: darks, flats, a 0/+2 bracket with two
// frames per exposure, native merge, movement check, CA, TIFF, and the
// OFX metadata rule.
func TestSimulatorEndToEnd(t *testing.T) {
	sim := openSim(t)
	store := Store{Dir: t.TempDir()}
	light := SimLight{S: sim}
	base := 4000.0
	evs := []int{0, 2}
	exps := []float64{base, base * 4}
	cal := CalOptions{Cam: sim, Light: light, Frames: 4, Store: store, Software: "test"}
	darks, err := CaptureDarks(cal, exps)
	if err != nil || len(darks) != 2 {
		t.Fatal(err, darks)
	}
	if math.Abs(darks[0].Mean16-512) > 1 {
		t.Fatalf("sim dark mean %.1f, want pedestal 512", darks[0].Mean16)
	}
	flats, err := CaptureFlats(cal, base)
	if err != nil || len(flats) != 3 {
		t.Fatal(err)
	}
	if s := store.Summary(); !strings.Contains(s, "flats RGB") || !strings.Contains(s, "darks at 4000 us, 16000 us") {
		t.Fatal(s)
	}
	var logs []string
	res, err := Capture(Options{Cam: sim, Light: light, Settle: time.Millisecond, BaseUS: base, EVs: evs, FramesPerExposure: 2,
		Noise: hdrscan.DefaultMonoNoise(), Calib: store, UseDark: true, UseFlat: true, Log: func(s string) { logs = append(logs, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Settle[0], "stable") {
		t.Fatalf("settle: %v", res.Settle)
	}
	if res.Mono.BlackSubtracted16 != 512 || res.Mono.BlackLevelDN != 32 || res.Mono.PixelFormat != "Mono12" {
		t.Fatalf("mono record %+v", res.Mono)
	}
	if !strings.Contains(res.Mono.Dark, "dark 4000us") || !strings.Contains(res.Mono.Dark, "dark 16000us") || !strings.Contains(res.Mono.Flat, "flat B") {
		t.Fatalf("calibration ids: dark %q flat %q", res.Mono.Dark, res.Mono.Flat)
	}
	if !strings.Contains(res.Mono.HDR, "native mono HDR") || len(res.Notes) != 0 {
		t.Fatalf("hdr %q notes %v", res.Mono.HDR, res.Notes)
	}
	for ch, p := range res.Planes {
		if p.W != 960 || p.H != 654 {
			t.Fatalf("%d: %dx%d", ch, p.W, p.H)
		}
	}
	base3 := [3]float64{.5, .5, .5}
	rgb := [3]int{20, 80, 30}
	meta := scanmeta.Metadata{Encoding: "linear", LightRGB: &rgb, FilmBase: &base3, FilmBaseSpace: scanmeta.AfterProfile, BaseSource: scanmeta.LevelingSource}
	mc := res.Mono
	meta.Mono = &mc
	out, err := hdrscan.ProcessMonoTrichrome(res.Planes[0], res.Planes[1], res.Planes[2], hdrscan.MonoTriOptions{
		OutPath: filepath.Join(t.TempDir(), "sim_RGB.tiff"), LZW: true, CorrectCA: true, CheckMovement: true, Software: "test", Metadata: meta})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(out.Movement.String())
	t.Log(out.Report.String())
	// The simulator offsets R by (+1.6, -0.8) and B by (-1.1, +1.3) px:
	// a pure translation, which the check must flag (and not correct).
	if !out.Movement.Valid || !out.Movement.Warn || math.Abs(out.Movement.R[0]-1.6) > 0.3 || math.Abs(out.Movement.B[1]-1.3) > 0.3 {
		t.Fatalf("movement: %s", out.Movement.String())
	}
	ti, err := hdrscan.ReadTIFFInfo(out.Out)
	if err != nil {
		t.Fatal(err)
	}
	var j map[string]any
	if err := json.Unmarshal([]byte(ti.Description), &j); err != nil {
		t.Fatal(err)
	}
	mcj, _ := j["mono_capture"].(map[string]any)
	if mv, _ := mcj["movement_check"].(string); !strings.HasPrefix(mv, "WARNING") {
		t.Fatalf("movement warning not in metadata: %v", mcj)
	}
	if p, _ := j["processing"].(string); p != "" || j["encoding"] != "linear" {
		t.Fatalf("OFX rule: processing %q encoding %v", p, j["encoding"])
	}
	if runner := os.Getenv("NSS_OFX_RUNNER"); runner != "" {
		b, err := exec.Command(runner, out.Out).CombinedOutput()
		if err != nil {
			t.Fatalf("OFX math_runner: %v %s", err, b)
		}
		t.Logf("OFX math_runner: %s", strings.TrimSpace(string(b)))
	}
	t.Logf("metadata: %s", ti.Description)
}

func TestCaptureWithoutCalibrationIsPlainBlackSubtraction(t *testing.T) {
	sim := openSim(t)
	res, err := Capture(Options{Cam: sim, Light: SimLight{S: sim}, BaseUS: 4000, NoSettleCheck: true, Calib: Store{Dir: t.TempDir()}, UseDark: true, UseFlat: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) == 0 || res.Mono.Dark != "none found" || res.Mono.Flat != "none found" {
		t.Fatalf("expected missing-calibration notes: %+v %v", res.Mono, res.Notes)
	}
	// with calibration off, the plane is the raw frame minus the pedestal
	sim2 := openSim(t)
	res2, err := Capture(Options{Cam: sim2, Light: SimLight{S: sim2}, BaseUS: 4000, NoSettleCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Mono.Dark != "off" || res2.Mono.Flat != "off" || len(res2.Notes) != 0 {
		t.Fatalf("%+v %v", res2.Mono, res2.Notes)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	c := CalFrame{CalHeader: CalHeader{Kind: "dark", ID: "x", W: 3, H: 2, PixelFormat: "Mono12", ExposureUS: 1000}, Pix: []uint16{1, 2, 3, 4, 5, 65535}}
	if _, err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	got := s.FindDark(1010, 0, "Mono12", 3, 2)
	if got == nil || got.Pix[5] != 65535 || got.ID != "x" {
		t.Fatalf("%+v", got)
	}
	if s.FindDark(1100, 0, "Mono12", 3, 2) != nil || s.FindDark(1000, 0, "Mono10", 3, 2) != nil || s.FindDark(1000, 0, "Mono12", 4, 2) != nil {
		t.Fatal("dark matched the wrong exposure / format / size")
	}
}
