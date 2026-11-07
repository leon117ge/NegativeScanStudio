package filmdb

import (
	"strings"
	"testing"
)

func TestCondAperture(t *testing.T) {
	if s := Cond(Entry{ISO: 250, Shutter: 16667, Aperture: 5.6}); !strings.Contains(s, "f/5.6") {
		t.Fatal(s)
	}
	if s := Cond(Entry{ISO: 250, Shutter: 16667, Aperture: ApertureUnknown}); !strings.Contains(s, "aperture unknown") {
		t.Fatal(s)
	}
}
