//go:build linux && gio

package main

import (
	"errors"
	"time"
)

func runLoopProbe(time.Duration) error { return errors.New("the run-loop probe is macOS only") }
