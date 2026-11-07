package rgblight

import (
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"
)

// SimDevice is a fake RGB Light Air that answers the documented commands
// as the protocol reference describes them. It is the host side of an
// in-memory port (io.ReadWriteCloser): bytes written are frames to the
// light, bytes read are its responses, telemetry and events. Used by the
// tests and by the "Simulated light" entry of the app.
type SimDevice struct {
	Model, Serial, Hardware, Firmware, Protocol string

	Legacy       bool          // send the 8-data-byte state event (no focus field)
	Chunk        int           // >0: Read returns at most Chunk bytes (partial frames)
	TelEvery     time.Duration // telemetry interval (0 = none)
	StepTime     time.Duration // duration of each capture wait (default 20 ms)
	EventFirst   bool          // send the 20 state event before its acknowledgement
	NoAckMode    bool          // command 10 is executed but not acknowledged
	DropReplies  int           // drop the next n responses (command still executed)
	BadLenResult byte          // result for a wrong request length (default 02)

	// OddAck (v0.81) makes successful set commands listed in OddAckCmds
	// (nil = only 03) answer in a non-documented way, like the v0.80 field
	// fault. The command is always executed.
	OddAck     AckStyle
	OddAckCmds []byte
	LateAfter  time.Duration // AckLate delay (default 2 s)
	// IgnoreSetAll: the next n 03 commands are neither executed nor
	// acknowledged (tests the single verified resend).
	IgnoreSetAll int

	mu       sync.Mutex
	cond     *sync.Cond
	out      []byte
	closed   bool
	parser   Parser
	on       [3]bool
	val      [3]int
	mode     int
	speed    int
	focus    int
	custom   Timing
	capture  bool
	temp     float64
	volts    float64
	received []byte // every CMD byte received, in order
	stop     chan struct{}
}

// AckStyle selects how the simulator acknowledges a set command.
type AckStyle int

const (
	AckNormal     AckStyle = iota // documented 81 response
	AckNone                       // executed, no reply at all
	AckSeq0                       // 81 response with SEQ 0
	AckEventOnly                  // no 81, a state event (83 01) instead
	AckSwappedCRC                 // 81 response, CRC high byte first
	AckLate                       // 81 response after LateAfter
	AckWrongCmd                   // 81 response with our SEQ but CMD 20
	AckNoResult                   // 81 response without the RESULT byte
)

// AckStyleNames label the AckStyle values (sim port names, tests).
var AckStyleNames = []string{"normal", "no ack", "ack with SEQ 0", "state event instead of ack", "ack with swapped CRC", "late ack", "ack with wrong CMD", "ack without RESULT"}

// NewSimDevice returns a simulated light (local off state: all disabled,
// brightness 50) with telemetry every 500 ms.
func NewSimDevice() *SimDevice {
	d := &SimDevice{
		Model: "CUTENEW_RGB_AIR", Serial: "SIM0001", Hardware: "1.0", Firmware: "sim", Protocol: "1.3",
		TelEvery: 500 * time.Millisecond, StepTime: 20 * time.Millisecond,
		val:    [3]int{50, 50, 50},
		custom: Timing{1000, 60, 100, 450},
		temp:   30.3, volts: 18.86,
		stop: make(chan struct{}),
	}
	d.cond = sync.NewCond(&d.mu)
	return d
}

// Start begins the telemetry ticker.
func (d *SimDevice) Start() *SimDevice {
	if d.TelEvery > 0 {
		go func() {
			t := time.NewTicker(d.TelEvery)
			defer t.Stop()
			for {
				select {
				case <-d.stop:
					return
				case <-t.C:
					d.mu.Lock()
					d.sendTelemetry()
					d.mu.Unlock()
				}
			}
		}()
	}
	return d
}

// Read returns bytes sent by the light.
func (d *SimDevice) Read(b []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for len(d.out) == 0 && !d.closed {
		d.cond.Wait()
	}
	if len(d.out) == 0 {
		return 0, io.EOF
	}
	n := len(b)
	if d.Chunk > 0 && n > d.Chunk {
		n = d.Chunk
	}
	n = copy(b[:n], d.out)
	d.out = d.out[n:]
	return n, nil
}

// Write takes bytes sent to the light.
func (d *SimDevice) Write(b []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return 0, errors.New("port closed")
	}
	for _, f := range d.parser.Feed(b) {
		if f.Type == TypeCommand && len(f.Payload) > 0 {
			d.handle(f)
		}
	}
	return len(b), nil
}

// Close closes the port (the simulated light keeps its state).
func (d *SimDevice) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		d.closed = true
		close(d.stop)
		d.cond.Broadcast()
	}
	return nil
}

// Inject queues raw bytes from the light (noise, broken frames).
func (d *SimDevice) Inject(b []byte) {
	d.mu.Lock()
	d.out = append(d.out, b...)
	d.cond.Broadcast()
	d.mu.Unlock()
}

