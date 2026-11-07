package rgblight

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func hasLog(logs []string, sub string) bool {
	for _, l := range logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

type logSink struct {
	mu  sync.Mutex
	l   []string
	raw []string
}

func (s *logSink) hook(c *Client) {
	c.OnLog = func(m string) { s.mu.Lock(); s.l = append(s.l, m); s.mu.Unlock() }
	c.OnRaw = func(m string) { s.mu.Lock(); s.raw = append(s.raw, m); s.mu.Unlock() }
}

func (s *logSink) has(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return hasLog(s.l, sub)
}

func (s *logSink) hasRaw(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return hasLog(s.raw, sub)
}

func count(d *SimDevice, cmd byte) int {
	n := 0
	for _, b := range d.Received() {
		if b == cmd {
			n++
		}
	}
	return n
}

func TestSetAllOddAcks(t *testing.T) {
	for st := AckNone; st <= AckNoResult; st++ {
		t.Run(AckStyleNames[st], func(t *testing.T) {
			d, c := newPair(t, func(d *SimDevice) { d.OddAck = st; d.LateAfter = 450 * time.Millisecond })
			var ls logSink
			ls.hook(c)
			want := [3]bool{false, true, false}
			wv := [3]int{12, 34, 56}
			if err := c.SetAll(want, wv); err != nil {
				t.Fatalf("SetAll: %v", err)
			}
			s, err := c.SetAllConfirmed([3]bool{true, true, true}, [3]int{50, 50, 50})
			if err != nil || s.On != [3]bool{true, true, true} {
				t.Fatalf("SetAllConfirmed: %v %v", s, err)
			}
			if n := count(d, CmdSetAll); n != 2 {
				t.Fatalf("03 sent %d times for 2 calls", n)
			}
			if st == AckEventOnly {
				// v0.82: FW V1.0.5 behaviour - the state event is the confirmation
				if ls.has("verified by 20") || count(d, CmdGetState) != 0 {
					t.Fatalf("event-confirmed 03 must not warn or send 20 (20 sent %d times): %v", count(d, CmdGetState), ls.l)
				}
				if !ls.hasRaw("confirmed by state event 01") {
					t.Fatalf("event confirmation not in the raw log")
				}
				return
			}
			if !ls.has("verified by 20") {
				t.Fatalf("no warning: %v", ls.l)
			}
			if st == AckLate {
				time.Sleep(500 * time.Millisecond)
				if !ls.has("LATE reply") {
					t.Fatalf("late reply not logged: %v", ls.l)
				}
			}
			if st == AckSwappedCRC && !ls.hasRaw("CRC bytes swapped") {
				t.Fatalf("CRC variant not in raw log")
			}
		})
	}
}

func TestSetAllNotAppliedResentOnce(t *testing.T) {
	d, c := newPair(t, func(d *SimDevice) { d.OddAck = AckNone; d.IgnoreSetAll = 1 })
	var ls logSink
	ls.hook(c)
	if err := c.SetAll([3]bool{true, true, false}, [3]int{9, 8, 7}); err != nil {
		t.Fatal(err)
	}
	if n := count(d, CmdSetAll); n != 2 {
		t.Fatalf("03 sent %d times, want 2 (one verified resend)", n)
	}
	if !ls.has("NOT applied") {
		t.Fatalf("resend not logged: %v", ls.l)
	}
	// never applied: exactly one resend, then an error
	d.mu.Lock()
	d.IgnoreSetAll = 5
	d.mu.Unlock()
	err := c.SetAll([3]bool{false, false, true}, [3]int{1, 2, 3})
	if err == nil || !strings.Contains(err.Error(), "not applied after one resend") {
		t.Fatalf("want failure after one resend, got %v", err)
	}
	if n := count(d, CmdSetAll); n != 4 {
		t.Fatalf("03 sent %d times in total, want 4", n)
	}
}

func TestAckedErrorsNotVerified(t *testing.T) {
	d, c := newPair(t, nil)
	d.mu.Lock()
	d.capture = true
	d.mu.Unlock()
	err := c.SetAll([3]bool{true, true, true}, [3]int{1, 1, 1})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("want busy, got %v", err)
	}
	if count(d, CmdGetState) != 0 || count(d, CmdSetAll) != 1 {
		t.Fatalf("busy must not be verified or resent: %X", d.Received())
	}
	d.mu.Lock()
	d.capture = false
	d.mu.Unlock()
}

