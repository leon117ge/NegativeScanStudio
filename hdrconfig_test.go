package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBracketConfig(t *testing.T) {
	d := t.TempDir()
	t.Setenv("APPDATA", d)
	os.MkdirAll(configDir(), 0o755)
	write := func(s string) { os.WriteFile(hdrConfigFile(), []byte(s), 0o644) }
	cases := []struct {
		cfg  string
		want string
	}{
		{"", "0,2"},                  // new config: default 0/+2 (v0.77)
		{"TARGETFS=0.80\r\n", "0,2"}, // no bracket key
		{"BRACKET=0\r\n", "0,2"},     // v0.61 0/+2 respected
		{"BRACKET=1\r\n", "0,2"},     // v0.61 0/+2/+4: removed -> default
		{"BRACKET=0\r\nBRACKET_EV=0,2\r\n", "0,2"},
		{"BRACKET=0\r\nBRACKET_EV=0,3\r\n", "0,2"}, // removed preset -> default
		{"BRACKET=0\r\nBRACKET_EV=0,2,3\r\n", "0,2,3"},
		{"BRACKET_EV=0,2,3\r\nBRACKET=0\r\n", "0,2,3"},
		{"BRACKET=1\r\nBRACKET_EV=0,2,4\r\n", "0,2"},   // removed
		{"BRACKET=0\r\nBRACKET_EV=0,1,2,3\r\n", "0,2"}, // removed 4-frame
		{"BRACKET_EV=0,2,3,5\r\n", "0,2"},              // never existed
	}
	for _, c := range cases {
		os.Remove(hdrConfigFile())
		if c.cfg != "" {
			write(c.cfg)
		}
		got := loadHDRConfig()
		if k := evKey(bracketEVs[got.Bracket]); k != c.want {
			t.Errorf("%q: got %s want %s", c.cfg, k, c.want)
		}
		saveHDRConfig(got) // round trip
		if k := evKey(bracketEVs[loadHDRConfig().Bracket]); k != c.want {
			t.Errorf("%q round trip: got %s", c.cfg, k)
		}
	}
	if bracketDefault != 0 || len(bracketEVs) != 2 || len(bracketItems) != 2 || evKey(bracketEVs[0]) != "0,2" || evKey(bracketEVs[1]) != "0,2,3" {
		t.Error("bracket tables: v0.77 offers exactly 0/+2 (default) and 0/+2/+3")
	}
	_ = filepath.Join
}

// v0.73: the v0.72 colour keys are ignored on load and dropped on save.
func TestLegacyColorKeysIgnored(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	os.MkdirAll(configDir(), 0o755)
	os.WriteFile(hdrConfigFile(), []byte("DUST=light\r\nMERGE=1\r\nCOLOR_ENC=cineon\r\nMASK_COMP=on\r\nCROSSTALK=100\r\nXT_MATRIX=chart\r\nCOLOR_BASE=stock\r\n"), 0o644)
	c := loadHDRConfig()
	if c.Dust != 1 || c.Merge != 1 {
		t.Fatalf("dust %d merge %d", c.Dust, c.Merge)
	}
	saveHDRConfig(c)
	b, _ := os.ReadFile(hdrConfigFile())
	for _, k := range []string{"COLOR_ENC", "MASK_COMP", "CROSSTALK", "XT_MATRIX", "COLOR_BASE"} {
		if strings.Contains(string(b), k+"=") {
			t.Errorf("%s written back", k)
		}
	}
	if !strings.Contains(string(b), "DUST=light") {
		t.Errorf("dust lost: %s", b)
	}
}

// v0.74: Weighted is the default merge; MERGE_METHOD (v0.74+) is respected,
// a legacy MERGE=0 (v0.70-v0.73 default, written untouched) moves to Weighted.
func TestMergeDefault(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	os.MkdirAll(configDir(), 0o755)
	cases := []struct {
		cfg  string
		want int
	}{
		{"", 1},
		{"TARGETFS=0.80\r\n", 1},
		{"MERGE=0\r\n", 1},
		{"MERGE=1\r\n", 1},
		{"MERGE=0\r\nMERGE_METHOD=classic\r\n", 0},
		{"MERGE_METHOD=classic\r\nMERGE=0\r\n", 0},
		{"MERGE=1\r\nMERGE_METHOD=weighted\r\n", 1},
		{"MERGE=7\r\n", 1},
	}
	for _, c := range cases {
		os.Remove(hdrConfigFile())
		if c.cfg != "" {
			os.WriteFile(hdrConfigFile(), []byte(c.cfg), 0o644)
		}
		got := loadHDRConfig()
		if got.Merge != c.want {
			t.Errorf("%q: merge %d want %d", c.cfg, got.Merge, c.want)
		}
		saveHDRConfig(got)
		if m := loadHDRConfig().Merge; m != c.want {
			t.Errorf("%q round trip: %d", c.cfg, m)
		}
		b, _ := os.ReadFile(hdrConfigFile())
		if !strings.Contains(string(b), "MERGE_METHOD="+mergeKeys[c.want]) {
			t.Errorf("%q: saved %s", c.cfg, b)
		}
	}
}

