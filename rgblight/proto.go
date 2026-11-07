// Package rgblight talks to the CuteNewDesign RGB Light Air over its USB
// CDC serial port (protocol RGBAIR API 1.3).
//
// Only the documented customer commands are implemented: 01, 02, 03, 10,
// 11, 12, 13, 14, 15, 20, 21 and 22. There is deliberately no code for the
// serial-number write, firmware update / YMODEM or any other command, and
// Request refuses every command byte that is not in that list.
//
// Frame: AA 55 | TYPE | SEQ | LEN u16 LE | PAYLOAD (LEN bytes, CMD first)
// | CRC16 LE. The CRC is CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF, no
// reflection, no final xor) over TYPE..PAYLOAD.
package rgblight

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// Frame types.
const (
	TypeCommand   byte = 0x01
	TypeResponse  byte = 0x81
	TypeTelemetry byte = 0x82
	TypeEvent     byte = 0x83
)

// Documented customer commands (the only ones this package can send).
const (
	CmdSetBrightness byte = 0x01 // channel, value 1..100
	CmdSetEnable     byte = 0x02 // channel, on
	CmdSetAll        byte = 0x03 // R_on R_val G_on G_val B_on B_val
	CmdSetMode       byte = 0x10 // 0 sequential, 1 direct
	CmdSetSpeed      byte = 0x11 // 0 fast, 1 compatibility, 2 custom
	CmdSetTiming     byte = 0x12 // 4 x u32 LE: focus, stable, shot, exposure (ms)
	CmdGetTiming     byte = 0x13
	CmdSetFocus      byte = 0x14 // 0 mixed, 1 green
	CmdGetFocus      byte = 0x15
	CmdGetState      byte = 0x20 // ack + a separate state event (83 01)
	CmdGetInfo       byte = 0x21
	CmdStartCapture  byte = 0x22
)

// Allowed reports whether cmd is one of the documented customer commands.
func Allowed(cmd byte) bool {
	switch cmd {
	case CmdSetBrightness, CmdSetEnable, CmdSetAll, CmdSetMode, CmdSetSpeed, CmdSetTiming,
		CmdGetTiming, CmdSetFocus, CmdGetFocus, CmdGetState, CmdGetInfo, CmdStartCapture:
		return true
	}
	return false
}

// SideEffect reports whether cmd changes the light (never retried blindly).
func SideEffect(cmd byte) bool {
	switch cmd {
	case CmdGetTiming, CmdGetFocus, CmdGetState, CmdGetInfo:
		return false
	}
	return true
}

// CmdName is a short name for logs.
func CmdName(cmd byte) string {
	switch cmd {
	case CmdSetBrightness:
		return "set brightness"
	case CmdSetEnable:
		return "set enable"
	case CmdSetAll:
		return "set all channels"
	case CmdSetMode:
		return "set capture mode"
	case CmdSetSpeed:
		return "set speed"
	case CmdSetTiming:
		return "set custom timing"
	case CmdGetTiming:
		return "read timing"
	case CmdSetFocus:
		return "set focus light"
	case CmdGetFocus:
		return "read focus light"
	case CmdGetState:
		return "read state"
	case CmdGetInfo:
		return "read device info"
	case CmdStartCapture:
		return "start capture"
	}
	return fmt.Sprintf("cmd %02X", cmd)
}

// Result codes (second payload byte of a response).
const (
	ResultOK           byte = 0x00
	ResultUnknown      byte = 0x01 // unknown command / invalid length (firmware-dependent)
	ResultBadLength    byte = 0x02
	ResultBadParam     byte = 0x03
	ResultBusy         byte = 0x04
	ResultNotSupported byte = 0x05
)

// Event types (first payload byte of a TYPE 83 frame).
const (
	EventState    byte = 0x01
	EventProgress byte = 0x02
	EventFault    byte = 0x03
)

// Channels.
const (
	ChR = 0
	ChG = 1
	ChB = 2
)

// ChannelNames are "R", "G", "B".
var ChannelNames = [3]string{"R", "G", "B"}

const (
	sof0     = 0xAA
	sof1     = 0x55
	MaxLen   = 64 // largest LEN (payload incl. CMD) the firmware accepts
	overhead = 8  // SOF(2) TYPE SEQ LEN(2) CRC(2)
)

