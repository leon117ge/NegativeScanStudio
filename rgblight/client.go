package rgblight

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout is how long a request waits for its response.
const DefaultTimeout = 1500 * time.Millisecond

// ErrClosed is returned after the connection closed or failed.
var ErrClosed = errors.New("light connection is closed")

// TimeoutError is a request without a matching response. The request is
// NOT repeated blindly: for a side-effecting command the light may or may
// not have executed it. The Set* methods read the state with command 20
// and decide from what the light reports (v0.81).
type TimeoutError struct {
	Cmd byte
}

func (e *TimeoutError) Error() string {
	if SideEffect(e.Cmd) {
		return fmt.Sprintf("no reply to %s - not retried; the light may or may not have applied it (read state to check)", CmdName(e.Cmd))
	}
	return fmt.Sprintf("no reply to %s", CmdName(e.Cmd))
}

// UncertainAckError is a reply that plausibly belongs to the request but
// does not match the documented form exactly (other SEQ, other CMD, no
// RESULT byte, CRC variant, unexpected frame TYPE with our SEQ). It is
// never taken as success on its own: the Set* methods verify with 20.
type UncertainAckError struct {
	Cmd  byte
	Why  string
	Data []byte // payload as received
}

func (e *UncertainAckError) Error() string {
	return fmt.Sprintf("unclear reply to %s (%s)", CmdName(e.Cmd), e.Why)
}

// unconfirmed reports whether err means "no clear acknowledgement" (timeout
// or uncertain reply), i.e. the outcome must be read back.
func unconfirmed(err error) bool {
	var te *TimeoutError
	var ue *UncertainAckError
	return errors.As(err, &te) || errors.As(err, &ue)
}

// Client is one connection to a light. All exported methods are safe for
// concurrent use; requests are serialised (one outstanding request). The
// On* callbacks run on the reader goroutine and must not block; set them
// before Start.
type Client struct {
	rw      io.ReadWriteCloser
	Timeout time.Duration

	OnTelemetry func(Telemetry)
	OnState     func(State)
	OnProgress  func(Progress)
	OnFault     func(Fault)
	OnLog       func(string)
	OnClosed    func(error)
	// OnRaw (v0.81) receives the raw serial log: every frame sent and
	// received in hex with direction, time since Start and decoded
	// TYPE/SEQ/LEN, plus every run of discarded bytes. Telemetry frames are
	// thinned out while no request is in flight (see RawTelemetryEvery).
	OnRaw func(string)
	// RawTelemetryEvery: outside a request window, log the first 10
	// telemetry frames and then one in this many (0 = 20).
	RawTelemetryEvery int

	reqMu sync.Mutex // one request at a time

	mu       sync.Mutex
	seq      byte
	pend     *pending
	expired  []expiredReq // recent requests that timed out (late-reply detection)
	ackMiss  map[byte]int // consecutive set commands per CMD without a clean ack
	state    *State
	stateGen uint64
	tel      *Telemetry
	telGen   uint64
	info     *Info
	changed  chan struct{} // closed and replaced on every state/telemetry update
	closed   bool
	closeErr error
	done     chan struct{}
	started  bool
	parser   Parser
	pmu      sync.Mutex // guards parser (reader goroutine vs. diagnostics)

	t0      time.Time
	lastTX  time.Time
	telSeen int
	telSkip int
	rawMu   sync.Mutex
}

type reply struct {
	pl  []byte
	why string // non-empty: uncertain form
}

type pending struct {
	seq, cmd byte
	sent     time.Time
	ch       chan reply
	alt      string // a non-matching but plausible reply seen while waiting
}

type expiredReq struct {
	seq, cmd byte
	sent     time.Time
}

// NewClient wraps an open port. Call Start to begin reading.
func NewClient(rw io.ReadWriteCloser) *Client {
	return &Client{rw: rw, Timeout: DefaultTimeout, changed: make(chan struct{}), done: make(chan struct{}), t0: time.Now()}
}

// Start launches the reader goroutine.
func (c *Client) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return
	}
	c.started = true
	c.t0 = time.Now()
	c.parser.OnDiscard = func(b []byte, why string) {
		c.raw("RX DISCARDED %d bytes (%s): % X", len(b), why, b)
	}
	go c.readLoop()
}

