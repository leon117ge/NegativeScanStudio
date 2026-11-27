package hdrctl

import (
	"errors"
	"strings"
	"testing"
	"time"

	"basetool/camera"
)

// The macOS camera list over the W6 ImageCaptureCore transport, driven by
// its simulator (the same path as mactether -sim): list, connect, live view,
// status poll, disconnect.
func TestICCSimulatedXT3(t *testing.T) {
	isolate(t)
	h := newHost()
	c := New(h)
	noPTP := camera.ICCCamera{Name: "Card reader", UUID: "x", CanPTP: false}
	var said []string
	opened := 0
	c.ListDevices = func() ([]Device, error) {
		return iccDevices(
			func(time.Duration) ([]camera.ICCCamera, error) {
				return []camera.ICCCamera{noPTP, camera.SimICCCamera}, nil
			},
			func(uuid string, _ time.Duration) (*camera.ICCTransport, camera.ICCCamera, error) {
				if uuid != camera.SimICCCamera.UUID {
					return nil, camera.ICCCamera{}, errors.New("wrong uuid " + uuid)
				}
				opened++
				return camera.OpenICCSim(), camera.SimICCCamera, nil
			},
			func(s string) { said = append(said, s) })
	}
	c.Start()
	h.pump(t, "device list", 5*time.Second, func() bool { return len(c.DeviceItems()) == 2 })
	if it := c.DeviceItems(); it[0] != camera.SimICCCamera.Name {
		t.Fatalf("items %q", it)
	}
	if len(said) != 1 || !strings.Contains(said[0], "Card reader") {
		t.Fatalf("skip message %q", said)
	}
	c.SetLiveVisible(true)
	c.Connect()
	h.pump(t, "connect", 10*time.Second, c.Connected)
	if opened != 1 || !strings.HasPrefix(c.CamLbl, "Connected: ") {
		t.Fatalf("opened %d label %q", opened, c.CamLbl)
	}
	h.pump(t, "live frames", 10*time.Second, func() bool { return c.FrameSeq >= 3 })
	if c.Frame == nil || c.Frame.Bounds().Dx() < 16 {
		t.Fatal("no live image")
	}
	h.pump(t, "status poll", 10*time.Second, func() bool { return c.CamISO != "" && c.CamSh != "" })
	t.Logf("%s, camera now %s %s %s", c.CamLbl, c.CamISO, c.CamSh, c.CamF)
	c.Disconnect()
	h.pump(t, "disconnect", 5*time.Second, func() bool { return !c.Connected() && !c.LiveRunning() })
}

func TestICCListError(t *testing.T) {
	_, err := iccDevices(func(time.Duration) ([]camera.ICCCamera, error) { return nil, errors.New("denied") }, nil, nil)
	if err == nil || err.Error() != "denied" {
		t.Fatal(err)
	}
}

// A lost ImageCaptureCore session (PTP timeout or camera gone) disconnects
// the camera once and asks the user to reconnect; live view and the status
// poll stop instead of retrying commands that now fail at once.
func TestICCLostSessionDisconnects(t *testing.T) {
	isolate(t)
	h := newHost()
	c := New(h)
	var tr *camera.ICCTransport
	c.ListDevices = func() ([]Device, error) {
		return iccDevices(
			func(time.Duration) ([]camera.ICCCamera, error) { return []camera.ICCCamera{camera.SimICCCamera}, nil },
			func(string, time.Duration) (*camera.ICCTransport, camera.ICCCamera, error) {
				tr = camera.OpenICCSim()
				return tr, camera.SimICCCamera, nil
			},
			func(string) {})
	}
	c.Start()
	h.pump(t, "device list", 5*time.Second, func() bool { return len(c.DeviceItems()) == 2 })
	c.SetLiveVisible(true)
	c.Connect()
	h.pump(t, "connect", 10*time.Second, c.Connected)
	h.pump(t, "live frames", 10*time.Second, func() bool { return c.FrameSeq >= 2 })
	camera.SimICCLoseSession(tr)
	h.pump(t, "lost-session disconnect", 10*time.Second, func() bool { return !c.Connected() && !c.LiveRunning() })
	h.pump(t, "status poll stop", 5*time.Second, func() bool { return c.statStop == nil })
	prompts := func() int {
		n := 0
		for _, m := range h.messages {
			if strings.HasPrefix(m, "Camera not responding: ") {
				n++
			}
		}
		return n
	}
	if prompts() != 1 || c.CamLbl != "Not connected" {
		t.Fatalf("%d reconnect prompts, label %q, messages %q", prompts(), c.CamLbl, h.messages)
	}
	// The camera worker is free again at once (commands on the lost session
	// fail without waiting out another timeout), and the second report of the
	// same loss (live view and status poll both hit it) stays quiet.
	free := make(chan struct{})
	c.worker.do(func() { close(free) })
	select {
	case <-free:
	case <-time.After(2 * time.Second):
		t.Fatal("camera worker still busy after the lost session")
	}
	h.drain()
	if prompts() != 1 {
		t.Fatalf("second reconnect prompt: %q", h.messages)
	}
}

// A Hikrobot preview loop that does not release the camera in time is
// reported, not silently shared with the leveling or the Mono live view.
func TestHikStopPreviewTimeoutReported(t *testing.T) {
	old := hikStopTimeout
	hikStopTimeout = 50 * time.Millisecond
	defer func() { hikStopTimeout = old }()
	h := newHikCams()
	if err := h.stopPreviewSync(); err != nil || h.previewRunning() {
		t.Fatalf("no preview: %v", err)
	}
	stuck := make(chan struct{})
	h.prevDone = stuck
	if !h.previewRunning() {
		t.Fatal("running preview not reported")
	}
	if err := h.stopPreviewSync(); !errors.Is(err, errPreviewStuck) {
		t.Fatalf("stuck preview: %v", err)
	}
	close(stuck)
	if err := h.stopPreviewSync(); err != nil || h.previewRunning() {
		t.Fatalf("stopped preview: %v", err)
	}
}
