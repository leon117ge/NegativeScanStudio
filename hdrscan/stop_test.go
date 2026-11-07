package hdrscan

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"basetool/rgblight"
)

// v0.83: Stop during a trichrome HDR merge removes every TIFF it wrote.
func TestTrichromeMergeStopRemovesPartialTIFFs(t *testing.T) {
	r := newTriRig(t, rgblight.AckNormal)
	r.lock(t)
	dir := t.TempDir()
	sw := &rgblight.ChannelSwitch{C: r.cl, Values: [3]int{40, 55, 70}}
	if _, err := sw.Snapshot(); err != nil {
		t.Fatal(err)
	}
	sh, err := r.sess.CaptureTrichrome(context.Background(), dir, []int{0, 2}, time.Millisecond, sw, func(time.Time) string { return "Roll_20261102_120000_0002" })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	po := ProcessOptions{Merge: DefaultOptions(), Software: "test", OutDir: dir, KeepTIFF: true, Ctx: ctx}
	po.Progress = func(m string) {
		if strings.HasPrefix(m, "B:") { // R and G HDRs are written by now
			cancel()
		}
	}
	res, err := ProcessTrichrome(sh.Raws, sh.Exposures, po)
	if !errors.Is(err, context.Canceled) || res != nil {
		t.Fatalf("got %v %v, want Canceled", res, err)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tiff")); len(m) != 0 {
		t.Fatalf("partial TIFFs left after Stop: %v", m)
	}
	// a stopped bracket capture shoots nothing
	ctx2, c2 := context.WithCancel(context.Background())
	c2()
	shot, err := r.sess.CaptureBracketEVCtx(ctx2, dir, []int{0, 2}, nil)
	if !errors.Is(err, context.Canceled) || len(shot.Raws) != 0 {
		t.Fatalf("stopped bracket: %v %v", shot, err)
	}
}