func (c *Client) logf(f string, a ...any) {
	if c.OnLog != nil {
		c.OnLog(fmt.Sprintf(f, a...))
	}
}

func (c *Client) raw(f string, a ...any) {
	if c.OnRaw == nil {
		return
	}
	c.rawMu.Lock()
	defer c.rawMu.Unlock()
	c.OnRaw(fmt.Sprintf("t+%.3fs ", time.Since(c.t0).Seconds()) + fmt.Sprintf(f, a...))
}

// describe decodes a frame for the raw log.
func describe(f Frame) string {
	var d string
	switch f.Type {
	case TypeCommand:
		d = "command"
		if len(f.Payload) > 0 {
			d += fmt.Sprintf(" %02X (%s)", f.Payload[0], CmdName(f.Payload[0]))
		}
	case TypeResponse:
		d = "response"
		if len(f.Payload) > 0 {
			d += fmt.Sprintf(" cmd %02X", f.Payload[0])
		}
		if len(f.Payload) > 1 {
			d += fmt.Sprintf(" result %02X", f.Payload[1])
		}
	case TypeTelemetry:
		d = "telemetry"
	case TypeEvent:
		d = "event"
		if len(f.Payload) > 0 {
			d += fmt.Sprintf(" %02X", f.Payload[0])
		}
	default:
		d = "UNKNOWN TYPE"
	}
	s := fmt.Sprintf("type %02X seq %d len %d [%s]: % X", f.Type, f.Seq, len(f.Payload), d, f.Raw)
	if f.AltCRC != "" {
		s += " (documented CRC mismatch; matches variant: " + f.AltCRC + ")"
	}
	return s
}

func (c *Client) rawRX(f Frame) {
	if c.OnRaw == nil {
		return
	}
	if f.Type == TypeTelemetry && f.AltCRC == "" {
		c.mu.Lock()
		busy := c.pend != nil || time.Since(c.lastTX) < 3*time.Second
		c.telSeen++
		every := c.RawTelemetryEvery
		if every <= 0 {
			every = 20
		}
		show := busy || c.telSeen <= 10 || c.telSeen%every == 0
		skipped := c.telSkip
		if show {
			c.telSkip = 0
		} else {
			c.telSkip++
		}
		c.mu.Unlock()
		if !show {
			return
		}
		if skipped > 0 {
			c.raw("RX %s (%d idle telemetry frames not shown)", describe(f), skipped)
			return
		}
	}
	c.raw("RX %s", describe(f))
}

func (c *Client) readLoop() {
	buf := make([]byte, 512)
	for {
		n, err := c.rw.Read(buf)
		if n > 0 {
			c.pmu.Lock()
			fs := c.parser.Feed(buf[:n])
			c.pmu.Unlock()
			for _, f := range fs {
				c.rawRX(f)
				c.dispatch(f)
			}
		}
		if err != nil {
			c.fail(err)
			return
		}
		select {
		case <-c.done:
			return
		default:
		}
	}
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	if errors.Is(err, io.EOF) {
		err = errors.New("the light disconnected")
	}
	c.closeErr = err
	close(c.done)
	c.mu.Unlock()
	c.rw.Close()
	if c.OnClosed != nil {
		c.OnClosed(err)
	}
}

func (c *Client) bump() {
	close(c.changed)
	c.changed = make(chan struct{})
}

// deliver completes the pending request p (c.mu held; released here).
func (c *Client) deliver(p *pending, r reply) {
	c.pend = nil
	c.mu.Unlock()
	p.ch <- r
}

// lateReply logs a response to a request that already timed out (c.mu held).
func (c *Client) lateReply(f Frame) bool {
	if len(f.Payload) < 1 {
		return false
	}
	for i := len(c.expired) - 1; i >= 0; i-- {
		e := c.expired[i]
		if e.seq == f.Seq && e.cmd == f.Payload[0] {
			c.expired = append(c.expired[:i], c.expired[i+1:]...)
			c.mu.Unlock()
			res := "?"
			if len(f.Payload) > 1 {
				res = fmt.Sprintf("%02X", f.Payload[1])
			}
			c.logf("LATE reply to %s (seq %d, result %s) %d ms after it was sent - the request had already timed out", CmdName(e.cmd), e.seq, res, time.Since(e.sent).Milliseconds())
			c.mu.Lock()
			return true
		}
	}
	return false
}

