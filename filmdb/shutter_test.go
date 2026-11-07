package filmdb

import (
	"path/filepath"
	"testing"
)

// v0.81: the base shutter (1/60 s or 1/125 s) is stored with the light values.
func TestRGBLightShutterRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	db, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Put(Entry{Name: "Portra 400", R: 0.5, G: 0.4, B: 0.3, ISO: 250, Shutter: 7812}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetRGBLight("Portra 400", RGBLight{Values: [3]int{40, 55, 70}, Base: [3]float64{0.8, 0.8, 0.8}, Target: 0.8, ISO: 250, Shutter: 7812, Date: "2026-11-02"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}
	db2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	e := db2.Find("Portra 400")
	if e == nil || e.RGBLight == nil || e.RGBLight.Shutter != 7812 || e.RGBLight.ISO != 250 || e.RGBLight.Values != [3]int{40, 55, 70} {
		t.Fatalf("%+v", e)
	}
	if got := shutterLabel(7812); got != "1/125 s" {
		t.Fatalf("label %q", got)
	}
}
