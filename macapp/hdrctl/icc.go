package hdrctl

import (
	"fmt"
	"time"

	"basetool/camera"
)

// ICC timeouts: listing waits for ImageCaptureCore to report devices,
// opening waits for the session and the first PTP exchange.
const (
	iccListTimeout = 5 * time.Second
	iccOpenTimeout = 15 * time.Second
)

// iccDevices builds the camera list from ImageCaptureCore cameras (W6).
// list and open are camera.ListICC and camera.OpenICC on macOS; tests pass
// the ICC simulator. Called on the camera worker goroutine: the main thread
// keeps running the Cocoa run loop (app.Main), which delivers the
// ImageCaptureCore callbacks.
func iccDevices(list func(time.Duration) ([]camera.ICCCamera, error),
	open func(uuid string, timeout time.Duration) (*camera.ICCTransport, camera.ICCCamera, error),
	say func(string)) ([]Device, error) {
	cams, err := list(iccListTimeout)
	if err != nil {
		return nil, err
	}
	var devs []Device
	for _, cam := range cams {
		if !cam.CanPTP {
			say(fmt.Sprintf("Skipping %s: it does not accept PTP commands.", cam))
			continue
		}
		cam := cam
		devs = append(devs, Device{Name: cam.Name, Open: func(log *camera.Logger) (camera.Device, error) {
			t, _, err := open(cam.UUID, iccOpenTimeout)
			if err != nil {
				return nil, err
			}
			f, err := camera.OpenFuji(t, log, "ICC")
			if err != nil {
				t.Close()
				return nil, err
			}
			return f, nil
		}})
	}
	return devs, nil
}
