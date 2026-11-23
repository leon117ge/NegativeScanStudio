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

// End to end on the simulator: the default 0/+3/+5 bracket, native merge,
// movement check, CA, TIFF, and the OFX metadata rule.
func TestSimulatorEndToEnd(t *testing.T) {
	sim := openSim(t)
	light := SimLight{S: sim}
	base := 4000.0
	evs := DefaultEVs
	var logs []string
	res, err := Capture(Options{Cam: sim, Light: light, Settle: time.Millisecond, BaseUS: base, EVs: evs,
		Log: func(s string) { logs = append(logs, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Settle[0], "stable") {
		t.Fatalf("settle: %v", res.Settle)
	}
	if res.Mono.BlackSubtracted16 != 512 || res.Mono.BlackLevelDN != 32 || res.Mono.PixelFormat != "Mono12" {
		t.Fatalf("mono record %+v", res.Mono)
	}
	if !strings.Contains(res.Mono.HDR, "native mono HDR") || !strings.Contains(res.Mono.HDR, "+5 EV ratio") || len(res.Notes) != 0 {
		t.Fatalf("hdr %q notes %v", res.Mono.HDR, res.Notes)
	}
	if res.Mono.NoiseModel != "full well 12100 e-, read noise 2.70 e- at 0 dB" {
		t.Fatalf("noise model %q", res.Mono.NoiseModel)
	}
	t.Log(res.Mono.HDR)
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

func TestSingleFrameIsPlainBlackSubtraction(t *testing.T) {
	sim := openSim(t)
	res, err := Capture(Options{Cam: sim, Light: SimLight{S: sim}, BaseUS: 4000, NoSettleCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mono.HDR != "single exposure per channel" || len(res.Notes) != 0 {
		t.Fatalf("%+v %v", res.Mono, res.Notes)
	}
}

func TestEstimateCapture(t *testing.T) {
	// scan 0281 base exposure: 14344 us
	for _, c := range []struct {
		evs  []int
		want float64
	}{{[]int{0}, 0.448}, {[]int{0, 2}, 1.025}, {[]int{0, 2, 3}, 1.774}, {DefaultEVs, 3.556}} {
		got := EstimateCapture(14344, c.evs, 1).Seconds()
		if math.Abs(got-c.want) > 0.01 {
			t.Errorf("%v: %.3f s, want %.3f", c.evs, got, c.want)
		}
	}
	if got := EstimateCapture(400000, DefaultEVs, 1).Seconds(); got > 3*(0.4*(1+4+8)+10+4*0.135)+0.01 {
		t.Errorf("+5 EV not capped at 10 s: %.1f", got)
	}
}
