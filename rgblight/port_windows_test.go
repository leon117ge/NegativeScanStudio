package rgblight

import "testing"

// Smoke test of the Windows port code (no light attached on the test PC).
func TestWindowsPorts(t *testing.T) {
	ps, err := ListPorts()
	if err != nil {
		t.Fatalf("ListPorts: %v", err)
	}
	for _, p := range ps {
		t.Logf("port %s light=%v present=%v", p.Name, p.Light, p.Present)
	}
	if _, err := OpenPort("COM250"); err == nil {
		t.Fatal("opening a missing port succeeded")
	}
	if _, err := OpenPort(`..\x`); err == nil {
		t.Fatal("bad port name accepted")
	}
}
