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
