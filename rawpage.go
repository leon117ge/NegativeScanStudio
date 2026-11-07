package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"basetool/rawconv"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// ---------- settings (separate file, like module 2) ----------

type RawConfig struct {
	Subfolder bool
	LZW       bool
	ICC       string
	UseDNG    bool
	Workers   int
	WatchDir  string
}

func rawConfigFile() string { return filepath.Join(configDir(), "raw_config.txt") }

func loadRawConfig() RawConfig {
	c := RawConfig{Workers: rawconv.DefaultWorkers()}
	b, err := os.ReadFile(rawConfigFile())
	if err != nil {
		return c
	}
	for _, ln := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
		k, v, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		switch k {
		case "SUBFOLDER":
			c.Subfolder = v == "1"
		case "LZW":
			c.LZW = v == "1"
		case "ICC":
			c.ICC = v
		case "USE_DNG":
			c.UseDNG = v == "1"
		case "WORKERS":
			if n, err := strconv.Atoi(v); err == nil && n >= 1 {
				c.Workers = n
			}
		case "WATCH_DIR":
			c.WatchDir = v
		}
	}
	return c
}

func saveRawConfig(c RawConfig) {
	os.MkdirAll(configDir(), 0755)
	b := func(x bool) string {
		if x {
			return "1"
		}
		return "0"
	}
	s := fmt.Sprintf("SUBFOLDER=%s\nLZW=%s\nICC=%s\nUSE_DNG=%s\nWORKERS=%d\nWATCH_DIR=%s\n",
		b(c.Subfolder), b(c.LZW), c.ICC, b(c.UseDNG), c.Workers, c.WatchDir)
	os.WriteFile(rawConfigFile(), []byte(s), 0644)
}

func maxRawWorkers() int {
	n := runtime.NumCPU() / 2
	if n < 1 {
		n = 1
	}
	return n
}

// ---------- table ----------

const (
	rsPending = iota
	rsWorking
	rsDone
	rsSkipped
	rsError
)

type RawRow struct {
	Path, Name string
	State      int
	Status     string
	Secs       float64
}

type RawModel struct {
	walk.TableModelBase
	rows []*RawRow
}

func (m *RawModel) StyleCell(style *walk.CellStyle) {
	if style.Row() >= len(m.rows) {
		return
	}
	switch m.rows[style.Row()].State {
	case rsError:
		style.TextColor = walk.RGB(240, 100, 86)
	case rsPending, rsSkipped:
		style.TextColor = walk.RGB(118, 118, 126)
	case rsWorking:
		style.TextColor = walk.RGB(240, 150, 70)
	}
}

func (m *RawModel) RowCount() int { return len(m.rows) }
func (m *RawModel) Value(row, col int) interface{} {
	r := m.rows[row]
	switch col {
	case 0:
		return r.Name
	case 1:
		return filepath.Dir(r.Path)
	case 2:
		return r.Status
	case 3:
		if r.Secs > 0 {
			return fmt.Sprintf("%.1f s", r.Secs)
		}
	}
	return ""
}

// ---------- UI ----------

type RAWUI struct {
	mw    **walk.MainWindow
	model *RawModel
	cfg   RawConfig
	mu    sync.Mutex // guards row states, cfg reads from workers, running counters

	tv                               *walk.TableView
	destCB, compCB, iccCB, workersCB *walk.ComboBox
	dngCB                            *walk.CheckBox
	dngLbl, info, watchLbl           *walk.Label
	watchEd                          *walk.LineEdit
	watchBtn, convBtn, stopBtn       *walk.PushButton
	pb                               *walk.ProgressBar

	active  int // running workers
	cancel  context.CancelFunc
	stopReq bool

	watchStop chan struct{}
}

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
	u.mu.Unlock()
	u.refresh()
	u.updateProgress()
	if added > 0 && u.info != nil {
		u.info.SetText(fmt.Sprintf("%d RAW file(s) added, %d in the list. Press Convert to TIFF.", added, len(u.model.rows)))
	} else if added == 0 && len(paths) > 0 && u.info != nil {
		u.info.SetText("No new RAW files found (RAF, NEF, CR2, CR3, ARW, DNG, ORF, RW2, PEF ...).")
	}
	return added
}

// refresh redraws the list. RowsReset alone does not repaint rows whose
// count did not change on every list-view implementation, so also ask for
// an explicit redraw of all rows.
func (u *RAWUI) refresh() {
	u.model.PublishRowsReset()
	if n := len(u.model.rows); n > 0 {
		u.model.PublishRowsChanged(0, n-1)
	}
	if u.tv != nil {
		u.tv.Invalidate()
	}
}