// InjectFault sends a fault event.
func (d *SimDevice) InjectFault(code int, active byte, value uint16) {
	d.mu.Lock()
	pl := []byte{EventFault, byte(code), active, 0, 0, 1}
	binary.LittleEndian.PutUint16(pl[3:], value)
	d.send(TypeEvent, 0, pl)
	d.mu.Unlock()
}

// Received lists every command byte the light has received.
func (d *SimDevice) Received() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]byte(nil), d.received...)
}

// Lights returns the current enables and values.
func (d *SimDevice) Lights() ([3]bool, [3]int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.on, d.val
}

// Capturing reports a running capture.
func (d *SimDevice) Capturing() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.capture
}

// SetLights sets the state directly (test setup).
func (d *SimDevice) SetLights(on [3]bool, v [3]int) {
	d.mu.Lock()
	d.on, d.val = on, v
	d.mu.Unlock()
}

func (d *SimDevice) send(typ, seq byte, pl []byte) {
	b, err := Encode(Frame{Type: typ, Seq: seq, Payload: pl})
	if err != nil {
		return
	}
	d.out = append(d.out, b...)
	d.cond.Broadcast()
}

func (d *SimDevice) sendTelemetry() {
	pl := make([]byte, 9)
	binary.LittleEndian.PutUint16(pl[0:], uint16(int16(d.temp*10)))
	binary.LittleEndian.PutUint16(pl[2:], uint16(d.volts*100+0.5))
	binary.LittleEndian.PutUint16(pl[4:], 0x1234)
	binary.LittleEndian.PutUint16(pl[6:], 0xBEEF)
	if d.capture {
		pl[8] = 1
	}
	d.send(TypeTelemetry, 0, pl)
}

func (d *SimDevice) stateEvent() []byte {
	pl := []byte{EventState, b2u(d.on[0]), byte(d.val[0]), b2u(d.on[1]), byte(d.val[1]), b2u(d.on[2]), byte(d.val[2]), byte(d.mode), byte(d.speed)}
	if !d.Legacy {
		pl = append(pl, byte(d.focus))
	}
	return pl
}

func (d *SimDevice) reply(seq, cmd, res byte, data ...byte) {
	if d.DropReplies > 0 {
		d.DropReplies--
		return
	}
	d.send(TypeResponse, seq, append([]byte{cmd, res}, data...))
}

var simLen = map[byte]int{
	CmdSetBrightness: 3, CmdSetEnable: 3, CmdSetAll: 7, CmdSetMode: 2, CmdSetSpeed: 2, CmdSetTiming: 17,
	CmdGetTiming: 1, CmdSetFocus: 2, CmdGetFocus: 1, CmdGetState: 1, CmdGetInfo: 1, CmdStartCapture: 1,
}

func (d *SimDevice) timing() Timing {
	switch d.speed {
	case 0:
		return Timing{400, 60, 100, 150}
	case 1:
		return Timing{1000, 60, 100, 450}
	}
	return d.custom
}

