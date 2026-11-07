//go:build windows

package rgblight

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ListPorts lists the present COM ports, lights (VID 0483 / PID 5740)
// first. Lights are found through the USB enumerator in the registry.
func ListPorts() ([]PortInfo, error) {
	present := map[string]bool{}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE); err == nil {
		names, _ := k.ReadValueNames(-1)
		for _, n := range names {
			if v, _, err := k.GetStringValue(n); err == nil && v != "" {
				present[strings.ToUpper(v)] = true
			}
		}
		k.Close()
	}
	light := map[string]bool{}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Enum\USB`, registry.ENUMERATE_SUB_KEYS); err == nil {
		devs, _ := k.ReadSubKeyNames(-1)
		for _, dev := range devs {
			if !hwMatch(dev) {
				continue
			}
			dk, err := registry.OpenKey(k, dev, registry.ENUMERATE_SUB_KEYS)
			if err != nil {
				continue
			}
			insts, _ := dk.ReadSubKeyNames(-1)
			for _, in := range insts {
				pk, err := registry.OpenKey(dk, in+`\Device Parameters`, registry.QUERY_VALUE)
				if err != nil {
					continue
				}
				if v, _, err := pk.GetStringValue("PortName"); err == nil && v != "" {
					light[strings.ToUpper(v)] = true
				}
				pk.Close()
			}
			dk.Close()
		}
		k.Close()
	}
	var out []PortInfo
	for n := range present {
		out = append(out, PortInfo{Name: n, Present: true, Light: light[n]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Light != out[j].Light {
			return out[i].Light
		}
		return comNum(out[i].Name) < comNum(out[j].Name)
	})
	return out, nil
}

func comNum(s string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(s), "COM"))
	return n
}

type winPort struct {
	h      windows.Handle
	closed atomic.Bool
}

// OpenPort opens a COM port at 115200 8N1 (the CDC link ignores the baud
// rate) with DTR and RTS asserted and short read timeouts.
func OpenPort(name string) (io.ReadWriteCloser, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("no port selected")
	}
	path := `\\.\` + name
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	ok := false
	defer func() {
		if !ok {
			windows.CloseHandle(h)
		}
	}()
	windows.SetupComm(h, 4096, 4096)
	var dcb windows.DCB
	dcb.DCBlength = uint32(unsafeSizeofDCB)
	if err := windows.GetCommState(h, &dcb); err != nil {
		return nil, fmt.Errorf("%s: GetCommState: %w", name, err)
	}
	dcb.BaudRate = 115200
	dcb.ByteSize = 8
	dcb.Parity = 0   // NOPARITY
	dcb.StopBits = 0 // ONESTOPBIT
	// fBinary=1, DTR control enable (bits 4-5 = 01), RTS control enable
	// (bits 12-13 = 01); everything else (flow control, parity check) off.
	dcb.Flags = 1 | 1<<4 | 1<<12
	if err := windows.SetCommState(h, &dcb); err != nil {
		return nil, fmt.Errorf("%s: SetCommState: %w", name, err)
	}
	// ReadFile returns at once with what is buffered, else waits up to 20 ms.
	to := windows.CommTimeouts{ReadIntervalTimeout: 0xFFFFFFFF, ReadTotalTimeoutMultiplier: 0xFFFFFFFF, ReadTotalTimeoutConstant: 20,
		WriteTotalTimeoutConstant: 1000}
	if err := windows.SetCommTimeouts(h, &to); err != nil {
		return nil, fmt.Errorf("%s: SetCommTimeouts: %w", name, err)
	}
	windows.PurgeComm(h, windows.PURGE_RXCLEAR|windows.PURGE_TXCLEAR)
	ok = true
	return &winPort{h: h}, nil
}

func (p *winPort) Read(b []byte) (int, error) {
	if p.closed.Load() {
		return 0, io.EOF
	}
	var n uint32
	err := windows.ReadFile(p.h, b, &n, nil)
	if p.closed.Load() {
		return int(n), io.EOF
	}
	return int(n), err
}

func (p *winPort) Write(b []byte) (int, error) {
	if p.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	var n uint32
	err := windows.WriteFile(p.h, b, &n, nil)
	if err == nil && int(n) != len(b) {
		err = fmt.Errorf("short write (%d of %d bytes)", n, len(b))
	}
	return int(n), err
}

func (p *winPort) Close() error {
	if p.closed.Swap(true) {
		return nil
	}
	return windows.CloseHandle(p.h)
}
