package cropctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"basetool/rawconv"
)

// RawModel is the file table (Value and RowCount are in copied.go).
type RawModel struct{ rows []*RawRow }

// RAWUI is the Batch RAW to TIFF page. The unexported fields are the
// Windows ones the copied methods use; the exported fields are what the
// walk widgets showed.
type RAWUI struct {
	h     Host
	model *RawModel
	cfg   RawConfig
	mu    sync.Mutex // guards row states, cfg reads from workers, running counters

	active  int // running workers
	cancel  context.CancelFunc
	stopReq bool

	watchStop chan struct{}
	sel       map[int]bool

	// Widgets (UI goroutine).
	Info, WatchLbl  string
	WatchText       string // "Start watching" / "Stop watching"
	ConvOn, StopOn  bool   // Convert to TIFF / Stop enabled
	PBMax, PBVal    int
	Seq             int
	WorkerItems     []string
	ICCNames        []string
	DNGPath, DNGLbl string

	// workers is the number of conversions started by start (tests).
	workers int
}

// NewRaw is Page(): settings, the worker count limit and the ICC list.
func NewRaw(h Host) *RAWUI {
	u := &RAWUI{h: h, model: &RawModel{}, cfg: loadRawConfig(), sel: map[int]bool{}, ConvOn: true,
		WatchText: "Start watching", WatchLbl: "Not watching.",
		Info: "Drop RAW files or a folder here, or use Add files. Output is a linear 16-bit TIFF, like open_make_tiff."}
	maxW := maxRawWorkers()
	if u.cfg.Workers > maxW {
		u.cfg.Workers = maxW
	}
	for i := 1; i <= maxW; i++ {
		u.WorkerItems = append(u.WorkerItems, strconv.Itoa(i))
	}
	for _, p := range rawconv.ICCProfiles {
		u.ICCNames = append(u.ICCNames, p.Name)
	}
	// rawconv.DNGConverterPath only knows the Windows install; on macOS
	// the converter is never found and the check box stays disabled.
	u.DNGPath = rawconv.DNGConverterPath()
	u.DNGLbl = "Not available on macOS"
	if u.DNGPath != "" {
		u.DNGLbl = "Installed"
	}
	u.updateProgress()
	return u
}

// Cfg is the current settings (UI goroutine).
func (u *RAWUI) Cfg() RawConfig {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.cfg
}

// ICCIndex is the combo index of the configured ICC profile.
func (u *RAWUI) ICCIndex() int {
	c := u.Cfg()
	for i, p := range rawconv.ICCProfiles {
		if p.Key == c.ICC {
			return i
		}
	}
	return 0
}

// SetOptions is the Output card's save(): dest (0 same folder,
// 1 make_tiff), comp (1 LZW), icc, workers (combo index) and DNG.
func (u *RAWUI) SetOptions(dest, comp, icc, workers int, dng bool) {
	u.mu.Lock()
	u.cfg.Subfolder = dest == 1
	u.cfg.LZW = comp == 1
	if icc >= 0 && icc < len(rawconv.ICCProfiles) {
		u.cfg.ICC = rawconv.ICCProfiles[icc].Key
	}
	if workers >= 0 {
		u.cfg.Workers = workers + 1
	}
	u.cfg.UseDNG = dng
	c := u.cfg
	u.mu.Unlock()
	saveRawConfig(c)
}

func (u *RAWUI) saveCfg() {
	u.mu.Lock()
	c := u.cfg
	u.mu.Unlock()
	saveRawConfig(c)
}

// AddFiles adds RAW files (folders are not opened here, see addFolder).
func (u *RAWUI) AddFiles(paths []string) int {
	u.mu.Lock()
	have := map[string]bool{}
	for _, r := range u.model.rows {
		have[strings.ToLower(r.Path)] = true
	}
	added := 0
	for _, p := range paths {
		if !rawconv.IsRaw(p) || have[strings.ToLower(p)] {
			continue
		}
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			continue
		}
		have[strings.ToLower(p)] = true
		u.model.rows = append(u.model.rows, &RawRow{Path: p, Name: filepath.Base(p), State: rsPending, Status: "Pending"})
		added++
	}
	n := len(u.model.rows)
	u.mu.Unlock()
	u.refresh()
	u.updateProgress()
	if added > 0 {
		u.Info = fmt.Sprintf("%d RAW file(s) added, %d in the list. Press Convert to TIFF.", added, n)
	} else if added == 0 && len(paths) > 0 {
		u.Info = "No new RAW files found (RAF, NEF, CR2, CR3, ARW, DNG, ORF, RW2, PEF ...)."
	}
	return added
}