// CRC16 is CRC-16/CCITT-FALSE.
func CRC16(b []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, c := range b {
		crc ^= uint16(c) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// Frame is one decoded frame. Payload starts with CMD (responses /
// commands) or the event type (events); telemetry has no leading byte.
type Frame struct {
	Type    byte
	Seq     byte
	Payload []byte
	// Raw is the complete wire frame (SOF..CRC), for the raw serial log.
	Raw []byte
	// AltCRC is non-empty when the documented CRC did not match but a
	// plausible variant did (v0.81 diagnostic, e.g. "CRC bytes swapped").
	// Such a frame is never taken as a confirmed acknowledgement.
	AltCRC string
}

func (f Frame) String() string {
	return fmt.Sprintf("type %02X seq %d payload % X", f.Type, f.Seq, f.Payload)
}

// Encode builds the wire bytes of f.
func Encode(f Frame) ([]byte, error) {
	if len(f.Payload) > MaxLen {
		return nil, fmt.Errorf("payload %d bytes > %d", len(f.Payload), MaxLen)
	}
	b := make([]byte, 0, len(f.Payload)+overhead)
	b = append(b, sof0, sof1, f.Type, f.Seq, 0, 0)
	binary.LittleEndian.PutUint16(b[4:], uint16(len(f.Payload)))
	b = append(b, f.Payload...)
	crc := CRC16(b[2:])
	b = binary.LittleEndian.AppendUint16(b, crc)
	return b, nil
}

func crc16init(b []byte, init uint16) uint16 {
	crc := init
	for _, c := range b {
		crc ^= uint16(c) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// altCRC checks the frame candidate fr (SOF..CRC) against CRC variants a
// firmware might use by mistake. It returns the variant's name or "".
func altCRC(fr []byte) string {
	n := len(fr)
	body := fr[2 : n-2]
	le := binary.LittleEndian.Uint16(fr[n-2:])
	be := binary.BigEndian.Uint16(fr[n-2:])
	switch {
	case CRC16(body) == be:
		return "CRC bytes swapped (high byte first)"
	case CRC16(fr[:n-2]) == le:
		return "CRC includes SOF"
	case CRC16(fr[:n-2]) == be:
		return "CRC includes SOF, high byte first"
	case crc16init(body, 0) == le:
		return "CRC init 0x0000 (XMODEM)"
	case crc16init(body, 0) == be:
		return "CRC init 0x0000 (XMODEM), high byte first"
	}
	return ""
}

// Parser is a streaming frame decoder: feed it whatever the port returns
// (partial frames, several frames, garbage) and it returns the complete
// frames. A bad length or CRC drops one byte and resynchronises on the next
// AA 55.
type Parser struct {
	buf []byte
	// Dropped counts bytes discarded while resynchronising.
	Dropped int
	// BadCRC counts candidate frames rejected for a CRC mismatch.
	BadCRC int
	// AltCRCs counts frames accepted only through a CRC variant.
	AltCRCs int
	// OnDiscard (optional) receives every run of discarded bytes with the
	// reason the run started (bad CRC, bad LEN, no SOF).
	OnDiscard func(b []byte, why string)

	disc     []byte
	discWhy  string
	discKind string
}

func (p *Parser) discard(b []byte, why string) {
	p.Dropped += len(b)
	if p.OnDiscard == nil {
		return
	}
	kind, _, _ := strings.Cut(why, ":")
	if len(p.disc) > 0 && kind != p.discKind {
		p.flushDiscard()
	}
	if len(p.disc) == 0 {
		p.discWhy, p.discKind = why, kind
	}
	p.disc = append(p.disc, b...)
	if len(p.disc) >= 256 {
		p.flushDiscard()
	}
}

func (p *Parser) flushDiscard() {
	if len(p.disc) > 0 && p.OnDiscard != nil {
		p.OnDiscard(p.disc, p.discWhy)
	}
	p.disc, p.discWhy, p.discKind = nil, "", ""
}

// Feed appends b and returns the frames completed by it.
func (p *Parser) Feed(b []byte) []Frame {
	p.buf = append(p.buf, b...)
	var out []Frame
	for {
		// find SOF
		i := 0
		for i < len(p.buf) {
			if p.buf[i] == sof0 && (i+1 == len(p.buf) || p.buf[i+1] == sof1) {
				break
			}
			i++
		}
		if i > 0 {
			p.discard(p.buf[:i], "bytes outside a frame (no AA 55)")
			p.buf = p.buf[i:]
		}
		if len(p.buf) < 6 {
			break
		}
		n := int(binary.LittleEndian.Uint16(p.buf[4:]))
		if n > MaxLen {
			p.drop1(fmt.Sprintf("bad LEN: %d > %d in header % X", n, MaxLen, p.buf[:6]))
			continue
		}
		if len(p.buf) < n+overhead {
			break
		}
		fr := p.buf[:n+overhead]
		body := fr[2 : 6+n]
		got := binary.LittleEndian.Uint16(fr[6+n:])
		alt := ""
		if want := CRC16(body); want != got {
			alt = altCRC(fr)
			if alt == "" {
				p.BadCRC++
				p.drop1(fmt.Sprintf("bad CRC: candidate frame % X (CRC field %04X, computed %04X)", fr, got, want))
				continue
			}
			p.AltCRCs++
		}
		p.flushDiscard()
		raw := append([]byte(nil), fr...)
		out = append(out, Frame{Type: raw[2], Seq: raw[3], Payload: raw[6 : 6+n : 6+n], Raw: raw, AltCRC: alt})
		p.buf = p.buf[n+overhead:]
	}
	if len(p.buf) == 0 {
		p.buf = nil // let the backing array go
	}
	p.flushDiscard()
	return out
}

func (p *Parser) drop1(why string) {
	p.discard(p.buf[:1], why)
	p.buf = p.buf[1:]
}

// Pending is the number of buffered bytes not yet forming a frame.
func (p *Parser) Pending() int { return len(p.buf) }

// PendingBytes is a copy of the buffered bytes not yet forming a frame.
func (p *Parser) PendingBytes() []byte { return append([]byte(nil), p.buf...) }

// ResultError is a non-zero RESULT byte.
type ResultError struct {
	Cmd, Code byte
}

func (e *ResultError) Error() string {
	var s string
	switch e.Code {
	case ResultUnknown:
		s = "unknown command or invalid length (01)"
	case ResultBadLength:
		s = "invalid length (02)"
	case ResultBadParam:
		s = "invalid parameter (03)"
	case ResultBusy:
		s = "busy - the light is running a capture (04)"
	case ResultNotSupported:
		s = "not supported in the current mode (05)"
	default:
		s = fmt.Sprintf("error %02X", e.Code)
	}
	return fmt.Sprintf("light refused %s: %s", CmdName(e.Cmd), s)
}

// ErrBusy matches a RESULT 04 with errors.Is.
var ErrBusy = errors.New("light busy")

func (e *ResultError) Is(t error) bool { return t == ErrBusy && e.Code == ResultBusy }

// Telemetry is a TYPE 82 frame (about every 500 ms).
type Telemetry struct {
	TempC     float64 // controller temperature
	Volts     float64 // supply voltage
	Capturing bool
	Diag      [2]uint16 // undocumented diagnostic fields, shown raw only
}

// ParseTelemetry decodes the 9-byte telemetry payload.
func ParseTelemetry(p []byte) (Telemetry, error) {
	if len(p) != 9 {
		return Telemetry{}, fmt.Errorf("telemetry has %d bytes, want 9", len(p))
	}
	return Telemetry{
		TempC:     float64(int16(binary.LittleEndian.Uint16(p[0:]))) / 10,
		Volts:     float64(binary.LittleEndian.Uint16(p[2:])) / 100,
		Diag:      [2]uint16{binary.LittleEndian.Uint16(p[4:]), binary.LittleEndian.Uint16(p[6:])},
		Capturing: p[8] != 0,
	}, nil
}

// State is the state event (83 01).
type State struct {
	On    [3]bool
	Value [3]int // 1..100
	Mode  int    // 0 sequential, 1 direct
	Speed int    // 0 fast, 1 compatibility, 2 custom
	Focus int    // 0 mixed, 1 green; -1 = not reported (legacy 9-byte layout)
}

// ModeNames / SpeedNames / FocusNames label the state fields.
var (
	ModeNames  = []string{"sequential", "direct"}
	SpeedNames = []string{"fast", "compatibility", "custom"}
	FocusNames = []string{"mixed", "green"}
)

func name(n []string, i int) string {
	if i >= 0 && i < len(n) {
		return n[i]
	}
	return fmt.Sprintf("%d?", i)
}

func (s State) String() string {
	t := ""
	for c := 0; c < 3; c++ {
		on := "off"
		if s.On[c] {
			on = "on"
		}
		t += fmt.Sprintf("%s %s %d, ", ChannelNames[c], on, s.Value[c])
	}
	t += "mode " + name(ModeNames, s.Mode) + ", speed " + name(SpeedNames, s.Speed)
	if s.Focus >= 0 {
		t += ", focus " + name(FocusNames, s.Focus)
	}
	return t
}

// ParseState decodes the event data after the event byte: 9 bytes
// (protocol 1.3: en/val x3, capture mode, speed, focus) or 8 bytes (legacy
// layout without the focus field; Focus is then -1, never invented).
func ParseState(d []byte) (State, error) {
	if len(d) != 9 && len(d) != 8 {
		return State{}, fmt.Errorf("state event has %d data bytes, want 9 (or 8 legacy)", len(d))
	}
	var s State
	for c := 0; c < 3; c++ {
		s.On[c] = d[2*c] != 0
		s.Value[c] = int(d[2*c+1])
	}
	s.Mode, s.Speed, s.Focus = int(d[6]), int(d[7]), -1
	if len(d) == 9 {
		s.Focus = int(d[8])
	}
	return s, nil
}

// Progress is the progress event (83 02).
type Progress struct {
	Phase, Channel, Step, Total int
	Active                      bool
}

// PhaseNames label Progress.Phase (0 idle .. 5 complete).
var PhaseNames = []string{"idle", "focus", "stabilise", "shot", "exposure", "complete"}

func (p Progress) String() string {
	return fmt.Sprintf("capture %s, channel %d, step %d/%d, active %v", name(PhaseNames, p.Phase), p.Channel, p.Step, p.Total, p.Active)
}

// ParseProgress decodes the 5 data bytes after the event byte.
func ParseProgress(d []byte) (Progress, error) {
	if len(d) != 5 {
		return Progress{}, fmt.Errorf("progress event has %d data bytes, want 5", len(d))
	}
	return Progress{Phase: int(d[0]), Channel: int(d[1]), Step: int(d[2]), Total: int(d[3]), Active: d[4] != 0}, nil
}

// Fault is the fault event (83 03). Active is the raw byte: the protocol
// does not define whether non-zero means raised or cleared, so it is shown
// as received and not interpreted.
type Fault struct {
	Code     int
	Active   byte
	Value    uint16
	Severity int
}

// FaultNames for Fault.Code.
var FaultNames = map[int]string{1: "over-temperature", 2: "under-voltage", 3: "LED driver"}

func (f Fault) String() string {
	n, ok := FaultNames[f.Code]
	if !ok {
		n = fmt.Sprintf("fault %d", f.Code)
	}
	return fmt.Sprintf("%s (code %d), active field 0x%02X, value %d, severity %d", n, f.Code, f.Active, f.Value, f.Severity)
}

// ParseFault decodes the 5 data bytes after the event byte.
func ParseFault(d []byte) (Fault, error) {
	if len(d) != 5 {
		return Fault{}, fmt.Errorf("fault event has %d data bytes, want 5", len(d))
	}
	return Fault{Code: int(d[0]), Active: d[1], Value: binary.LittleEndian.Uint16(d[2:]), Severity: int(d[4])}, nil
}

// Info is the device information (cmd 21).
type Info struct {
	Model, Serial, Hardware, Firmware, Protocol string
}

func (i Info) String() string {
	return fmt.Sprintf("%s, serial %s, HW %s, FW %s, protocol %s", i.Model, i.Serial, i.Hardware, i.Firmware, i.Protocol)
}

// ParseInfo decodes the data after CMD RESULT: five NUL-terminated strings.
func ParseInfo(d []byte) (Info, error) {
	var f []string
	for len(f) < 5 {
		i := -1
		for k, c := range d {
			if c == 0 {
				i = k
				break
			}
		}
		if i < 0 {
			return Info{}, fmt.Errorf("device info field %d is not NUL-terminated", len(f)+1)
		}
		f = append(f, string(d[:i]))
		d = d[i+1:]
	}
	return Info{Model: f[0], Serial: f[1], Hardware: f[2], Firmware: f[3], Protocol: f[4]}, nil
}

// Timing is the custom timing (cmd 12 / 13), milliseconds.
type Timing struct {
	Focus, Stable, Shot, Exposure uint32
}

// Validate checks the documented ranges.
func (t Timing) Validate() error {
	chk := func(n string, v, max uint32) error {
		if v < 1 || v > max {
			return fmt.Errorf("%s %d ms is outside 1..%d", n, v, max)
		}
		return nil
	}
	for _, e := range []error{chk("focus", t.Focus, 10000), chk("stable", t.Stable, 10000), chk("shot", t.Shot, 1000), chk("exposure", t.Exposure, 60000)} {
		if e != nil {
			return e
		}
	}
	return nil
}