func (c *Client) dispatch(f Frame) {
	switch f.Type {
	case TypeResponse:
		c.mu.Lock()
		p := c.pend
		if p == nil {
			if c.lateReply(f) {
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()
			c.logf("unmatched response ignored (no request pending): %s", f)
			return
		}
		cmdOK := len(f.Payload) >= 1 && f.Payload[0] == p.cmd
		if f.Seq == p.seq && cmdOK && len(f.Payload) >= 2 && f.AltCRC == "" {
			c.deliver(p, reply{pl: f.Payload})
			return
		}
		if f.Seq != p.seq && c.lateReply(f) {
			c.mu.Unlock()
			return
		}
		var why string
		switch {
		case f.Seq == p.seq && cmdOK && len(f.Payload) >= 2:
			why = "CRC variant: " + f.AltCRC
		case f.Seq == p.seq && cmdOK:
			why = "response without RESULT byte"
		case f.Seq != p.seq && cmdOK && len(f.Payload) >= 2:
			why = fmt.Sprintf("response carries SEQ %d, request had SEQ %d", f.Seq, p.seq)
		case f.Seq == p.seq && len(f.Payload) >= 1 && SideEffect(p.cmd):
			why = fmt.Sprintf("response with our SEQ but CMD %02X instead of %02X", f.Payload[0], p.cmd)
		}
		if why != "" && f.Seq != p.seq && !SideEffect(p.cmd) {
			// a read is only completed by its own SEQ: the data could be stale
			p.alt = why
			c.mu.Unlock()
			c.logf("WARNING: %s: %s - ignored, still waiting for SEQ %d", CmdName(p.cmd), why, p.seq)
			return
		}
		if why == "" {
			c.mu.Unlock()
			c.logf("unmatched response ignored (pending %s seq %d): %s", CmdName(p.cmd), p.seq, f)
			return
		}
		c.mu.Unlock()
		c.logf("WARNING: %s: %s - not treated as a confirmed acknowledgement", CmdName(p.cmd), why)
		c.mu.Lock()
		if c.pend == p {
			c.deliver(p, reply{pl: f.Payload, why: why})
			return
		}
		c.mu.Unlock()
	case TypeTelemetry:
		t, err := ParseTelemetry(f.Payload)
		if err != nil {
			c.logf("telemetry ignored: %v (raw % X)", err, f.Raw)
			return
		}
		c.mu.Lock()
		c.tel = &t
		c.telGen++
		c.bump()
		c.mu.Unlock()
		if c.OnTelemetry != nil {
			c.OnTelemetry(t)
		}
	case TypeEvent:
		if len(f.Payload) < 1 {
			c.logf("empty event ignored")
			return
		}
		d := f.Payload[1:]
		switch f.Payload[0] {
		case EventState:
			s, err := ParseState(d)
			if err != nil {
				c.logf("state event ignored: %v (raw % X)", err, f.Raw)
				return
			}
			c.mu.Lock()
			c.state = &s
			c.stateGen++
			c.bump()
			c.mu.Unlock()
			if c.OnState != nil {
				c.OnState(s)
			}
		case EventProgress:
			p, err := ParseProgress(d)
			if err != nil {
				c.logf("progress event ignored: %v (raw % X)", err, f.Raw)
				return
			}
			if c.OnProgress != nil {
				c.OnProgress(p)
			}
		case EventFault:
			ft, err := ParseFault(d)
			if err != nil {
				c.logf("fault event ignored: %v (raw % X)", err, f.Payload)
				return
			}
			if c.OnFault != nil {
				c.OnFault(ft)
			}
		default:
			c.logf("unknown event %02X ignored (raw % X)", f.Payload[0], f.Payload)
		}
	default:
		c.mu.Lock()
		p := c.pend
		if p != nil && f.Seq == p.seq && f.Seq != 0 && SideEffect(p.cmd) {
			why := fmt.Sprintf("frame TYPE %02X (not 81) with our SEQ %d: % X", f.Type, f.Seq, f.Payload)
			if f.Type == TypeCommand {
				why = "the light echoed the command frame"
				c.mu.Unlock()
				c.logf("note: %s (%s) - still waiting for a response", why, f)
				return
			}
			c.mu.Unlock()
			c.logf("WARNING: %s: %s - not treated as a confirmed acknowledgement", CmdName(p.cmd), why)
			c.mu.Lock()
			if c.pend == p {
				c.deliver(p, reply{pl: f.Payload, why: why})
				return
			}
		}
		c.mu.Unlock()
		c.logf("unexpected frame ignored: %s", f)
	}
}

// Request sends one documented command and waits for its response (SEQ
// and CMD must both match). It returns the response data after CMD and
// RESULT. A non-zero RESULT is a *ResultError (errors.Is(err, ErrBusy)
// for 04); a missing response is a *TimeoutError; a reply in an
// undocumented form is an *UncertainAckError. Nothing is retried here.
func (c *Client) Request(cmd byte, args ...byte) ([]byte, error) {
	pl, _, err := c.request(cmd, nil, args...)
	return pl, err
}

// request is Request; with match set, a state event (83 01) that arrives
// after the command was sent and matches also completes it (v0.82: FW
// V1.0.5 / protocol 1.4 sends no 81 for 03, only the new state as an
// event ~20-30 ms later). The matching state is returned.
func (c *Client) request(cmd byte, match func(State) bool, args ...byte) ([]byte, *State, error) {
	if !Allowed(cmd) {
		return nil, nil, fmt.Errorf("command %02X is not a documented customer command - refused", cmd)
	}
	c.reqMu.Lock()
	defer c.reqMu.Unlock()
	c.mu.Lock()
	if c.closed {
		err := c.closeErr
		c.mu.Unlock()
		return nil, nil, fmt.Errorf("%w: %v", ErrClosed, err)
	}
	c.seq++
	if c.seq == 0 {
		c.seq = 1
	}
	p := &pending{seq: c.seq, cmd: cmd, ch: make(chan reply, 1)}
	c.pend = p
	done := c.done
	c.mu.Unlock()
	clear := func() {
		c.mu.Lock()
		if c.pend == p {
			c.pend = nil
		}
		c.mu.Unlock()
	}
	fr := Frame{Type: TypeCommand, Seq: p.seq, Payload: append([]byte{cmd}, args...)}
	b, err := Encode(fr)
	if err != nil {
		clear()
		return nil, nil, err
	}
	fr.Raw = b
	c.mu.Lock()
	p.sent = time.Now()
	c.lastTX = p.sent
	gen := c.stateGen
	c.mu.Unlock()
	c.raw("TX %s", describe(fr))
	if _, err := c.rw.Write(b); err != nil {
		clear()
		c.raw("TX FAILED: %v", err)
		c.fail(err)
		return nil, nil, fmt.Errorf("write %s: %w", CmdName(cmd), err)
	}
	to := c.ackWait(cmd)
	tm := time.NewTimer(to)
	defer tm.Stop()
	for {
		var changed chan struct{}
		var hit *State
		c.mu.Lock()
		if match != nil {
			changed = c.changed
			if c.stateGen > gen && c.state != nil && match(*c.state) {
				st := *c.state
				hit = &st
			}
		}
		c.mu.Unlock()
		if hit != nil {
			clear()
			c.mu.Lock()
			c.expired = append(c.expired, expiredReq{seq: p.seq, cmd: cmd, sent: p.sent})
			if len(c.expired) > 16 {
				c.expired = c.expired[len(c.expired)-16:]
			}
			c.mu.Unlock()
			c.raw("seq %d cmd %02X (%s) confirmed by state event 01 after %d ms (no 81 for this command; no 20 needed)", p.seq, cmd, CmdName(cmd), time.Since(p.sent).Milliseconds())
			c.ackSeen(cmd, true)
			return nil, hit, nil
		}
		select {
		case <-changed:
			continue
		case r := <-p.ch:
			ms := time.Since(p.sent).Milliseconds()
			if r.why != "" {
				c.raw("reply to seq %d cmd %02X after %d ms is UNCERTAIN: %s", p.seq, cmd, ms, r.why)
				if !SideEffect(cmd) && len(r.pl) >= 2 && r.pl[0] == cmd && r.pl[1] == ResultOK {
					// a read answered with the right CMD but another SEQ or a
					// CRC variant: the data is usable, the warning is logged
					return r.pl[2:], nil, nil
				}
				return nil, nil, &UncertainAckError{Cmd: cmd, Why: r.why, Data: r.pl}
			}
			c.raw("reply to seq %d cmd %02X after %d ms", p.seq, cmd, ms)
			c.ackSeen(cmd, true)
			if r.pl[1] != ResultOK {
				return nil, nil, &ResultError{Cmd: cmd, Code: r.pl[1]}
			}
			return r.pl[2:], nil, nil
		case <-tm.C:
			clear()
			c.mu.Lock()
			c.expired = append(c.expired, expiredReq{seq: p.seq, cmd: cmd, sent: p.sent})
			if len(c.expired) > 16 {
				c.expired = c.expired[len(c.expired)-16:]
			}
			alt := p.alt
			c.mu.Unlock()
			c.ackSeen(cmd, false)
			c.pmu.Lock()
			pb, dr, bc, ac := c.parser.PendingBytes(), c.parser.Dropped, c.parser.BadCRC, c.parser.AltCRCs
			c.pmu.Unlock()
			c.raw("TIMEOUT: no reply to seq %d cmd %02X (%s) within %v; parser: %d bytes buffered [% X], %d bytes discarded, %d bad-CRC and %d CRC-variant frames since connect",
				p.seq, cmd, CmdName(cmd), to, len(pb), pb, dr, bc, ac)
			if alt != "" {
				return nil, nil, &UncertainAckError{Cmd: cmd, Why: alt}
			}
			return nil, nil, &TimeoutError{Cmd: cmd}
		case <-done:
			clear()
			return nil, nil, ErrClosed
		}
	}
}

// ShortAckWait is how long a set command waits for its acknowledgement once
// the light has failed to acknowledge that command twice in a row (the
// outcome is verified with command 20 anyway, so waiting the full timeout
// each time would only slow leveling down). A clean ack restores the full
// timeout.
var ShortAckWait = 400 * time.Millisecond

func (c *Client) ackWait(cmd byte) time.Duration {
	to := c.timeout()
	if !SideEffect(cmd) || cmd == CmdStartCapture {
		return to
	}
	c.mu.Lock()
	miss := c.ackMiss[cmd]
	c.mu.Unlock()
	if miss >= 2 && ShortAckWait < to {
		return ShortAckWait
	}
	return to
}

func (c *Client) ackSeen(cmd byte, ok bool) {
	if !SideEffect(cmd) {
		return
	}
	c.mu.Lock()
	if c.ackMiss == nil {
		c.ackMiss = map[byte]int{}
	}
	prev := c.ackMiss[cmd]
	if ok {
		c.ackMiss[cmd] = 0
	} else {
		c.ackMiss[cmd]++
	}
	n := c.ackMiss[cmd]
	c.mu.Unlock()
	switch {
	case !ok && n == 2 && cmd != CmdStartCapture && ShortAckWait < c.timeout():
		c.logf("note: %s was not acknowledged twice in a row - waiting only %v for its ack from now on (the result is checked with 20)", CmdName(cmd), ShortAckWait)
	case ok && prev >= 2:
		c.logf("note: %s acknowledged again - back to the normal reply timeout", CmdName(cmd))
	}
}

func (c *Client) wait(timeout time.Duration, ok func() bool) bool {
	end := time.Now().Add(timeout)
	for {
		c.mu.Lock()
		if ok() {
			c.mu.Unlock()
			return true
		}
		ch, done := c.changed, c.done
		c.mu.Unlock()
		left := time.Until(end)
		if left <= 0 {
			return false
		}
		select {
		case <-ch:
		case <-done:
			return false
		case <-time.After(left):
		}
	}
}

// ReadState sends command 20 and waits for the state event that follows
// (the event and the acknowledgement may arrive in either order). If the
// 20 acknowledgement is missing or unclear but a state event arrived after
// the request was sent, that state is used and a warning is logged.
func (c *Client) ReadState() (State, error) {
	c.mu.Lock()
	gen := c.stateGen
	c.mu.Unlock()
	fresh := func(s *State) func() bool {
		return func() bool {
			if c.stateGen > gen && c.state != nil {
				*s = *c.state
				return true
			}
			return false
		}
	}
	var s State
	if _, err := c.Request(CmdGetState); err != nil {
		if !unconfirmed(err) {
			return State{}, err
		}
		// a state event may still be on its way right behind a lost ack
		if c.wait(c.timeout()/3, fresh(&s)) {
			c.logf("WARNING: read state: %v, but a state event arrived - using it", err)
			return s, nil
		}
		return State{}, err
	}
	if !c.wait(c.timeout(), fresh(&s)) {
		return State{}, errors.New("read state: acknowledged but no state event arrived")
	}
	return s, nil
}

// VerifyWarning prefixes the log line written when a set command had no
// clear acknowledgement but command 20 showed it was applied.
const VerifyWarning = "WARNING: no %02X ack (%s), verified by 20"

// applyVerified sends one side-effecting set command and settles its outcome
// with command 20 when needed:
//
//   - RESULT != 00 (incl. BUSY): returned as is, nothing else is sent.
//   - Clean ack: the state is read only when verify is set (confirmed
//     setters); a mismatch then is an error and nothing is resent.
//   - No reply / unclear reply: the state is read. If it shows the change,
//     that is success with a logged warning. If it shows the change was NOT
//     applied, the command is sent once more (it demonstrably had no effect,
//     so a repeat cannot double it) and verified again; a second miss is an
//     error. If the state cannot be read, the original error is returned and
//     nothing is resent.
//
// want reports whether a state shows the change (ok) and whether the state
// can show it at all (verifiable; e.g. the legacy layout has no focus).
func (c *Client) applyVerified(verify bool, want func(State) (ok, verifiable bool), cmd byte, args ...byte) (State, error) {
	for attempt := 1; ; attempt++ {
		_, ev, err := c.request(cmd, func(s State) bool { ok, v := want(s); return ok && v }, args...)
		if err != nil && !unconfirmed(err) {
			return State{}, err
		}
		if ev != nil {
			return *ev, nil // the light's own state event shows the change
		}
		if err == nil && !verify {
			return State{}, nil
		}
		s, serr := c.ReadState()
		if serr != nil {
			if err != nil {
				return State{}, fmt.Errorf("%w; verifying with 20 failed too: %v", err, serr)
			}
			return State{}, fmt.Errorf("%s acknowledged, but confirming failed: %w", CmdName(cmd), serr)
		}
		ok, verifiable := want(s)
		if err == nil {
			if verifiable && !ok {
				return s, fmt.Errorf("light reports %s after %s", s, CmdName(cmd))
			}
			return s, nil
		}
		if !verifiable {
			return s, fmt.Errorf("%w; this light's state does not show the setting, so it is not resent", err)
		}
		if ok {
			c.logf(VerifyWarning+" - light reports %s", cmd, ackReason(err), s)
			return s, nil
		}
		if attempt >= 2 {
			return s, fmt.Errorf("%s not applied after one resend: light reports %s (%v)", CmdName(cmd), s, err)
		}
		c.logf("WARNING: %s: %v and 20 shows it was NOT applied (light reports %s) - sending it once more", CmdName(cmd), err, s)
	}
}

// ackReason is the short reason for the VerifyWarning line: the
// TimeoutError text says "not retried / read state to check", which is
// misleading once 20 has confirmed the setting.
func ackReason(err error) string {
	var te *TimeoutError
	if errors.As(err, &te) {
		return "no reply to " + CmdName(te.Cmd)
	}
	return err.Error()
}

func (c *Client) timeout() time.Duration {
	if c.Timeout <= 0 {
		return DefaultTimeout
	}
	return c.Timeout
}

// CachedState is the last state event (nil before the first).
func (c *Client) CachedState() *State {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == nil {
		return nil
	}
	s := *c.state
	return &s
}

// LastTelemetry is the last telemetry report (nil before the first).
func (c *Client) LastTelemetry() *Telemetry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tel == nil {
		return nil
	}
	t := *c.tel
	return &t
}

