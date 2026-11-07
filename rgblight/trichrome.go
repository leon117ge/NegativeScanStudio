package rgblight

import (
	"errors"
	"fmt"
	"time"
)

// ChannelSwitch (v0.81) drives the light for a trichrome scan: one
// channel at a time at its scan brightness. Every switch is 03 confirmed by
// the light's state event or 20 (SetAllConfirmed: an unconfirmed 03 is
// decided by 20 and resent at most once); BUSY waits for idle once.
// v0.82: Snapshot reads the light's state at scan start and Restore writes
// exactly that state back afterwards (no forced all-on / all-off).
type ChannelSwitch struct {
	C      *Client
	Values [3]int // brightness per channel (the levelled/stored values)
	Log    func(string)

	Before     State // the light before the scan (Snapshot)
	HaveBefore bool
}

func (s *ChannelSwitch) set(on [3]bool, v [3]int) error {
	st, err := s.C.SetAllConfirmed(on, v)
	if errors.Is(err, ErrBusy) {
		if werr := s.C.WaitIdle(60 * time.Second); werr != nil {
			return fmt.Errorf("light busy: %w", werr)
		}
		st, err = s.C.SetAllConfirmed(on, v)
	}
	if err != nil {
		return err
	}
	if s.Log != nil {
		s.Log(fmt.Sprintf("Light confirmed: %s", st))
	}
	return nil
}

// Snapshot reads the light's current state (20) as the state to restore.
func (s *ChannelSwitch) Snapshot() (State, error) {
	st, err := s.C.ReadState()
	if err != nil {
		return st, err
	}
	s.Before, s.HaveBefore = st, true
	return st, nil
}

// Restore writes the pre-scan state back (03, confirmed). Without a
// snapshot the light is left as it is.
func (s *ChannelSwitch) Restore() error {
	if !s.HaveBefore {
		return errors.New("the light's pre-scan state is unknown - light left as it is")
	}
	return s.set(s.Before.On, s.Before.Value)
}

// Only lights channel ch alone.
func (s *ChannelSwitch) Only(ch int) error {
	if ch < 0 || ch > 2 {
		return fmt.Errorf("channel %d", ch)
	}
	var on [3]bool
	on[ch] = true
	return s.set(on, s.Values)
}

// Off switches every channel off (values kept) - disconnect / app close.
func (s *ChannelSwitch) Off() error { return s.set([3]bool{}, s.Values) }
