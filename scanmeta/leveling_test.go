package scanmeta

import (
	"encoding/json"
	"math"
	"testing"
)

func TestSavedLevelingSnapshot(t *testing.T) {
	values := [3]int{24, 73, 43}
	l := Leveling{Base: [3]float64{.49844, .49639, .50210}, Values: values, At: "2026-11-07T22:57:46+08:00"}
	data, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	var restored Leveling
	if err = json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	got := (Metadata{LightRGB: &values}).WithLeveling(&restored, values).ForCapture()
	if got.FilmBase == nil || *got.FilmBase != l.Base || got.BaseSource != LevelingSource || got.FilmBaseSpace != AfterProfile || restored.At != l.At {
		t.Fatalf("lost accepted result after restart: %+v", got)
	}
	restored.Base[0] = .1
	if *got.FilmBase != l.Base {
		t.Fatal("capture did not freeze the saved result")
	}
	for _, absent := range []Metadata{(Metadata{}).WithLeveling(nil, values), (Metadata{}).WithLeveling(&l, [3]int{25, 73, 43}), {FilmBase: &l.Base}, {FilmBase: &l.Base, FilmBaseSpace: AfterProfile}} {
		if absent.ForCapture().FilmBase != nil {
			t.Fatal("missing/disabled/unverified leveling produced metadata")
		}
	}
	for _, bad := range []float64{0, math.NaN(), math.Inf(1)} {
		l.Base[0] = bad
		if (Metadata{}).WithLeveling(&l, values).FilmBase != nil {
			t.Fatal("invalid leveling accepted")
		}
	}
}