// Info reads the device identification (command 21).
func (c *Client) Info() (Info, error) {
	d, err := c.Request(CmdGetInfo)
	if err != nil {
		return Info{}, err
	}
	in, err := ParseInfo(d)
	if err != nil {
		return in, err
	}
	c.mu.Lock()
	c.info = &in
	c.mu.Unlock()
	return in, nil
}

func checkValue(v int) error {
	if v < 1 || v > 100 {
		return fmt.Errorf("brightness %d is outside 1..100 (turn a channel off with its enable flag)", v)
	}
	return nil
}

func b2u(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// SetBrightness sets one channel's brightness (command 01), verified with
// command 20 when the acknowledgement is missing.
func (c *Client) SetBrightness(ch, v int) error {
	if ch < 0 || ch > 2 {
		return fmt.Errorf("channel %d", ch)
	}
	if err := checkValue(v); err != nil {
		return err
	}
	_, err := c.applyVerified(false, func(s State) (bool, bool) { return s.Value[ch] == v, true }, CmdSetBrightness, byte(ch), byte(v))
	return err
}

// SetEnable switches one channel on or off (command 02), verified with
// command 20 when the acknowledgement is missing.
func (c *Client) SetEnable(ch int, on bool) error {
	if ch < 0 || ch > 2 {
		return fmt.Errorf("channel %d", ch)
	}
	_, err := c.applyVerified(false, func(s State) (bool, bool) { return s.On[ch] == on, true }, CmdSetEnable, byte(ch), b2u(on))
	return err
}

func setAllArgs(on [3]bool, v [3]int) ([]byte, error) {
	for i := range v {
		if err := checkValue(v[i]); err != nil {
			return nil, fmt.Errorf("%s: %w", ChannelNames[i], err)
		}
	}
	return []byte{b2u(on[0]), byte(v[0]), b2u(on[1]), byte(v[1]), b2u(on[2]), byte(v[2])}, nil
}

// SetAll sets all three channels (command 03). Every value must be 1..100,
// also for disabled channels. Without a clear acknowledgement the state is
// read with command 20 and decides (see applyVerified).
func (c *Client) SetAll(on [3]bool, v [3]int) error {
	a, err := setAllArgs(on, v)
	if err != nil {
		return err
	}
	_, err = c.applyVerified(false, func(s State) (bool, bool) { return s.On == on && s.Value == v, true }, CmdSetAll, a...)
	return err
}

// SetAllConfirmed is SetAll followed by command 20; it fails unless the
// state reports exactly the requested enables and values.
func (c *Client) SetAllConfirmed(on [3]bool, v [3]int) (State, error) {
	a, err := setAllArgs(on, v)
	if err != nil {
		return State{}, err
	}
	return c.applyVerified(true, func(s State) (bool, bool) { return s.On == on && s.Value == v, true }, CmdSetAll, a...)
}

// SetMode sets the capture mode (command 10). Its acknowledgement is not
// reliably specified, so the state is always read with command 20 and the
// mode it reports decides.
func (c *Client) SetMode(mode int) (State, error) {
	if mode != 0 && mode != 1 {
		return State{}, fmt.Errorf("capture mode %d", mode)
	}
	return c.applyVerified(true, func(s State) (bool, bool) { return s.Mode == mode, true }, CmdSetMode, byte(mode))
}

// SetSpeed sets the speed mode (command 11), verified with command 20 when
// the acknowledgement is missing.
func (c *Client) SetSpeed(speed int) error {
	if speed < 0 || speed > 2 {
		return fmt.Errorf("speed mode %d", speed)
	}
	_, err := c.applyVerified(false, func(s State) (bool, bool) { return s.Speed == speed, true }, CmdSetSpeed, byte(speed))
	return err
}

// SetTiming sets the custom timing (command 12) after validating it. With
// no clear acknowledgement it is read back with command 13; it is resent
// once only when 13 shows different values.
func (c *Client) SetTiming(t Timing) error {
	if err := t.Validate(); err != nil {
		return err
	}
	a := make([]byte, 0, 16)
	for _, v := range []uint32{t.Focus, t.Stable, t.Shot, t.Exposure} {
		a = binary.LittleEndian.AppendUint32(a, v)
	}
	for attempt := 1; ; attempt++ {
		_, err := c.Request(CmdSetTiming, a...)
		if err == nil || !unconfirmed(err) {
			return err
		}
		got, rerr := c.Timing()
		if rerr != nil {
			return fmt.Errorf("%w; reading it back with 13 failed too: %v", err, rerr)
		}
		if got == t {
			c.logf("WARNING: no 12 ack (%v), verified by 13", err)
			return nil
		}
		if attempt >= 2 {
			return fmt.Errorf("custom timing not applied after one resend (13 reports %+v): %w", got, err)
		}
		c.logf("WARNING: set timing: %v and 13 shows it was NOT applied - sending it once more", err)
	}
}

// Timing reads the effective timing (command 13).
func (c *Client) Timing() (Timing, error) {
	d, err := c.Request(CmdGetTiming)
	if err != nil {
		return Timing{}, err
	}
	if len(d) != 16 {
		return Timing{}, fmt.Errorf("timing response has %d data bytes, want 16", len(d))
	}
	u := func(i int) uint32 { return binary.LittleEndian.Uint32(d[4*i:]) }
	return Timing{u(0), u(1), u(2), u(3)}, nil
}

// SetFocus selects the focus illumination (command 14). Green is refused
// locally while the cached state says direct mode. A missing ack is
// settled with command 20 (the legacy state layout cannot show it).
func (c *Client) SetFocus(focus int) error {
	if focus != 0 && focus != 1 {
		return fmt.Errorf("focus illumination %d", focus)
	}
	if s := c.CachedState(); s != nil && s.Mode == 1 && focus == 1 {
		return errors.New("green focus is not available in direct mode")
	}
	_, err := c.applyVerified(false, func(s State) (bool, bool) { return s.Focus == focus, s.Focus >= 0 }, CmdSetFocus, byte(focus))
	return err
}

// Focus reads the focus illumination (command 15); only while idle, since
// its behaviour during a capture is unspecified.
func (c *Client) Focus() (int, error) {
	if t := c.LastTelemetry(); t != nil && t.Capturing {
		return 0, errors.New("read focus only while the light is idle")
	}
	d, err := c.Request(CmdGetFocus)
	if err != nil {
		return 0, err
	}
	if len(d) != 1 || d[0] > 1 {
		return 0, fmt.Errorf("unexpected focus response % X", d)
	}
	return int(d[0]), nil
}

// StartCapture starts the light's own capture sequence (command 22). It is
// sent at most once: the state is read first and, in sequential mode, at
// least one channel must be enabled. A timeout is reported, never retried
// (a repeat could start a second capture).
func (c *Client) StartCapture() error {
	if t := c.LastTelemetry(); t != nil && t.Capturing {
		return errors.New("the light is already capturing")
	}
	s, err := c.ReadState()
	if err != nil {
		return err
	}
	if s.Mode == 0 && !(s.On[0] || s.On[1] || s.On[2]) {
		return errors.New("sequential capture needs at least one enabled channel")
	}
	_, err = c.Request(CmdStartCapture)
	return err
}

// WaitIdle waits for a telemetry report received after the call that says
// the light is idle.
func (c *Client) WaitIdle(timeout time.Duration) error {
	c.mu.Lock()
	gen := c.telGen
	c.mu.Unlock()
	var last *Telemetry
	if c.wait(timeout, func() bool {
		if c.telGen > gen && c.tel != nil {
			last = c.tel
			return !c.tel.Capturing
		}
		return false
	}) {
		return nil
	}
	if c.Closed() {
		return ErrClosed
	}
	if last == nil {
		return fmt.Errorf("no telemetry from the light within %v", timeout)
	}
	return fmt.Errorf("the light is still capturing after %v", timeout)
}

// Closed reports whether the connection is closed.
func (c *Client) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Close closes the port without changing the light.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.closeErr = errors.New("closed by the application")
	close(c.done)
	c.mu.Unlock()
	return c.rw.Close()
}

// SafeClose is the planned shutdown: wait for idle, switch R, G and B off
// with command 03 (keeping valid brightness values), confirm with command
// 20 and close. The port is closed even when a step fails; the returned
// error says which step.
func (c *Client) SafeClose(idleTimeout time.Duration) error {
	defer c.Close()
	var msgs []string
	if err := c.WaitIdle(idleTimeout); err != nil {
		return fmt.Errorf("not switched off: %w", err)
	}
	v := [3]int{50, 50, 50}
	if s := c.CachedState(); s != nil {
		for i := range v {
			if s.Value[i] >= 1 && s.Value[i] <= 100 {
				v[i] = s.Value[i]
			}
		}
	}
	if _, err := c.SetAllConfirmed([3]bool{}, v); err != nil {
		if strings.Contains(err.Error(), "light reports") || strings.Contains(err.Error(), "confirming failed") {
			msgs = append(msgs, "switching off not confirmed: "+err.Error())
		} else {
			return fmt.Errorf("switching off failed: %w", err)
		}
	}
	if len(msgs) > 0 {
		return errors.New(strings.Join(msgs, "; "))
	}
	return nil
}
