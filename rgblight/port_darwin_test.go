//go:build darwin

package rgblight

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Smoke test of the macOS port code (no light attached on CI runners).
func TestDarwinPorts(t *testing.T) {
	t.Logf("enumerator: %s", EnumMethod)
	ps, err := ListPorts()
	if err != nil {
		t.Fatalf("ListPorts: %v", err)
	}
	for _, p := range ps {
		t.Logf("port %s light=%v present=%v", p.Label(), p.Light, p.Present)
		if !strings.HasPrefix(p.Name, "/dev/cu.") {
			t.Errorf("listed a non-callout node %s", p.Name)
		}
	}
	if _, err := OpenPort("/dev/cu.nss-missing-port"); err == nil {
		t.Fatal("opening a missing port succeeded")
	}
	if _, err := OpenPort("../x"); err == nil {
		t.Fatal("bad port name accepted")
	}
	if _, err := OpenPort("/dev/disk0"); err == nil {
		t.Fatal("non-serial node accepted")
	}
}

// The ioreg fallback must run on this macOS and agree with IOKit.
func TestDarwinEnumeratorsAgree(t *testing.T) {
	io2, err := enumerateIoreg()
	if err != nil {
		t.Fatalf("ioreg: %v", err)
	}
	all, err := enumerate()
	if err != nil {
		t.Fatalf("%s: %v", EnumMethod, err)
	}
	have := map[string]serialEntry{}
	for _, e := range all {
		have[e.Path] = e
	}
	for _, e := range io2 {
		t.Logf("ioreg: %s usb=%v %04x:%04x", e.Path, e.USB, e.VID, e.PID)
		h, ok := have[e.Path]
		if !ok {
			t.Errorf("%s: ioreg lists %s, the enumerator does not", EnumMethod, e.Path)
			continue
		}
		if h.USB && (h.VID != e.VID || h.PID != e.PID) {
			t.Errorf("%s: %04x:%04x vs ioreg %04x:%04x", e.Path, h.VID, h.PID, e.VID, e.PID)
		}
	}
	for _, e := range all {
		t.Logf("%s: %s usb=%v %04x:%04x", EnumMethod, e.Path, e.USB, e.VID, e.PID)
	}
}

// openPTY returns the master file and the slave path of a new pseudo terminal.
func openPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	var name string
	var ierr error
	rc, err := m.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	rc.Control(func(fd uintptr) {
		if ierr = unix.IoctlSetInt(int(fd), unix.TIOCPTYGRANT, 0); ierr != nil {
			return
		}
		if ierr = unix.IoctlSetInt(int(fd), unix.TIOCPTYUNLK, 0); ierr != nil {
			return
		}
		buf := make([]byte, 128)
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&buf[0]))); e != 0 {
			ierr = e
			return
		}
		name = string(buf[:bytes.IndexByte(buf, 0)])
	})
	if ierr != nil {
		m.Close()
		t.Fatalf("pty setup: %v", ierr)
	}
	t.Cleanup(func() { m.Close() })
	return m, name
}

