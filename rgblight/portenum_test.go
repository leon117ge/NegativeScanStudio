//go:build !windows

package rgblight

import (
	"os"
	"reflect"
	"testing"
)

func TestParseIoregUSB(t *testing.T) {
	data, err := os.ReadFile("testdata/ioreg_usb.plist")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseIoregUSB(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []serialEntry{
		{Path: "/dev/cu.usbmodem205E33A1544B1", VID: 0x0483, PID: 0x5740, USB: true},
		{Path: "/dev/cu.usbserial-A10KZ", VID: 0x0403, PID: 0x6001, USB: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if !isLightID(got[0].VID, got[0].PID) || isLightID(got[1].VID, got[1].PID) {
		t.Fatal("light tag wrong")
	}
	if e, err := parseIoregUSB(nil); err != nil || e != nil {
		t.Fatalf("empty input: %v %v", e, err)
	}
	empty := []byte(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><array/></plist>`)
	if e, err := parseIoregUSB(empty); err != nil || len(e) != 0 {
		t.Fatalf("empty array: %v %v", e, err)
	}
	if _, err := parseIoregUSB([]byte("<plist><array><dict><key>x</key>")); err == nil {
		t.Fatal("truncated plist accepted")
	}
}

func TestMergeEntries(t *testing.T) {
	a := []serialEntry{{Path: "/dev/cu.usbmodem1"}, {Path: "/dev/cu.Bluetooth-Incoming-Port"}}
	b := []serialEntry{{Path: "/dev/cu.usbmodem1", VID: 0x0483, PID: 0x5740, USB: true}, {Path: "/dev/cu.usbmodem2", VID: 1, PID: 2, USB: true}}
	got := mergeEntries(a, b)
	want := []serialEntry{
		{Path: "/dev/cu.usbmodem1", VID: 0x0483, PID: 0x5740, USB: true},
		{Path: "/dev/cu.Bluetooth-Incoming-Port"},
		{Path: "/dev/cu.usbmodem2", VID: 1, PID: 2, USB: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestSortPortsAndLabel(t *testing.T) {
	ps := []PortInfo{
		{Name: "/dev/cu.debug-console", Present: true},
		{Name: "/dev/cu.usbserial-A10KZ", Present: true},
		{Name: "/dev/cu.usbmodem2", Present: true},
		{Name: "/dev/cu.usbmodem9", Present: true, Light: true},
		{Name: "/dev/cu.Bluetooth-Incoming-Port", Present: true},
	}
	sortPorts(ps)
	var names []string
	for _, p := range ps {
		names = append(names, p.Name)
	}
	want := []string{"/dev/cu.usbmodem9", "/dev/cu.usbmodem2", "/dev/cu.usbserial-A10KZ", "/dev/cu.Bluetooth-Incoming-Port", "/dev/cu.debug-console"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("order %v", names)
	}
	if ps[0].Label() != "/dev/cu.usbmodem9  (RGB Light Air, USB 0483:5740)" {
		t.Fatalf("label %q", ps[0].Label())
	}
}

func TestDevicePath(t *testing.T) {
	ok := map[string]string{
		"/dev/cu.usbmodem1":   "/dev/cu.usbmodem1",
		"cu.usbmodem1":        "/dev/cu.usbmodem1",
		" /dev/tty.usbmodem1": "/dev/cu.usbmodem1",
		"tty.usbmodem1":       "/dev/cu.usbmodem1",
	}
	for in, want := range ok {
		got, err := devicePath(in)
		if err != nil || got != want {
			t.Errorf("devicePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "  ", "../x", "/dev/../etc/passwd", "/dev/cu.a/b", "COM5", "/dev/disk0", `cu.x\y`} {
		if got, err := devicePath(in); err == nil {
			t.Errorf("devicePath(%q) = %q, want error", in, got)
		}
	}
}