func (u *RAWUI) refresh() { u.Seq++ }

// AddFilesDialog is "Add files...".
func (u *RAWUI) AddFilesDialog() {
	u.h.PickFiles("Select camera RAW files", "", []string{"raf", "nef", "nrw", "cr2", "cr3", "crw", "arw", "dng", "orf", "rw2", "pef", "srw", "3fr", "fff", "iiq"},
		func(ps []string, ok bool) {
			if ok {
				u.AddFiles(ps)
			}
		})
}

// AddFolderDialog is "Add folder...".
func (u *RAWUI) AddFolderDialog() {
	u.h.PickFolder("Select a folder with RAW files", "", func(p string, ok bool) {
		if ok {
			u.addFolder(p)
		}
	})
}

// AddFolder adds the RAW files of one folder (a dropped or chosen folder).
func (u *RAWUI) AddFolder(dir string) { u.addFolder(dir) }

// RemoveSelected, ClearFinished and Clear are the list buttons.
func (u *RAWUI) RemoveSelected() {
	del := u.sel
	u.mu.Lock()
	var keep []*RawRow
	for i, r := range u.model.rows {
		if !del[i] || r.State == rsWorking {
			keep = append(keep, r)
		}
	}
	u.model.rows = keep
	u.mu.Unlock()
	u.sel = map[int]bool{}
	u.refresh()
	u.updateProgress()
}

func (u *RAWUI) ClearFinished() {
	u.mu.Lock()
	var keep []*RawRow
	for _, r := range u.model.rows {
		if r.State == rsPending || r.State == rsWorking || r.State == rsError {
			keep = append(keep, r)
		}
	}
	u.model.rows = keep
	u.mu.Unlock()
	u.sel = map[int]bool{}
	u.refresh()
	u.updateProgress()
}

func (u *RAWUI) Clear() {
	u.mu.Lock()
	var keep []*RawRow
	for _, r := range u.model.rows {
		if r.State == rsWorking {
			keep = append(keep, r)
		}
	}
	u.model.rows = keep
	u.mu.Unlock()
	u.sel = map[int]bool{}
	u.refresh()
	u.updateProgress()
	u.Info = "Drop RAW files or a folder here, or use Add files."
}

// ---------- table ----------

// Rows, Cell, CellStyle, Selected and ClickRow are the table.
func (u *RAWUI) Rows() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.model.RowCount()
}

func (u *RAWUI) Cell(row, col int) string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if row >= len(u.model.rows) {
		return ""
	}
	s, _ := u.model.Value(row, col).(string)
	return s
}

// CellStyle is RawModel.StyleCell (CellNormal, CellError, CellDim, CellBusy).
func (u *RAWUI) CellStyle(row int) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	if row >= len(u.model.rows) {
		return CellNormal
	}
	switch u.model.rows[row].State {
	case rsError:
		return CellError
	case rsPending, rsSkipped:
		return CellDim
	case rsWorking:
		return CellBusy
	}
	return CellNormal
}

func (u *RAWUI) Selected(row int) bool { return u.sel[row] }

func (u *RAWUI) ClickRow(i int, extend, span bool, last int) {
	if i < 0 || i >= u.Rows() {
		return
	}
	switch {
	case span && last >= 0:
		u.sel = map[int]bool{}
		for k := min(last, i); k <= max(last, i); k++ {
			u.sel[k] = true
		}
	case extend:
		if u.sel[i] {
			delete(u.sel, i)
		} else {
			u.sel[i] = true
		}
	default:
		u.sel = map[int]bool{i: true}
	}
	u.Seq++
}

// ---------- converting ----------

func (u *RAWUI) updateProgress() {
	u.mu.Lock()
	total, fin := len(u.model.rows), 0
	for _, r := range u.model.rows {
		if r.State >= rsDone {
			fin++
		}
	}
	u.mu.Unlock()
	if total == 0 {
		total = 1
	}
	u.PBMax, u.PBVal = total, fin
}

