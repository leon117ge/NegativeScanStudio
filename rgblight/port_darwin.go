//go:build darwin

package rgblight

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// macOS: the light is a USB CDC ACM device (STM32 VCP, 0483:5740). The
// built-in AppleUSBCDCACM driver publishes it as /dev/cu.usbmodem* and
// /dev/tty.usbmodem*; no extra driver is needed. We always use the cu.*
// (callout) node: opening tty.* blocks until carrier detect.

// enumerate is set by enum_darwin_cgo.go (IOKit) or enum_darwin_nocgo.go
// (ioreg). It returns the cu.* nodes with their USB identity.
var enumerate func() ([]serialEntry, error)

// ListPorts lists the callout serial ports (/dev/cu.*), lights
// (USB 0483:5740) first, then other USB modems, then the rest.
func ListPorts() ([]PortInfo, error) {
	ents, err := enumerate()
	seen := map[string]bool{}
	var out []PortInfo
	for _, e := range ents {
		if !strings.HasPrefix(filepath.Base(e.Path), "cu.") || seen[e.Path] {
			continue
		}
		seen[e.Path] = true
		out = append(out, PortInfo{Name: e.Path, Present: true, Light: isLightID(e.VID, e.PID)})
	}
	// Safety net: list USB modem nodes the enumerator missed (no tag).
	if g, _ := filepath.Glob("/dev/cu.usbmodem*"); len(g) > 0 {
		for _, p := range g {
			if !seen[p] {
				seen[p] = true
				out = append(out, PortInfo{Name: p, Present: true})
			}
		}
	}
	sortPorts(out)
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return out, nil
}

// Read timeout: like the Windows port (ReadTotalTimeoutConstant 20 ms),
// Read returns at once with what is buffered, else waits up to 20 ms and
// returns (0, nil).
const readWait = 20 * time.Millisecond

// writeTimeout matches WriteTotalTimeoutConstant on Windows.
const writeTimeout = 1000 * time.Millisecond

type unixPort struct {
	mu     sync.RWMutex // Read/Write hold R; Close holds W, so fd stays valid
	fd     int
	name   string
	closed atomic.Bool
}

// OpenPort opens a /dev/cu.* port at 115200 8N1 raw (the CDC link ignores
// the baud rate) with DTR and RTS asserted, exclusive access and short read
// timeouts. Same settings as the Windows port.
func OpenPort(name string) (io.ReadWriteCloser, error) {
	path, err := devicePath(name)
	if err != nil {
		return nil, err
	}
	return openPath(path)
}

// openPath opens any tty path with the light's settings (tests use a pty).
func openPath(path string) (*unixPort, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	ok := false
	defer func() {
		if !ok {
			unix.Close(fd)
		}
	}()
	if fd >= 1024 { // FD_SETSIZE: waitFD uses select
		return nil, fmt.Errorf("open %s: descriptor %d too high for select", path, fd)
	}
	// Exclusive like the Windows share mode 0: a second open fails with EBUSY.
	if err := unix.IoctlSetInt(fd, unix.TIOCEXCL, 0); err != nil {
		return nil, fmt.Errorf("%s: TIOCEXCL: %w", path, err)
	}
	// TIOCEXCL does not stop root, and macOS does not enforce it on every
	// tty type; an advisory lock (as pyserial and others use) also stops a
	// second open from this or another cooperating program.
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if err == unix.EWOULDBLOCK {
			return nil, fmt.Errorf("open %s: port is in use by another program", path)
		}
		return nil, fmt.Errorf("%s: flock: %w", path, err)
	}
	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil, fmt.Errorf("%s: get termios: %w", path, err)
	}
	makeRaw(t)
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, t); err != nil {
		return nil, fmt.Errorf("%s: set termios: %w", path, err)
	}
	// DTR and RTS on (Windows: DTR_CONTROL_ENABLE, RTS_CONTROL_ENABLE). The
	// tty layer already raises DTR on open; a pty has no modem lines, so a
	// failure here is not fatal.
	_ = unix.IoctlSetPointerInt(fd, unix.TIOCMBIS, unix.TIOCM_DTR|unix.TIOCM_RTS)
	// Purge both directions (Windows: PurgeComm RX|TX). FREAD=1, FWRITE=2.
	_ = unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, 1|2)
	ok = true
	return &unixPort{fd: fd, name: path}, nil
}

// makeRaw sets 115200 8N1, no flow control, no line processing, VMIN=0
// VTIME=0 (reads never block; Read waits with select instead).
func makeRaw(t *unix.Termios) {
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON | unix.IXOFF | unix.IXANY | unix.INPCK
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB | unix.CSTOPB | unix.CRTSCTS
	t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL
	t.Cc[unix.VMIN] = 0
	t.Cc[unix.VTIME] = 0
	t.Ispeed = unix.B115200
	t.Ospeed = unix.B115200
}

func (p *unixPort) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed.Load() {
		return 0, io.EOF
	}
	// select, not poll: poll(2) on macOS does not support device nodes.
	n, err := waitFD(p.fd, false, readWait)
	if err == unix.EINTR {
		return 0, nil
	}
	if err != nil {
		if err == unix.EBADF {
			return 0, io.EOF
		}
		return 0, fmt.Errorf("%s: select: %w", p.name, err)
	}
	if n == 0 {
		return 0, nil // timeout, nothing buffered
	}
	m, err := unix.Read(p.fd, b)
	if err == unix.EAGAIN || err == unix.EINTR {
		return 0, nil
	}
	if err != nil {
		if err == unix.ENXIO || err == unix.EIO {
			return 0, io.EOF // unplugged
		}
		return 0, fmt.Errorf("%s: read: %w", p.name, err)
	}
	if m == 0 {
		// select said readable but nothing came: hang-up (cable pulled).
		return 0, io.EOF
	}
	return m, nil
}

func (p *unixPort) Write(b []byte) (int, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	deadline := time.Now().Add(writeTimeout)
	done := 0
	for done < len(b) {
		n, err := unix.Write(p.fd, b[done:])
		if n > 0 {
			done += n
		}
		switch {
		case err == nil:
			continue
		case err == unix.EINTR:
			continue
		case err == unix.EAGAIN:
			left := time.Until(deadline)
			if left <= 0 {
				return done, fmt.Errorf("short write (%d of %d bytes)", done, len(b))
			}
			waitFD(p.fd, true, left)
		default:
			return done, fmt.Errorf("%s: write: %w", p.name, err)
		}
	}
	return done, nil
}

func (p *unixPort) Close() error {
	if p.closed.Swap(true) {
		return nil
	}
	p.mu.Lock() // waits for an in-flight Read (at most readWait)
	defer p.mu.Unlock()
	return unix.Close(p.fd)
}

// waitFD waits until fd is readable (or writable) or d passes. It returns
// the number of ready descriptors (0 on timeout).
func waitFD(fd int, write bool, d time.Duration) (int, error) {
	var set unix.FdSet
	set.Zero()
	set.Set(fd)
	tv := unix.NsecToTimeval(d.Nanoseconds())
	if write {
		return unix.Select(fd+1, nil, &set, nil, &tv)
	}
	return unix.Select(fd+1, &set, nil, nil, &tv)
}
