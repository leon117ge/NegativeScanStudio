package rgblight

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func hx(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCRCKnownVectors(t *testing.T) {
	if got := CRC16([]byte("123456789")); got != 0x29B1 {
		t.Fatalf("CRC16(123456789) = %04X, want 29B1", got)
	}
	if got := CRC16(nil); got != 0xFFFF {
		t.Fatalf("CRC16(empty) = %04X", got)
	}
}

// Example frames from the reverse-engineering report (computed with the
// documented CRC, not captured from a device).
var vectors = []struct {
	name string
	f    Frame
	wire string
}{
	{"R brightness 50", Frame{Type: TypeCommand, Seq: 1, Payload: []byte{0x01, 0x00, 0x32}}, "AA 55 01 01 03 00 01 00 32 FC C3"},
	{"R on", Frame{Type: TypeCommand, Seq: 2, Payload: []byte{0x02, 0x00, 0x01}}, "AA 55 01 02 03 00 02 00 01 7C 52"},
	// REPORT.md labels this frame "all on 100", but its payload enables R only
	// (03 01 64 00 64 00 64); the bytes and CRC are what is checked here.
	{"R on, G/B off, all 100", Frame{Type: TypeCommand, Seq: 3, Payload: []byte{0x03, 1, 100, 0, 100, 0, 100}}, "AA 55 01 03 07 00 03 01 64 00 64 00 64 53 6A"},
	{"all on 100 (computed)", Frame{Type: TypeCommand, Seq: 3, Payload: []byte{0x03, 1, 100, 1, 100, 1, 100}}, "AA 55 01 03 07 00 03 01 64 01 64 01 64 D6 2F"},
	{"all off keep 100", Frame{Type: TypeCommand, Seq: 4, Payload: []byte{0x03, 0, 100, 0, 100, 0, 100}}, "AA 55 01 04 07 00 03 00 64 00 64 00 64 09 33"},
}

func TestEncodeVectors(t *testing.T) {
	for _, v := range vectors {
		b, err := Encode(v.f)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b, hx(t, v.wire)) {
			t.Errorf("%s: % X, want %s", v.name, b, v.wire)
		}
		var p Parser
		fs := p.Feed(b)
		if len(fs) != 1 || fs[0].Type != v.f.Type || fs[0].Seq != v.f.Seq || !bytes.Equal(fs[0].Payload, v.f.Payload) {
			t.Errorf("%s: decode %v", v.name, fs)
		}
	}
	if _, err := Encode(Frame{Payload: make([]byte, 65)}); err == nil {
		t.Fatal("65-byte payload encoded")
	}
}

func TestParserChunkedAndMulti(t *testing.T) {
	var all []byte
	for _, v := range vectors {
		all = append(all, hx(t, v.wire)...)
	}
	// byte by byte
	var p Parser
	var got []Frame
	for _, c := range all {
		got = append(got, p.Feed([]byte{c})...)
	}
	if len(got) != len(vectors) {
		t.Fatalf("byte-wise: %d frames", len(got))
	}
	// all at once
	var q Parser
	if n := len(q.Feed(all)); n != len(vectors) {
		t.Fatalf("one read: %d frames", n)
	}
	// odd chunk sizes
	for cs := 2; cs < 9; cs++ {
		var r Parser
		n := 0
		for i := 0; i < len(all); i += cs {
			e := min(i+cs, len(all))
			n += len(r.Feed(all[i:e]))
		}
		if n != len(vectors) || r.Pending() != 0 {
			t.Fatalf("chunk %d: %d frames, %d pending", cs, n, r.Pending())
		}
	}
}

func TestParserResync(t *testing.T) {
	good := hx(t, vectors[0].wire)
	bad := append([]byte(nil), hx(t, vectors[1].wire)...)
	bad[len(bad)-1] ^= 0xFF // CRC error
	hugeLen := []byte{0xAA, 0x55, 0x81, 0x05, 0xFF, 0x00, 1, 2, 3}
	var s []byte
	s = append(s, 0x00, 0xAA, 0x12, 0x55) // noise incl. a lone AA
	s = append(s, bad...)
	s = append(s, hugeLen...)
	s = append(s, 0xAA, 0xAA) // AA AA then a real frame
	s = append(s, good...)
	s = append(s, hx(t, vectors[2].wire)...)
	var p Parser
	fs := p.Feed(s)
	if len(fs) != 2 || fs[0].Seq != 1 || fs[1].Seq != 3 {
		t.Fatalf("resync frames: %v", fs)
	}
	if p.BadCRC < 1 || p.Dropped == 0 {
		t.Fatalf("counters: crc %d dropped %d", p.BadCRC, p.Dropped)
	}
	// an embedded AA 55 inside a corrupted frame must not hide the next frame
	var q Parser
	emb := []byte{0xAA, 0x55, 0x01, 0x09, 0x03, 0x00, 0xAA, 0x55, 0x00, 0x00, 0x00}
	fs = q.Feed(append(emb, good...))
	if len(fs) != 1 || fs[0].Seq != 1 {
		t.Fatalf("embedded SOF: %v", fs)
	}
}

