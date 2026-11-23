package main

import "testing"

func TestClampFloat(t *testing.T) {
	if g := clampFloat(0, 1, 16); g != 1 {
		t.Fatalf("below min: %v", g)
	}
	if g := clampFloat(20, 1, 16); g != 16 {
		t.Fatalf("above max: %v", g)
	}
	if g := clampFloat(4, 1, 16); g != 4 {
		t.Fatalf("inside: %v", g)
	}
	if g := clampFloat(99, 0, 0); g != 99 {
		t.Fatalf("unbounded (min==max): %v", g)
	}
}
