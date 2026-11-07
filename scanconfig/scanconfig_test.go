package scanconfig

import (
	"path/filepath"
	"testing"
)

func TestStoreRoundTripAndMigrate(t *testing.T) {
	Shutters = []uint32{15625, 7812}
	dir := t.TempDir()
	s, ok, err := Load(Path(dir))
	if err != nil || ok {
		t.Fatalf("fresh load: %v %v", ok, err)
	}
	n := s.Migrate([]Legacy{{Name: "Session 1", Values: [3]int{42, 65, 95}, ISO: 160, ShutterUS: 15625, Levelled: true}})
	if n != 1 {
		t.Fatalf("migrated %d", n)
	}
	if c := s.Find("session 1"); c == nil || c.ShutterUS != 15625 || c.Light.Values != [3]int{42, 65, 95} || c.ISO != 160 {
		t.Fatalf("migrated entry wrong: %+v", c)
	}
	if s.Migrate([]Legacy{{Name: "Session 1"}}) != 0 {
		t.Fatal("migrate must not duplicate")
	}
	c := Config{Name: "Portra 400", Light: Light{Values: [3]int{40, 60, 90}, On: [3]bool{true, true, false}}, ISO: 200, ShutterUS: 7812}
	if rep, err := s.Put(c); err != nil || rep {
		t.Fatal(rep, err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, ok, err := Load(Path(dir))
	if err != nil || !ok || len(s2.Presets) != 2 || s2.Find("Portra 400").Light.On != [3]bool{true, true, false} {
		t.Fatalf("reload: %v %v %+v", ok, err, s2)
	}
	f := filepath.Join(dir, "p.nssconfig")
	if err := WriteFile(f, c); err != nil {
		t.Fatal(err)
	}
	r, err := ReadFile(f)
	if err != nil || r.ISO != 200 || r.ShutterUS != 7812 || r.Light.Values != c.Light.Values {
		t.Fatalf("file: %+v %v", r, err)
	}
	if _, err := s.Put(Config{Name: "bad", ShutterUS: 1234}); err == nil {
		t.Fatal("bad shutter accepted")
	}
	if _, err := s.Put(Config{Name: "bad", Light: Light{Values: [3]int{101, 0, 0}}}); err == nil {
		t.Fatal("bad value accepted")
	}
	if !s.Delete("portra 400") || s.Find("Portra 400") != nil {
		t.Fatal("delete")
	}
}
