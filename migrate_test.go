package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyConfig(t *testing.T) {
	d := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", d)
	t.Setenv("APPDATA", d)
	old := legacyConfigDir()
	os.MkdirAll(old, 0o755)
	os.WriteFile(filepath.Join(old, "config.txt"), []byte("// FBM_PRESET|Portra 400|0.80000|0.50000|0.20000\nDCTL_PATH=x\n"), 0o644)
	os.WriteFile(filepath.Join(old, "film_base_db.json"), []byte(`{"version":1,"entries":[]}`), 0o644)
	os.WriteFile(filepath.Join(old, "crash_log.txt"), []byte("old"), 0o644)
	got := migrateLegacyConfig()
	if len(got) != 2 {
		t.Fatalf("copied %v", got)
	}
	if c := loadConfig(); len(c.Presets) != 1 || c.Presets[0].Name != "Portra 400" {
		t.Fatalf("presets %+v", c.Presets)
	}
	if _, err := os.Stat(filepath.Join(configDir(), "crash_log.txt")); err == nil {
		t.Fatal("crash log must not be migrated")
	}
	// new files win, old files untouched
	os.WriteFile(filepath.Join(configDir(), "config.txt"), []byte("NEW"), 0o644)
	os.WriteFile(filepath.Join(old, "config.txt"), []byte("CHANGED-OLD"), 0o644)
	if got := migrateLegacyConfig(); len(got) != 0 {
		t.Fatalf("second run copied %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(configDir(), "config.txt")); string(b) != "NEW" {
		t.Fatal("overwrote new config")
	}
	if b, _ := os.ReadFile(filepath.Join(old, "config.txt")); string(b) != "CHANGED-OLD" {
		t.Fatal("touched old config")
	}
	if filepath.Base(configDir()) != "NegativeScanStudio" || filepath.Base(old) != "FilmBaseMeter" {
		t.Fatal(configDir(), old)
	}
}
