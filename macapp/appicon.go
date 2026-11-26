//go:build darwin || (linux && gio)

package main

// The header icon is not a file in this module (the repository holds no
// binary files besides the Windows icon). It is the 256 px PNG entry of the
// Windows icon ico06/app.ico:
//   - in the .app, mkbundle.py extracts it to Contents/Resources/appicon.png
//     (next to AppIcon.icns, which it builds from the same .ico);
//   - in a source tree (go run, go test), ico06/app.ico is found by walking
//     up from the executable or the working directory.
// Without either the header simply shows no icon.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
)

// icoLargestPNG returns the largest PNG-encoded entry of a Windows .ico.
func icoLargestPNG(b []byte) ([]byte, error) {
	if len(b) < 6 || binary.LittleEndian.Uint16(b[2:]) != 1 {
		return nil, errors.New("not an .ico")
	}
	n := int(binary.LittleEndian.Uint16(b[4:]))
	var best []byte
	bestW := 0
	for i := 0; i < n; i++ {
		e := 6 + 16*i
		if e+16 > len(b) {
			return nil, errors.New("truncated .ico directory")
		}
		w := int(b[e])
		if w == 0 {
			w = 256
		}
		size := int(binary.LittleEndian.Uint32(b[e+8:]))
		off := int(binary.LittleEndian.Uint32(b[e+12:]))
		if off < 0 || size < 8 || off+size > len(b) {
			continue
		}
		d := b[off : off+size]
		if bytes.HasPrefix(d, []byte("\x89PNG\r\n\x1a\n")) && w > bestW {
			best, bestW = d, w
		}
	}
	if best == nil {
		return nil, errors.New("no PNG entry in the .ico")
	}
	return best, nil
}

// appIconCandidates lists where the icon is looked for, in order.
func appIconCandidates() (pngs, icos []string) {
	var starts []string
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		d := filepath.Dir(exe)
		pngs = append(pngs, filepath.Join(d, "..", "Resources", "appicon.png"))
		starts = append(starts, d)
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, s := range starts {
		for d, i := s, 0; i < 8; i++ {
			icos = append(icos, filepath.Join(d, "ico06", "app.ico"))
			p := filepath.Dir(d)
			if p == d {
				break
			}
			d = p
		}
	}
	return pngs, icos
}

// loadAppIcon finds and decodes the header icon (nil when not found).
func loadAppIcon() image.Image {
	pngs, icos := appIconCandidates()
	for _, p := range pngs {
		if b, err := os.ReadFile(p); err == nil {
			if img, err := png.Decode(bytes.NewReader(b)); err == nil {
				return img
			}
		}
	}
	for _, p := range icos {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if d, err := icoLargestPNG(b); err == nil {
			if img, err := png.Decode(bytes.NewReader(d)); err == nil {
				return img
			}
		}
	}
	return nil
}
