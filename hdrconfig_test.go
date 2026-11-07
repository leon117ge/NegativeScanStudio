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
