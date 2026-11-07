package hdrscan

import (
	"strings"
	"testing"

	"basetool/camera"
)

func TestBracket(t *testing.T) {
	es, err := Bracket(camera.Exposure{ISO: 160, Shutter: 9843}, 3) // 1/100
	if err != nil || len(es) != 3 {
		t.Fatal(es, err)
	}
	want := []string{"1/100", "1/25", "1/6"}
	for i, e := range es {
		if camera.ShutterLabel(e.Shutter) != want[i] || e.ISO != 160 {
			t.Errorf("frame %d: %s, want %s ISO 160", i, e, want[i])
		}
	}
	if _, err := Bracket(camera.Exposure{ISO: 160, Shutter: 4000000}, 3); err == nil || !strings.Contains(err.Error(), "slowest timed shutter") {
		t.Errorf("4 s frame 0: want slowest-shutter error, got %v", err)
	}
	if _, err := Bracket(camera.Exposure{ISO: 160, Shutter: 250000}, 3); err != nil {
		t.Errorf("1/4 s frame 0 (+4 = 4 s) should work: %v", err)
	}
}
