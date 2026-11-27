//go:build darwin || (linux && gio)

package main

import (
	"fmt"
	"io"
	"strings"

	"basetool/hdrscan"
	"basetool/macapp/appstate"
)

// dustCLI is the Windows "--dust light|strong <tiff> [...]" command line
// (hdrdb.go dustCLI): it cleans the TIFFs with the HDR merge's dust
// removal and writes <stem>_dust.tiff next to each, without opening the
// window. Windows prints the lines and then shows them in a message box;
// on macOS the command is run from a terminal, so the lines (and the usage
// text) only go to out. The exit codes are the Windows ones: 2 for a bad
// level, 1 when a file failed, 0 otherwise.
func dustCLI(exe, level string, files []string, out io.Writer) int {
	l, ok := hdrscan.ParseDust(level)
	if !ok || l == hdrscan.DustOff {
		fmt.Fprintf(out, "Usage: %s --dust light|strong <file.tiff> [...]\n", exe)
		return 2
	}
	var lines []string
	code := 0
	for _, f := range files {
		o, ds, err := hdrscan.DespeckleFile(f, l, appstate.AppVersion)
		if err != nil {
			lines = append(lines, f+": "+err.Error())
			code = 1
			continue
		}
		lines = append(lines, fmt.Sprintf("%s -> %s: %d spots (%d hairs), %d fibres, %d px, %.1f s", f, o, ds.Spots, ds.Hairs, ds.Fibres, ds.Pixels, ds.Elapsed.Seconds()))
	}
	fmt.Fprintln(out, strings.Join(lines, "\n"))
	return code
}
