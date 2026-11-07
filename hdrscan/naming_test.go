package hdrscan

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"basetool/camera"
)

func TestSanitizeRoll(t *testing.T) {
	for in, want := range map[string]string{
		"Portra400":          "Portra400",
		" Portra 400 / #3 ":  "Portra_400_#3",
		`a:b*c?d"e<f>g|h\i`:  "a_b_c_d_e_f_g_h_i",
		"...":                "",
		"con":                "con_",
		"Ektar100\t\n":       "Ektar100",
		"Gold 200: roll 7..": "Gold_200_roll_7",
	} {
		if got := SanitizeRoll(in); got != want {
			t.Errorf("SanitizeRoll(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNextShot(t *testing.T) {
	dir := t.TempDir()
	tm := time.Date(2026, 10, 30, 17, 24, 36, 0, time.Local)
	b, n := NextShot(dir, "Portra400", tm, 0)
	if b != "Portra400_20261030_172436_0001" || n != 1 {
		t.Fatal(b, n)
	}
	os.WriteFile(filepath.Join(dir, b+"_HDR.tiff"), nil, 0o644)
	// Config counter lost (0) but the folder has 0001: next is 0002.
	if b, n = NextShot(dir, "Portra400", tm, 0); n != 2 {
		t.Fatal(b, n)
	}
	// Other roll names and other dates count too.
	os.WriteFile(filepath.Join(dir, "Ektar_20250101_101010_0041_0EV.RAF"), nil, 0o644)
	if b, n = NextShot(dir, "", tm, 5); b != "20261030_172436_0042" {
		t.Fatal(b, n)
	}
	if HDRPath(dir, filepath.Join(dir, "Ektar_20250101_101010_0041_0EV.RAF")) != filepath.Join(dir, "Ektar_20250101_101010_0041_HDR.tiff") {
		t.Error("HDRPath")
	}
}

func TestBracketPlus3(t *testing.T) {
	for _, c := range []struct {
		f0, want string
	}{{"1/80", "1/10"}, {"1/60", "1/8"}, {"1/100", "1/13"}, {"1/125", "1/15"}, {"1/4", "2s"}} {
		var e0 camera.Exposure
		for _, r := range camera.Shutters {
			if r.Label == c.f0 {
				e0 = camera.Exposure{ISO: 160, Shutter: r.Value}
			}
		}
		es, err := BracketEV(e0, []int{0, 2, 3})
		if err != nil {
			t.Fatal(err)
		}
		x := ExposureValue(es[2]) / ExposureValue(es[0])
		if camera.ShutterLabel(es[2].Shutter) != c.want || x < 7.9 || x > 8.1 {
			t.Errorf("%s +3: %s (x%.4f), want %s", c.f0, camera.ShutterLabel(es[2].Shutter), x, c.want)
		}
	}
}