func (d *SimDevice) handle(f Frame) {
	cmd, a := f.Payload[0], f.Payload[1:]
	d.received = append(d.received, cmd)
	want, ok := simLen[cmd]
	if !ok {
		d.reply(f.Seq, cmd, ResultUnknown)
		return
	}
	if len(f.Payload) != want {
		r := d.BadLenResult
		if r == 0 {
			r = ResultBadLength
		}
		d.reply(f.Seq, cmd, r)
		return
	}
	busyCmd := cmd != CmdGetTiming && cmd != CmdGetState && cmd != CmdGetInfo && cmd != CmdGetFocus
	if d.capture && busyCmd {
		d.reply(f.Seq, cmd, ResultBusy)
		return
	}
	okv := func(v byte) bool { return v >= 1 && v <= 100 }
	okb := func(v byte) bool { return v <= 1 }
	switch cmd {
	case CmdSetBrightness:
		if a[0] > 2 || !okv(a[1]) {
			d.reply(f.Seq, cmd, ResultBadParam)
			return
		}
		d.val[a[0]] = int(a[1])
	case CmdSetEnable:
		if a[0] > 2 || !okb(a[1]) {
			d.reply(f.Seq, cmd, ResultBadParam)
			return
		}
		d.on[a[0]] = a[1] == 1
	case CmdSetAll:
		if d.IgnoreSetAll > 0 {
			d.IgnoreSetAll--
			return
		}
		for c := 0; c < 3; c++ {
			if !okb(a[2*c]) || !okv(a[2*c+1]) {
				d.reply(f.Seq, cmd, ResultBadParam)
				return
			}
		}
		for c := 0; c < 3; c++ {
			d.on[c], d.val[c] = a[2*c] == 1, int(a[2*c+1])
		}
	case CmdSetMode:
		if !okb(a[0]) {
			d.reply(f.Seq, cmd, ResultBadParam)
			return
		}
		d.mode = int(a[0])
		if d.mode == 1 {
			d.focus = 0
		}
		if d.NoAckMode {
			return
		}
	case CmdSetSpeed:
		if a[0] > 2 {
			d.reply(f.Seq, cmd, ResultBadParam)
			return
		}
		d.speed = int(a[0])
	case CmdSetTiming:
		u := func(i int) uint32 { return binary.LittleEndian.Uint32(a[4*i:]) }
		t := Timing{u(0), u(1), u(2), u(3)}
		if t.Validate() != nil {
			d.reply(f.Seq, cmd, ResultBadParam)
			return
		}
		d.custom = t
	case CmdGetTiming:
		t := d.timing()
		var b []byte
		for _, v := range []uint32{t.Focus, t.Stable, t.Shot, t.Exposure} {
			b = binary.LittleEndian.AppendUint32(b, v)
		}
		d.reply(f.Seq, cmd, ResultOK, b...)
		return
	case CmdSetFocus:
		if !okb(a[0]) || (d.mode == 1 && a[0] == 1) {
			d.reply(f.Seq, cmd, ResultBadParam)
			return
		}
		d.focus = int(a[0])
	case CmdGetFocus:
		d.reply(f.Seq, cmd, ResultOK, byte(d.focus))
		return
	case CmdGetState:
		if d.EventFirst {
			d.send(TypeEvent, 0, d.stateEvent())
			d.reply(f.Seq, cmd, ResultOK)
		} else {
			d.reply(f.Seq, cmd, ResultOK)
			d.send(TypeEvent, 0, d.stateEvent())
		}
		return
	case CmdGetInfo:
		var b []byte
		for _, s := range []string{d.Model, d.Serial, d.Hardware, d.Firmware, d.Protocol} {
			b = append(append(b, s...), 0)
		}
		d.reply(f.Seq, cmd, ResultOK, b...)
		return
	case CmdStartCapture:
		d.reply(f.Seq, cmd, ResultOK)
		if d.mode == 0 && !(d.on[0] || d.on[1] || d.on[2]) {
			return // documented: success, but no capture
		}
		d.capture = true
		d.sendTelemetry()
		go d.runCapture()
		return
	}
	d.ack(f.Seq, cmd)
}

// ack acknowledges a successfully executed set command (honours OddAck).
func (d *SimDevice) ack(seq, cmd byte) {
	style := d.OddAck
	if style != AckNormal {
		list := d.OddAckCmds
		if list == nil {
			list = []byte{CmdSetAll}
		}
		hit := false
		for _, c := range list {
			hit = hit || c == cmd
		}
		if !hit {
			style = AckNormal
		}
	}
	switch style {
	case AckNone:
	case AckSeq0:
		d.reply(0, cmd, ResultOK)
	case AckEventOnly:
		d.send(TypeEvent, 0, d.stateEvent())
	case AckSwappedCRC:
		b, _ := Encode(Frame{Type: TypeResponse, Seq: seq, Payload: []byte{cmd, ResultOK}})
		n := len(b)
		b[n-2], b[n-1] = b[n-1], b[n-2]
		d.out = append(d.out, b...)
		d.cond.Broadcast()
	case AckLate:
		wait := d.LateAfter
		if wait <= 0 {
			wait = 2 * time.Second
		}
		go func() {
			time.Sleep(wait)
			d.mu.Lock()
			if !d.closed {
				d.send(TypeResponse, seq, []byte{cmd, ResultOK})
			}
			d.mu.Unlock()
		}()
	case AckWrongCmd:
		d.reply(seq, CmdGetState, ResultOK)
	case AckNoResult:
		d.send(TypeResponse, seq, []byte{cmd})
	default:
		d.reply(seq, cmd, ResultOK)
	}
}

func (d *SimDevice) runCapture() {
	d.mu.Lock()
	var chans []int
	if d.mode == 0 {
		for c := 0; c < 3; c++ {
			if d.on[c] {
				chans = append(chans, c)
			}
		}
	} else {
		chans = []int{0}
	}
	step := d.StepTime
	d.mu.Unlock()
	ev := func(phase, ch, n int, active bool) {
		d.mu.Lock()
		d.send(TypeEvent, 0, []byte{EventProgress, byte(phase), byte(ch), byte(n), byte(len(chans)), b2u(active)})
		d.mu.Unlock()
		time.Sleep(step)
	}
	ev(1, 0, 0, true)
	for i, c := range chans {
		for ph := 2; ph <= 4; ph++ {
			ev(ph, c, i+1, true)
		}
	}
	d.mu.Lock()
	d.send(TypeEvent, 0, []byte{EventProgress, 5, 0, byte(len(chans)), byte(len(chans)), 0})
	d.capture = false
	d.sendTelemetry()
	d.mu.Unlock()
}