func (u *RAWUI) addFolder(dir string) {
	ents, _ := os.ReadDir(dir)
	var all []string
	for _, e := range ents {
		all = append(all, filepath.Join(dir, e.Name()))
	}
	sort.Strings(all)
	u.AddFiles(all)
}

func (u *RAWUI) options() rawconv.Options {
	return rawconv.Options{
		Subfolder: u.cfg.Subfolder,
		LZW:       u.cfg.LZW,
		ICC:       u.cfg.ICC,
		UseDNG:    u.cfg.UseDNG && rawconv.DNGConverterPath() != "",
		Software:  appVersion,
	}
}

func (u *RAWUI) updateProgress() {
	if u.pb == nil {
		return
	}
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
	u.pb.SetRange(0, total)
	u.pb.SetValue(fin)
}

// start launches workers if there is pending work. Must run on the UI thread.
func (u *RAWUI) start() {
	mw := *u.mw
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
	u.mu.Unlock()

	u.convBtn.SetEnabled(false)
	u.stopBtn.SetEnabled(true)
	u.info.SetText(fmt.Sprintf("Converting %d file(s) with %d worker(s)...", pending, n))
	for i := 0; i < n; i++ {
		go u.worker(ctx, mw)
	}
}

func (u *RAWUI) next() (*RawRow, rawconv.Options) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.stopReq {
		return nil, rawconv.Options{}
	}
	for _, r := range u.model.rows {
		if r.State == rsPending {
			r.State = rsWorking
			r.Status = "Converting..."
			return r, u.options()
		}
	}
	return nil, rawconv.Options{}
}

