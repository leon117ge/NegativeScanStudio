package main

import (
	"strings"

	"github.com/lxn/walk"
)

// Column budgets for wrapped labels inside fixed-width right-hand panels
// (at 9 pt Segoe UI):
//   - statusWrapCols (58): ~450-480 px panels (HDR Camera / White light / Light)
//   - monoStatusWrapCols (50): ~380-490 px panels (Mono trichrome / Trichrome)
//   - focusStatusWrapCols (42): ~340-420 px panel (Focus check)
//   - triWrapCols (150): wide/unconstrained pages
const (
	statusWrapCols      = 58
	monoStatusWrapCols  = 50
	focusStatusWrapCols = 42
	triWrapCols         = 150
)

// wrapLines word-wraps every line of s to at most cols characters.
func wrapLines(s string, cols int) string {
	if cols <= 0 {
		return s
	}
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

// setStatusCols shows text wrapped to cols and puts tip (or text if tip is
// empty) in the tooltip, so narrow panels never clip or expand past MaxSize.
func setStatusCols(l *walk.Label, text, tip string, cols int) {
	if l == nil {
		return
	}
	if tip == "" {
		tip = text
	}
	wrapped := wrapLines(text, cols)
	if l.Text() != wrapped {
		l.SetText(wrapped)
	}
	if l.ToolTipText() != tip {
		l.SetToolTipText(tip)
	}
}

// setStatus shows s wrapped to the HDR right-hand panel width and keeps the
// full text as the tooltip, so long status lines are never cut off.
func setStatus(l *walk.Label, s string) {
	setStatusCols(l, s, s, statusWrapCols)
}

func setMonoStatus(l *walk.Label, s string) {
	setStatusCols(l, s, s, monoStatusWrapCols)
}

func setMonoStatusTip(l *walk.Label, text, tip string) {
	setStatusCols(l, text, tip, monoStatusWrapCols)
}

func setFocusStatus(l *walk.Label, s string) {
	setStatusCols(l, s, s, focusStatusWrapCols)
}

func setFocusStatusTip(l *walk.Label, text, tip string) {
	setStatusCols(l, text, tip, focusStatusWrapCols)
}

// setStatusWide is setStatus for full-width pages.
func setStatusWide(l *walk.Label, s string) {
	setStatusCols(l, s, s, triWrapCols)
}
