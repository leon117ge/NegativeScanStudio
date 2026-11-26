package hdrctl

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"basetool/camera"
	"basetool/hdrscan"
	"basetool/lightlevel"
	"basetool/rgblight"
	"basetool/scanconfig"
)

// newLightCtl starts a controller with the Light page ready (AfterCreate).
func newLightCtl(t *testing.T) (*testHost, *Ctl, *Light) {
	t.Helper()
	isolate(t)
	h := newHost()
	c := New(h)
	// No real camera scan in tests. On macOS ListDevices is the
	// ImageCaptureCore scan, which starts its ICDeviceBrowser on the main
	// queue and waits up to 5 s for the enumeration. A go test binary runs
	// no main run loop, so the scan only finishes when the camera worker
	// goroutine happens to run on the main thread (then it pumps the run
	// loop itself); otherwise it waits the full 5 s. Connect is queued
	// behind it on the same worker, so the Trichrome tests' 5 s connect
	// waits failed on macos-26. These pages use the simulators, so no list
	// is scanned, as on Linux; the ICC tests cover the listing.
	c.ListDevices = nil
	c.Start()
	l := c.Light
	h.pump(t, "light page ready", 5*time.Second, func() bool { return strings.Contains(l.LogText(), "Light page ready") })
	h.pump(t, "port list", 5*time.Second, func() bool { return len(l.PortItems) >= 2 && l.InfoText != "Not connected" })
	return h, c, l
}

func connectSim(t *testing.T, h *testHost, l *Light) {
	t.Helper()
	l.PortIdx = len(l.PortItems) - 2 // Simulated light
	l.ToggleConnect()
	h.pump(t, "connect", 5*time.Second, func() bool { return l.Connected() && !l.busy })
	if l.sim == nil {
		t.Fatal("simulated light not selected")
	}
}

func TestLightPageStartAndPorts(t *testing.T) {
	_, _, l := newLightCtl(t)
	n := len(l.PortItems)
	if l.PortItems[n-2] != simLightItem || l.PortItems[n-1] != simNoAckItem {
		t.Fatalf("port list must end with the two simulators: %q", l.PortItems)
	}
	if l.StateText != "Light reports: -" || l.ResText != "Clear film base on the light; exposure fixed, only the light changes." {
		t.Fatalf("start texts: %q / %q", l.StateText, l.ResText)
	}
	if l.En != [3]bool{true, true, true} {
		t.Fatal("channels must start enabled")
	}
	b := l.Buttons()
	if b.ConnText != "Connect" || b.Apply || b.Start || b.Stop || !b.Ports {
		t.Fatalf("disconnected buttons: %+v", b)
	}
	// with no serial ports the simulated light is preselected
	if len(l.ports) == 0 && l.PortIdx != n-2 {
		t.Fatalf("no ports: selection %d, want the simulator", l.PortIdx)
	}
}

func TestLightConnectApplyReadOffDisconnect(t *testing.T) {
	h, _, l := newLightCtl(t)
	connectSim(t, h, l)
	if !strings.HasPrefix(l.InfoText, "Connected: SIM\n") {
		t.Fatalf("info %q", l.InfoText)
	}
	if b := l.Buttons(); b.ConnText != "Disconnect" || !b.Apply || b.Ports {
		t.Fatalf("connected buttons %+v", b)
	}
	// edits: 0 is rejected with the Windows message
	l.Val[1] = "0"
	l.Apply()
	if len(h.messages) == 0 || !strings.Contains(h.messages[len(h.messages)-1], "G brightness must be 1..100") {
		t.Fatalf("messages %q", h.messages)
	}
	l.Val = [3]string{"40", "61", "88"}
	l.En = [3]bool{true, false, true}
	l.Apply()
	h.pump(t, "apply", 5*time.Second, func() bool { return !l.busy && strings.Contains(l.LogText(), "Set channels (03, confirmed with 20)") })
	if on, v := l.sim.Lights(); on != [3]bool{true, false, true} || v != [3]int{40, 61, 88} {
		t.Fatalf("light %v %v", on, v)
	}
	if l.Settings().Values != [3]int{40, 61, 88} {
		t.Fatal("applied values not saved")
	}
	l.ReadState()
	h.pump(t, "read", 5*time.Second, func() bool { return !l.busy && strings.Contains(l.LogText(), "Read state (20)") })
	l.AllOff()
	h.pump(t, "off", 5*time.Second, func() bool { return !l.busy && strings.Contains(l.LogText(), "All off (03") })
	if on, _ := l.sim.Lights(); on != [3]bool{} {
		t.Fatalf("all off left %v", on)
	}
	sd := l.sim
	l.ToggleConnect()
	h.pump(t, "disconnect", 10*time.Second, func() bool { return !l.Connected() && !l.busy })
	if l.InfoText != "Disconnected (light switched off)." {
		t.Fatalf("info %q", l.InfoText)
	}
	if on, _ := sd.Lights(); on != [3]bool{} {
		t.Fatal("disconnect must switch the light off")
	}
}

