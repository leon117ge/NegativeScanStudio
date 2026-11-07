package rgblight

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func newPair(t *testing.T, cfg func(*SimDevice)) (*SimDevice, *Client) {
	d := NewSimDevice()
	d.TelEvery = 20 * time.Millisecond
	d.StepTime = 5 * time.Millisecond
	if cfg != nil {
		cfg(d)
	}
	d.Start()
	c := NewClient(d)
	c.Timeout = 300 * time.Millisecond
	c.Start()
	t.Cleanup(func() { c.Close() })
	return d, c
}

func TestClientBasic(t *testing.T) {
	for _, chunk := range []int{0, 1, 3, 7} {
		for _, legacy := range []bool{false, true} {
			d, c := newPair(t, func(d *SimDevice) { d.Chunk = chunk; d.Legacy = legacy; d.EventFirst = chunk == 3 })
			in, err := c.Info()
			if err != nil || in.Model != "CUTENEW_RGB_AIR" || in.Protocol != "1.3" {
				t.Fatalf("info %+v %v", in, err)
			}
			s, err := c.SetAllConfirmed([3]bool{true, false, true}, [3]int{64, 1, 100})
			if err != nil {
				t.Fatalf("chunk %d: %v", chunk, err)
			}
			if legacy != (s.Focus == -1) {
				t.Fatalf("legacy %v focus %d", legacy, s.Focus)
			}
			on, v := d.Lights()
			if on != [3]bool{true, false, true} || v != [3]int{64, 1, 100} {
				t.Fatalf("device %v %v", on, v)
			}
			if err := c.SetBrightness(ChG, 33); err != nil {
				t.Fatal(err)
			}
			if err := c.SetEnable(ChG, true); err != nil {
				t.Fatal(err)
			}
			if _, v := d.Lights(); v[1] != 33 {
				t.Fatal("brightness not set")
			}
			if err := c.SetSpeed(2); err != nil {
				t.Fatal(err)
			}
			if err := c.SetTiming(Timing{500, 70, 120, 800}); err != nil {
				t.Fatal(err)
			}
			tm, err := c.Timing()
			if err != nil || tm != (Timing{500, 70, 120, 800}) {
				t.Fatalf("timing %+v %v", tm, err)
			}
			if err := c.SetFocus(1); err != nil {
				t.Fatal(err)
			}
			if f, err := c.Focus(); err != nil || f != 1 {
				t.Fatalf("focus %d %v", f, err)
			}
			if err := c.WaitIdle(time.Second); err != nil {
				t.Fatal(err)
			}
			if tl := c.LastTelemetry(); tl == nil || tl.TempC != 30.3 {
				t.Fatalf("telemetry %+v", tl)
			}
			c.Close()
		}
	}
}

func TestBrightnessRangeLocal(t *testing.T) {
	d, c := newPair(t, nil)
	if err := c.SetAll([3]bool{}, [3]int{0, 50, 50}); err == nil {
		t.Fatal("brightness 0 sent")
	}
	if err := c.SetBrightness(0, 101); err == nil {
		t.Fatal("brightness 101 sent")
	}
	if len(d.Received()) != 0 {
		t.Fatalf("invalid values reached the light: % X", d.Received())
	}
}

func TestResultErrors(t *testing.T) {
	_, c := newPair(t, nil)
	// green focus in direct mode is refused by the light with 03 (bypass the
	// local check by clearing the cache through a direct Request)
	if _, err := c.SetMode(1); err != nil {
		t.Fatal(err)
	}
	_, err := c.Request(CmdSetFocus, 1)
	var re *ResultError
	if !errors.As(err, &re) || re.Code != ResultBadParam {
		t.Fatalf("want 03, got %v", err)
	}
	if err := c.SetFocus(1); err == nil {
		t.Fatal("green focus in direct mode allowed locally")
	}
	// wrong length: 02 and 01 are both failures
	for _, code := range []byte{ResultBadLength, ResultUnknown} {
		_, c2 := newPair(t, func(d *SimDevice) { d.BadLenResult = code })
		_, err := c2.Request(CmdGetState, 0x00)
		if !errors.As(err, &re) || re.Code != code {
			t.Fatalf("bad length: %v", err)
		}
	}
}

