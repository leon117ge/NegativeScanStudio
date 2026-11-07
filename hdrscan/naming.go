package hdrscan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Shot file names: <roll>_<YYYYMMDD_HHMMSS of frame 0>_<NNNN>_<step>.RAF and
// <...>_<NNNN>_HDR.tiff. The camera's own PTP names (IMG_00000008) restart
// with every connection, so they are never used for output files.

// SanitizeRoll makes a roll name safe as a Windows file-name prefix:
// illegal characters (\ / : * ? " < > | and control characters) and spaces
// become "_", runs of "_" collapse, and leading/trailing "._ " are dropped.
// At most 40 characters are kept.
func SanitizeRoll(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r < 32 || strings.ContainsRune(`\/:*?"<>| `, r) {
			r = '_'
		}
		b.WriteRune(r)
	}
	out := b.String()
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	out = strings.Trim(out, "._ ")
	if r := []rune(out); len(r) > 40 {
		out = strings.TrimRight(string(r[:40]), "._ ")
	}
	switch strings.ToUpper(out) { // reserved device names
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "LPT1", "LPT2", "LPT3":
		out += "_"
	}
	return out
}

// StepTag is the file-name tag of an EV step: "0EV", "+2EV", "+3EV".
func StepTag(ev int) string {
	if ev == 0 {
		return "0EV"
	}
	return fmt.Sprintf("+%dEV", ev)
}

var shotNum = regexp.MustCompile(`(?:^|_)\d{8}_\d{6}_(\d{4,})(?:_|\.|$)`)

// HighestShot returns the highest shot number NNNN among the files in dir
// named like this app's shots (0 if none).
func HighestShot(dir string) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	hi := 0
	for _, e := range ents {
		if m := shotNum.FindStringSubmatch(e.Name()); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > hi {
				hi = n
			}
		}
	}
	return hi
}

// ShotBase returns the base name for a shot: [roll_]YYYYMMDD_HHMMSS_NNNN.
func ShotBase(roll string, t time.Time, n int) string {
	b := fmt.Sprintf("%s_%04d", t.Format("20060102_150405"), n)
	if r := SanitizeRoll(roll); r != "" {
		b = r + "_" + b
	}
	return b
}

// NextShot picks the next shot number and base name for a bracket shot at
// time t into dir: one above both the remembered counter (last) and the
// highest number already in dir, bumped further while any file in dir
// already starts with the base name. It never returns a name in use.
func NextShot(dir, roll string, t time.Time, last int) (base string, n int) {
	n = last
	if h := HighestShot(dir); h > n {
		n = h
	}
	for {
		n++
		base = ShotBase(roll, t, n)
		if m, _ := filepath.Glob(filepath.Join(dir, globEscape(base)+"_*")); len(m) == 0 {
			return base, n
		}
	}
}

func globEscape(s string) string {
	r := strings.NewReplacer(`[`, `\[`, `]`, `\]`, `*`, `\*`, `?`, `\?`)
	if filepath.Separator == '\\' { // no escaping on Windows: replace with ?
		r = strings.NewReplacer(`[`, `?`, `]`, `?`)
	}
	return r.Replace(s)
}

// renameNoClobber moves src to dst, choosing dst_2, dst_3 ... if dst exists.
func renameNoClobber(src, dst string) (string, error) {
	dst = uniqueFile(dst)
	if err := os.Rename(src, dst); err != nil {
		return src, err
	}
	return dst, nil
}

// RollFolder is the per-roll subfolder name (v0.63): the sanitized roll
// name, or <YYYYMMDD>_roll (date of the session's first capture) when the
// roll name is empty.
func RollFolder(roll string, first time.Time) string {
	if r := SanitizeRoll(roll); r != "" {
		return r
	}
	return first.Format("20060102") + "_roll"
}

// RollDir returns <saveTo>\<roll folder> and creates it if missing (an
// existing folder is reused; NextShot then numbers past its files).
func RollDir(saveTo, roll string, first time.Time) (string, error) {
	d := filepath.Join(saveTo, RollFolder(roll, first))
	return d, os.MkdirAll(d, 0o755)
}
