package filmdb

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveReloadCompare(t *testing.T) {
	dir := t.TempDir()
	db, err := Load(Path(dir))
	if err != nil || len(db.Entries) != 0 {
		t.Fatal(err, db.Entries)
	}
	if n := db.Import([]Preset{{"Portra 400", 0.61, 0.45, 0.14}, {"Bad", 0, 1, 1}}); n != 1 {
		t.Fatalf("import %d", n)
	}
	e := Entry{Name: "Gold 200", R: 0.70, G: 0.52, B: 0.18, ISO: 250, Shutter: 15625, Light: "CS-Lite", Source: "record"}
	if _, err := db.Put(e, false); err != nil {
		t.Fatal(err)
	}
	// averaging (geometric, weighted by samples)
	e2 := e
	e2.R, e2.G, e2.B = 0.70*2, 0.52*2, 0.18*2
	got, err := db.Put(e2, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Samples != 2 || math.Abs(got.R-0.70*math.Sqrt2) > 1e-9 {
		t.Fatalf("average %+v", got)
	}
	e3 := e
	e3.ISO = 160
	if _, err := db.Put(e3, true); err == nil {
		t.Fatal("averaging across ISO must fail")
	}
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}
	db2, err := Load(Path(dir))
	if err != nil || len(db2.Entries) != 2 {
		t.Fatal(err, db2.Entries)
	}
	g := db2.Find("gold 200")
	if g == nil || g.Samples != 2 || g.Aperture != 4 || g.ISO != 250 || g.Light != "CS-Lite" {
		t.Fatalf("reloaded %+v", g)
	}
	// compare: +0.05 EV in R is OK at 0.10 EV
	m := [3]float64{g.R * math.Exp2(0.05), g.G, g.B * math.Exp2(-0.03)}
	c := Compare(*g, m, 250, 15625, 0.10)
	if !c.OK || math.Abs(c.DevEV[0]-0.05) > 1e-9 {
		t.Fatalf("compare %+v", c)
	}
	m[1] = g.G * math.Exp2(-0.2)
	c = Compare(*g, m, 250, 15625, 0.10)
	if c.OK || len(c.Warnings) != 1 {
		t.Fatalf("compare G %+v", c)
	}
	c = Compare(*g, [3]float64{g.R, g.G, g.B}, 320, 15625, 0.10)
	if c.OK {
		t.Fatal("ISO mismatch not reported")
	}
	t.Log(c)
	// rename / delete
	if err := db2.Rename("Gold 200", "Portra 400"); err == nil {
		t.Fatal("rename onto existing must fail")
	}
	if err := db2.Rename("Gold 200", "Gold 200 (2024)"); err != nil {
		t.Fatal(err)
	}
	if !db2.Delete("portra 400") || len(db2.Entries) != 1 {
		t.Fatal("delete")
	}
	if db2.Import([]Preset{{"Portra 400", 0.61, 0.45, 0.14}}) != 0 {
		t.Fatal("deleted preset re-imported")
	}
	db2.Save()
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	t.Logf("%s", b)
}

func TestCrosstalkKeptOnPut(t *testing.T) {
	dir := t.TempDir()
	db, _ := Load(Path(dir))
	if _, err := db.Put(Entry{Name: "Portra 400", R: 0.5, G: 0.6, B: 0.2, ISO: 160, Shutter: 15625}, false); err != nil {
		t.Fatal(err)
	}
	m := [9]float64{1.2, -0.2, 0, 0, 1.1, -0.1, 0, 0, 1}
	if err := db.SetCrosstalk("portra 400", &m, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Put(Entry{Name: "Portra 400", R: 0.51, G: 0.6, B: 0.2, ISO: 160, Shutter: 15625}, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Save(); err != nil {
		t.Fatal(err)
	}
	db2, err := Load(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	e := db2.Find("Portra 400")
	if e == nil || e.Crosstalk == nil || *e.Crosstalk != m || e.CrosstalkInfo != "test" {
		t.Fatalf("crosstalk not kept: %+v", e)
	}
	if err := db2.SetCrosstalk("nope", &m, ""); err == nil {
		t.Fatal("expected error for unknown stock")
	}
}