func TestBusyDuringCapture(t *testing.T) {
	d, c := newPair(t, func(d *SimDevice) { d.StepTime = 40 * time.Millisecond })
	if err := c.StartCapture(); err == nil {
		t.Fatal("sequential capture started with all channels off")
	}
	if d.Capturing() {
		t.Fatal("capture running")
	}
	if err := c.SetAll([3]bool{true, true, true}, [3]int{10, 20, 30}); err != nil {
		t.Fatal(err)
	}
	if err := c.StartCapture(); err != nil {
		t.Fatal(err)
	}
	err := c.SetAll([3]bool{true, false, false}, [3]int{10, 20, 30})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("want busy, got %v", err)
	}
	if _, err := c.ReadState(); err != nil {
		t.Fatalf("20 must work while busy: %v", err)
	}
	if _, err := c.Timing(); err != nil {
		t.Fatalf("13 while busy: %v", err)
	}
	if _, err := c.Info(); err != nil {
		t.Fatalf("21 while busy: %v", err)
	}
	if err := c.WaitIdle(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := c.SetAll([3]bool{true, false, false}, [3]int{10, 20, 30}); err != nil {
		t.Fatal(err)
	}
}

func TestTimeoutNotRetried(t *testing.T) {
	// v0.81: a lost 03 reply is settled with 20 - the light applied it, so
	// it is a success and 03 is sent exactly once
	d, c := newPair(t, func(d *SimDevice) { d.DropReplies = 1 })
	var logs []string
	var mu sync.Mutex
	c.OnLog = func(s string) { mu.Lock(); logs = append(logs, s); mu.Unlock() }
	if err := c.SetAll([3]bool{true, false, false}, [3]int{5, 5, 5}); err != nil {
		t.Fatalf("lost reply, applied: want success, got %v", err)
	}
	if on, v := d.Lights(); on != [3]bool{true, false, false} || v != [3]int{5, 5, 5} {
		t.Fatalf("lights %v %v", on, v)
	}
	mu.Lock()
	if !hasLog(logs, "no 03 ack") || !hasLog(logs, "verified by 20") {
		t.Fatalf("warning not logged: %v", logs)
	}
	mu.Unlock()
	n := 0
	for _, b := range d.Received() {
		if b == CmdSetAll {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("command 03 sent %d times", n)
	}
	// a start-capture timeout is never resent either
	d.DropReplies = 0
	c.SetAll([3]bool{true, false, false}, [3]int{5, 5, 5})
	d.mu.Lock()
	d.DropReplies = 1
	d.mu.Unlock()
	if err := c.StartCapture(); err == nil {
		// the state read consumed the dropped reply; fine either way
	}
	n = 0
	for _, b := range d.Received() {
		if b == CmdStartCapture {
			n++
		}
	}
	if n > 1 {
		t.Fatalf("command 22 sent %d times", n)
	}
}

func TestSetModeWithoutAck(t *testing.T) {
	d, c := newPair(t, func(d *SimDevice) { d.NoAckMode = true })
	s, err := c.SetMode(1)
	if err != nil || s.Mode != 1 {
		t.Fatalf("mode confirm via state: %+v %v", s, err)
	}
	n := 0
	for _, b := range d.Received() {
		if b == CmdSetMode {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("command 10 sent %d times", n)
	}
}

func TestCorrelationIgnoresStrays(t *testing.T) {
	d, c := newPair(t, nil)
	// a stray response with another SEQ and one with the right SEQ but the
	// wrong CMD must not complete the request
	stray := func(seq, cmd byte) []byte {
		b, _ := Encode(Frame{Type: TypeResponse, Seq: seq, Payload: []byte{cmd, 0}})
		return b
	}
	var logs []string
	var mu sync.Mutex
	c.OnLog = func(s string) { mu.Lock(); logs = append(logs, s); mu.Unlock() }
	d.Inject(stray(200, CmdGetInfo))
	d.Inject(stray(1, CmdGetTiming))                           // next seq will be 1, wrong cmd
	d.Inject([]byte{0xAA, 0x55, 0x82, 0x00, 0x02, 0x00, 0x01}) // truncated garbage
	in, err := c.Info()
	if err != nil || in.Model == "" {
		t.Fatalf("info after strays: %+v %v", in, err)
	}
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(logs) < 2 {
		t.Fatalf("strays not logged: %v", logs)
	}
}

func TestSeqWrapsWithoutZero(t *testing.T) {
	_, c := newPair(t, func(d *SimDevice) { d.TelEvery = 0 })
	seen := map[byte]bool{}
	for i := 0; i < 300; i++ {
		if _, err := c.Request(CmdGetTiming); err != nil {
			t.Fatal(err)
		}
		c.mu.Lock()
		seen[c.seq] = true
		c.mu.Unlock()
	}
	if seen[0] || len(seen) != 255 {
		t.Fatalf("seq set: zero %v, %d values", seen[0], len(seen))
	}
}

func TestEventsAndFaults(t *testing.T) {
	d, c := newPair(t, nil)
	var mu sync.Mutex
	var faults []Fault
	var prog []Progress
	c.OnFault = func(f Fault) { mu.Lock(); faults = append(faults, f); mu.Unlock() }
	c.OnProgress = func(p Progress) { mu.Lock(); prog = append(prog, p); mu.Unlock() }
	d.InjectFault(2, 1, 1650)
	d.Inject(mustEncode(Frame{Type: TypeEvent, Payload: []byte{EventFault, 1, 0}})) // malformed: ignored
	c.SetAll([3]bool{true, false, true}, [3]int{1, 1, 1})
	if err := c.StartCapture(); err != nil {
		t.Fatal(err)
	}
	if err := c.WaitIdle(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(faults) != 1 || faults[0].Code != 2 || faults[0].Value != 1650 {
		t.Fatalf("faults %+v", faults)
	}
	if len(prog) == 0 || prog[len(prog)-1].Phase != 5 {
		t.Fatalf("progress %+v", prog)
	}
}

func mustEncode(f Frame) []byte {
	b, err := Encode(f)
	if err != nil {
		panic(err)
	}
	return b
}

func TestSafeClose(t *testing.T) {
	d, c := newPair(t, nil)
	c.SetAll([3]bool{true, true, true}, [3]int{80, 70, 60})
	c.ReadState()
	if err := c.SafeClose(time.Second); err != nil {
		t.Fatal(err)
	}
	on, v := d.Lights()
	if on != [3]bool{} || v != [3]int{80, 70, 60} {
		t.Fatalf("after safe close %v %v", on, v)
	}
	if !c.Closed() {
		t.Fatal("not closed")
	}
	// waits for a running capture to finish first
	d2, c2 := newPair(t, func(d *SimDevice) { d.StepTime = 30 * time.Millisecond })
	c2.SetAll([3]bool{true, true, true}, [3]int{80, 70, 60})
	c2.StartCapture()
	if err := c2.SafeClose(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	if on, _ := d2.Lights(); on != [3]bool{} || d2.Capturing() {
		t.Fatalf("after capture + safe close: %v", on)
	}
}

func TestUnplug(t *testing.T) {
	d, c := newPair(t, nil)
	got := make(chan error, 1)
	c.OnClosed = func(err error) { got <- err }
	d.Close()
	select {
	case err := <-got:
		if err == nil {
			t.Fatal("nil error")
		}
	case <-time.After(time.Second):
		t.Fatal("unplug not reported")
	}
	if _, err := c.Info(); !errors.Is(err, ErrClosed) {
		t.Fatalf("after unplug: %v", err)
	}
}
