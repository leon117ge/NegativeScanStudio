package cropctl

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"basetool/rawconv"
)

func (u *RAWUI) rowStates() []int {
	u.mu.Lock()
	defer u.mu.Unlock()
	var s []int
	for _, r := range u.model.rows {
		s = append(s, r.State)
	}
	return s
}

func TestRawAddFilterAndTable(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	var raws []string
	for _, n := range []string{"b.RAF", "a.nef", "c.dng"} {
		p := filepath.Join(d, n)
		os.WriteFile(p, []byte("x"), 0644)
		raws = append(raws, p)
	}
	os.WriteFile(filepath.Join(d, "x.tif"), []byte("x"), 0644)
	h := newHost()
	u := NewRaw(h)
	if u.DNGLbl != "Not available on macOS" || u.DNGPath != "" {
		t.Fatalf("DNG %q %q", u.DNGLbl, u.DNGPath)
	}
	if len(u.WorkerItems) != maxRawWorkers() || len(u.ICCNames) != len(rawconv.ICCProfiles) {
		t.Fatalf("combos %v %v", u.WorkerItems, u.ICCNames)
	}
	if n := u.AddFiles(append(raws, filepath.Join(d, "x.tif"), strings.ToUpper(raws[0][:len(raws[0])-3])+"RAF")); n != 3 {
		t.Fatalf("added %d", n)
	}
	h.folder = d
	u.AddFolderDialog()
	h.drain()
	if u.Rows() != 3 || u.Cell(0, 2) != "Pending" || u.Cell(0, 1) != d {
		t.Fatalf("rows %d %q %q", u.Rows(), u.Cell(0, 2), u.Cell(0, 1))
	}
	u.ClickRow(0, false, false, -1)
	u.ClickRow(2, false, true, 0)
	if !u.Selected(0) || !u.Selected(1) || !u.Selected(2) {
		t.Fatal("shift-click range")
	}
	u.ClickRow(1, true, false, 2)
	u.RemoveSelected()
	if u.Rows() != 1 || u.Cell(0, 0) != "a.nef" {
		t.Fatalf("after remove: %d %q", u.Rows(), u.Cell(0, 0))
	}
	u.Clear()
	if u.Rows() != 0 {
		t.Fatal("clear")
	}
	u.SetOptions(1, 1, len(rawconv.ICCProfiles)-1, 0, true)
	c := loadRawConfig()
	if !c.Subfolder || !c.LZW || c.Workers != 1 || !c.UseDNG || c.ICC != rawconv.ICCProfiles[len(rawconv.ICCProfiles)-1].Key {
		t.Fatalf("saved %+v", c)
	}
	if u.options().UseDNG {
		t.Fatal("DNG converter used without an install")
	}
}

// TestRawErrorsSkipsRetry runs the workers on files LibRaw rejects and on
// one whose TIFF already exists.
func TestRawErrorsSkipsRetry(t *testing.T) {
	isolate(t)
	d := t.TempDir()
	bad := filepath.Join(d, "bad.RAF")
	os.WriteFile(bad, []byte("not a raw file"), 0644)
	have := filepath.Join(d, "have.RAF")
	os.WriteFile(have, []byte("x"), 0644)
	os.WriteFile(rawconv.DstPath(have, false), []byte("tiff"), 0644)
	h := newHost()
	u := NewRaw(h)
	u.SetOptions(0, 1, 0, 1, false)
	u.Convert()
	if u.Info != "Nothing pending. Add RAW files first (files already converted are not repeated)." {
		t.Fatalf("info %q", u.Info)
	}
	u.AddFiles([]string{bad, have})
	u.Convert()
	if !u.StopOn || u.ConvOn || u.workers != 2 {
		t.Fatalf("running buttons conv=%v stop=%v workers=%d", u.ConvOn, u.StopOn, u.workers)
	}
	h.pump(t, "conversion", 30*time.Second, func() bool { return !u.Running() && u.ConvOn })
	if s := u.rowStates(); s[0] != rsError || s[1] != rsSkipped {
		t.Fatalf("states %v (%q, %q)", s, u.Cell(0, 2), u.Cell(1, 2))
	}
	if !strings.HasPrefix(u.Cell(0, 2), "ERROR: ") || u.CellStyle(0) != CellError {
		t.Fatalf("error row %q", u.Cell(0, 2))
	}
	if u.Cell(1, 2) != "Skipped: have.RAF.tiff already exists" {
		t.Fatalf("skip row %q", u.Cell(1, 2))
	}
	if u.Info != "Finished. 0 converted, 1 skipped (TIFF exists), 1 failed." || u.PBVal != 2 || u.PBMax != 2 {
		t.Fatalf("info %q %d/%d", u.Info, u.PBVal, u.PBMax)
	}
	u.RetryFailed()
	if s := u.rowStates(); s[0] != rsPending || s[1] != rsSkipped || u.PBVal != 1 {
		t.Fatalf("retry: %v %d", s, u.PBVal)
	}
	u.ClearFinished()
	if u.Rows() != 1 {
		t.Fatalf("clear finished left %d rows", u.Rows())
	}
}

