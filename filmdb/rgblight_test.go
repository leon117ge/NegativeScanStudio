package filmdb

import (
	"os"
	"strings"
	"testing"
)

func TestRGBLightRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db, _ := Load(Path(dir))
	if _, err := db.Put(Entry{Name: "Portra 400", R: 0.7, G: 0.5, B: 0.2, ISO: 250, Shutter: 15625}, false); err != nil {
		t.Fatal(err)
	}
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}
	plain, _ := os.ReadFile(Path(dir))
	if strings.Contains(string(plain), "rgb_light") {
		t.Fatal("entries without a light setting must serialise as before")
	}
	l := RGBLight{Values: [3]int{42, 65, 95}, Base: [3]float64{0.497, 0.502, 0.498}, Target: 0.5, ISO: 160, Shutter: 15625, Levelled: true}
	if _, err := db.SetRGBLight("portra 400", l); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetRGBLight("Nope", l); err == nil {
		t.Fatal("light stored on a missing stock")
	}
	if _, err := db.SetRGBLight("Portra 400", RGBLight{Values: [3]int{0, 1, 1}}); err == nil {
		t.Fatal("brightness 0 stored")
	}
	db.Save()
	db2, err := Load(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	e := db2.Find("Portra 400")
	if e.RGBLight == nil || e.RGBLight.Values != l.Values || e.R != 0.7 || e.ISO != 250 {
		t.Fatalf("round trip %+v %+v", e, e.RGBLight)
	}
	// a new base reading keeps the light setting
	if _, err := db2.Put(Entry{Name: "Portra 400", R: 0.71, G: 0.5, B: 0.2, ISO: 250, Shutter: 15625}, false); err != nil {
		t.Fatal(err)
	}
	if db2.Find("Portra 400").RGBLight == nil {
		t.Fatal("Put dropped the light setting")
	}
}
