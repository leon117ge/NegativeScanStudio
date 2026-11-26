//go:build darwin

package rgblight

import (
	"context"
	"os/exec"
	"time"
)

// enumerateIoreg asks ioreg for every USB device subtree as a plist and
// pulls the serial nodes out of it (no cgo needed). Both class names are
// queried: IOUSBHostDevice (macOS 10.11+) and the older IOUSBDevice.
func enumerateIoreg() ([]serialEntry, error) {
	var all []serialEntry
	var firstErr error
	for _, class := range []string{"IOUSBHostDevice", "IOUSBDevice"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-a", "-l", "-r", "-c", class).Output()
		cancel()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ents, err := parseIoregUSB(out)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		all = mergeEntries(all, ents)
	}
	if len(all) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}
