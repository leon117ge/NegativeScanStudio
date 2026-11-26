package hdrctl

// Crop & Merge > Merge files (the merge TabPage of hdrpage.go, mergeFiles,
// DropFiles and openInDevelop on Windows).

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"basetool/hdrscan"

	"basetool/macapp/appstate"
)

// MergeFiles is the state of the Merge files page (UI goroutine).
type MergeFiles struct {
	// Frame 0, brighter frame, optional +3/+4 frame (the three LineEdits).
	Paths [3]string
	// PathSeq bumps when the page sets Paths (Browse..., dropped files).
	PathSeq int
	Align   int // alignItems, default 0
	Merge   int // mergeItems, default mergeDefault
	Comp    int // 0 none, 1 LZW (default)
	Dust    int // dustItems, default the Camera page's setting
}

// MergeResText is the Merge files result box before the first merge.
const MergeResText = "Output: <frame 0 name>_HDR.tiff next to frame 0 (a trailing _0EV is dropped; _2, _3 ... if that name exists). Always one file per frame: with Dust removal it is the cleaned merge (no uncleaned copy), linear 16-bit. The source files are never changed or deleted here."

func (c *Ctl) initMerge() {
	c.MF = MergeFiles{Merge: appstate.MergeDefault, Comp: 1, Dust: c.Cfg.Dust}
	c.MRes, c.MResSeq = MergeResText, 1
}

// MergeBusy reports a merge in progress (the Merge button is disabled,
// as Windows' mBtn while processing).
func (c *Ctl) MergeBusy() bool { return c.Working }

// MergeStoppable reports whether Stop applies (a Merge files merge runs).
func (c *Ctl) MergeStoppable() bool { return c.mergeCancel != nil }

// CanOpenMerge is the Open in Develop button's enabled state.
func (c *Ctl) CanOpenMerge() bool { return c.LastMerge != nil && c.LastMerge.HDR != "" }

// BrowseMerge is a frame's "Browse..." (k = 0, 1, 2).
func (c *Ctl) BrowseMerge(k int) {
	titles := []string{"Frame 0 (clear base metered)", "Frame shot 2 EV brighter", "Frame shot 3 or 4 EV brighter (optional)"}
	c.Host.PickFile(titles[k], false, c.MF.Paths[k], []string{"raf", "tif", "tiff", "dng", "nef", "cr2", "cr3", "arw"}, func(p string, ok bool) {
		if ok {
			c.MF.Paths[k] = p
			c.MF.PathSeq++
		}
	})
}

// DropMergeFiles fills the manual merge fields from dropped or picked
// files (DropFiles on Windows).
func (c *Ctl) DropMergeFiles(files []string) {
	var ok []string
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		if ext == ".raf" || ext == ".tif" || ext == ".tiff" || ext == ".dng" || ext == ".nef" || ext == ".cr3" || ext == ".arw" {
			ok = append(ok, f)
		}
	}
	sort.Strings(ok)
	if len(ok) >= 2 {
		for i := range c.MF.Paths {
			if i < len(ok) {
				c.MF.Paths[i] = ok[i]
			} else {
				c.MF.Paths[i] = ""
			}
		}
		c.MF.PathSeq++
	} else if len(ok) == 1 {
		for i := range c.MF.Paths {
			if c.MF.Paths[i] == "" {
				c.MF.Paths[i] = ok[0]
				c.MF.PathSeq++
				break
			}
		}
	}
}

// RunMerge is "Merge to HDR TIFF" (mergeFiles on Windows). It runs on a
// worker goroutine; StopMerge cancels it between stages.
func (c *Ctl) RunMerge() {
	if c.Working {
		return
	}
	srcs := []string{strings.TrimSpace(c.MF.Paths[0]), strings.TrimSpace(c.MF.Paths[1])}
	if f4 := strings.TrimSpace(c.MF.Paths[2]); f4 != "" {
		srcs = append(srcs, f4)
	}
	for i, f := range srcs {
		if _, err := os.Stat(f); err != nil {
			msg := "Choose frame 0 and a brighter frame (+2 or +3 EV) first; the third file is optional."
			if i == 2 {
				msg = "The third file does not exist: " + f + "\nClear the field to merge two files."
			}
			c.Host.Message("Merge files", msg)
			return
		}
		for _, g := range srcs[:i] {
			if strings.EqualFold(f, g) {
				c.Host.Message("Merge files", "The same file is chosen twice: "+filepath.Base(f))
				return
			}
		}
	}
	c.process(srcs, c.mergeOptions(), true)
}

// mergeOptions are the ProcessOptions of mergeFiles plus the macOS Stop.
func (c *Ctl) mergeOptions() hdrscan.ProcessOptions {
	o := MergeFilesOptions(c.MF)
	ctx, cancel := context.WithCancel(context.Background())
	c.mergeCancel = cancel
	o.Ctx = ctx
	return o
}

// MergeFilesOptions is mergeFiles' hdrscan.ProcessOptions for the page
// state m (no Ctx: Windows has no Stop on this page).
func MergeFilesOptions(m MergeFiles) hdrscan.ProcessOptions {
	mo := hdrscan.DefaultOptions()
	mo.Align = appstate.AlignModes[max(0, m.Align)]
	mo.Method = appstate.MergeModes[max(0, m.Merge)]
	return hdrscan.ProcessOptions{Merge: mo, LZW: m.Comp == 1, Software: appstate.AppVersion, LeakPct: 20, Dust: max(0, m.Dust)}
}

// StopMerge cancels the running Merge files merge at its next stage.
func (c *Ctl) StopMerge() {
	if c.mergeCancel != nil {
		c.mergeCancel()
		c.MRes = "Stopping the merge ..."
		c.MResSeq++
	}
}

// OpenMergeInDevelop is "Open in Develop" (openInDevelop(u.lastMerge)).
func (c *Ctl) OpenMergeInDevelop() {
	r := c.LastMerge
	if r == nil || r.HDR == "" || c.OpenInDevelop == nil {
		return
	}
	if _, err := os.Stat(r.HDR); err != nil {
		c.Host.Message("Open in Develop", "The HDR TIFF is gone: "+r.HDR)
		return
	}
	c.OpenInDevelop(r.HDR)
}
