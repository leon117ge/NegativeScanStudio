package hdrscan

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRollDir(t *testing.T) {
	save := t.TempDir()
	t0 := time.Date(2026, 10, 30, 19, 0, 12, 0, time.Local)
	d, err := RollDir(save, `Portra 400/Kyoto`, t0)
	if err != nil || d != filepath.Join(save, "Portra_400_Kyoto") {
		t.Fatal(d, err)
	}
	if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
		t.Fatal("not created", err)
	}
	e, _ := RollDir(save, "  ", t0)
	if filepath.Base(e) != "20261030_roll" {
		t.Fatal(e)
	}
	// existing folder: continue numbering, never reuse a name
	for _, f := range []string{"Portra_400_Kyoto_20261030_190012_0007_0EV.RAF", "Portra_400_Kyoto_20261030_190012_0007_HDR.tiff"} {
		os.WriteFile(filepath.Join(d, f), []byte("x"), 0o644)
	}
	d2, err := RollDir(save, "Portra 400/Kyoto", t0)
	if err != nil || d2 != d {
		t.Fatal(d2, err)
	}
	b, n := NextShot(d2, "Portra 400/Kyoto", t0, 3)
	if n != 8 || b != "Portra_400_Kyoto_20261030_190012_0008" {
		t.Fatal(b, n)
	}
	// a BASE record takes a number too
	os.WriteFile(filepath.Join(d2, b+"_BASE.RAF"), []byte("x"), 0o644)
	if _, n := NextShot(d2, "Portra 400/Kyoto", t0, 0); n != 9 {
		t.Fatal("BASE not counted", n)
	}
	// dedup of an existing file name
	p := filepath.Join(d2, b+"_BASE.RAF")
	if u := uniqueFile(p); u == p {
		t.Fatal("uniqueFile overwrote")
	}
}