func TestOtherSettersVerified(t *testing.T) {
	all := []byte{CmdSetBrightness, CmdSetEnable, CmdSetAll, CmdSetMode, CmdSetSpeed, CmdSetTiming, CmdSetFocus}
	d, c := newPair(t, func(d *SimDevice) { d.OddAck = AckNone; d.OddAckCmds = all })
	var ls logSink
	ls.hook(c)
	if err := c.SetBrightness(1, 77); err != nil {
		t.Fatal("01:", err)
	}
	if err := c.SetEnable(2, true); err != nil {
		t.Fatal("02:", err)
	}
	if err := c.SetSpeed(2); err != nil {
		t.Fatal("11:", err)
	}
	if err := c.SetTiming(Timing{500, 60, 100, 300}); err != nil {
		t.Fatal("12:", err)
	}
	if err := c.SetFocus(1); err != nil {
		t.Fatal("14:", err)
	}
	if s, err := c.SetMode(1); err != nil || s.Mode != 1 {
		t.Fatal("10:", s, err)
	}
	for _, cmd := range []byte{CmdSetBrightness, CmdSetEnable, CmdSetSpeed, CmdSetTiming, CmdSetFocus, CmdSetMode} {
		if n := count(d, cmd); n != 1 {
			t.Fatalf("%02X sent %d times", cmd, n)
		}
	}
	if !ls.has("no 12 ack") || !ls.has("verified by 13") || !ls.has("no 01 ack") {
		t.Fatalf("warnings: %v", ls.l)
	}
	if s, _ := c.ReadState(); s.Value[1] != 77 || !s.On[2] || s.Speed != 2 {
		t.Fatalf("state %v", s)
	}
}

func TestLegacyFocusNotResent(t *testing.T) {
	d, c := newPair(t, func(d *SimDevice) { d.Legacy = true; d.OddAck = AckNone; d.OddAckCmds = []byte{CmdSetFocus} })
	err := c.SetFocus(1)
	if err == nil || !strings.Contains(err.Error(), "not resent") {
		t.Fatalf("want unverifiable error, got %v", err)
	}
	if n := count(d, CmdSetFocus); n != 1 {
		t.Fatalf("14 sent %d times", n)
	}
}

func TestShortAckWaitAfterMisses(t *testing.T) {
	d, c := newPair(t, func(d *SimDevice) { d.OddAck = AckNone })
	c.Timeout = 1500 * time.Millisecond
	var ls logSink
	ls.hook(c)
	for i := 0; i < 2; i++ {
		if err := c.SetAll([3]bool{true, false, false}, [3]int{10 + i, 10, 10}); err != nil {
			t.Fatal(err)
		}
	}
	t0 := time.Now()
	if err := c.SetAll([3]bool{true, false, false}, [3]int{40, 10, 10}); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(t0); el > time.Second {
		t.Fatalf("third unacknowledged 03 took %v", el)
	}
	if !ls.has("waiting only") {
		t.Fatalf("no note: %v", ls.l)
	}
	// a clean ack restores the normal timeout
	d.mu.Lock()
	d.OddAck = AckNormal
	d.mu.Unlock()
	if err := c.SetAll([3]bool{true, false, false}, [3]int{41, 10, 10}); err != nil {
		t.Fatal(err)
	}
	if !ls.has("acknowledged again") {
		t.Fatalf("no restore note: %v", ls.l)
	}
}