// Convert is "Convert to TIFF".
func (u *RAWUI) Convert() {
	u.mu.Lock()
	n := 0
	for _, r := range u.model.rows {
		if r.State == rsPending {
			n++
		}
	}
	u.mu.Unlock()
	if n == 0 {
		u.Info = "Nothing pending. Add RAW files first (files already converted are not repeated)."
		return
	}
	u.start()
}

// RetryFailed is "Retry failed".
func (u *RAWUI) RetryFailed() {
	u.mu.Lock()
	for _, r := range u.model.rows {
		if r.State == rsError {
			r.State, r.Status, r.Secs = rsPending, "Pending", 0
		}
	}
	u.mu.Unlock()
	u.refresh()
	u.updateProgress()
}

// start launches workers if there is pending work (UI goroutine).
func (u *RAWUI) start() {
	u.mu.Lock()
	if u.active > 0 {
		u.mu.Unlock()
		return
	}
	pending := 0
	for _, r := range u.model.rows {
		if r.State == rsPending {
			pending++
		}
	}
	if pending == 0 {
		u.mu.Unlock()
		return
	}
	n := u.cfg.Workers
	if n < 1 {
		n = 1
	}
	if n > pending {
		n = pending
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.stopReq = false
	u.active = n
	u.workers = n
	u.mu.Unlock()

	u.ConvOn, u.StopOn = false, true
	u.Info = fmt.Sprintf("Converting %d file(s) with %d worker(s)...", pending, n)
	for i := 0; i < n; i++ {
		go u.worker(ctx)
	}
}

func (u *RAWUI) worker(ctx context.Context) {
	for {
		r, opts := u.next()
		if r == nil {
			break
		}
		u.h.Post(func() { u.refresh() })
		t0 := time.Now()
		res, err := rawconv.Convert(ctx, r.Path, opts)
		u.mu.Lock()
		r.Secs = time.Since(t0).Seconds()
		switch {
		case err != nil && ctx.Err() != nil:
			r.State, r.Status, r.Secs = rsPending, "Pending", 0
		case errors.Is(err, rawconv.ErrDstExists):
			r.State, r.Status = rsSkipped, "Skipped: "+filepath.Base(rawconv.DstPath(r.Path, opts.Subfolder))+" already exists"
		case err != nil:
			r.State, r.Status = rsError, "ERROR: "+err.Error()
		default:
			s := "Done: "
			if opts.Subfolder {
				s += "make_tiff/"
			}
			s += filepath.Base(res.Dst) + fmt.Sprintf("  (%dx%d", res.Width, res.Height)
			if opts.LZW {
				s += ", LZW"
			}
			if res.Path != "LibRaw" {
				s += ", via Adobe DNG Converter"
			}
			s += ")"
			if res.Note != "" {
				s += "  - " + res.Note
			}
			r.State, r.Status = rsDone, s
		}
		u.mu.Unlock()
		debug.FreeOSMemory()
		u.h.Post(func() {
			u.refresh()
			u.updateProgress()
		})
	}
	u.h.Post(func() {
		u.mu.Lock()
		u.active--
		last := u.active == 0
		stopped := u.stopReq
		u.mu.Unlock()
		if !last {
			return
		}
		u.ConvOn, u.StopOn = true, false
		if stopped {
			u.mu.Lock()
			for _, r := range u.model.rows {
				if r.State == rsWorking {
					r.State, r.Status = rsPending, "Pending"
				}
			}
			u.mu.Unlock()
			u.refresh()
			u.Info = "Stopped. Files not converted yet stay Pending."
			return
		}
		u.summary()
		// files may have arrived (watch folder) while the last worker was exiting
		u.start()
	})
}

func (u *RAWUI) summary() {
	u.mu.Lock()
	var d, s, e, p int
	for _, r := range u.model.rows {
		switch r.State {
		case rsDone:
			d++
		case rsSkipped:
			s++
		case rsError:
			e++
		case rsPending:
			p++
		}
	}
	u.mu.Unlock()
	msg := fmt.Sprintf("Finished. %d converted, %d skipped (TIFF exists), %d failed.", d, s, e)
	if p > 0 {
		msg += fmt.Sprintf(" %d pending.", p)
	}
	if u.watchStop != nil {
		msg += "  Watching folder for new RAW files..."
	}
	u.Info = msg
}

// Stop is "Stop": the files being converted are cancelled and stay Pending.
func (u *RAWUI) Stop() {
	u.mu.Lock()
	u.stopReq = true
	if u.cancel != nil {
		u.cancel()
	}
	u.mu.Unlock()
	u.Info = "Stopping after the files being converted now..."
}

// Running reports active workers.
func (u *RAWUI) Running() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.active > 0
}

