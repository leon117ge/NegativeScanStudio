package main

import (
	"strings"
	"testing"
)

func TestWrapLines(t *testing.T) {
	s := "QA Portra400: Film base WARNING vs DB (R -0.02 G +0.09 B +0.26 EV): blue is off by more than the tolerance"
	w := wrapLines(s, 58)
	for _, l := range strings.Split(w, "\n") {
		if len(l) > 58 {
			t.Fatalf("line too long: %q", l)
		}
	}
	if strings.Join(strings.Fields(w), " ") != s {
		t.Fatalf("text changed: %q", w)
	}
	if wrapLines(w, 58) != w {
		t.Fatal("not idempotent")
	}
}
