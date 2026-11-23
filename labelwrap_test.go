package main

import (
	"strings"
	"testing"
)

func TestWrapLinesRespectsColumns(t *testing.T) {
	in := "Capture disabled: Hikrobot camera not connected (Connect above, or use the simulator)"
	for _, cols := range []int{focusStatusWrapCols, monoStatusWrapCols, statusWrapCols} {
		got := wrapLines(in, cols)
		for _, line := range strings.Split(got, "\n") {
			if len(line) > cols {
				t.Fatalf("cols=%d line %q exceeds max length %d", cols, line, len(line))
			}
		}
	}
}

func TestSetStatusNilSafe(t *testing.T) {
	setStatus(nil, "ok")
	setMonoStatus(nil, "ok")
	setMonoStatusTip(nil, "short", "full")
	setFocusStatus(nil, "ok")
	setFocusStatusTip(nil, "short", "full")
	setStatusWide(nil, "ok")
}
