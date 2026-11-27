//go:build darwin || (linux && gio)

package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// The result and log tooltips are the Windows ToolTipText literals.
func TestResultTipsMatchWindows(t *testing.T) {
	for _, c := range []struct{ file, tip string }{
		{"../hdrpage.go", hdrResTip},
		{"../tripage.go", triResTip},
		{"../monopage.go", monoRepTip},
		{"../croppage.go", cropLogTip},
		{"../lightpage.go", "Composite of the three single-channel captures; the target level is shown as mid grey."},
	} {
		b, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), strconv.Quote(c.tip)) {
			t.Errorf("%s has no tooltip %q", c.file, c.tip)
		}
	}
	b, _ := os.ReadFile("hdrctl/light_level.go")
	if !strings.Contains(string(b), `l.ViewTip = "Composite of the three single-channel captures; the target level is shown as mid grey."`) {
		t.Error("hdrctl/light_level.go: leveling captures tooltip differs")
	}
}
