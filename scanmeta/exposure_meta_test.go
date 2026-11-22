package scanmeta

import (
	"strings"
	"testing"
)

func TestExposureFieldsInJSON(t *testing.T) {
	leveled := true
	m := Metadata{
		Mode:          "mono-trichrome",
		Encoding:      "linear",
		ExposureUS:    14289,
		GainDB:        0,
		ExposureMode:  "shared",
		BaseLeveled:   &leveled,
		BaseLeveledAt: "2026-11-22T18:05:00+09:00",
	}
	s, err := m.JSON("NegativeScan Studio v1.06")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"exposure_us":14289`,
		`"exposure_mode":"shared"`,
		`"base_leveled":true`,
		`"base_leveled_at":"2026-11-22T18:05:00+09:00"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
	// gain_db is 0 and uses omitempty; that is fine. Unleveled must still
	// write base_leveled=false.
	off := false
	s, err = (Metadata{ExposureUS: 4000, ExposureMode: "shared", BaseLeveled: &off}).JSON("NSS")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, `"base_leveled":false`) {
		t.Fatalf("false leveled flag omitted: %s", s)
	}
}
