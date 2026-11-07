package camera

import (
	"encoding/binary"
	"os"
	"testing"
	"unicode/utf16"
)

// mockXT3 behaves like the X-T3 in the user's first log: 0x0304 without a
// preceding S1 half-press gives GeneralError; PriorityMode is busy right
// after live view; the shot appears as a new object.
type mockXT3 struct {
	t           *testing.T
	ctl         int64
	halfPressed bool
	needHalf    bool
	busyD207    int
	objects     []uint32
	ops         []string
}

func u16(v uint16) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); return b }
func u32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }
func pstr(s string) []byte {
	u := utf16.Encode([]rune(s + "\x00"))
	b := []byte{byte(len(u))}
	for _, c := range u {
		b = append(b, u16(c)...)
	}
	return b
}

func (m *mockXT3) Exec(op uint16, params []uint32, out []byte, want bool) (uint16, []uint32, []byte, error) {
	switch op {
	case OpSetDevicePropValue:
		v := int64(0)
		if len(out) >= 2 {
			v = int64(binary.LittleEndian.Uint16(out))
		}
		switch uint16(params[0]) {
		case PropFujiPriority:
			if m.busyD207 > 0 {
				m.busyD207--
				return RespDeviceBusy, nil, nil, nil
			}
		case PropFujiCaptureCtl:
			m.ctl = v
		}
		return RespOK, nil, nil, nil
	case OpGetDevicePropValue:
		switch uint16(params[0]) {
		case PropFujiAFStatus:
			return RespOK, nil, u16(2), nil
		case PropExposureTime:
			return RespOK, nil, u32(7812), nil
		case PropFujiCurrentState:
			return RespOK, nil, append(u16(1), append(u16(0xD20E), u32(0)...)...), nil
		}
		return RespOK, nil, u16(1), nil
	case OpInitiateCapture:
		m.ops = append(m.ops, "IC")
		switch m.ctl {
		case 0x0200:
			m.halfPressed = true
		case 0x0004:
			m.halfPressed = false
		case 0x0304:
			if m.needHalf && !m.halfPressed {
				return RespGeneralError, nil, nil, nil
			}
			m.objects = append(m.objects, 0x90000001)
			m.halfPressed = false
		}
		return RespOK, nil, nil, nil
	case OpGetObjectHandles:
		b := u32(uint32(len(m.objects)))
		for _, h := range m.objects {
			b = append(b, u32(h)...)
		}
		return RespOK, nil, b, nil
	case OpGetObjectInfo:
		b := append(u32(0x10000001), u16(0xB103)...)
		b = append(b, u16(0)...)
		b = append(b, u32(10)...)
		b = append(b, u16(0)...)
		for i := 0; i < 8; i++ {
			b = append(b, u32(0)...)
		}
		b = append(b, u16(0)...)
		b = append(b, u32(0)...)
		b = append(b, u32(0)...)
		b = append(b, pstr("DSCF0001.RAF")...)
		return RespOK, nil, b, nil
	case OpGetObject:
		return RespOK, nil, []byte("FUJIFILMCCD-RAW 0201"), nil
	case OpDeleteObject:
		m.objects = nil
		return RespOK, nil, nil, nil
	}
	return RespOK, nil, nil, nil
}
func (m *mockXT3) SupportedVendorOps() ([]uint16, error) { return nil, nil }
func (m *mockXT3) Close() error                          { return nil }

func newMockFuji(t *testing.T, m *mockXT3) *Fuji {
	log := NewLogger("")
	log.OnLine = func(s string) { t.Log(s) }
	f := &Fuji{p: &PTP{T: m, Log: log, Verbose: false}, log: log, okStrategy: -1,
		ctlEnum: map[int64]bool{0x104: true, 0x200: true, 0x4: true, 0x304: true, 0x500: true, 0xc: true}}
	return f
}

func TestCaptureUsesHalfPressFirst(t *testing.T) {
	m := &mockXT3{t: t, needHalf: true, busyD207: 2}
	f := newMockFuji(t, m)
	dir := t.TempDir()
	p, err := f.Capture(dir, "t_")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	if f.okStrategy != 0 {
		t.Fatalf("strategy %d, want A (0)", f.okStrategy)
	}
	if len(m.ops) != 2 {
		t.Fatalf("InitiateCapture calls %d, want 2 (S1 + shoot)", len(m.ops))
	}
}

func TestCaptureFallsBackToDirect(t *testing.T) {
	// a body that rejects 0x0200 (not in its enum) still shoots with C
	m := &mockXT3{t: t}
	f := newMockFuji(t, m)
	delete(f.ctlEnum, 0x200)
	if _, err := f.Capture(t.TempDir(), "t_"); err != nil {
		t.Fatal(err)
	}
	if f.okStrategy != 2 {
		t.Fatalf("strategy %d, want C (2)", f.okStrategy)
	}
}
