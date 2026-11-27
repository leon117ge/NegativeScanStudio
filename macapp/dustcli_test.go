//go:build darwin || (linux && gio)

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"basetool/hdrscan"
	"basetool/rawconv"
)

// --dust: a bad level prints the usage and exits 2; each TIFF gets a
// <stem>_dust.tiff and the Windows result line; a missing file is reported
// and makes the exit code 1; the input is left unchanged.
func TestDustCLI(t *testing.T) {
	var out bytes.Buffer
	if code := dustCLI("NegativeScanStudio", "off", []string{"a.tiff"}, &out); code != 2 || out.String() != "Usage: NegativeScanStudio --dust light|strong <file.tiff> [...]\n" {
		t.Fatalf("off: %d %q", code, out.String())
	}

	d := t.TempDir()
	const w, h = 320, 240
	pix := make([]uint16, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint16(9000 + 40*x + 20*y)
			if (x-150)*(x-150)+(y-110)*(y-110) < 4 { // a dark speck
				v = 1500
			}
			i := (y*w + x) * 3
			pix[i], pix[i+1], pix[i+2] = v, v*7/10, v/2
		}
	}
	in := filepath.Join(d, "Roll_0001_HDR.tiff")
	if err := rawconv.WriteTIFF16Description(in, w, h, pix, true, "FUJIFILM", "X-T3", "test", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(in)
	missing := filepath.Join(d, "gone.tiff")

	out.Reset()
	code := dustCLI("NegativeScanStudio", "Strong", []string{in, missing}, &out)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if code != 1 || len(lines) != 2 {
		t.Fatalf("code %d, output %q", code, out.String())
	}
	want := filepath.Join(d, "Roll_0001_HDR_dust.tiff")
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(in+" -> "+want) + `: \d+ spots \(\d+ hairs\), \d+ fibres, \d+ px, \d+\.\d s$`)
	if !re.MatchString(lines[0]) {
		t.Fatalf("result line %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], missing+": ") {
		t.Fatalf("missing file line %q", lines[1])
	}
	p, err := hdrscan.Load(want)
	if err != nil {
		t.Fatal(err)
	}
	if p.W != w || p.H != h {
		t.Fatalf("output %dx%d", p.W, p.H)
	}
	p.Free()
	if after, _ := os.ReadFile(in); !bytes.Equal(before, after) {
		t.Fatal("input changed")
	}

	// a second run does not overwrite: _dust_2
	out.Reset()
	if code := dustCLI("NegativeScanStudio", "light", []string{in}, &out); code != 0 || !strings.Contains(out.String(), " -> "+filepath.Join(d, "Roll_0001_HDR_dust_2.tiff")+": ") {
		t.Fatalf("second run %d %q", code, out.String())
	}
}