// Read semantics match the Windows port: buffered bytes at once, else
// (0, nil) after about 20 ms; Write goes out unchanged (raw mode).
func TestDarwinPortPTY(t *testing.T) {
	m, slave := openPTY(t)
	p, err := openPath(slave)
	if err != nil {
		t.Fatalf("open %s: %v", slave, err)
	}
	defer p.Close()
	if _, err := openPath(slave); err == nil {
		t.Error("second open of an exclusive port succeeded")
	} else {
		t.Logf("second open: %v", err)
	}
	tm, err := unix.IoctlGetTermios(p.fd, unix.TIOCGETA)
	if err != nil {
		t.Fatal(err)
	}
	if tm.Cflag&unix.CSIZE != unix.CS8 || tm.Cflag&(unix.PARENB|unix.CSTOPB|unix.CRTSCTS) != 0 ||
		tm.Lflag&(unix.ICANON|unix.ECHO|unix.ISIG) != 0 || tm.Oflag&unix.OPOST != 0 ||
		tm.Iflag&(unix.IXON|unix.IXOFF|unix.ICRNL) != 0 || tm.Cc[unix.VMIN] != 0 || tm.Cc[unix.VTIME] != 0 {
		t.Fatalf("termios not raw 8N1: %+v", tm)
	}
	if tm.Ispeed != unix.B115200 || tm.Ospeed != unix.B115200 {
		t.Fatalf("speed %d/%d", tm.Ispeed, tm.Ospeed)
	}
	buf := make([]byte, 64)
	t0 := time.Now()
	n, err := p.Read(buf)
	el := time.Since(t0)
	if n != 0 || err != nil {
		t.Fatalf("idle read = %d, %v", n, err)
	}
	if el < 10*time.Millisecond || el > 250*time.Millisecond {
		t.Fatalf("idle read took %v, want about %v", el, readWait)
	}
	// Bytes that a cooked tty would rewrite: CR, NL, XON/XOFF, ^C, 0xFF.
	payload := []byte{0x55, 0xAA, 0x0D, 0x0A, 0x11, 0x13, 0x03, 0x7F, 0xFF, 0x00}
	if _, err := m.Write(payload); err != nil {
		t.Fatal(err)
	}
	var got []byte
	deadline := time.Now().Add(time.Second)
	for len(got) < len(payload) && time.Now().Before(deadline) {
		n, err := p.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, buf[:n]...)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("read % X, want % X", got, payload)
	}
	if n, err := p.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("write %d %v", n, err)
	}
	got = got[:0]
	m.SetReadDeadline(time.Now().Add(time.Second))
	for len(got) < len(payload) {
		n, err := m.Read(buf)
		if err != nil {
			t.Fatalf("master read after % X: %v", got, err)
		}
		got = append(got, buf[:n]...)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("master got % X, want % X", got, payload)
	}
	// Close while a Read is waiting: the Read ends with EOF, Close returns quickly.
	done := make(chan error, 1)
	go func() {
		for {
			if _, err := p.Read(buf); err != nil {
				done <- err
				return
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	t0 = time.Now()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(t0); d > 200*time.Millisecond {
		t.Fatalf("Close took %v", d)
	}
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("read after close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not stop after Close")
	}
	if _, err := p.Write(payload); err == nil {
		t.Fatal("write after close succeeded")
	}
}

// Full protocol over a real tty: the simulated light sits on the pty master.
func TestDarwinClientOverPTY(t *testing.T) {
	m, slave := openPTY(t)
	d := NewSimDevice()
	d.TelEvery = 20 * time.Millisecond
	d.StepTime = 5 * time.Millisecond
	d.Start()
	defer d.Close()
	go func() { // light -> host
		b := make([]byte, 256)
		for {
			n, err := d.Read(b)
			if n > 0 {
				if _, werr := m.Write(b[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { // host -> light
		b := make([]byte, 256)
		for {
			n, err := m.Read(b)
			if n > 0 {
				d.Write(b[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	p, err := openPath(slave)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(p)
	c.Timeout = 500 * time.Millisecond
	c.Start()
	defer c.Close()
	in, err := c.Info()
	if err != nil || in.Model != "CUTENEW_RGB_AIR" {
		t.Fatalf("info %+v %v", in, err)
	}
	if _, err := c.ReadState(); err != nil {
		t.Fatal(err)
	}
	s, err := c.SetAllConfirmed([3]bool{true, false, true}, [3]int{64, 1, 100})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("state %+v", s)
	if on, v := d.Lights(); on != [3]bool{true, false, true} || v != [3]int{64, 1, 100} {
		t.Fatalf("device %v %v", on, v)
	}
	if err := c.SetSpeed(2); err != nil { // custom timing applies in speed 2
		t.Fatal(err)
	}
	if err := c.SetTiming(Timing{500, 70, 120, 800}); err != nil {
		t.Fatal(err)
	}
	if tm, err := c.Timing(); err != nil || tm != (Timing{500, 70, 120, 800}) {
		t.Fatalf("timing %+v %v", tm, err)
	}
	if err := c.StartCapture(); err != nil {
		t.Fatal(err)
	}
	if err := c.WaitIdle(3 * time.Second); err != nil {
		t.Fatal(err)
	}
}

// Real hardware: NSS_RGBLIGHT_PORT=/dev/cu.usbmodemXXXX go test -run RealLight ./rgblight
func TestDarwinRealLight(t *testing.T) {
	name := os.Getenv("NSS_RGBLIGHT_PORT")
	if name == "" {
		t.Skip("NSS_RGBLIGHT_PORT not set (no light attached)")
	}
	ps, _ := ListPorts()
	tagged := false
	for _, p := range ps {
		if dp, _ := devicePath(name); p.Name == dp && p.Light {
			tagged = true
		}
	}
	if !tagged {
		t.Errorf("%s is not tagged as USB %s:%s by %s", name, VID, PID, EnumMethod)
	}
	p, err := OpenPort(name)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(p)
	c.Start()
	defer c.Close()
	in, err := c.Info()
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("light: %s; state %+v", in, st)
}