func TestReports(t *testing.T) {
	tel, err := ParseTelemetry([]byte{0x2F, 0x01, 0x5E, 0x07, 1, 2, 3, 4, 1})
	if err != nil || tel.TempC != 30.3 || tel.Volts != 18.86 || !tel.Capturing {
		t.Fatalf("telemetry %+v %v", tel, err)
	}
	neg, _ := ParseTelemetry([]byte{0xF6, 0xFF, 0, 0, 0, 0, 0, 0, 0})
	if neg.TempC != -1 || neg.Capturing {
		t.Fatalf("negative temp %+v", neg)
	}
	if _, err := ParseTelemetry(make([]byte, 8)); err == nil {
		t.Fatal("8-byte telemetry accepted")
	}
	s, err := ParseState([]byte{1, 64, 0, 10, 1, 100, 0, 2, 1})
	if err != nil || s.On != [3]bool{true, false, true} || s.Value != [3]int{64, 10, 100} || s.Mode != 0 || s.Speed != 2 || s.Focus != 1 {
		t.Fatalf("state %+v %v", s, err)
	}
	l, err := ParseState([]byte{1, 64, 0, 10, 1, 100, 1, 0})
	if err != nil || l.Focus != -1 || l.Mode != 1 {
		t.Fatalf("legacy state %+v %v", l, err)
	}
	if _, err := ParseState(make([]byte, 7)); err == nil {
		t.Fatal("short state accepted")
	}
	pr, err := ParseProgress([]byte{3, 2, 1, 3, 1})
	if err != nil || pr.Phase != 3 || pr.Channel != 2 || !pr.Active {
		t.Fatalf("progress %+v %v", pr, err)
	}
	f, err := ParseFault([]byte{1, 0, 0x8A, 0x02, 1})
	if err != nil || f.Code != 1 || f.Value != 650 || f.Severity != 1 {
		t.Fatalf("fault %+v %v", f, err)
	}
	if strings.Contains(strings.ToLower(f.String()), "cleared") {
		t.Fatal("fault polarity must not be labelled")
	}
	in, err := ParseInfo([]byte("M\x00S1\x00H\x00F\x001.3\x00"))
	if err != nil || in.Protocol != "1.3" || in.Serial != "S1" {
		t.Fatalf("info %+v %v", in, err)
	}
	if _, err := ParseInfo([]byte("M\x00S1\x00H\x00F\x001.3")); err == nil {
		t.Fatal("unterminated info accepted")
	}
}

func TestOnlyDocumentedCommands(t *testing.T) {
	ok := []byte{0x01, 0x02, 0x03, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x20, 0x21, 0x22}
	for c := 0; c < 256; c++ {
		want := bytes.IndexByte(ok, byte(c)) >= 0
		if Allowed(byte(c)) != want {
			t.Fatalf("Allowed(%02X) = %v", c, !want)
		}
	}
	c := NewClient(NewSimDevice())
	c.Start()
	defer c.Close()
	for _, bad := range []byte{0x23, 0x30, 0x00, 0xF0} {
		if _, err := c.Request(bad); err == nil {
			t.Fatalf("command %02X was sent", bad)
		}
	}
}

func TestTimingValidate(t *testing.T) {
	if (Timing{1, 1, 1, 1}).Validate() != nil || (Timing{10000, 10000, 1000, 60000}).Validate() != nil {
		t.Fatal("valid timing rejected")
	}
	for _, x := range []Timing{{0, 1, 1, 1}, {1, 10001, 1, 1}, {1, 1, 1001, 1}, {1, 1, 1, 60001}} {
		if x.Validate() == nil {
			t.Fatalf("%+v accepted", x)
		}
	}
}