func TestRawLog(t *testing.T) {
	d, c := newPair(t, func(d *SimDevice) { d.OddAck = AckNone })
	var ls logSink
	ls.hook(c)
	d.Inject([]byte{0x11, 0x22, 0xAA, 0x55, 0x81, 0x05, 0x02, 0x00, 0x03, 0x00, 0xDE, 0xAD})
	if err := c.SetAll([3]bool{true, true, true}, [3]int{50, 50, 50}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	for _, w := range []string{
		"TX type 01 seq 1 len 7 [command 03 (set all channels)]: AA 55 01 01 07 00 03 01 32 01 32 01 32",
		"TIMEOUT: no reply to seq 1 cmd 03",
		"TX type 01 seq 2 len 1 [command 20",
		"RX type 81 seq 2 len 2 [response cmd 20 result 00]",
		"RX type 83 seq 0 len 10 [event 01]",
		"RX type 82 seq 0 len 9 [telemetry]",
		"RX DISCARDED",
		"bad CRC",
		"t+",
	} {
		if !ls.hasRaw(w) {
			ls.mu.Lock()
			t.Fatalf("raw log lacks %q:\n%s", w, strings.Join(ls.raw, "\n"))
		}
	}
}

func TestReadsIgnoreOtherSeq(t *testing.T) {
	d, c := newPair(t, nil)
	var ls logSink
	ls.hook(c)
	b, _ := Encode(Frame{Type: TypeResponse, Seq: 77, Payload: []byte{CmdGetInfo, 0, 'X', 0}})
	d.mu.Lock()
	d.DropReplies = 1
	d.mu.Unlock()
	go func() { time.Sleep(50 * time.Millisecond); d.Inject(b) }()
	_, err := c.Info()
	var ue *UncertainAckError
	if !errors.As(err, &ue) {
		t.Fatalf("a read answered only with another SEQ must not succeed: %v", err)
	}
}

func TestParserCRCVariants(t *testing.T) {
	b, _ := Encode(Frame{Type: TypeResponse, Seq: 4, Payload: []byte{CmdSetAll, 0}})
	n := len(b)
	b[n-2], b[n-1] = b[n-1], b[n-2]
	var p Parser
	var disc []string
	p.OnDiscard = func(x []byte, why string) { disc = append(disc, why) }
	fs := p.Feed(b)
	if len(fs) != 1 || fs[0].AltCRC == "" || p.AltCRCs != 1 {
		t.Fatalf("swapped CRC: %+v", fs)
	}
	bad := append([]byte(nil), b...)
	bad[n-1] ^= 0x5A
	bad[n-2] ^= 0x33
	fs = p.Feed(append(bad, 0xAA, 0x55, 0x82, 0, 9, 0)) // + an incomplete frame
	if len(fs) != 0 || len(disc) == 0 || !strings.Contains(disc[0], "bad CRC") {
		t.Fatalf("frames %v discards %v", fs, disc)
	}
	if p.Pending() == 0 || len(p.PendingBytes()) != p.Pending() {
		t.Fatal("pending bytes")
	}
}

// TestSetAllEventConfirmFast: 03 confirmed by the 83 event 01 returns as
// soon as the event arrives (no 400 ms / full timeout wait, no 20).
func TestSetAllEventConfirmFast(t *testing.T) {
	d, c := newPair(t, func(d *SimDevice) { d.OddAck = AckEventOnly })
	var ls logSink
	ls.hook(c)
	for i := 0; i < 6; i++ {
		on := [3]bool{i%3 == 0, i%3 == 1, i%3 == 2}
		t0 := time.Now()
		s, err := c.SetAllConfirmed(on, [3]int{59, 96, 38})
		if err != nil || s.On != on || s.Value != [3]int{59, 96, 38} {
			t.Fatalf("switch %d: %v %v", i, s, err)
		}
		if dt := time.Since(t0); dt > 150*time.Millisecond {
			t.Fatalf("switch %d took %v (should not wait for a timeout)", i, dt)
		}
	}
	if n := count(d, CmdGetState); n != 0 {
		t.Fatalf("20 sent %d times", n)
	}
	if ls.has("WARNING") {
		t.Fatalf("warning logged: %v", ls.l)
	}
	// a stale or non-matching event does not confirm: ignored 03 -> 20 decides, one resend
	d.mu.Lock()
	d.IgnoreSetAll = 1
	d.mu.Unlock()
	if _, err := c.SetAllConfirmed([3]bool{true, true, true}, [3]int{10, 20, 30}); err != nil {
		t.Fatalf("resend path: %v", err)
	}
	if !ls.has("NOT applied") {
		t.Fatalf("resend not logged: %v", ls.l)
	}
}
