package main

import (
	"strings"

	"github.com/lxn/walk"
)

// statusWrapCols is how many characters fit on one line of the narrow HDR
// right-hand panel; longer status lines are wrapped at word boundaries.
const statusWrapCols = 58

// wrapLines word-wraps every line of s to at most cols characters.
func wrapLines(s string, cols int) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		for len(line) > cols {
			cut := strings.LastIndex(line[:cols+1], " ")
			if cut <= cols/3 {
				cut = cols
			}
			out = append(out, strings.TrimRight(line[:cut], " "))
			line = strings.TrimLeft(line[cut:], " ")
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// setStatus shows s wrapped to the panel width and keeps the full text as the
// tooltip, so long status lines are never cut off.
func setStatus(l *walk.Label, s string) {
	if l == nil {
		return
	}
	l.SetText(wrapLines(s, statusWrapCols))
	l.SetToolTipText(s)
}

// triWrapCols (v0.82): the Trichrome page uses the full window width.
const triWrapCols = 150

// setStatusWide is setStatus for full-width pages (Trichrome).
func setStatusWide(l *walk.Label, s string) {
	if l == nil {
		return
	}
	l.SetText(wrapLines(s, triWrapCols))
	l.SetToolTipText(s)
}