// TestLightAreaAndISOLinks: the film-base rectangle and the ISO / shutter
// are shared with the Camera page as on Windows.
func TestLightAreaAndISOLinks(t *testing.T) {
	_, c, l := newLightCtl(t)
	r := hdrscan.Rect{X0: 0.1, Y0: 0.2, X1: 0.3, Y1: 0.4}
	c.SetROI(r)
	if box, ok := l.ManualBase(); !ok || box != toLightRect(r) {
		t.Fatalf("Camera ROI not on the Light page: %v %v", box, ok)
	}
	l.PickArea(lightlevel.Rect{X0: 0.5, Y0: 0.5, X1: 0.52, Y1: 0.6}) // too small: ignored
	if box, _ := l.ManualBase(); box != toLightRect(r) {
		t.Fatal("a drag under 3 % must be ignored")
	}
	l.PickArea(lightlevel.Rect{X0: 0.5, Y0: 0.5, X1: 0.6, Y1: 0.6})
	if c.ROI != (hdrscan.Rect{X0: 0.5, Y0: 0.5, X1: 0.6, Y1: 0.6}) || c.Cfg.AreaMode != 1 {
		t.Fatalf("Light rectangle not on Camera: %v mode %d", c.ROI, c.Cfg.AreaMode)
	}
	l.Settings().AreaMode = 0
	l.AreaModeChanged()
	if c.Cfg.AreaMode != 0 {
		t.Fatal("area mode not linked")
	}
	l.Settings().ISO = 3
	l.ISOChanged()
	if c.Cfg.ISO != 3 {
		t.Fatalf("Light ISO not linked: %d", c.Cfg.ISO)
	}
	c.Cfg.ISO = 1
	c.ISOChanged()
	if l.Settings().ISO != 1 {
		t.Fatal("Camera ISO not linked to the Light page")
	}
}

// TestLightPresetsSaveLoadFile: Save preset, file save / load through the
// native panel, and applying a preset to the (simulated) light with the
// frame-0 handoff.
func TestLightPresetsSaveLoadFile(t *testing.T) {
	h, c, l := newLightCtl(t)
	l.Val = [3]string{"30", "45", "70"}
	l.NameText, l.StockText, l.NotesText = "Vision3 500T", "5219", "roll 12"
	l.SavePreset()
	if l.PresetItems[l.PresetIdx] != "Vision3 500T" {
		t.Fatalf("preset not selected after save: %q %d", l.PresetItems, l.PresetIdx)
	}
	s, _, err := scanconfig.Load(scanconfig.Path(configDir()))
	if err != nil || s.Find("Vision3 500T") == nil {
		t.Fatalf("preset store: %v", err)
	}
	f := filepath.Join(t.TempDir(), "a")
	h.file = f
	l.SavePresetFile()
	if _, err := os.Stat(f + ".nssconfig"); err != nil {
		t.Fatalf("Save as file did not add .nssconfig: %v", err)
	}
	connectSim(t, h, l)
	h.file = f + ".nssconfig"
	l.Val = [3]string{"1", "1", "1"}
	l.LoadPresetFile() // same name exists: the Confirm (yes) stores it again, then applies
	h.pump(t, "preset applied", 5*time.Second, func() bool { return strings.HasPrefix(l.PresetText, "Applied: ") })
	if _, v := l.sim.Lights(); v != [3]int{30, 45, 70} {
		t.Fatalf("light %v", v)
	}
	if c.handoff == nil || c.handoff.Values != [3]int{30, 45, 70} || !strings.HasPrefix(c.handoff.Source, "preset ") {
		t.Fatalf("handoff %+v", c.handoff)
	}
	// a change of the light values drops the handoff
	l.Val = [3]string{"31", "45", "70"}
	l.Apply()
	h.pump(t, "apply", 5*time.Second, func() bool { return !l.busy })
	if c.handoff != nil {
		t.Fatal("handoff kept after the light changed")
	}
	h.confirm = true
	l.DeletePreset()
	if len(l.PresetItems) != 1 || l.PresetIdx != 0 {
		t.Fatalf("delete: %q", l.PresetItems)
	}
}

