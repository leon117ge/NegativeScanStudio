//go:build linux && gio

package main

import (
	"os"
	"os/exec"
	"strings"
)

// nativePick on Linux (development builds only) uses zenity when present.
// NSS_PICK answers without a dialog (tests, screenshots).
func nativePick(title string, kind pickKind, start string, exts []string) (string, bool) {
	if p, ok := os.LookupEnv("NSS_PICK"); ok {
		return p, p != ""
	}
	args := []string{"--file-selection", "--title=" + title}
	switch kind {
	case pickSave:
		args = append(args, "--save")
	case pickFolder:
		args = append(args, "--directory")
	}
	if start != "" {
		args = append(args, "--filename="+start)
	}
	out, err := exec.Command("zenity", args...).Output()
	if err != nil {
		return "", false
	}
	p := strings.TrimSpace(string(out))
	return p, p != ""
}