func TestRawWatchFolder(t *testing.T) {
	isolate(t)
	old := watchTick
	watchTick = 15 * time.Millisecond
	defer func() { watchTick = old }()
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "old.RAF"), []byte("x"), 0644)
	h := newHost()
	u := NewRaw(h)
	u.ToggleWatch()
	if len(h.messages) != 1 || !strings.Contains(h.messages[0], "Choose the folder your tether software saves RAW files into first.") {
		t.Fatalf("messages %q", h.messages)
	}
	h.folder = d
	u.ChooseWatchDir()
	if u.WatchDir() != d || loadRawConfig().WatchDir != d {
		t.Fatalf("watch dir %q", u.WatchDir())
	}
	u.ToggleWatch()
	if !u.Watching() || u.WatchText != "Stop watching" || u.WatchLbl != "Watching. 1 RAW file(s) already there are ignored." {
		t.Fatalf("watch %v %q %q", u.Watching(), u.WatchText, u.WatchLbl)
	}
	os.WriteFile(filepath.Join(d, "new.RAF"), []byte("still not a raw"), 0644)
	h.pump(t, "new file", 10*time.Second, func() bool { return u.Rows() == 1 })
	if u.Cell(0, 0) != "new.RAF" || !strings.HasPrefix(u.WatchLbl, "Watching. Last new file: new.RAF") {
		t.Fatalf("row %q label %q", u.Cell(0, 0), u.WatchLbl)
	}
	h.pump(t, "auto convert", 30*time.Second, func() bool { return !u.Running() && u.ConvOn })
	if !strings.HasSuffix(u.Info, "Watching folder for new RAW files...") {
		t.Fatalf("info %q", u.Info)
	}
	u.Shutdown()
	if u.Watching() || u.WatchText != "Start watching" {
		t.Fatal("shutdown did not stop the watch")
	}
}

// testRAFs are real RAW files for the conversion tests: NSS_TEST_RAF is a
// list of paths (OS path list separator). The tests skip without it.
func testRAFs(t *testing.T, n int) []string {
	t.Helper()
	var ps []string
	for _, p := range filepath.SplitList(os.Getenv("NSS_TEST_RAF")) {
		if _, err := os.Stat(p); err == nil {
			ps = append(ps, p)
		}
	}
	if len(ps) < n {
		t.Skipf("set NSS_TEST_RAF to %d RAW file(s) to run this test", n)
	}
	return ps[:n]
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0644); err != nil {
		t.Fatal(err)
	}
}

