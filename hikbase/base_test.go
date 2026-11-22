package hikbase

import (
	"math"
	"testing"
	"time"
)

func TestClampExposureUS(t *testing.T) {
	if got := ClampExposureUS(10); got != ExpMinUS {
		t.Fatalf("below min: %v", got)
	}
	if got := ClampExposureUS(20_000_000); got != ExpMaxUS {
		t.Fatalf("above max: %v", got)
	}
	if got := ClampExposureUS(14289); got != 14289 {
		t.Fatalf("passthrough: %v", got)
	}
}

func TestStatusLeveledAndManualChange(t *testing.T) {
	at := time.Date(2026, 11, 22, 18, 5, 0, 0, time.FixedZone("Asia/Tokyo", 9*3600))
	s := Snapshot{US: 14289, RGB: [3]int{21, 81, 32}, At: at}
	if !IsLeveled(s, 14289, [3]int{21, 81, 32}) {
		t.Fatal("should be leveled at the recorded values")
	}
	want := "Base exposure: 14289 us (leveled 18:05)"
	if got := Status(s, 14289, [3]int{21, 81, 32}); got != want {
		t.Fatalf("status: got %q want %q", got, want)
	}
	if IsLeveled(s, 4000, [3]int{21, 81, 32}) {
		t.Fatal("manual exposure change should un-level")
	}
	if got := Status(s, 4000, [3]int{21, 81, 32}); got != StatusUnleveled {
		t.Fatalf("manual exposure warning: %q", got)
	}
	if IsLeveled(s, 14289, [3]int{21, 80, 32}) {
		t.Fatal("light-value change should un-level")
	}
	if got := Status(s, 14289, [3]int{21, 80, 32}); got != StatusUnleveled {
		t.Fatalf("light warning: %q", got)
	}
	if IsLeveled(s, 14289, [3]int{21, 81, 32}) == false {
		t.Fatal("restoring the recorded exposure and lights should be leveled again")
	}
	if (Snapshot{}).Recorded() {
		t.Fatal("empty snapshot is not a recorded base")
	}
	if got := Status(Snapshot{}, 10000, [3]int{50, 50, 50}); got != StatusUnleveled {
		t.Fatalf("never leveled: %q", got)
	}
}

func TestCaptureLog(t *testing.T) {
	at := time.Date(2026, 11, 22, 18, 5, 0, 0, time.FixedZone("Asia/Tokyo", 9*3600))
	s := Snapshot{US: 14289, RGB: [3]int{21, 81, 32}, At: at}
	rgb := [3]int{21, 81, 32}
	if got := CaptureLog(false, 14289, s, rgb); got != "Capture at shared base 14289 us, 0.00 dB (leveled 18:05)" {
		t.Fatalf("leveled log: %q", got)
	}
	if got := CaptureLog(false, 4000, s, rgb); got != "Capture at 4000 us, 0.00 dB (base not leveled; using manual exposure)" {
		t.Fatalf("manual log: %q", got)
	}
	if got := CaptureLog(true, 14289, s, rgb); got != "Capture metering each channel separately (overrides the shared exposure of 14289 us)" {
		t.Fatalf("metered log: %q", got)
	}
}

func TestBracketExposuresFromSharedBase(t *testing.T) {
	got := BracketExposures(14289, []int{0, 2})
	want := []float64{14289, 14289 * 4}
	if len(got) != 2 || got[0] != want[0] || math.Abs(got[1]-want[1]) > 0.01 {
		t.Fatalf("0/+2: got %v want %v", got, want)
	}
	got = BracketExposures(10000, []int{0, 2, 3})
	if math.Abs(got[1]-40000) > 0.01 || math.Abs(got[2]-80000) > 0.01 {
		t.Fatalf("0/+2/+3: %v", got)
	}
	// All three channels use this same list; there is no per-channel offset.
	r, g, b := BracketExposures(14289, []int{0, 2}), BracketExposures(14289, []int{0, 2}), BracketExposures(14289, []int{0, 2})
	if r[0] != g[0] || g[0] != b[0] || r[1] != g[1] || g[1] != b[1] {
		t.Fatalf("channels diverged: R %v G %v B %v", r, g, b)
	}
}

func TestSharedSetBothDirections(t *testing.T) {
	s := &Shared{US: DefaultExposureUS, A: DefaultExposureUS, B: DefaultExposureUS}
	s.Set(14289)
	if s.A != 14289 || s.B != 14289 || s.US != 14289 || s.Sets != 1 {
		t.Fatalf("from A: %+v", s)
	}
	// A change on B updates A.
	s.Set(4000)
	if s.A != 4000 || s.B != 4000 || s.Sets != 2 {
		t.Fatalf("from B: %+v", s)
	}
	// Re-entrant Set (NumberEdit echo) must not loop or stomp.
	s.Syncing = true
	s.Set(9999)
	if s.US != 4000 || s.Sets != 2 {
		t.Fatalf("re-entrant write must be ignored: %+v", s)
	}
}

func TestParseRGBAndTime(t *testing.T) {
	rgb, ok := ParseRGB("21,81,32")
	if !ok || rgb != [3]int{21, 81, 32} {
		t.Fatalf("parse rgb: %v %v", rgb, ok)
	}
	if FormatRGB(rgb) != "21,81,32" {
		t.Fatal(FormatRGB(rgb))
	}
	at := ParseLeveledAt("2026-11-22T18:05:00+09:00")
	if at.IsZero() || at.Hour() != 18 || at.Minute() != 5 {
		t.Fatalf("parse at: %v", at)
	}
	if !ParseLeveledAt("not-a-time").IsZero() {
		t.Fatal("invalid timestamp should be zero")
	}
}
