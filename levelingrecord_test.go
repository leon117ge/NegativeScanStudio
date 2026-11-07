package main

import (
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"basetool/hdrscan"
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

func TestCurrentLevelingBaseRoundTripAndRejectInvalid(t *testing.T) {
	b := [3]float64{.50123456, .49999999, .50000001}
	got, ok := parseCurrentBase(currentBaseLine(b))
	if !ok || got != b {
		t.Fatalf("roundtrip: %v %v", got, ok)
	}
	for _, v := range []float64{0, -1, math.NaN(), math.Inf(1), 16.01} {
		bad := [3]float64{.5, v, .5}
		if validLevelingBase(bad) {
			t.Fatalf("accepted %v", v)
		}
	}
	for _, s := range []string{"// FBM_CURRENT_BASE|NaN|.5|.5", "// FBM_CURRENT_BASE|.5|.5", "// FBM_CURRENT_BASE|0|.5|.5"} {
		if _, ok := parseCurrentBase(s); ok {
			t.Fatalf("accepted %q", s)
		}
	}
}

func TestLevelingCSVReportsWriteFailure(t *testing.T) {
	dir := t.TempDir()
	if err := appendLevelingLog(dir, levelingRecord{}); err == nil {
		t.Fatal("expected failure opening directory as CSV")
	}
}