func (u *RAWUI) worker(ctx context.Context, mw *walk.MainWindow) {
	for {
		r, opts := u.next()
		if r == nil {
			break
		}
		mw.Synchronize(func() { u.refresh() })
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
				s += "make_tiff\\"
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
		mw.Synchronize(func() {
			u.refresh()
			u.updateProgress()
		})
	}
	mw.Synchronize(func() {
		u.mu.Lock()
		u.active--
		last := u.active == 0
		stopped := u.stopReq
		u.mu.Unlock()
		if !last {
			return
		}
		u.convBtn.SetEnabled(true)
		u.stopBtn.SetEnabled(false)
		if stopped {
			u.mu.Lock()
			for _, r := range u.model.rows {
				if r.State == rsWorking {
					r.State, r.Status = rsPending, "Pending"
				}
			}
			u.mu.Unlock()
			u.refresh()
			u.info.SetText("Stopped. Files not converted yet stay Pending.")
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
	u.info.SetText(msg)
}

func (u *RAWUI) stop() {
	u.mu.Lock()
	u.stopReq = true
	if u.cancel != nil {
		u.cancel()
	}
	u.mu.Unlock()
	u.info.SetText("Stopping after the files being converted now...")
}

// ---------- watch folder ----------

type fileSeen struct {
	size   int64
	mod    time.Time
	stable int
	done   bool
}

func (u *RAWUI) startWatch() {
	mw := *u.mw
	dir := u.cfg.WatchDir
	if dir == "" {
		walk.MsgBox(mw, "Watch folder", "Choose the folder your tether software saves RAW files into first.", walk.MsgBoxIconInformation)
		return
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		walk.MsgBox(mw, "Watch folder", "Folder not found:\n"+dir, walk.MsgBoxIconWarning)
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
	u.watchBtn.SetText("Stop watching")
	u.watchLbl.SetText(fmt.Sprintf("Watching. %d RAW file(s) already there are ignored.", existing))
	u.info.SetText("Watching " + dir + " for new RAW files...")
	go func() {
		tk := time.NewTicker(time.Second)
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
				mw.Synchronize(func() {
					if u.watchStop != stopCh {
						return
					}
					n := u.AddFiles(ready)
					u.watchLbl.SetText(fmt.Sprintf("Watching. Last new file: %s (%s)", filepath.Base(ready[len(ready)-1]), time.Now().Format("15:04:05")))
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
	u.watchBtn.SetText("Start watching")
	u.watchLbl.SetText("Not watching.")
}

// ---------- page ----------

func (u *RAWUI) Page() TabPage {
	mw := func() *walk.MainWindow { return *u.mw }
	u.model = &RawModel{}
	u.cfg = loadRawConfig()
	maxW := maxRawWorkers()
	if u.cfg.Workers > maxW {
		u.cfg.Workers = maxW
	}
	var workerItems []string
	for i := 1; i <= maxW; i++ {
		workerItems = append(workerItems, strconv.Itoa(i))
	}
	var iccNames []string
	iccIdx := 0
	for i, p := range rawconv.ICCProfiles {
		iccNames = append(iccNames, p.Name)
		if p.Key == u.cfg.ICC {
			iccIdx = i
		}
	}
	dngPath := rawconv.DNGConverterPath()
	dngText := "Not installed (" + rawconv.DefaultDNGConverter + ")"
	if dngPath != "" {
		dngText = "Installed"
	}
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	save := func() {
		u.mu.Lock()
		if u.destCB != nil && u.compCB != nil && u.iccCB != nil && u.workersCB != nil && u.dngCB != nil {
			u.cfg.Subfolder = u.destCB.CurrentIndex() == 1
			u.cfg.LZW = u.compCB.CurrentIndex() == 1
			if i := u.iccCB.CurrentIndex(); i >= 0 && i < len(rawconv.ICCProfiles) {
				u.cfg.ICC = rawconv.ICCProfiles[i].Key
			}
			if i := u.workersCB.CurrentIndex(); i >= 0 {
				u.cfg.Workers = i + 1
			}
			u.cfg.UseDNG = u.dngCB.Checked()
		}
		c := u.cfg
		u.mu.Unlock()
		saveRawConfig(c)
	}
	return TabPage{
		Title:  "  3. RAW to TIFF  ",
		Layout: VBox{Margins: Margins{Left: 10, Top: 10, Right: 10, Bottom: 10}, Spacing: 8},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					PushButton{Text: "Add files...", OnClicked: func() {
						dlg := new(walk.FileDialog)
						dlg.Title = "Select camera RAW files"
						dlg.Filter = "Camera RAW (*.raf;*.nef;*.cr2;*.cr3;*.arw;*.dng;*.orf;*.rw2;*.pef)|*.raf;*.nef;*.nrw;*.cr2;*.cr3;*.crw;*.arw;*.dng;*.orf;*.rw2;*.pef;*.srw;*.3fr;*.fff;*.iiq|All files (*.*)|*.*"
						if ok, _ := dlg.ShowOpenMultiple(mw()); ok {
							u.AddFiles(dlg.FilePaths)
						}
					}},
					PushButton{Text: "Add folder...", OnClicked: func() {
						dlg := new(walk.FileDialog)
						dlg.Title = "Select a folder with RAW files"
						if ok, _ := dlg.ShowBrowseFolder(mw()); ok {
							u.addFolder(dlg.FilePath)
						}
					}},
					PushButton{Text: "Remove selected", OnClicked: func() {
						del := map[int]bool{}
						for _, i := range u.tv.SelectedIndexes() {
							del[i] = true
						}
						u.mu.Lock()
						var keep []*RawRow
						for i, r := range u.model.rows {
							if !del[i] || r.State == rsWorking {
								keep = append(keep, r)
							}
						}
						u.model.rows = keep
						u.mu.Unlock()
						u.refresh()
						u.updateProgress()
					}},
					PushButton{Text: "Clear finished", OnClicked: func() {
						u.mu.Lock()
						var keep []*RawRow
						for _, r := range u.model.rows {
							if r.State == rsPending || r.State == rsWorking || r.State == rsError {
								keep = append(keep, r)
							}
						}
						u.model.rows = keep
						u.mu.Unlock()
						u.refresh()
						u.updateProgress()
					}},
					PushButton{Text: "Clear", OnClicked: func() {
						u.mu.Lock()
						var keep []*RawRow
						for _, r := range u.model.rows {
							if r.State == rsWorking {
								keep = append(keep, r)
							}
						}
						u.model.rows = keep
						u.mu.Unlock()
						u.refresh()
						u.updateProgress()
						u.info.SetText("Drop RAW files or a folder here, or use Add files.")
					}},
					HSpacer{},
				},
			},
			TableView{
				AssignTo:         &u.tv,
				AlternatingRowBG: true,
				ColumnsOrderable: false,
				MultiSelection:   true,
				Model:            u.model,
				StretchFactor:    1,
				MinSize:          Size{Height: 120},
				Columns: []TableViewColumn{
					{Title: "File", Width: 170},
					{Title: "Folder", Width: 260},
					{Title: "Status", Width: 440},
					{Title: "Time", Width: 70},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 8, Alignment: AlignHNearVNear},
				Children: []Widget{
					Card{
						Title:         "Output",
						StretchFactor: 1,
						Layout:        Grid{Columns: 2, Spacing: 8, Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}},
						Children: []Widget{
							Label{Text: "Save TIFF to"},
							ComboBox{AssignTo: &u.destCB, Model: []string{"Same folder as the RAW file", "\"make_tiff\" subfolder"}, CurrentIndex: b2i(u.cfg.Subfolder), MaxSize: Size{Width: 260}, OnCurrentIndexChanged: save},
							Label{Text: "Compression"},
							ComboBox{AssignTo: &u.compCB, Model: []string{"None (fastest)", "LZW + predictor (lossless)"}, CurrentIndex: b2i(u.cfg.LZW), MaxSize: Size{Width: 260}, OnCurrentIndexChanged: save},
							Label{Text: "Embedded ICC profile"},
							ComboBox{AssignTo: &u.iccCB, Model: iccNames, CurrentIndex: iccIdx, MaxSize: Size{Width: 260}, OnCurrentIndexChanged: save,
								ToolTipText: "Only a tag in the file, the pixels are not changed. open_make_tiff default: none."},
							Label{Text: "Files in parallel"},
							ComboBox{AssignTo: &u.workersCB, Model: workerItems, CurrentIndex: u.cfg.Workers - 1, MaxSize: Size{Width: 70}, OnCurrentIndexChanged: save,
								ToolTipText: "Each file needs about 0.5 GB of memory while converting."},
							Label{Text: "Adobe DNG Converter"},
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								CheckBox{AssignTo: &u.dngCB, Text: "Decode through it", Checked: u.cfg.UseDNG && dngPath != "", Enabled: dngPath != "", OnCheckedChanged: save,
									ToolTipText: "RAW > DNG > linear DNG with Adobe's demosaic, then the same LibRaw settings. Falls back to LibRaw if it fails."},
								Label{AssignTo: &u.dngLbl, Text: dngText, TextColor: walk.RGB(130, 130, 130)},
								HSpacer{},
							}},
						},
					},
					Card{
						Title:         "Watch folder (tethered shooting)",
						Icon:          iconCamera,
						StretchFactor: 1,
						Layout:        VBox{Spacing: 6, Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}},
						Children: []Widget{
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								LineEdit{AssignTo: &u.watchEd, ReadOnly: true, Text: u.cfg.WatchDir},
								PushButton{Text: "Choose...", OnClicked: func() {
									dlg := new(walk.FileDialog)
									dlg.Title = "Folder your tether software saves RAW files into"
									dlg.FilePath = u.cfg.WatchDir
									if ok, _ := dlg.ShowBrowseFolder(mw()); ok {
										if u.watchStop != nil {
											u.stopWatch()
										}
										u.mu.Lock()
										u.cfg.WatchDir = dlg.FilePath
										u.mu.Unlock()
										u.watchEd.SetText(dlg.FilePath)
										save()
									}
								}},
							}},
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								PushButton{AssignTo: &u.watchBtn, Text: "Start watching", MinSize: Size{Width: 120}, OnClicked: func() {
									if u.watchStop != nil {
										u.stopWatch()
									} else {
										u.startWatch()
									}
								}},
								Label{AssignTo: &u.watchLbl, Text: "Not watching."},
								HSpacer{},
							}},
							Label{Text: "New RAW files saved into this folder (for example by FUJIFILM X Acquire\nor Tether App) are converted automatically with the settings on the left,\nonce the camera has finished writing them. Files already there are ignored."},
						},
					},
				},
			},
			Label{Text: "Fixed processing (same as open_make_tiff): LibRaw " + rawconv.LibRawVersion() + ", camera native RGB (no colour matrix), no white balance (1,1,1,1), gamma 1.0, no auto brightness,\nhighlights unclipped, AHD / X-Trans demosaic, no rotation, 16-bit. Output: <RAW name>.tiff, an existing TIFF is never overwritten.", TextColor: walk.RGB(110, 110, 110)},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					PushButton{AssignTo: &u.convBtn, Text: "Convert to TIFF", MinSize: Size{Width: 220, Height: 34}, Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}, OnClicked: func() {
						u.mu.Lock()
						n := 0
						for _, r := range u.model.rows {
							if r.State == rsPending {
								n++
							}
						}
						u.mu.Unlock()
						if n == 0 {
							u.info.SetText("Nothing pending. Add RAW files first (files already converted are not repeated).")
							return
						}
						u.start()
					}},
					PushButton{AssignTo: &u.stopBtn, Text: "Stop", Enabled: false, MinSize: Size{Height: 34}, OnClicked: u.stop},
					PushButton{Text: "Retry failed", MinSize: Size{Height: 34}, OnClicked: func() {
						u.mu.Lock()
						for _, r := range u.model.rows {
							if r.State == rsError {
								r.State, r.Status, r.Secs = rsPending, "Pending", 0
							}
						}
						u.mu.Unlock()
						u.refresh()
						u.updateProgress()
					}},
					HSpacer{},
				},
			},
			ProgressBar{AssignTo: &u.pb},
			Label{AssignTo: &u.info, Text: "Drop RAW files or a folder here, or use Add files. Output is a linear 16-bit TIFF, like open_make_tiff."},
		},
	}
}
