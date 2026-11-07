package hdrscan

import (
	"os"
	"path/filepath"
	"testing"
)

// v0.73: one output per frame; an existing name (or leftover v0.72
// companion) is never overwritten.
func TestUniqueFile(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x_HDR.tiff")
	if q := uniqueFile(p); q != p {
		t.Fatal(q)
	}
	os.WriteFile(filepath.Join(d, "x_HDR_cineon.tiff"), nil, 0o644)
	if q := uniqueFile(p); q != p {
		t.Fatal(q)
	}
	os.WriteFile(p, nil, 0o644)
	if q := uniqueFile(p); q != filepath.Join(d, "x_HDR_2.tiff") {
		t.Fatal(q)
	}
	os.WriteFile(filepath.Join(d, "x_HDR_2.tiff"), nil, 0o644)
	if q := uniqueFile(p); q != filepath.Join(d, "x_HDR_3.tiff") {
		t.Fatal(q)
	}
}