// ---------- watch folder ----------

// Watching reports a running folder watch.
func (u *RAWUI) Watching() bool { return u.watchStop != nil }

// WatchDir is the folder shown in the watch card.
func (u *RAWUI) WatchDir() string { return u.Cfg().WatchDir }

// ChooseWatchDir is the watch card's "Choose...".
func (u *RAWUI) ChooseWatchDir() {
	u.h.PickFolder("Folder your tether software saves RAW files into", u.WatchDir(), func(p string, ok bool) {
		if !ok {
			return
		}
		if u.watchStop != nil {
			u.stopWatch()
		}
		u.mu.Lock()
		u.cfg.WatchDir = p
		u.mu.Unlock()
		u.saveCfg()
		u.Seq++
	})
}

// ToggleWatch is "Start watching" / "Stop watching".
func (u *RAWUI) ToggleWatch() {
	if u.watchStop != nil {
		u.stopWatch()
	} else {
		u.startWatch()
	}
}

// watchTick is the watch poll interval (tests shorten it).
var watchTick = time.Second

func (u *RAWUI) startWatch() {
	dir := u.WatchDir()
	if dir == "" {
		u.h.Message("Watch folder", "Choose the folder your tether software saves RAW files into first.")
		return
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		u.h.Message("Watch folder", "Folder not found:\n"+dir)
		return
	}
	seen := map[string]*fileSeen{}
	ents, _ := os.ReadDir(dir)
	existing := 0
	for _, e := range ents {
		if !e.IsDir() && rawconv.IsRaw(e.Name()) {
			seen[e.Name()] = &fileSeen{done: true}
			existing++
		}
	}
	stopCh := make(chan struct{})
	u.watchStop = stopCh
	u.WatchText = "Stop watching"
	u.WatchLbl = fmt.Sprintf("Watching. %d RAW file(s) already there are ignored.", existing)
	u.Info = "Watching " + dir + " for new RAW files..."
	go func() {
		tk := time.NewTicker(watchTick)
		defer tk.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-tk.C:
			}
			ents, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			var ready []string
			for _, e := range ents {
				if e.IsDir() || !rawconv.IsRaw(e.Name()) {
					continue
				}
				fi, err := e.Info()
				if err != nil {
					continue
				}
				s := seen[e.Name()]
				if s == nil {
					seen[e.Name()] = &fileSeen{size: fi.Size(), mod: fi.ModTime()}
					continue
				}
				if s.done {
					continue
				}
				if fi.Size() > 0 && fi.Size() == s.size && fi.ModTime().Equal(s.mod) {
					s.stable++
				} else {
					s.size, s.mod, s.stable = fi.Size(), fi.ModTime(), 0
				}
				if s.stable >= 2 { // unchanged for ~2 s: camera has finished writing
					p := filepath.Join(dir, e.Name())
					if f, err := os.Open(p); err == nil {
						f.Close()
						s.done = true
						ready = append(ready, p)
					}
				}
			}
			if len(ready) > 0 {
				sort.Strings(ready)
				u.h.Post(func() {
					if u.watchStop != stopCh {
						return
					}
					n := u.AddFiles(ready)
					u.WatchLbl = fmt.Sprintf("Watching. Last new file: %s (%s)", filepath.Base(ready[len(ready)-1]), time.Now().Format("15:04:05"))
					if n > 0 {
						u.start()
					}
				})
			}
		}
	}()
}

func (u *RAWUI) stopWatch() {
	if u.watchStop != nil {
		close(u.watchStop)
		u.watchStop = nil
	}
	u.WatchText = "Start watching"
	u.WatchLbl = "Not watching."
}

// Shutdown stops the watch and the conversions (window closed).
func (u *RAWUI) Shutdown() {
	if u.watchStop != nil {
		u.stopWatch()
	}
	u.mu.Lock()
	if u.cancel != nil {
		u.stopReq = true
		u.cancel()
	}
	u.mu.Unlock()
}
