//go:build !darwin

package hdrctl

// platformDevices: no real camera transport outside macOS in this front end.
func (c *Ctl) platformDevices() func() ([]Device, error) { return nil }
