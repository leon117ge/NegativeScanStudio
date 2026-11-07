package filmdb

import (
	"path/filepath"
	"testing"
)

func TestColourCastRoundTripAndBaseUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	db, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	c := &ColourCast{Kelvin: 5500, Offset: [3]float64{.1, 0, -.2}, Model: "test model", Enabled: true}
	_, e = db.Put(Entry{Name: "500T @ 5500K", R: .5, G: .5, B: .5, ColourCast: c}, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Put(Entry{Name: "500T @ 5500K", R: .49, G: .5, B: .51}, false); e != nil {
		t.Fatal(e)
	}
	if e = db.Save(); e != nil {
		t.Fatal(e)
	}
	db, e = Load(path)
	if e != nil {
		t.Fatal(e)
	}
	stock := db.Find("500T @ 5500K")
	if stock.ColourCast == nil || *stock.ColourCast != *c || stock.R != .49 || stock.RGBLight != nil {
		t.Fatal(stock)
	}
}