func TestLightCastCalculator(t *testing.T) {
	h, _, l := newLightCtl(t)
	l.CastIdx = 1
	l.CastChoiceChanged()
	if l.KelvinText != "3200" || !strings.Contains(l.CastText, "500T @ 3200 K") {
		t.Fatalf("cast %q %q", l.KelvinText, l.CastText)
	}
	l.KelvinText = "abc"
	l.CastPreview()
	if !strings.Contains(h.messages[len(h.messages)-1], "scene illuminant must be a number") {
		t.Fatalf("messages %q", h.messages)
	}
}

func csvRows(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestLevelingMatchesWindowsSequence runs a full auto film-base leveling on
// the simulated light through the Light page controller and replays the
// Windows startLevel sequence for the simulated light by hand on a fresh
// simulated light (connect: 21 + 20; lightlevel.Run with busyOnceLight, the
// same parameters and the restore set; final state read 20; the record of
// leveling_log.csv). Every step, reading, final value, serial byte and the
// CSV row (except its time stamp) must be identical.
func TestLevelingMatchesWindowsSequence(t *testing.T) {
	h, c, l := newLightCtl(t)
	connectSim(t, h, l)
	sdMac := l.sim
	cfg := *l.Settings()
	l.StartLevel()
	if !l.Leveling() {
		t.Fatalf("leveling did not start: %q", h.messages)
	}
	h.pump(t, "leveling", 120*time.Second, func() bool { return !l.Leveling() })
	if l.lastRes == nil {
		t.Fatalf("no result: %s\n%q", l.ResText, h.messages)
	}
	got := *l.lastRes
	if !strings.HasPrefix(l.ResText, "LEVELLED: ") {
		t.Fatalf("result %q", l.ResText)
	}

	// ---- the Windows sequence, by hand ----
	sd := rgblight.NewSimDevice().Start()
	cl := rgblight.NewClient(sd)
	cl.Start()
	defer cl.Close()
	if _, err := cl.Info(); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.ReadState(); err != nil {
		t.Fatal(err)
	}
	restore := [3]bool{true, true, true}
	if s := cl.CachedState(); s != nil {
		restore = s.On
		if restore == [3]bool{} {
			restore = [3]bool{true, true, true}
		}
	}
	syn := lightlevel.NewSynth(sd.Lights)
	syn.Exp = camera.Exposure{ISO: isoVals[clampIdx(cfg.ISO, len(isoVals))], Shutter: c.fixedShutter()}.String()
	p := lightlevel.Params{Target: targetVals[cfg.Target], Tol: cfg.Tol / 100, MaxIter: cfg.MaxIter,
		Settle: time.Duration(cfg.SettleMS) * time.Millisecond, Area: lightlevel.Centre, Start: cfg.Values,
		SelectArea: hdrscan.FindLevelingArea}
	var wantLog []string
	want, err := lightlevel.Run(context.Background(), busyOnceLight{cl}, syn, p, restore, func(s string) { wantLog = append(wantLog, s) })
	if err != nil {
		t.Fatal(err)
	}
	st, err := cl.ReadState()
	if err != nil || st.Value != want.Values {
		t.Fatalf("final state %v %v", st, err)
	}

	if l.lastPlan.on {
		t.Fatal("no camera profile in a fresh config: the plan must be off")
	}
	if !reflect.DeepEqual(got.Steps, want.Steps) {
		t.Fatalf("steps differ:\nmac     %+v\nwindows %+v", got.Steps, want.Steps)
	}
	if got.Values != want.Values || got.Readings != want.Readings || got.Rounds != want.Rounds || got.Done != want.Done ||
		got.Area != want.Area || got.Verified != want.Verified || got.Limited != want.Limited || got.TooBright != want.TooBright {
		t.Fatalf("result differs:\nmac     %+v\nwindows %+v", got, want)
	}
	if !got.Converged() {
		t.Fatal("simulated leveling must converge")
	}
	if a, b := sdMac.Received(), sd.Received(); !reflect.DeepEqual(a, b) {
		t.Fatalf("serial bytes differ: mac %d bytes, windows %d bytes", len(a), len(b))
	}
	// the page log holds the same leveling lines in the same order
	logText := l.LogText()
	pos := 0
	for _, s := range wantLog {
		i := strings.Index(logText[pos:], s)
		if i < 0 {
			t.Fatalf("log line missing or out of order: %q", s)
		}
		pos += i + len(s)
	}
	// leveling_log.csv: same row as the Windows record
	rows := csvRows(t, filepath.Join(configDir(), "leveling_log.csv"))
	if len(rows) != 2 {
		t.Fatalf("leveling_log.csv rows %d", len(rows))
	}
	exp := filepath.Join(t.TempDir(), "want.csv")
	rec := levelingRecord{Time: time.Now(), Version: appVersion, Mode: "Auto", Box: toScanRect(want.Area), Values: want.Values,
		Base: camPlan{want: p.Target}.post(want.Readings), Profile: camPlan{want: p.Target}.label(), Source: "Simulated measurement", Measured: true}
	if err := appendLevelingLog(exp, rec); err != nil {
		t.Fatal(err)
	}
	wrows := csvRows(t, exp)
	if !reflect.DeepEqual(rows[0], wrows[0]) || !reflect.DeepEqual(rows[1][1:], wrows[1][1:]) {
		t.Fatalf("csv row:\nmac     %q\nwindows %q", rows[1], wrows[1])
	}
	if _, err := os.Stat(filepath.Join(configDir(), "leveling_failures.csv")); err == nil {
		t.Fatal("a converged leveling must not write leveling_failures.csv")
	}
	// accepted: the config keeps the levelled values and the record; Copy works
	if l.Settings().Values != want.Values || l.Settings().Leveling == nil || l.lastSuccess == nil {
		t.Fatal("accepted leveling not stored")
	}
	l.Copy()
	if h.clip != l.lastSuccess.text() || !strings.Contains(h.clip, "Simulated measurement") {
		t.Fatalf("clipboard %q", h.clip)
	}
	if l.Comp == nil || l.Thumbs[0] == nil || l.ImgSeq == 0 {
		t.Fatal("leveling captures not shown")
	}
	t.Logf("levelled R %d G %d B %d in %d rounds, %d steps, %d serial bytes", got.Values[0], got.Values[1], got.Values[2], got.Rounds, len(got.Steps), len(sd.Received()))
}

// TestLevelingRectangleWithoutPick: Film base = Rectangle with no drawn
// rectangle stops with the Windows message and sends nothing.
func TestLevelingRectangleWithoutPick(t *testing.T) {
	h, _, l := newLightCtl(t)
	connectSim(t, h, l)
	n := len(l.sim.Received())
	l.Settings().AreaMode, l.Settings().AreaPicked = 1, false
	l.StartLevel()
	if l.Leveling() || !strings.Contains(h.messages[len(h.messages)-1], "Film base is set to Rectangle") {
		t.Fatalf("messages %q", h.messages)
	}
	if len(l.sim.Received()) != n {
		t.Fatal("commands sent")
	}
}

// TestLevelingStop: Stop cancels, nothing is accepted, the failure is recorded.
func TestLevelingStop(t *testing.T) {
	h, _, l := newLightCtl(t)
	connectSim(t, h, l)
	l.StartLevel()
	l.StopLevel()
	h.pump(t, "stopped", 60*time.Second, func() bool { return !l.Leveling() })
	if !strings.HasPrefix(l.ResText, "Stopped. Light left at") || l.lastSuccess != nil {
		t.Fatalf("result %q", l.ResText)
	}
	if rows := csvRows(t, filepath.Join(configDir(), "leveling_failures.csv")); len(rows) != 2 {
		t.Fatalf("failures rows %d", len(rows))
	}
}

// TestLevelingSimHikrobot: the simulated Hikrobot leveling camera with the
// simulated light, including the mono trichrome TIFF check.
func TestLevelingSimHikrobot(t *testing.T) {
	h, c, l := newLightCtl(t)
	connectSim(t, h, l)
	l.Settings().CamSrc = camSrcHikSim
	l.CamSourceChanged()
	if l.CamProfileOK || l.CamNote != camProfileMonoShort {
		t.Fatal("mono camera must disable the camera profile")
	}
	l.StartLevel()
	h.pump(t, "hik leveling", 180*time.Second, func() bool { return !l.Leveling() })
	if !strings.HasPrefix(l.ResText, "LEVELLED: ") {
		t.Fatalf("result %q\n%s", l.ResText, l.LogText())
	}
	if !strings.Contains(l.LogText(), "TIFF check 1: film base in the mono trichrome TIFF") {
		t.Fatal("no mono TIFF check")
	}
	if c.Cfg.HikExposureUS <= 0 {
		t.Fatal("shared Hikrobot exposure not saved")
	}
	h.pump(t, "temp folder removed", 10*time.Second, func() bool { return strings.Contains(l.LogText(), "Leveling temp frames deleted") })
}
