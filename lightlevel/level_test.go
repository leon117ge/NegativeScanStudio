package lightlevel

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"basetool/rgblight"
)

type simLight struct {
	c *rgblight.Client
}

func (l simLight) SetAll(on [3]bool, v [3]int) error { return l.c.SetAll(on, v) }

func setup(t *testing.T) (*rgblight.SimDevice, *rgblight.Client) {
	d := rgblight.NewSimDevice()
	d.TelEvery = 0
	c := rgblight.NewClient(d)
	c.Start()
	t.Cleanup(func() { c.Close() })
	return d, c
}

type spy struct {
	*Synth
	d       *rgblight.SimDevice
	t       *testing.T
	prepare int
}

func (s *spy) Prepare() (string, error) { s.prepare++; return s.Synth.Prepare() }
func (s *spy) Shoot(ctx context.Context, ch int, p string) (*Image, error) {
	on, _ := s.d.Lights()
	for c := 0; c < 3; c++ {
		if on[c] != (c == ch) {
			s.t.Fatalf("shot %d with lights %v", ch, on)
		}
	}
	return s.Synth.Shoot(ctx, ch, p)
}

func TestLevelConverges(t *testing.T) {
	for _, target := range []float64{0.50, 0.60, 0.80} {
		d, c := setup(t)
		cam := &spy{Synth: NewSynth(d.Lights), d: d, t: t}
		cam.Gain = [3]float64{1.6, 1.1, 0.95}
		res, err := Run(context.Background(), simLight{c}, cam, Params{Target: target, Settle: time.Millisecond, Start: [3]int{50, 50, 50}}, [3]bool{true, true, true}, func(s string) { t.Log(s) })
		if err != nil {
			t.Fatal(err)
		}
		if !res.Converged() {
			t.Fatalf("target %.2f not converged: %v", target, res.Problems())
		}
		if cam.prepare != 1 {
			t.Fatalf("exposure prepared %d times", cam.prepare)
		}
		for ch := 0; ch < 3; ch++ {
			if e := math.Abs(res.Readings[ch]/target - 1); e > 0.02 {
				t.Fatalf("target %.2f ch %d reading %.4f", target, ch, res.Readings[ch])
			}
		}
		if res.Rounds > 4 {
			t.Fatalf("%d rounds", res.Rounds)
		}
		on, v := d.Lights()
		if on != [3]bool{true, true, true} || v != res.Values {
			t.Fatalf("light left at %v %v, result %v", on, v, res.Values)
		}
		if res.Values[2] <= res.Values[0] {
			t.Fatalf("blue should need more light than red: %v", res.Values)
		}
		if Composite(res.Images, target) == nil || Gray(res.Images[0], target) == nil {
			t.Fatal("no preview")
		}
		for _, b := range d.Received() {
			if b != rgblight.CmdSetAll {
				t.Fatalf("leveling sent command %02X", b)
			}
		}
	}
}

func TestLevelNeedsMoreThan100(t *testing.T) {
	d, c := setup(t)
	cam := NewSynth(d.Lights)
	cam.Gain[2] = 0.30 // blue would need ~150
	res, err := Run(context.Background(), simLight{c}, cam, Params{Target: 0.50, Settle: time.Millisecond, Start: [3]int{50, 50, 50}}, [3]bool{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Limited[2] || res.Values[2] != 100 || res.Needed[2] <= 100 {
		t.Fatalf("blue limit not reported: %+v", res)
	}
	if !res.Done[0] || !res.Done[1] {
		t.Fatalf("R/G should still converge: %v", res.Problems())
	}
	if !res.Verified || !strings.Contains(strings.Join(res.Problems(), "; "), "increase camera exposure") {
		t.Fatalf("problems %v", res.Problems())
	}
	if on, _ := d.Lights(); on != [3]bool{} {
		t.Fatalf("restore enables: %v", on)
	}
}

func TestLevelClipAndCancel(t *testing.T) {
	d, c := setup(t)
	cam := NewSynth(d.Lights)
	cam.Gain[0] = 3 // clips at 50
	res, err := Run(context.Background(), simLight{c}, cam, Params{Target: 0.5, Settle: time.Millisecond, MaxIter: 6, Start: [3]int{50, 50, 50}}, [3]bool{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Steps[0].Reading.BaseClipped() || !res.Done[0] {
		t.Fatalf("clip handling: %v / %v", res.Steps[0], res.Problems())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, simLight{c}, NewSynth(d.Lights), Params{Start: [3]int{50, 50, 50}}, [3]bool{}, nil); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestMeasure(t *testing.T) {
	im := &Image{W: 10, H: 10, V: make([]float32, 100), Clip: make([]bool, 100)}
	for i := range im.V {
		im.V[i] = 0.4
	}
	im.V[0], im.Clip[0] = 1, true
	r, err := Measure(im, Rect{0, 0, 1, 1})
	if err != nil || math.Abs(r.Level-0.4) > 1e-6 || r.ClipPct != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if r, _ := Measure(im, Rect{}); r.ClipPct != 0 {
		t.Fatal("centre area should exclude the corner")
	}
}

// v0.81: the v0.80 field fault - the light executes 03 but never
// acknowledges it (or answers in an odd form). Leveling must still complete,
// settling each 03 with a 20 read, without extra 03s.
func TestLevelWithoutSetAllAck(t *testing.T) {
	for _, st := range []rgblight.AckStyle{rgblight.AckNone, rgblight.AckSeq0, rgblight.AckEventOnly, rgblight.AckSwappedCRC} {
		d := rgblight.NewSimDevice()
		d.TelEvery = 0
		d.OddAck = st
		c := rgblight.NewClient(d)
		c.Timeout = 200 * time.Millisecond
		c.Start()
		cam := &spy{Synth: NewSynth(d.Lights), d: d, t: t}
		cam.Gain = [3]float64{1.6, 1.1, 0.95}
		res, err := Run(context.Background(), simLight{c}, cam, Params{Target: 0.6, Settle: time.Millisecond, Start: [3]int{50, 50, 50}}, [3]bool{true, true, true}, nil)
		if err != nil {
			t.Fatalf("%s: %v", rgblight.AckStyleNames[st], err)
		}
		if !res.Converged() {
			t.Fatalf("%s: not converged %v", rgblight.AckStyleNames[st], res.Problems())
		}
		if on, v := d.Lights(); on != [3]bool{true, true, true} || v != res.Values {
			t.Fatalf("light left at %v %v", on, v)
		}
		n03 := 0
		for _, b := range d.Received() {
			switch b {
			case rgblight.CmdSetAll:
				n03++
			case rgblight.CmdGetState:
			default:
				t.Fatalf("leveling sent command %02X", b)
			}
		}
		if n03 != len(res.Steps)+1 {
			t.Fatalf("%s: %d x 03 for %d steps + restore", rgblight.AckStyleNames[st], n03, len(res.Steps))
		}
		c.Close()
	}
}
