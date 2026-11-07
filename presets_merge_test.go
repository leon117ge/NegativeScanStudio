package main

import "testing"

func TestMergeByName(t *testing.T) {
	type p struct {
		n string
		v int
	}
	name := func(x p) string { return x.n }
	out, upd, add, skip := mergeByName([]p{{"A", 1}, {"B", 2}}, []p{{"a", 9}, {"C", 3}, {"D", 4}}, name, 3)
	if upd != 1 || add != 1 || skip != 1 || len(out) != 3 || out[0].v != 9 || out[2].n != "C" {
		t.Fatalf("%v %d %d %d", out, upd, add, skip)
	}
	if s := importSummary(2, 3, upd, add, skip); s == "" {
		t.Fatal("empty summary")
	}
}