// TestRawGoldenMatchesWindows converts with the macOS page and with the
// Windows worker's call (rawconv.Convert with RAWUI.options) and compares
// the TIFFs byte for byte.
func TestRawGoldenMatchesWindows(t *testing.T) {
	isolate(t)
	src := testRAFs(t, 1)[0]
	cases := []struct {
		name               string
		dest, comp, icc, w int
	}{
		{"same_folder_uncompressed", 0, 0, 0, 0},
		{"make_tiff_lzw", 1, 1, 0, 1},
		{"icc_last", 0, 1, len(rawconv.ICCProfiles) - 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mac, win := t.TempDir(), t.TempDir()
			pm, pw := filepath.Join(mac, "frame.RAF"), filepath.Join(win, "frame.RAF")
			copyFile(t, src, pm)
			copyFile(t, src, pw)
			h := newHost()
			u := NewRaw(h)
			u.SetOptions(c.dest, c.comp, c.icc, c.w, false)
			u.AddFiles([]string{pm})
			u.Convert()
			h.pump(t, "conversion", 120*time.Second, func() bool { return !u.Running() && u.ConvOn })
			if s := u.rowStates(); s[0] != rsDone || !strings.HasPrefix(u.Cell(0, 2), "Done: ") {
				t.Fatalf("state %v %q", s, u.Cell(0, 2))
			}
			w := &RAWUI{cfg: u.Cfg()}
			if _, err := rawconv.Convert(context.Background(), pw, w.options()); err != nil {
				t.Fatal(err)
			}
			sub := c.dest == 1
			gm, err := os.ReadFile(rawconv.DstPath(pm, sub))
			if err != nil {
				t.Fatal(err)
			}
			gw, err := os.ReadFile(rawconv.DstPath(pw, sub))
			if err != nil {
				t.Fatal(err)
			}
			if len(gm) < 1<<20 || !bytes.Equal(gm, gw) {
				t.Fatalf("macOS TIFF (%d bytes) differs from the Windows path (%d bytes)", len(gm), len(gw))
			}
			t.Logf("%s: %d bytes identical (%s)", c.name, len(gm), u.Cell(0, 2))
		})
	}
}

// TestRawStopKeepsPending stops a run: the file being converted is
// cancelled and every unconverted file stays Pending, with no TIFF left.
func TestRawStopKeepsPending(t *testing.T) {
	isolate(t)
	src := testRAFs(t, 1)[0]
	d := t.TempDir()
	var ps []string
	for _, n := range []string{"a.RAF", "b.RAF", "c.RAF"} {
		p := filepath.Join(d, n)
		copyFile(t, src, p)
		ps = append(ps, p)
	}
	h := newHost()
	u := NewRaw(h)
	u.SetOptions(0, 1, 0, 0, false) // one worker
	u.AddFiles(ps)
	u.Convert()
	h.pump(t, "first file started", 10*time.Second, func() bool { return u.rowStates()[0] == rsWorking })
	u.Stop()
	if u.Info != "Stopping after the files being converted now..." {
		t.Fatalf("info %q", u.Info)
	}
	h.pump(t, "stop", 120*time.Second, func() bool { return !u.Running() && u.ConvOn })
	if u.Info != "Stopped. Files not converted yet stay Pending." || u.StopOn {
		t.Fatalf("info %q stop=%v", u.Info, u.StopOn)
	}
	s := u.rowStates()
	for i, st := range s {
		if st != rsPending && !(i == 0 && st == rsDone) {
			t.Fatalf("states %v", s)
		}
	}
	for i, p := range ps[1:] {
		if _, err := os.Stat(rawconv.DstPath(p, false)); err == nil {
			t.Fatalf("file %d converted after Stop", i+1)
		}
	}
	ents, _ := os.ReadDir(d)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".part") || strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("partial file left: %s", e.Name())
		}
	}
	// Convert picks up where it stopped
	u.Convert()
	h.pump(t, "resume", 300*time.Second, func() bool { return !u.Running() && u.ConvOn })
	if u.Info != "Finished. 3 converted, 0 skipped (TIFF exists), 0 failed." {
		t.Fatalf("info %q", u.Info)
	}
}
