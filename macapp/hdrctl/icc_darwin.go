//go:build darwin

package hdrctl

import "basetool/camera"

// platformDevices lists Fujifilm USB tether cameras through ImageCaptureCore.
func (c *Ctl) platformDevices() func() ([]Device, error) {
	return func() ([]Device, error) {
		return iccDevices(camera.ListICC, camera.OpenICC, func(s string) { c.Log.Printf("%s", s) })
	}
}