func TestCenterMarkConfig(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	os.MkdirAll(configDir(), 0o755)
	if c := loadHDRConfig(); !c.CenterMark {
		t.Fatal("missing file: Center mark should default on")
	}
	os.WriteFile(hdrConfigFile(), []byte("TARGETFS=0.80\r\n"), 0o644)
	if c := loadHDRConfig(); !c.CenterMark {
		t.Fatal("legacy file without key: Center mark should default on")
	}
	os.WriteFile(hdrConfigFile(), []byte("CENTER_MARK=0\r\n"), 0o644)
	if c := loadHDRConfig(); c.CenterMark {
		t.Fatal("CENTER_MARK=0 should load off")
	}
	saveHDRConfig(loadHDRConfig())
	b, _ := os.ReadFile(hdrConfigFile())
	if !strings.Contains(string(b), "CENTER_MARK=0") {
		t.Fatalf("off state not saved: %s", b)
	}
	os.WriteFile(hdrConfigFile(), []byte("CENTER_MARK=1\r\n"), 0o644)
	c := loadHDRConfig()
	if !c.CenterMark {
		t.Fatal("CENTER_MARK=1 should load on")
	}
	saveHDRConfig(c)
	b, _ = os.ReadFile(hdrConfigFile())
	if !strings.Contains(string(b), "CENTER_MARK=1") {
		t.Fatalf("on state not saved: %s", b)
	}
}

func TestHikExposureConfig(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	os.MkdirAll(configDir(), 0o755)
	if c := loadHDRConfig(); c.HikExposureUS != 10000 || c.HikMeterEach {
		t.Fatalf("defaults: us=%v meter=%v", c.HikExposureUS, c.HikMeterEach)
	}
	os.WriteFile(hdrConfigFile(), []byte("TARGETFS=0.80\r\n"), 0o644)
	if c := loadHDRConfig(); c.HikExposureUS != 10000 || c.HikMeterEach || c.HikLeveledAt != "" {
		t.Fatalf("legacy file should default shared exposure: %+v", c)
	}
	c := loadHDRConfig()
	c.HikExposureUS = 14289
	c.HikMeterEach = false
	c.HikLeveledUS = 14289
	c.HikLeveledRGB = [3]int{21, 81, 32}
	c.HikLeveledAt = "2026-11-22T18:05:00+09:00"
	saveHDRConfig(c)
	b, _ := os.ReadFile(hdrConfigFile())
	s := string(b)
	for _, want := range []string{"HIK_EXPOSURE=14289", "HIK_METER_EACH=0", "HIK_LEVELED_US=14289", "HIK_LEVELED_RGB=21,81,32", "HIK_LEVELED_AT=2026-11-22T18:05:00+09:00"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
	got := loadHDRConfig()
	if got.HikExposureUS != 14289 || got.HikMeterEach || got.HikLeveledUS != 14289 || got.HikLeveledRGB != [3]int{21, 81, 32} || got.HikLeveledAt != c.HikLeveledAt {
		t.Fatalf("round trip: %+v", got)
	}
	os.WriteFile(hdrConfigFile(), []byte("HIK_EXPOSURE=4000\r\nHIK_METER_EACH=1\r\n"), 0o644)
	got = loadHDRConfig()
	if got.HikExposureUS != 4000 || !got.HikMeterEach {
		t.Fatalf("manual + meter: %+v", got)
	}
	if got.HikLeveledAt != "" {
		t.Fatalf("unleveled record should stay empty: %q", got.HikLeveledAt)
	}
	// Below the camera minimum is ignored; keep the default.
	os.WriteFile(hdrConfigFile(), []byte("HIK_EXPOSURE=10\r\n"), 0o644)
	if c := loadHDRConfig(); c.HikExposureUS != 10000 {
		t.Fatalf("invalid exposure should stay default: %v", c.HikExposureUS)
	}
}
