package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"basetool/hdrscan"
	"basetool/lightlevel"
	"basetool/scanmeta"
)

func TestLevelingCSVAndClipboardKeepMeasuredSpace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "leveling_log.csv")
	r := levelingRecord{Time: time.Date(2026, 11, 7, 2, 3, 4, 0, time.UTC), Version: "v0.88", Mode: "Rectangle",
		Box: hdrscan.Rect{X0: .1, Y0: .2, X1: .3, Y1: .4}, Values: [3]int{12, 34, 56}, Base: [3]float64{.50123, .49876, .50001},
		Profile: "profile, with comma", Source: "TIFF-checked", Measured: true}
	if err := appendLevelingLog(path, r); err != nil {
		t.Fatal(err)
	}
	r.Values = [3]int{13, 35, 57}
	if err := appendLevelingLog(path, r); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || len(rows[0]) != 15 {
		t.Fatalf("wrong rows: %v", rows)
	}
	if rows[1][0] != "2026-11-07T10:03:04+08:00" || rows[1][3] != "0.100000" || rows[1][7] != "12" || rows[2][7] != "13" || rows[1][10] != "0.50123000" || rows[1][13] != r.Profile {
		t.Fatalf("wrong values: %v", rows)
	}
	text := r.text()
	for _, want := range []string{"R 0.50123 G 0.49876 B 0.50001", "R 13 G 35 B 57", "TIFF-checked", "+08:00"} {
		if !strings.Contains(text, want) {
			t.Fatalf("clipboard missing %q: %s", want, text)
		}
	}
}

func TestLevelingCSVReportsWriteFailure(t *testing.T) {
	dir := t.TempDir()
	if err := appendLevelingLog(dir, levelingRecord{}); err == nil {
		t.Fatal("expected failure opening directory as CSV")
	}
}

func TestFailedLevelingCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leveling_failures.csv")
	r := levelingRecord{Time: time.Date(2026, 11, 8, 12, 30, 0, 0, time.UTC), Version: "v0.92", Mode: "Rectangle", Values: [3]int{24, 73, 43}, Base: [3]float64{.48, .5, .5}}
	res := lightlevel.Result{Captured: [3]bool{true, false, true}, Rounds: 2}
	reason := "capture failed, no new base accepted"
	if err := appendLevelingFailure(path, r, res, .5, .02, reason); err != nil {
		t.Fatal(err)
	}
	res.Captured = [3]bool{true, true, true}
	res.Verified = true
	if err := appendLevelingFailure(path, r, res, .5, .02, "integer resolution limit"); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || len(rows[0]) != 19 || rows[1][8] != "" || rows[1][10] != "" || rows[1][14] != reason || rows[1][18] != "false" || rows[2][18] != "true" || rows[2][10] != "0.48000000" {
		t.Fatalf("failed run CSV: %v", rows)
	}
	if rows[1][0] != "2026-11-08T20:30:00+08:00" {
		t.Fatal(rows[1][0])
	}
}

func TestFailedRunPreservesAcceptedMetadataBase(t *testing.T) {
	previous := &scanmeta.Leveling{Base: [3]float64{.49844, .49639, .50210}, Values: [3]int{24, 73, 43}, At: "2026-11-07T22:57:46+08:00"}
	if got := acceptedLeveling(previous, nil); got != previous {
		t.Fatal("failure replaced accepted base")
	}
	unverified := &levelingRecord{Base: [3]float64{.5, .5, .5}}
	if got := acceptedLeveling(previous, unverified); got != previous {
		t.Fatal("unverified estimate replaced accepted base")
	}
	good := &levelingRecord{Base: [3]float64{.501, .499, .5}, Values: [3]int{25, 73, 43}, Time: time.Now(), Measured: true}
	got := acceptedLeveling(previous, good)
	if got == previous || got.Base != good.Base || got.Values != good.Values {
		t.Fatal("accepted base not replaced")
	}
	good.Base = [3]float64{}
	if got.Base[0] != .501 {
		t.Fatal("accepted base not frozen")
	}
}
