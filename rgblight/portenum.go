//go:build !windows

package rgblight

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Platform-independent helpers for the macOS port code (tested everywhere).

// serialEntry is one serial node found by a port enumerator (macOS).
type serialEntry struct {
	Path     string // "/dev/cu.usbmodem205E33A1544B1"
	VID, PID uint16 // valid when USB is true
	USB      bool   // a USB device with idVendor/idProduct was found above the node
}

func isUSBModem(path string) bool {
	return strings.HasPrefix(filepath.Base(path), "cu.usbmodem")
}

// mergeEntries adds entries from b to a; b fills in a missing USB identity.
func mergeEntries(a, b []serialEntry) []serialEntry {
	idx := map[string]int{}
	for i, e := range a {
		idx[e.Path] = i
	}
	for _, e := range b {
		if i, ok := idx[e.Path]; ok {
			if !a[i].USB && e.USB {
				a[i] = e
			}
			continue
		}
		idx[e.Path] = len(a)
		a = append(a, e)
	}
	return a
}

// parseIoregUSB reads the output of `ioreg -a -l -r -c IOUSBHostDevice`
// (an XML plist: an array of USB device dictionaries with their subtrees in
// IORegistryEntryChildren) and returns every node that has an
// IOCalloutDevice, tagged with the idVendor/idProduct of its nearest USB
// ancestor. Empty input means no USB devices.
func parseIoregUSB(data []byte) ([]serialEntry, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	root, err := decodePlist(data)
	if err != nil {
		return nil, err
	}
	var out []serialEntry
	var walk func(v any, vid, pid int)
	walk = func(v any, vid, pid int) {
		switch t := v.(type) {
		case []any:
			for _, c := range t {
				walk(c, vid, pid)
			}
		case map[string]any:
			if v, ok := plistInt(t["idVendor"]); ok {
				if p, ok := plistInt(t["idProduct"]); ok {
					vid, pid = int(v&0xFFFF), int(p&0xFFFF)
				}
			}
			if s, ok := t["IOCalloutDevice"].(string); ok && s != "" {
				e := serialEntry{Path: s}
				if vid >= 0 {
					e.VID, e.PID, e.USB = uint16(vid), uint16(pid), true
				}
				out = append(out, e)
			}
			if c, ok := t["IORegistryEntryChildren"]; ok {
				walk(c, vid, pid)
			}
		}
	}
	walk(root, -1, -1)
	return out, nil
}

func plistInt(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case uint64:
		return int64(t), true
	}
	return 0, false
}

// decodePlist is a minimal XML property list reader: dict, array, string,
// integer, real, true, false, data, date. Unknown keys are kept as strings.
func decodePlist(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil, errors.New("plist: no root value")
		}
		if err != nil {
			return nil, fmt.Errorf("plist: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "plist" {
				continue
			}
			return plistValue(d, se)
		}
	}
}

func plistValue(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "dict":
		m := map[string]any{}
		key := ""
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, fmt.Errorf("plist dict: %w", err)
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "key" {
					s, err := plistText(d)
					if err != nil {
						return nil, err
					}
					key = s
					continue
				}
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				m[key] = v
			case xml.EndElement:
				return m, nil
			}
		}
	case "array":
		var a []any
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, fmt.Errorf("plist array: %w", err)
			}
			switch t := tok.(type) {
			case xml.StartElement:
				v, err := plistValue(d, t)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			case xml.EndElement:
				return a, nil
			}
		}
	case "true", "false":
		if err := d.Skip(); err != nil {
			return nil, err
		}
		return se.Name.Local == "true", nil
	case "integer":
		s, err := plistText(d)
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if n, err := strconv.ParseInt(s, 0, 64); err == nil {
			return n, nil
		}
		if n, err := strconv.ParseUint(s, 0, 64); err == nil {
			return n, nil
		}
		return s, nil
	default: // string, real, data, date, anything else
		return plistText(d)
	}
}

// plistText reads character data up to the end of the current element.
func plistText(d *xml.Decoder) (string, error) {
	var b strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := d.Token()
		if err != nil {
			return "", fmt.Errorf("plist text: %w", err)
		}
		switch t := tok.(type) {
		case xml.CharData:
			if depth == 1 {
				b.Write(t)
			}
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return b.String(), nil
}

func isLightID(vid, pid uint16) bool {
	return fmt.Sprintf("%04X", vid) == VID && fmt.Sprintf("%04X", pid) == PID
}

func portRank(p PortInfo) int {
	b := filepath.Base(p.Name)
	switch {
	case p.Light:
		return 0
	case strings.HasPrefix(b, "cu.usbmodem"):
		return 1
	case strings.HasPrefix(b, "cu.usbserial"), strings.HasPrefix(b, "cu.SLAB"), strings.HasPrefix(b, "cu.wchusbserial"):
		return 2
	}
	return 3
}

func sortPorts(ps []PortInfo) {
	sort.SliceStable(ps, func(i, j int) bool {
		ri, rj := portRank(ps[i]), portRank(ps[j])
		if ri != rj {
			return ri < rj
		}
		return ps[i].Name < ps[j].Name
	})
}

// devicePath turns a port name into a /dev/cu.* path. It accepts
// "/dev/cu.x", "cu.x" and "/dev/tty.x" or "tty.x" (mapped to cu.x).
func devicePath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("no port selected")
	}
	base := strings.TrimPrefix(name, "/dev/")
	if strings.ContainsAny(base, "/\\") || base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("bad port name %q", name)
	}
	if strings.HasPrefix(base, "tty.") {
		base = "cu." + strings.TrimPrefix(base, "tty.")
	}
	if !strings.HasPrefix(base, "cu.") {
		return "", fmt.Errorf("bad port name %q (expected /dev/cu.*)", name)
	}
	return "/dev/" + base, nil
}
