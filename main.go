package main

import (
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"

	"basetool/basestat"
	"basetool/develop"
	"basetool/filmdb"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type Row struct {
	Path      string
	Name      string
	R, G, B   float64
	OK        bool
	Flag      bool
	Status    string
	Edges     [4][3]float64
	Rejected  float64
	EdgeValid [4]bool
	// Region is set for a base sent from the HDR scan module: the row is
	// measured over that base region instead of the edge bands.
	Region *basestat.Region
}

type Model struct {
	walk.TableModelBase
	rows []*Row
}

func (m *Model) StyleCell(style *walk.CellStyle) {
	r := m.rows[style.Row()]
	st := strings.ToUpper(r.Status)
	if strings.Contains(st, "CHECK") || strings.Contains(st, "ERROR") {
		style.TextColor = walk.RGB(240, 100, 86)
	} else if st == "PENDING" {
		style.TextColor = walk.RGB(118, 118, 126)
	}
}

func (m *Model) RowCount() int { return len(m.rows) }
func (m *Model) Value(row, col int) interface{} {
	r := m.rows[row]
	switch col {
	case 0:
		return r.Name
	case 1, 2, 3, 4, 5:
		if !r.OK {
			return ""
		}
		switch col {
		case 1:
			return fmt.Sprintf("%.4f", r.R)
		case 2:
			return fmt.Sprintf("%.4f", r.G)
		case 3:
			return fmt.Sprintf("%.4f", r.B)
		case 4:
			return fmt.Sprintf("%.3f", r.R/r.G)
		case 5:
			return fmt.Sprintf("%.3f", r.B/r.G)
		}
	case 6:
		return r.Status
	}
	return ""
}

func median(v []float64) float64 { return basestat.Median(v) }

// pixel accessor returning 0..1 floats
func getter(img image.Image) (func(x, y int) (float64, float64, float64), error) {
	switch m := img.(type) {
	case *image.RGBA64:
		return func(x, y int) (float64, float64, float64) {
			i := m.PixOffset(x, y)
			p := m.Pix
			return float64(uint16(p[i])<<8|uint16(p[i+1])) / 65535, float64(uint16(p[i+2])<<8|uint16(p[i+3])) / 65535, float64(uint16(p[i+4])<<8|uint16(p[i+5])) / 65535
		}, nil
	case *image.NRGBA64:
		return func(x, y int) (float64, float64, float64) {
			i := m.PixOffset(x, y)
			p := m.Pix
			return float64(uint16(p[i])<<8|uint16(p[i+1])) / 65535, float64(uint16(p[i+2])<<8|uint16(p[i+3])) / 65535, float64(uint16(p[i+4])<<8|uint16(p[i+5])) / 65535
		}, nil
	case *image.RGBA:
		return func(x, y int) (float64, float64, float64) {
			i := m.PixOffset(x, y)
			p := m.Pix
			return float64(p[i]) / 255, float64(p[i+1]) / 255, float64(p[i+2]) / 255
		}, nil
	case *image.NRGBA:
		return func(x, y int) (float64, float64, float64) {
			i := m.PixOffset(x, y)
			p := m.Pix
			return float64(p[i]) / 255, float64(p[i+1]) / 255, float64(p[i+2]) / 255
		}, nil
	}
	b := img.Bounds()
	_ = b
	return func(x, y int) (float64, float64, float64) {
		r, g, bb, _ := img.At(x, y).RGBA()
		return float64(r) / 65535, float64(g) / 65535, float64(bb) / 65535
	}, nil
}

var _ = color.RGBA{}

// measureRow measures a row: over its base region when it has one (sent
// from the HDR scan module), else over the edge bands.
func measureRow(r *Row, startPct, endPct, leakPct float64) (*Row, error) {
	if r.Region == nil {
		return measure(r.Path, startPct, endPct, leakPct)
	}
	return measureRegion(r.Path, r.Name, r.Region, leakPct)
}

// measureRegion applies the same statistic (value/65535, leak rejection,
// per-channel median) to a base region.
func measureRegion(path, name string, reg *basestat.Region, leakPct float64) (*Row, error) {
	img, err := decodeImage(path)
	if err != nil {
		return nil, err
	}
	get, _ := getter(img)
	b := img.Bounds()
	rgb, rej, n, err := basestat.Measure(func(x, y int) (float64, float64, float64) { return get(b.Min.X+x, b.Min.Y+y) },
		b.Dx(), b.Dy(), *reg, leakPct)
	if err != nil {
		return nil, err
	}
	row := &Row{Path: path, Name: name, Region: reg, OK: true, Rejected: rej}
	row.R, row.G, row.B = rgb[0], rgb[1], rgb[2]
	row.Status = fmt.Sprintf("OK (base region from HDR scan, %d samples, %.0f%% leak/specks removed)", n, rej*100)
	return row, nil
}

// measure one image. start/end are percentages of the image size measured from each edge.
func measure(path string, startPct, endPct, leakPct float64) (*Row, error) {
	img, err := decodeImage(path)
	if err != nil {
		return nil, err
	}
	get, _ := getter(img)
	b := img.Bounds()
	W, H := b.Dx(), b.Dy()
	x0, y0 := b.Min.X, b.Min.Y
	bandA := func(total int) int { return int(float64(total) * startPct / 100.0) }
	bandB := func(total int) int {
		v := int(float64(total) * endPct / 100.0)
		return v
	}
	cx := int(float64(W) * 0.03) // keep clear of corners
	cy := int(float64(H) * 0.03)
	row := &Row{Path: path, Name: filepath.Base(path)}
	type rect struct{ xa, xb, ya, yb int } // [xa,xb) x [ya,yb)
	rects := [4]rect{
		{cx, W - cx, bandA(H), bandB(H)},         // top
		{cx, W - cx, H - bandB(H), H - bandA(H)}, // bottom
		{bandA(W), bandB(W), cy, H - cy},         // left
		{W - bandB(W), W - bandA(W), cy, H - cy}, // right
	}
	totalRej, nEdges := 0.0, 0
	for e, rc := range rects {
		if rc.xb-rc.xa < 1 || rc.yb-rc.ya < 1 {
			continue
		}
		var vr, vg, vb []float64
		stepX, stepY := 1, 1
		n := (rc.xb - rc.xa) * (rc.yb - rc.ya)
		if n > 400000 {
			s := n / 400000
			if (rc.xb - rc.xa) > (rc.yb - rc.ya) {
				stepX = s + 1
			} else {
				stepY = s + 1
			}
		}
		for y := rc.ya; y < rc.yb; y += stepY {
			for x := rc.xa; x < rc.xb; x += stepX {
				r, g, bl := get(x0+x, y0+y)
				vr = append(vr, r)
				vg = append(vg, g)
				vb = append(vb, bl)
			}
		}
		// Reject light leak (sprocket holes, bright specks) and dark specks
		// (shared with the HDR scan module's "Send base to Film base").
		med, frac, ok := basestat.CleanMedian(vr, vg, vb, leakPct)
		totalRej += frac
		nEdges++
		if !ok {
			continue // too little clean base on this edge: ignore the edge
		}
		row.Edges[e] = med
		row.EdgeValid[e] = true
	}
	var er, eg, eb []float64
	for e := 0; e < 4; e++ {
		if row.EdgeValid[e] {
			er = append(er, row.Edges[e][0])
			eg = append(eg, row.Edges[e][1])
			eb = append(eb, row.Edges[e][2])
		}
	}
	if len(er) == 0 {
		return nil, fmt.Errorf("no clean base found on any edge")
	}
	if nEdges > 0 {
		row.Rejected = totalRej / float64(nEdges)
	}
	row.R, row.G, row.B = median(er), median(eg), median(eb)
	row.OK = true
	row.Status = fmt.Sprintf("OK (%.0f%% leak/specks removed, %d/4 edges)", row.Rejected*100, len(er))
	return row, nil
}

type Summary struct {
	R, G, B          float64
	N, Used, Flagged int
}

func summarize(rows []*Row, excludeFlagged bool, tolPct float64) Summary {
	var rr, gg, bb []float64
	for _, r := range rows {
		if r.OK {
			rr = append(rr, r.R)
			gg = append(gg, r.G)
			bb = append(bb, r.B)
		}
	}
	s := Summary{N: len(rr)}
	if len(rr) == 0 {
		return s
	}
	mr, mg, mb := median(append([]float64{}, rr...)), median(append([]float64{}, gg...)), median(append([]float64{}, bb...))
	var ur, ug, ub []float64
	for _, r := range rows {
		if !r.OK {
			continue
		}
		dev := func(a, m float64) bool { return abs(a/m-1)*100 > tolPct }
		r.Flag = dev(r.R, mr) || dev(r.G, mg) || dev(r.B, mb)
		if r.Flag {
			worst := math.Max(abs(r.R/mr-1), math.Max(abs(r.G/mg-1), abs(r.B/mb-1))) * 100
			r.Status = fmt.Sprintf("CHECK: %.0f%% off the roll median", worst)
			if excludeFlagged {
				r.Status += " (left out)"
			}
			s.Flagged++
		}
		if r.Flag && excludeFlagged {
			continue
		}
		ur = append(ur, r.R)
		ug = append(ug, r.G)
		ub = append(ub, r.B)
	}
	if len(ur) == 0 {
		ur, ug, ub = rr, gg, bb
	}
	s.Used = len(ur)
	s.R, s.G, s.B = median(ur), median(ug), median(ub)
	return s
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func main() {
	migrated := migrateLegacyConfig() // before anything reads settings or writes crash_log
	installCrashLogger()
	defer mainCrash()
	debug.SetGCPercent(20)
	// "NegativeScanStudio.exe --dust light|strong <tiff> [...]" cleans TIFFs without the window
	if len(os.Args) > 3 && os.Args[1] == "--dust" {
		os.Exit(dustCLI(os.Args[2], os.Args[3:]))
	}
	model := &Model{}
	var mw *walk.MainWindow
	var tv *walk.TableView
	var startEd, endEd, tolEd, leakEd *walk.LineEdit
	var exclCB *walk.CheckBox
	var pb *walk.ProgressBar
	var resR, resG, resB *walk.LineEdit
	var info *walk.Label
	var runBtn *walk.PushButton
	var last Summary
	cfg := loadConfig()
	pmodel := &PresetModel{items: cfg.Presets}
	var ptv *walk.TableView
	var nameEd *walk.LineEdit
	var autoCB *walk.CheckBox
	var dctlLbl *walk.Label
	persist := func() {
		cfg.Presets = pmodel.items
		saveConfig(cfg)
	}
	syncDCTL := func() string {
		if cfg.DCTLPath == "" || !cfg.AutoSync {
			return ""
		}
		if err := writeDensityDCTL(cfg.DCTLPath, pmodel.items, cfg.CurrentBase); err != nil {
			return "Could not update " + cfg.DCTLPath + ": " + err.Error()
		}
		if err := writeMaskDCTL(cfg.DCTLPath, calibratedStocks()); err != nil {
			return "DCTL file updated. " + maskDCTLName + ": " + err.Error()
		}
		return "DCTL file updated."
	}
	refreshPath := func() {
		if cfg.DCTLPath == "" {
			dctlLbl.SetText("DCTL file: not exported yet")
		} else {
			dctlLbl.SetText("DCTL file: " + cfg.DCTLPath)
		}
	}
	exportDCTL := func(forceDialog bool) {
		p := cfg.DCTLPath
		if p == "" || forceDialog {
			dlg := new(walk.FileDialog)
			dlg.Title = "Save FilmScan_Density_In.dctl (put it in your Resolve DCTL folder)"
			dlg.Filter = "DCTL (*.dctl)|*.dctl"
			dlg.FilePath = "FilmScan_Density_In.dctl"
			if ok, _ := dlg.ShowSave(mw); !ok {
				return
			}
			p = dlg.FilePath
			if !strings.HasSuffix(strings.ToLower(p), ".dctl") {
				p += ".dctl"
			}
		}
		if err := writeDensityDCTL(p, pmodel.items, cfg.CurrentBase); err != nil {
			walk.MsgBox(mw, "Export DCTL", "Could not write the file:\n"+err.Error(), walk.MsgBoxIconError)
			return
		}
		cfg.DCTLPath = p
		persist()
		refreshPath()
		extra := " (+ " + maskDCTLName + " in the same folder)"
		if err := writeMaskDCTL(p, calibratedStocks()); err != nil {
			extra = " - " + maskDCTLName + ": " + err.Error()
		}
		info.SetText(fmt.Sprintf("DCTL exported with %d preset(s): %s", len(pmodel.items), p) + extra)
	}
	running := false
	// v0.73: main pages DCTL, HDR scan, Crop, Develop; DCTL has the sub-pages
	// Film base, White balance and Mask & Crosstalk.
	// v0.80: fifth page Light (RGB Light Air control and film-base leveling)
	// v0.83: five modules. Camera & Light (Camera, Light), HDR Scan (White
	// light scan, Trichrome) and Crop & Merge (Crop, Merge files, Remove
	// hairs, Batch RAW to TIFF) have sub-pages; one state backs them all.
	tabs := newPager(5)
	pageBar := newNavBar(tabs, false,
		navItem{"DCTL", iconFilm}, navItem{"Camera & Light", iconLight}, navItem{"HDR Scan", iconHDR}, navItem{"Crop & Merge", iconCrop}, navItem{"Develop", iconDevelop})
	camTabs := newPager(2)
	camSeg := newNavBar(camTabs, true, navItem{label: "Camera"}, navItem{label: "Light"})
	scanTabs := newPager(2)
	scanSeg := newNavBar(scanTabs, true, navItem{label: "White light scan"}, navItem{label: "Trichrome"})
	cropTabs := newPager(4)
	cropSeg := newNavBar(cropTabs, true, navItem{label: "Crop"}, navItem{label: "Merge files"}, navItem{label: "Remove hairs"}, navItem{label: "Batch RAW to TIFF"})
	module := func(title string, seg *navBar, pg *pager, pages ...TabPage) TabPage {
		return TabPage{Title: title, Layout: VBox{Margins: Margins{Left: 4, Top: 2, Right: 4, Bottom: 4}, Spacing: 2},
			Children: []Widget{seg.Widget(), Composite{StretchFactor: 100, Layout: VBox{MarginsZero: true, Spacing: 0},
				Children: append(pg.Stack(pages...), VSpacer{StretchFactor: 1})}}}
	}
	dctlTabs := newPager(3)
	dctlSeg := newNavBar(dctlTabs, true, navItem{label: "Film base"}, navItem{label: "White balance"}, navItem{label: "Mask & Crosstalk"})
	pageBar.right = appVersion
	wbui := &WBUI{mw: &mw, getBase: func() (string, string, string) { return resR.Text(), resG.Text(), resB.Text() }}
	rawui := &RAWUI{mw: &mw}
	hdrui := &HDRUI{mw: &mw}
	cropui := &CropUI{mw: &mw}
	devui := &DevUI{mw: &mw}
	lightui := &LightUI{mw: &mw, hdr: hdrui}
	hdrui.light = lightui
	triui := &TriUI{hdr: hdrui, light: lightui}
	hdrui.tri = triui
	hdrui.subTabs = cropTabs // Merge files = sub-page 1, Remove hairs = 2
	hp := hdrui.Pages(rawui.Page())
	maskui := &MaskUI{mw: &mw, getBase: func() (string, string, string) { return resR.Text(), resG.Text(), resB.Text() },
		db: func() *filmdb.DB {
			if hdrui.db == nil {
				hdrui.loadDB()
			}
			return hdrui.db
		}}
	dctlTabs.onChange = func(i int) {
		if i == 2 {
			maskui.refresh()
		}
	}

	addFiles := func(paths []string) {
		have := map[string]bool{}
		for _, r := range model.rows {
			if r.Region == nil { // a base-region row does not block the normal edge-band row
				have[strings.ToLower(r.Path)] = true
			}
		}
		for _, p := range paths {
			ext := strings.ToLower(filepath.Ext(p))
			if ext != ".tif" && ext != ".tiff" && ext != ".png" {
				continue
			}
			if have[strings.ToLower(p)] {
				continue
			}
			have[strings.ToLower(p)] = true
			model.rows = append(model.rows, &Row{Path: p, Name: filepath.Base(p), Status: "Pending"})
		}
		sort.Slice(model.rows, func(i, j int) bool { return model.rows[i].Name < model.rows[j].Name })
		model.PublishRowsReset()
		info.SetText(fmt.Sprintf("%d files loaded. Supported: 16-bit or 8-bit TIFF / PNG (linear export).", len(model.rows)))
	}

	parse := func(e *walk.LineEdit, def float64) float64 {
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(e.Text(), ",", ".")), 64)
		if err != nil {
			return def
		}
		return v
	}

	run := func() {
		if running || len(model.rows) == 0 {
			return
		}
		sp, ep, tol, lk := parse(startEd, 0.5), parse(endEd, 1.0), parse(tolEd, 10), parse(leakEd, 20)
		if sp < 0 || ep <= sp || ep > 20 {
			walk.MsgBox(mw, "Settings", "Edge band must satisfy 0 <= start < end <= 20 (percent).", walk.MsgBoxIconWarning)
			return
		}
		running = true
		runBtn.SetEnabled(false)
		pb.SetRange(0, len(model.rows))
		pb.SetValue(0)
		rows := model.rows
		excl := exclCB.Checked()
		go func() {
			var wg sync.WaitGroup
			sem := make(chan struct{}, 2)
			var mu sync.Mutex
			done := 0
			for _, r := range rows {
				wg.Add(1)
				sem <- struct{}{}
				go func(r *Row) {
					defer wg.Done()
					defer func() { <-sem }()
					res, err := measureRow(r, sp, ep, lk)
					mu.Lock()
					if err != nil {
						r.OK = false
						r.Status = "ERROR: " + err.Error()
					} else {
						*r = *res
					}
					done++
					d := done
					mu.Unlock()
					mw.Synchronize(func() {
						pb.SetValue(d)
						model.PublishRowChanged(indexOf(rows, r))
					})
					debug.FreeOSMemory()
				}(r)
			}
			wg.Wait()
			mw.Synchronize(func() {
				last = summarize(rows, excl, tol)
				model.PublishRowsReset()
				if last.N == 0 {
					info.SetText("No file could be measured.")
				} else {
					resR.SetText(fmt.Sprintf("%.4f", last.R))
					resG.SetText(fmt.Sprintf("%.4f", last.G))
					resB.SetText(fmt.Sprintf("%.4f", last.B))
					if last.N > 1 && last.Flagged == last.N {
						t := fmt.Sprintf("WARNING: all %d files are flagged (each is more than %.0f%% off the roll median). They do not agree, so the result (median of all files) is not reliable.", last.N, tol)
						info.SetText(t)
						walk.MsgBox(mw, "Film base: every file flagged", t+"\n\nCheck that the files are from the same roll and scan setup, or raise \"Flag files off by more than\".", walk.MsgBoxIconWarning)
					} else {
						info.SetText(fmt.Sprintf("Done. %d measured, %d used, %d flagged. Result = median across files.", last.N, last.Used, last.Flagged))
					}
				}
				running = false
				runBtn.SetEnabled(true)
			})
		}()
	}

	var appImg walk.Image
	for id := 1; id <= 10; id++ {
		if ic, e := walk.NewIconFromResourceIdWithSize(id, walk.Size{Width: 40, Height: 40}); e == nil {
			appImg = ic
			break
		}
	}
	err := MainWindow{
		AssignTo: &mw,
		Title:    appVersion,
		MinSize:  Size{Width: 640, Height: 420},
		Size:     Size{Width: 1280, Height: 860},
		Font:     Font{Family: "Segoe UI", PointSize: 9},
		Layout:   VBox{MarginsZero: true, Spacing: 0},
		OnDropFiles: func(files []string) {
			var all []string
			for _, f := range files {
				if st, err := os.Stat(f); err == nil && st.IsDir() {
					ents, _ := os.ReadDir(f)
					for _, e := range ents {
						all = append(all, filepath.Join(f, e.Name()))
					}
				} else {
					all = append(all, f)
				}
			}
			if tabs != nil && tabs.CurrentIndex() == 4 {
				devui.Open(files)
			} else if tabs != nil && tabs.CurrentIndex() == 0 && dctlTabs.CurrentIndex() == 1 {
				wbui.AddFiles(all)
			} else if tabs != nil && tabs.CurrentIndex() == 0 && dctlTabs.CurrentIndex() == 2 {
				// Mask & Crosstalk takes no files
			} else if tabs != nil && (tabs.CurrentIndex() == 1 || tabs.CurrentIndex() == 2) {
				// Camera & Light and HDR Scan take no files
			} else if tabs != nil && tabs.CurrentIndex() == 3 {
				switch cropTabs.CurrentIndex() {
				case 1:
					hdrui.DropFiles(all)
				case 2:
					hdrui.hair.DropFiles(all)
				case 3:
					rawui.AddFiles(all)
				default:
					cropui.AddFiles(files)
				}
			} else {
				addFiles(all)
			}
		},
		Children: []Widget{
			Composite{
				Background: SolidColorBrush{Color: cBar},
				MaxSize:    Size{Height: 46},
				Layout:     HBox{Margins: Margins{Left: 14, Top: 5, Right: 14, Bottom: 5}, Spacing: 10},
				Children: []Widget{
					ImageView{Image: appImg, MinSize: Size{Width: 30, Height: 30}, MaxSize: Size{Width: 30, Height: 30}, Mode: ImageViewModeZoom},
					Composite{
						Layout: VBox{MarginsZero: true, Spacing: 0},
						Children: []Widget{
							Label{Text: appVersion, TextColor: walk.RGB(236, 236, 240), Font: Font{Family: "Segoe UI", PointSize: 12, Bold: true}},
							Label{Text: "Film scan tools for DaVinci Resolve: DCTL export (film base, white balance, mask & crosstalk), tethered HDR scanning, RAW to linear TIFF, crop, negative to positive", TextColor: walk.RGB(128, 128, 136)},
						},
					},
					HSpacer{},
					PushButton{Text: "About", OnClicked: func() { showAbout(mw) }},
				},
			},
			Composite{Background: SolidColorBrush{Color: cBorder}, MinSize: Size{Height: 1}, MaxSize: Size{Height: 1}, Layout: HBox{MarginsZero: true}},
			// The pages scroll when the window is smaller than their layout, so
			// the window itself never needs to be taller than the screen.
			ScrollView{StretchFactor: 10, Layout: VBox{MarginsZero: true, Spacing: 0}, Children: append(tabs.Stack(
				TabPage{Title: "  1. DCTL  ", Layout: VBox{Margins: Margins{Left: 4, Top: 2, Right: 4, Bottom: 4}, Spacing: 2}, Children: []Widget{
					dctlSeg.Widget(), Composite{StretchFactor: 100, Layout: VBox{MarginsZero: true, Spacing: 0}, Children: dctlTabs.Stack(
						TabPage{
							Title:  "  Film base  ",
							Layout: VBox{Margins: Margins{Left: 10, Top: 10, Right: 10, Bottom: 10}, Spacing: 8},
							Children: []Widget{
								Composite{
									Layout: HBox{MarginsZero: true},
									Children: []Widget{
										PushButton{Text: "Add files...", OnClicked: func() {
											dlg := new(walk.FileDialog)
											dlg.Title = "Select scans from the same roll / session"
											dlg.Filter = "Images (*.tif;*.tiff;*.png)|*.tif;*.tiff;*.png"
											if ok, _ := dlg.ShowOpenMultiple(mw); ok {
												addFiles(dlg.FilePaths)
											}
										}},
										PushButton{Text: "Add folder...", OnClicked: func() {
											dlg := new(walk.FileDialog)
											dlg.Title = "Pick any file inside the folder"
											dlg.Filter = "Images (*.tif;*.tiff;*.png)|*.tif;*.tiff;*.png"
											if ok, _ := dlg.ShowOpen(mw); ok {
												dir := filepath.Dir(dlg.FilePath)
												ents, _ := os.ReadDir(dir)
												var all []string
												for _, e := range ents {
													all = append(all, filepath.Join(dir, e.Name()))
												}
												addFiles(all)
											}
										}},
										PushButton{Text: "Remove selected", OnClicked: func() {
											idx := tv.SelectedIndexes()
											if len(idx) == 0 {
												return
											}
											del := map[int]bool{}
											for _, i := range idx {
												del[i] = true
											}
											var keep []*Row
											for i, r := range model.rows {
												if !del[i] {
													keep = append(keep, r)
												}
											}
											model.rows = keep
											model.PublishRowsReset()
										}},
										PushButton{Text: "Clear", OnClicked: func() {
											model.rows = nil
											model.PublishRowsReset()
											resR.SetText("")
											resG.SetText("")
											resB.SetText("")
											info.SetText("Drop TIFF / PNG files here, or use Add files.")
										}},
										HSpacer{},
									},
								},
								TableView{
									AssignTo:         &tv,
									AlternatingRowBG: true,
									ColumnsOrderable: false,
									MultiSelection:   true,
									Model:            model,
									StretchFactor:    1,
									MinSize:          Size{Height: 140},
									Columns: []TableViewColumn{
										{Title: "File", Width: 320},
										{Title: "R", Width: 70},
										{Title: "G", Width: 70},
										{Title: "B", Width: 70},
										{Title: "R/G", Width: 60},
										{Title: "B/G", Width: 60},
										{Title: "Status", Width: 300},
									},
								},
								Composite{
									Layout: HBox{MarginsZero: true, Spacing: 8},
									Children: []Widget{
										Card{
											Title:         "Measurement settings",
											Icon:          iconMeter,
											StretchFactor: 3,
											Layout:        Grid{Columns: 2, Spacing: 8, Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}},
											Children: []Widget{
												Label{Text: "Edge band, % from each edge"},
												Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
													LineEdit{AssignTo: &startEd, Text: "0.5", MaxSize: Size{Width: 45}},
													Label{Text: "to"},
													LineEdit{AssignTo: &endEd, Text: "1.0", MaxSize: Size{Width: 45}},
													Label{Text: "% of the image size"},
													HSpacer{},
												}},
												Label{Text: "Light-leak rejection"},
												Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
													LineEdit{AssignTo: &leakEd, Text: "20", MaxSize: Size{Width: 45}, ToolTipText: "Pixels brighter than the clean film base by more than this are dropped (sprocket holes, bright specks)."},
													Label{Text: "% brighter than the clean base is dropped"},
													HSpacer{},
												}},
												Label{Text: "Flag files off by more than"},
												Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
													LineEdit{AssignTo: &tolEd, Text: "10", MaxSize: Size{Width: 45}},
													Label{Text: "% from the roll median"},
													HSpacer{},
												}},
												Label{Text: ""},
												CheckBox{AssignTo: &exclCB, Text: "Leave flagged files out of the result", Checked: true},
												VSpacer{ColumnSpan: 2},
											},
										},
										Card{
											Title:         "Result: film base",
											Icon:          iconFilm,
											StretchFactor: 2,
											Layout:        VBox{Spacing: 8, Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}},
											Children: []Widget{
												Composite{Layout: Grid{Columns: 2, Spacing: 6, MarginsZero: true}, Children: []Widget{
													Label{Text: "R", TextColor: walk.RGB(200, 50, 50), Font: Font{Family: "Segoe UI", PointSize: 11, Bold: true}},
													LineEdit{AssignTo: &resR, ReadOnly: true, MinSize: Size{Width: 130, Height: 26}, Font: Font{Family: "Consolas", PointSize: 12, Bold: true}},
													Label{Text: "G", TextColor: walk.RGB(40, 150, 60), Font: Font{Family: "Segoe UI", PointSize: 11, Bold: true}},
													LineEdit{AssignTo: &resG, ReadOnly: true, MinSize: Size{Width: 130, Height: 26}, Font: Font{Family: "Consolas", PointSize: 12, Bold: true}},
													Label{Text: "B", TextColor: walk.RGB(50, 90, 200), Font: Font{Family: "Segoe UI", PointSize: 11, Bold: true}},
													LineEdit{AssignTo: &resB, ReadOnly: true, MinSize: Size{Width: 130, Height: 26}, Font: Font{Family: "Consolas", PointSize: 12, Bold: true}},
												}},
												Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
													PushButton{Text: "Copy", OnClicked: func() {
														if resR.Text() == "" {
															return
														}
														walk.Clipboard().SetText(fmt.Sprintf("R %s\r\nG %s\r\nB %s", resR.Text(), resG.Text(), resB.Text()))
													}},
													PushButton{Text: "Export CSV...", OnClicked: func() {
														if len(model.rows) == 0 {
															return
														}
														dlg := new(walk.FileDialog)
														dlg.Filter = "CSV (*.csv)|*.csv"
														dlg.Title = "Save per-file results"
														if ok, _ := dlg.ShowSave(mw); ok {
															p := dlg.FilePath
															if !strings.HasSuffix(strings.ToLower(p), ".csv") {
																p += ".csv"
															}
															var sb strings.Builder
															sb.WriteString("file,R,G,B,R/G,B/G,top_G,bottom_G,left_G,right_G,status\r\n")
															for _, r := range model.rows {
																if !r.OK {
																	sb.WriteString(fmt.Sprintf("%s,,,,,,,,,,%s\r\n", r.Name, r.Status))
																	continue
																}
																sb.WriteString(fmt.Sprintf("%s,%.5f,%.5f,%.5f,%.4f,%.4f,%.5f,%.5f,%.5f,%.5f,%s\r\n", r.Name, r.R, r.G, r.B, r.R/r.G, r.B/r.G, r.Edges[0][1], r.Edges[1][1], r.Edges[2][1], r.Edges[3][1], r.Status))
															}
															sb.WriteString(fmt.Sprintf("MEDIAN RESULT,%s,%s,%s\r\n", resR.Text(), resG.Text(), resB.Text()))
															os.WriteFile(p, []byte(sb.String()), 0644)
														}
													}},
													HSpacer{},
												}},
												PushButton{AssignTo: &runBtn, Text: "Measure", MinSize: Size{Height: 34}, Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}, OnClicked: run},
											},
										},
									},
								},
								Card{
									Title:  "Film base presets  (shown in the DCTL dropdown \"Film base preset\")",
									Layout: VBox{},
									Children: []Widget{
										TableView{
											AssignTo:         &ptv,
											AlternatingRowBG: true,
											ColumnsOrderable: false,
											Model:            pmodel,
											MinSize:          Size{Height: 110},
											MaxSize:          Size{Height: 130},
											Columns: []TableViewColumn{
												{Title: "#", Width: 40},
												{Title: "Name", Width: 300},
												{Title: "R", Width: 90},
												{Title: "G", Width: 90},
												{Title: "B", Width: 90},
											},
										},
										Composite{
											Layout: HBox{MarginsZero: true},
											Children: []Widget{
												Label{Text: "Name"},
												LineEdit{AssignTo: &nameEd, MaxSize: Size{Width: 200}, ToolTipText: "Letters, digits, space, - _ . only (max 40)"},
												PushButton{Text: "Save result as preset", OnClicked: func() {
													name, ok := cleanName(nameEd.Text())
													if !ok {
														walk.MsgBox(mw, "Preset name", "Use 1 to 40 characters: English letters, digits, space, - _ .", walk.MsgBoxIconWarning)
														return
													}
													if resR.Text() == "" {
														walk.MsgBox(mw, "Preset", "Run Measure first, then save the result.", walk.MsgBoxIconInformation)
														return
													}
													r, _ := strconv.ParseFloat(resR.Text(), 64)
													g, _ := strconv.ParseFloat(resG.Text(), 64)
													b, _ := strconv.ParseFloat(resB.Text(), 64)
													np := Preset{name, r, g, b}
													replaced := false
													for i := range pmodel.items {
														if strings.EqualFold(pmodel.items[i].Name, name) {
															pmodel.items[i] = np
															replaced = true
														}
													}
													if !replaced {
														if len(pmodel.items) >= maxPresets {
															walk.MsgBox(mw, "Preset", "Too many presets.", walk.MsgBoxIconWarning)
															return
														}
														pmodel.items = append(pmodel.items, np)
													}
													pmodel.PublishRowsReset()
													persist()
													msg := "Preset saved: " + name
													if replaced {
														msg = "Preset updated: " + name
													}
													if s := syncDCTL(); s != "" {
														msg += ". " + s
													}
													info.SetText(msg)
												}},
												PushButton{Text: "Rename selected", OnClicked: func() {
													i := ptv.CurrentIndex()
													name, ok := cleanName(nameEd.Text())
													if i < 0 || !ok {
														walk.MsgBox(mw, "Rename", "Select a preset and type a valid new name (English letters, digits, space, - _ .).", walk.MsgBoxIconInformation)
														return
													}
													pmodel.items[i].Name = name
													pmodel.PublishRowsReset()
													persist()
													info.SetText("Renamed. " + syncDCTL())
												}},
												PushButton{Text: "Delete selected", OnClicked: func() {
													i := ptv.CurrentIndex()
													if i < 0 {
														return
													}
													if walk.MsgBox(mw, "Delete preset", "Delete \""+pmodel.items[i].Name+"\"?\nPresets after it move up one place in the dropdown, so nodes already using them will point at a different preset.", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
														return
													}
													pmodel.items = append(pmodel.items[:i], pmodel.items[i+1:]...)
													pmodel.PublishRowsReset()
													persist()
													info.SetText("Deleted. " + syncDCTL())
												}},
												PushButton{Text: "Import presets from DCTL...", OnClicked: func() {
													dlg := new(walk.FileDialog)
													dlg.Title = "Pick a DCTL exported by this program"
													dlg.Filter = "DCTL (*.dctl)|*.dctl"
													if ok, _ := dlg.ShowOpen(mw); !ok {
														return
													}
													b, err := os.ReadFile(dlg.FilePath)
													if err != nil {
														walk.MsgBox(mw, "Import", err.Error(), walk.MsgBoxIconError)
														return
													}
													ps := parsePresets(string(b))
													if len(ps) == 0 {
														walk.MsgBox(mw, "Import", "No presets found. Only DCTL files exported by this program can be imported.", walk.MsgBoxIconWarning)
														return
													}
													mode := importMode(mw, len(pmodel.items), len(ps), filepath.Base(dlg.FilePath))
													if mode == 0 {
														return
													}
													upd, add, skip := 0, len(ps), 0
													if mode == 1 {
														pmodel.items = ps
													} else {
														pmodel.items, upd, add, skip = mergeByName(pmodel.items, ps, func(p Preset) string { return p.Name }, maxPresets)
													}
													pmodel.PublishRowsReset()
													persist()
													msg := importSummary(mode, len(ps), upd, add, skip)
													if s := syncDCTL(); s != "" {
														msg += " " + s
													}
													info.SetText(msg)
												}},
												HSpacer{},
											},
										},
										Composite{
											Layout: HBox{MarginsZero: true},
											Children: []Widget{
												PushButton{Text: "Export DCTL...", OnClicked: func() { exportDCTL(true) }},
												CheckBox{AssignTo: &autoCB, Text: "Auto-update this DCTL file whenever presets change", Checked: cfg.AutoSync, OnCheckedChanged: func() {
													cfg.AutoSync = autoCB.Checked()
													persist()
													if cfg.AutoSync && cfg.DCTLPath == "" {
														info.SetText("Export the DCTL once first; after that the file stays in sync.")
													}
												}},
												HSpacer{},
											},
										},
										Label{AssignTo: &dctlLbl, Text: ""},
									},
								},
								ProgressBar{AssignTo: &pb},
								Label{AssignTo: &info, Text: "Drop TIFF / PNG files here, or use Add files. Use linear scans (the same export you use for the density node)."},
							},
						},
						wbui.Page(),
						maskui.Page(),
					)}}},
				module("  2. Camera & Light  ", camSeg, camTabs, hp.Camera, lightui.Page()),
				module("  3. HDR Scan  ", scanSeg, scanTabs, hp.White, triui.Page()),
				module("  4. Crop & Merge  ", cropSeg, cropTabs, cropui.Page(), hp.Merge, hp.Hairs, hp.Batch),
				devui.Page(),
			), VSpacer{StretchFactor: 1})},
			pageBar.Widget(),
		},
	}.Create()
	if err != nil {
		fmt.Println(err)
		return
	}
	for id := 1; id <= 10; id++ {
		if ic, e := walk.NewIconFromResourceId(id); e == nil {
			mw.SetIcon(ic)
			break
		}
	}
	selectRow := func(path string, region bool) {
		for i, r := range model.rows {
			if strings.EqualFold(r.Path, path) && (r.Region != nil) == region {
				tv.SetCurrentIndex(i)
				return
			}
		}
	}
	// One camera session and live-view loop; stop polling outside pages with previews.
	updateLivePage := func(_ int) {
		k := -1
		switch tabs.CurrentIndex() {
		case 1:
			if camTabs.CurrentIndex() == 1 {
				k = 3
			} else {
				k = 0
			}
		case 2:
			if scanTabs.CurrentIndex() == 1 {
				k = 2
			} else {
				k = 1
			}
		}
		hdrui.setLivePage(k)
	}
	tabs.onChange, camTabs.onChange, scanTabs.onChange = updateLivePage, updateLivePage, updateLivePage
	updateLivePage(0)
	lightui.cast.sync = syncDCTL
	lightui.cast.toDevelop = func(c filmdb.ColourCast) {
		n := &devui.chain.Nodes[devui.nodeIndex(develop.TDensity)]
		n.V["cast_on"] = 0
		if c.Enabled {
			n.V["cast_on"] = 1
		}
		for i, key := range []string{"cast_r", "cast_g", "cast_b"} {
			n.V[key] = c.Offset[i]
		}
		devui.syncPanel(develop.TDensity)
		devui.changed()
		walk.MsgBox(mw, "500T colour cast", "Density_In correction sent to Develop. Existing creative gamma / auto-WB settings are unchanged; avoid double colour correction.", walk.MsgBoxIconInformation)
	}
	lightui.sendDCTL = func(b [3]float64) error {
		if running {
			return fmt.Errorf("Film base is measuring; wait until it finishes")
		}
		next, err := withLevelingBase(cfg, b)
		if err != nil {
			return err
		}
		if err = saveConfig(next); err != nil {
			return err
		}
		cfg = next
		pmodel.items = cfg.Presets
		pmodel.PublishRowsReset()
		resR.SetText(fmt.Sprintf("%.5f", b[0]))
		resG.SetText(fmt.Sprintf("%.5f", b[1]))
		resB.SetText(fmt.Sprintf("%.5f", b[2]))
		nameEd.SetText(levelingPresetName)
		for i, p := range pmodel.items {
			if p.Name == levelingPresetName {
				ptv.SetCurrentIndex(i)
				break
			}
		}
		note := "Light leveling saved: default Film base R/G/B and preset 'Light leveling'. Next Density_In export uses these values."
		if extra := syncDCTL(); extra != "" {
			note += " " + extra
		}
		info.SetText(note)
		return nil
	}
	hdrui.LeakPct = func() float64 { return parse(leakEd, 20) }
	hdrui.SendBase = func(b SentBase) {
		if running {
			walk.MsgBox(mw, "Send base to Film base", "Film base is measuring; try again when it is done.", walk.MsgBoxIconInformation)
			return
		}
		tabs.SetCurrentIndex(0)
		dctlTabs.SetCurrentIndex(0)
		if b.Path != "" && b.Region != nil {
			// one region row per TIFF: replace an older one
			keep := model.rows[:0]
			for _, r := range model.rows {
				if !(r.Region != nil && strings.EqualFold(r.Path, b.Path)) {
					keep = append(keep, r)
				}
			}
			reg := *b.Region
			status := "Sent from HDR scan (Measure re-reads it from the TIFF)"
			if b.Trichrome {
				status = "Sent from Trichrome scan (Measure re-reads it from the TIFF)"
			}
			model.rows = append(keep, &Row{Path: b.Path, Name: filepath.Base(b.Path) + "  [base region]", Region: &reg,
				R: b.RGB[0], G: b.RGB[1], B: b.RGB[2], OK: true, Status: status})
			sort.Slice(model.rows, func(i, j int) bool { return model.rows[i].Name < model.rows[j].Name })
			model.PublishRowsReset()
			selectRow(b.Path, true)
		}
		resR.SetText(fmt.Sprintf("%.4f", b.RGB[0]))
		resG.SetText(fmt.Sprintf("%.4f", b.RGB[1]))
		resB.SetText(fmt.Sprintf("%.4f", b.RGB[2]))
		if b.Suggest != "" {
			nameEd.SetText(b.Suggest)
		}
		if b.Trichrome {
			note := maskui.forTrichrome()
			info.SetText("Base from " + b.Source + " (FilmScan_Density_In Film base R/G/B).\r\n" + note + "\r\nCheck the name and press Save result as preset (the DCTL then updates as usual).")
		} else {
			info.SetText("Base from HDR scan: " + b.Source + ".\r\nCheck the name and press Save result as preset (the DCTL then updates as usual).")
		}
		nameEd.SetFocus()
	}
	hdrui.OpenInFilmBase = func(path string) {
		tabs.SetCurrentIndex(0)
		dctlTabs.SetCurrentIndex(0)
		addFiles([]string{path})
		selectRow(path, false)
		info.SetText("HDR TIFF added: " + filepath.Base(path) + ". Press Measure (edge bands, same settings as any scan).")
	}
	if b := cfg.CurrentBase; b != nil {
		resR.SetText(fmt.Sprintf("%.5f", b[0]))
		resG.SetText(fmt.Sprintf("%.5f", b[1]))
		resB.SetText(fmt.Sprintf("%.5f", b[2]))
		nameEd.SetText(levelingPresetName)
	}
	refreshPath()
	wbui.refreshPath()
	hdrui.AfterCreate()
	lightui.AfterCreate()
	triui.AfterCreate()
	maskui.init()
	devui.init()
	applyDark(mw)
	fitToWorkArea(mw)
	// "NegativeScanStudio.exe --crop <files or folders>" opens them on the Crop page
	if len(os.Args) > 2 && os.Args[1] == "--crop" {
		tabs.SetCurrentIndex(3)
		cropTabs.SetCurrentIndex(0)
		cropui.AddFiles(os.Args[2:])
	}
	// "NegativeScanStudio.exe --develop <files or folder>" opens them on the Develop page
	if len(os.Args) > 2 && os.Args[1] == "--develop" {
		tabs.SetCurrentIndex(4)
		devui.Open(os.Args[2:])
	}
	// "NegativeScanStudio.exe --light" opens the Light page
	if len(os.Args) > 1 && os.Args[1] == "--light" {
		tabs.SetCurrentIndex(1)
		camTabs.SetCurrentIndex(1)
	}
	// "--tri" opens the Trichrome page (v0.82)
	if len(os.Args) > 1 && os.Args[1] == "--tri" {
		tabs.SetCurrentIndex(2)
		scanTabs.SetCurrentIndex(1)
	}
	// "--camera" / "--white" / "--merge" (v0.83) open those sub-pages
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--camera":
			tabs.SetCurrentIndex(1)
		case "--white":
			tabs.SetCurrentIndex(2)
		case "--merge":
			tabs.SetCurrentIndex(3)
			cropTabs.SetCurrentIndex(1)
		case "--hairs":
			tabs.SetCurrentIndex(3)
			cropTabs.SetCurrentIndex(2)
		case "--batch":
			tabs.SetCurrentIndex(3)
			cropTabs.SetCurrentIndex(3)
		}
	}
	if len(migrated) > 0 {
		info.SetText("Settings and film base DB copied from Film Base Meter (old folder left unchanged): " + strings.Join(migrated, ", "))
	}
	mw.Run()
}

func indexOf(rows []*Row, r *Row) int {
	for i, x := range rows {
		if x == r {
			return i
		}
	}
	return 0
}
