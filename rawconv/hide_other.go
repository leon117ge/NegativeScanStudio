//go:build !windows

package rawconv

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
