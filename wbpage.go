package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type WBRow struct {
	Path, Name string
	R, B       float64
	N          int
	OK, Flag   bool
	Status     string
}

type WBModel struct {
	walk.TableModelBase
	rows []*WBRow
}

func (m *WBModel) StyleCell(style *walk.CellStyle) {
	r := m.rows[style.Row()]
	st := strings.ToUpper(r.Status)
	if strings.Contains(st, "CHECK") || strings.Contains(st, "ERROR") {
		style.TextColor = walk.RGB(240, 100, 86)
	} else if st == "PENDING" {
		style.TextColor = walk.RGB(118, 118, 126)
	}
}

func (m *WBModel) RowCount() int { return len(m.rows) }
func (m *WBModel) Value(row, col int) interface{} {
	r := m.rows[row]
	switch col {
	case 0:
		return r.Name
	case 1:
		if r.OK {
			return fmt.Sprintf("%+.2f", r.R)
		}
	case 2:
		if r.OK {
			return fmt.Sprintf("%+.2f", r.B)
		}
	case 3:
		return r.Status
	}
	return ""
}

type WBPresetModel struct {
	walk.TableModelBase
	items []WBPreset
}

func (m *WBPresetModel) RowCount() int { return len(m.items) }
func (m *WBPresetModel) Value(row, col int) interface{} {
	p := m.items[row]
	switch col {
	case 0:
		return fmt.Sprintf("%d", row+1)
	case 1:
		return p.Name
	case 2:
		return fmt.Sprintf("%.2f", p.R)
	case 3:
		return fmt.Sprintf("%.2f", p.G)
	case 4:
		return fmt.Sprintf("%.2f", p.B)
	}
	return ""
}

type WBUI struct {
	mw                                                            **walk.MainWindow
	getBase                                                       func() (string, string, string)
	model                                                         *WBModel
	pmodel                                                        *WBPresetModel
	cfg                                                           WBConfig
	tv, ptv                                                       *walk.TableView
	baseR, baseG, baseB, whiteEd, gamR, gamG, gamB, tolEd, nameEd *walk.LineEdit
	methodCB                                                      *walk.ComboBox
	exclCB, autoCB                                                *walk.CheckBox
	resR, resB, resGR, resGG, resGB                               *walk.LineEdit
	pb                                                            *walk.ProgressBar
	info, pathLbl                                                 *walk.Label
	runBtn                                                        *walk.PushButton
	running                                                       bool
	curStopsR, curStopsB                                          float64
	curParams                                                     WBParams
	haveResult                                                    bool
}

func (u *WBUI) parse(e *walk.LineEdit, def float64) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(e.Text(), ",", ".")), 64)
	if err != nil {
		return def
	}
	return v
}

func (u *WBUI) persist() {
	u.cfg.Presets = u.pmodel.items
	saveWBConfig(u.cfg)
}

func (u *WBUI) sync() string {
	if u.cfg.DCTLPath == "" || !u.cfg.AutoSync {
		return ""
	}
	if err := os.WriteFile(u.cfg.DCTLPath, []byte(buildCoreDCTL(u.pmodel.items)), 0644); err != nil {
		return "Could not update " + u.cfg.DCTLPath + ": " + err.Error()
	}
	return "DCTL file updated."
}

func (u *WBUI) refreshPath() {
	if u.cfg.DCTLPath == "" {
		u.pathLbl.SetText("DCTL file: not exported yet")
	} else {
		u.pathLbl.SetText("DCTL file: " + u.cfg.DCTLPath)
	}
}

func (u *WBUI) AddFiles(paths []string) {
	have := map[string]bool{}
	for _, r := range u.model.rows {
		have[strings.ToLower(r.Path)] = true
	}
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".tif" && ext != ".tiff" && ext != ".png" {
			continue
		}
		if have[strings.ToLower(p)] {
			continue
		}
		u.model.rows = append(u.model.rows, &WBRow{Path: p, Name: filepath.Base(p), Status: "Pending"})
	}
	sort.Slice(u.model.rows, func(i, j int) bool { return u.model.rows[i].Name < u.model.rows[j].Name })
	u.model.PublishRowsReset()
	u.info.SetText(fmt.Sprintf("%d files loaded. Use scans of one roll and one scanning session.", len(u.model.rows)))
}

func (u *WBUI) params() (WBParams, bool) {
	p := WBParams{
		Base:  [3]float64{u.parse(u.baseR, 0), u.parse(u.baseG, 0), u.parse(u.baseB, 0)},
		White: u.parse(u.whiteEd, 696),
		Gam:   [3]float64{u.parse(u.gamR, 0.55), u.parse(u.gamG, 0.635), u.parse(u.gamB, 0.62)},
		Mode:  u.methodCB.CurrentIndex(),
	}
	if p.Mode < 0 {
		p.Mode = 0
	}
	ok := p.Base[0] > 0 && p.Base[1] > 0 && p.Base[2] > 0 && p.White > 200 && p.White <= 1023 && p.Gam[0] > 0 && p.Gam[1] > 0 && p.Gam[2] > 0
	return p, ok
}

func (u *WBUI) run() {
	if u.running || len(u.model.rows) == 0 {
		return
	}
	mw := *u.mw
	p, ok := u.params()
	if !ok {
		walk.MsgBox(mw, "Settings", "Film base R/G/B and gamma must be above 0, and the white point between 200 and 1023.", walk.MsgBoxIconWarning)
		return
	}
	tol := u.parse(u.tolEd, 0.5)
	excl := u.exclCB.Checked()
	u.running = true
	u.runBtn.SetEnabled(false)
	rows := u.model.rows
	u.pb.SetRange(0, len(rows))
	u.pb.SetValue(0)
	go func() {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 2)
		var mu sync.Mutex
		done := 0
		for _, r := range rows {
			wg.Add(1)
			sem <- struct{}{}
			go func(r *WBRow) {
				defer wg.Done()
				defer func() { <-sem }()
				sr, sb, n, err := analyzeWB(r.Path, p)
				mu.Lock()
				if err != nil {
					r.OK = false
					r.Status = "ERROR: " + err.Error()
				} else {
					r.OK, r.R, r.B, r.N, r.Flag, r.Status = true, sr, sb, n, false, "OK"
				}
				done++
				d := done
				mu.Unlock()
				mw.Synchronize(func() { u.pb.SetValue(d) })
				debug.FreeOSMemory()
			}(r)
		}
		wg.Wait()
		mw.Synchronize(func() {
			var rs, bs []float64
			for _, r := range rows {
				if r.OK {
					rs = append(rs, r.R)
					bs = append(bs, r.B)
				}
			}
			if len(rs) == 0 {
				u.info.SetText("No file could be analyzed.")
			} else {
				mr, mb := median(append([]float64{}, rs...)), median(append([]float64{}, bs...))
				flagged := 0
				var ur, ub []float64
				for _, r := range rows {
					if !r.OK {
						continue
					}
					dev := math.Max(math.Abs(r.R-mr), math.Abs(r.B-mb))
					if dev > tol {
						r.Flag = true
						flagged++
						r.Status = fmt.Sprintf("CHECK: %.2f stops from the roll median", dev)
						if excl {
							r.Status += " (left out)"
							continue
						}
					}
					ur = append(ur, r.R)
					ub = append(ub, r.B)
				}
				if len(ur) == 0 {
					ur, ub = rs, bs
				}
				fr, fb := median(ur), median(ub)
				u.curStopsR, u.curStopsB, u.curParams, u.haveResult = fr, fb, p, true
				gr, gg, gb := grayFromStops(fr, fb, p)
				u.resR.SetText(fmt.Sprintf("%+.2f", fr))
				u.resB.SetText(fmt.Sprintf("%+.2f", fb))
				u.resGR.SetText(fmt.Sprintf("%.2f", gr))
				u.resGG.SetText(fmt.Sprintf("%.2f", gg))
				u.resGB.SetText(fmt.Sprintf("%.2f", gb))
				if len(rs) > 1 && flagged == len(rs) {
					t := fmt.Sprintf("WARNING: all %d files are flagged (each is more than %.2f stops off the roll median). They do not agree, so the result (median of all files) is not reliable.", len(rs), tol)
					u.info.SetText(t)
					walk.MsgBox(mw, "White balance: every file flagged", t+"\n\nCheck that the files are from the same roll and light, or raise the flag tolerance.", walk.MsgBoxIconWarning)
				} else {
					u.info.SetText(fmt.Sprintf("Done. %d analyzed, %d used, %d flagged. Result = median across files.", len(rs), len(ur), flagged))
				}
			}
			u.model.PublishRowsReset()
			u.running = false
			u.runBtn.SetEnabled(true)
		})
	}()
}

func (u *WBUI) export(force bool) {
	mw := *u.mw
	p := u.cfg.DCTLPath
	if p == "" || force {
		dlg := new(walk.FileDialog)
		dlg.Title = "Save FilmScan_Positive_Out.dctl (put it in your Resolve DCTL folder)"
		dlg.Filter = "DCTL (*.dctl)|*.dctl"
		dlg.FilePath = "FilmScan_Positive_Out.dctl"
		if ok, _ := dlg.ShowSave(mw); !ok {
			return
		}
		p = dlg.FilePath
		if !strings.HasSuffix(strings.ToLower(p), ".dctl") {
			p += ".dctl"
		}
	}
	if err := os.WriteFile(p, []byte(buildCoreDCTL(u.pmodel.items)), 0644); err != nil {
		walk.MsgBox(mw, "Export DCTL", "Could not write the file:\n"+err.Error(), walk.MsgBoxIconError)
		return
	}
	u.cfg.DCTLPath = p
	u.persist()
	u.refreshPath()
	u.info.SetText(fmt.Sprintf("DCTL exported with %d white balance preset(s): %s", len(u.pmodel.items), p))
}

func (u *WBUI) Page() TabPage {
	mw := func() *walk.MainWindow { return *u.mw }
	u.model = &WBModel{}
	u.cfg = loadWBConfig()
	u.pmodel = &WBPresetModel{items: u.cfg.Presets}
	return TabPage{
		Title:  "  2. White balance  ",
		Layout: VBox{Margins: Margins{Left: 10, Top: 10, Right: 10, Bottom: 10}, Spacing: 8},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					PushButton{Text: "Add files...", OnClicked: func() {
						dlg := new(walk.FileDialog)
						dlg.Title = "Select scans from the same roll / session"
						dlg.Filter = "Images (*.tif;*.tiff;*.png)|*.tif;*.tiff;*.png"
						if ok, _ := dlg.ShowOpenMultiple(mw()); ok {
							u.AddFiles(dlg.FilePaths)
						}
					}},
					PushButton{Text: "Add folder...", OnClicked: func() {
						dlg := new(walk.FileDialog)
						dlg.Title = "Pick any file inside the folder"
						dlg.Filter = "Images (*.tif;*.tiff;*.png)|*.tif;*.tiff;*.png"
						if ok, _ := dlg.ShowOpen(mw()); ok {
							dir := filepath.Dir(dlg.FilePath)
							ents, _ := os.ReadDir(dir)
							var all []string
							for _, e := range ents {
								all = append(all, filepath.Join(dir, e.Name()))
							}
							u.AddFiles(all)
						}
					}},
					PushButton{Text: "Remove selected", OnClicked: func() {
						idx := u.tv.SelectedIndexes()
						del := map[int]bool{}
						for _, i := range idx {
							del[i] = true
						}
						var keep []*WBRow
						for i, r := range u.model.rows {
							if !del[i] {
								keep = append(keep, r)
							}
						}
						u.model.rows = keep
						u.model.PublishRowsReset()
					}},
					PushButton{Text: "Clear", OnClicked: func() {
						u.model.rows = nil
						u.model.PublishRowsReset()
						for _, e := range []*walk.LineEdit{u.resR, u.resB, u.resGR, u.resGG, u.resGB} {
							e.SetText("")
						}
						u.haveResult = false
						u.info.SetText("Drop TIFF / PNG files here, or use Add files.")
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
					{Title: "File", Width: 340},
					{Title: "Red (stops)", Width: 90},
					{Title: "Blue (stops)", Width: 90},
					{Title: "Status", Width: 340},
				},
			},
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 8},
				Children: []Widget{
					Card{
						Title:         "Analysis settings",
						StretchFactor: 3,
						Layout:        Grid{Columns: 2, Spacing: 8, Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}},
						Children: []Widget{
							Label{Text: "Film base"},
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								Label{Text: "R"}, LineEdit{AssignTo: &u.baseR, Text: "0.1933", MaxSize: Size{Width: 65}},
								Label{Text: "G"}, LineEdit{AssignTo: &u.baseG, Text: "0.1883", MaxSize: Size{Width: 65}},
								Label{Text: "B"}, LineEdit{AssignTo: &u.baseB, Text: "0.0593", MaxSize: Size{Width: 65}},
								PushButton{Text: "Use film base result", OnClicked: func() {
									r, g, b := u.getBase()
									if r == "" {
										walk.MsgBox(mw(), "Film base", "Measure the film base on the first tab first.", walk.MsgBoxIconInformation)
										return
									}
									u.baseR.SetText(r)
									u.baseG.SetText(g)
									u.baseB.SetText(b)
								}},
								HSpacer{},
							}},
							Label{Text: "White point code"},
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								LineEdit{AssignTo: &u.whiteEd, Text: "696", MaxSize: Size{Width: 50}},
								HSpacer{},
							}},
							Label{Text: "Gamma"},
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								Label{Text: "R"}, LineEdit{AssignTo: &u.gamR, Text: "0.55", MaxSize: Size{Width: 50}},
								Label{Text: "G"}, LineEdit{AssignTo: &u.gamG, Text: "0.635", MaxSize: Size{Width: 50}},
								Label{Text: "B"}, LineEdit{AssignTo: &u.gamB, Text: "0.62", MaxSize: Size{Width: 50}},
								Label{Text: "(use the same values as in the Positive_Out DCTL)"},
								HSpacer{},
							}},
							Label{Text: "Method"},
							ComboBox{AssignTo: &u.methodCB, Model: []string{"Gray world, mid-tones (recommended)", "Gray world, all pixels"}, CurrentIndex: 0, MaxSize: Size{Width: 280}},
							Label{Text: "Flag files off by more than"},
							Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
								LineEdit{AssignTo: &u.tolEd, Text: "0.5", MaxSize: Size{Width: 45}},
								Label{Text: "stops from the roll median"},
								CheckBox{AssignTo: &u.exclCB, Text: "Leave flagged files out", Checked: true},
								HSpacer{},
							}},
							VSpacer{ColumnSpan: 2},
						},
					},
					Card{
						Title:         "Result: white balance",
						Icon:          iconWB,
						StretchFactor: 2,
						Layout:        VBox{Spacing: 8, Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}},
						Children: []Widget{
							Composite{Layout: Grid{Columns: 4, Spacing: 6, MarginsZero: true}, Children: []Widget{
								Label{Text: "Red balance"},
								LineEdit{AssignTo: &u.resR, ReadOnly: true, MinSize: Size{Width: 80, Height: 26}, Font: Font{Family: "Consolas", PointSize: 12, Bold: true}},
								Label{Text: "Blue balance"},
								LineEdit{AssignTo: &u.resB, ReadOnly: true, MinSize: Size{Width: 80, Height: 26}, Font: Font{Family: "Consolas", PointSize: 12, Bold: true}},
							}},
							Label{Text: "stops, relative to green. Gray point codes:", TextColor: walk.RGB(110, 110, 110)},
							Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
								Label{Text: "R", TextColor: walk.RGB(200, 50, 50), Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}},
								LineEdit{AssignTo: &u.resGR, ReadOnly: true, MinSize: Size{Width: 70, Height: 24}, Font: Font{Family: "Consolas", PointSize: 11, Bold: true}},
								Label{Text: "G", TextColor: walk.RGB(40, 150, 60), Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}},
								LineEdit{AssignTo: &u.resGG, ReadOnly: true, MinSize: Size{Width: 70, Height: 24}, Font: Font{Family: "Consolas", PointSize: 11, Bold: true}},
								Label{Text: "B", TextColor: walk.RGB(50, 90, 200), Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}},
								LineEdit{AssignTo: &u.resGB, ReadOnly: true, MinSize: Size{Width: 70, Height: 24}, Font: Font{Family: "Consolas", PointSize: 11, Bold: true}},
							}},
							Composite{Layout: HBox{MarginsZero: true, Spacing: 6}, Children: []Widget{
								PushButton{Text: "Copy", OnClicked: func() {
									if u.resR.Text() == "" {
										return
									}
									walk.Clipboard().SetText(fmt.Sprintf("Gray R %s\r\nGray G %s\r\nGray B %s\r\nRed balance %s stops\r\nBlue balance %s stops", u.resGR.Text(), u.resGG.Text(), u.resGB.Text(), u.resR.Text(), u.resB.Text()))
								}},
								HSpacer{},
							}},
							PushButton{AssignTo: &u.runBtn, Text: "Analyze", MinSize: Size{Height: 34}, Font: Font{Family: "Segoe UI", PointSize: 10, Bold: true}, OnClicked: u.run},
						},
					},
				},
			},
			Card{
				Title:  "White balance presets  (shown in the DCTL dropdown \"Film white balance\")",
				Layout: VBox{},
				Children: []Widget{
					TableView{
						AssignTo:         &u.ptv,
						AlternatingRowBG: true,
						ColumnsOrderable: false,
						Model:            u.pmodel,
						MinSize:          Size{Height: 100},
						MaxSize:          Size{Height: 120},
						Columns: []TableViewColumn{
							{Title: "#", Width: 40},
							{Title: "Name", Width: 300},
							{Title: "Gray R", Width: 90},
							{Title: "Gray G", Width: 90},
							{Title: "Gray B", Width: 90},
						},
					},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							Label{Text: "Name"},
							LineEdit{AssignTo: &u.nameEd, MaxSize: Size{Width: 180}, ToolTipText: "Letters, digits, space, - _ . only (max 40)"},
							PushButton{Text: "Save result as preset", OnClicked: func() {
								name, ok := cleanName(u.nameEd.Text())
								if !ok {
									walk.MsgBox(mw(), "Preset name", "Use 1 to 40 characters: English letters, digits, space, - _ .", walk.MsgBoxIconWarning)
									return
								}
								if !u.haveResult {
									walk.MsgBox(mw(), "Preset", "Run Analyze first, then save the result.", walk.MsgBoxIconInformation)
									return
								}
								gr, gg, gb := grayFromStops(u.curStopsR, u.curStopsB, u.curParams)
								if gr < 100 || gr > 1023 || gb < 100 || gb > 1023 {
									walk.MsgBox(mw(), "Preset", fmt.Sprintf("The gray point (R %.1f, B %.1f) is outside the 100 to 1023 range of the DCTL. Check the film base and white point.", gr, gb), walk.MsgBoxIconWarning)
									return
								}
								np := WBPreset{name, gr, gg, gb}
								replaced := false
								for i := range u.pmodel.items {
									if strings.EqualFold(u.pmodel.items[i].Name, name) {
										u.pmodel.items[i] = np
										replaced = true
									}
								}
								if !replaced {
									if len(u.pmodel.items) >= maxWB {
										walk.MsgBox(mw(), "Preset", "Too many presets.", walk.MsgBoxIconWarning)
										return
									}
									u.pmodel.items = append(u.pmodel.items, np)
								}
								u.pmodel.PublishRowsReset()
								u.persist()
								msg := "Preset saved: " + name
								if replaced {
									msg = "Preset updated: " + name
								}
								if s := u.sync(); s != "" {
									msg += ". " + s
								}
								u.info.SetText(msg)
							}},
							PushButton{Text: "Rename selected", OnClicked: func() {
								i := u.ptv.CurrentIndex()
								name, ok := cleanName(u.nameEd.Text())
								if i < 0 || !ok {
									walk.MsgBox(mw(), "Rename", "Select a preset and type a valid new name (English letters, digits, space, - _ .).", walk.MsgBoxIconInformation)
									return
								}
								u.pmodel.items[i].Name = name
								u.pmodel.PublishRowsReset()
								u.persist()
								u.info.SetText("Renamed. " + u.sync())
							}},
							PushButton{Text: "Delete selected", OnClicked: func() {
								i := u.ptv.CurrentIndex()
								if i < 0 {
									return
								}
								if walk.MsgBox(mw(), "Delete preset", "Delete \""+u.pmodel.items[i].Name+"\"?\nPresets after it move up one place in the dropdown, so nodes already using them will point at a different preset.", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
									return
								}
								u.pmodel.items = append(u.pmodel.items[:i], u.pmodel.items[i+1:]...)
								u.pmodel.PublishRowsReset()
								u.persist()
								u.info.SetText("Deleted. " + u.sync())
							}},
							PushButton{Text: "Import presets from DCTL...", OnClicked: func() {
								dlg := new(walk.FileDialog)
								dlg.Title = "Pick a white balance DCTL exported by this program"
								dlg.Filter = "DCTL (*.dctl)|*.dctl"
								if ok, _ := dlg.ShowOpen(mw()); !ok {
									return
								}
								b, err := os.ReadFile(dlg.FilePath)
								if err != nil {
									walk.MsgBox(mw(), "Import", err.Error(), walk.MsgBoxIconError)
									return
								}
								ps := parseWBPresets(string(b))
								if len(ps) == 0 {
									walk.MsgBox(mw(), "Import", "No presets found. Only DCTL files exported by this program can be imported.", walk.MsgBoxIconWarning)
									return
								}
								mode := importMode(mw(), len(u.pmodel.items), len(ps), filepath.Base(dlg.FilePath))
								if mode == 0 {
									return
								}
								upd, add, skip := 0, len(ps), 0
								if mode == 1 {
									u.pmodel.items = ps
								} else {
									u.pmodel.items, upd, add, skip = mergeByName(u.pmodel.items, ps, func(p WBPreset) string { return p.Name }, maxWB)
								}
								u.pmodel.PublishRowsReset()
								u.persist()
								msg := importSummary(mode, len(ps), upd, add, skip)
								if s := u.sync(); s != "" {
									msg += " " + s
								}
								u.info.SetText(msg)
							}},
							HSpacer{},
						},
					},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							PushButton{Text: "Export DCTL...", OnClicked: func() { u.export(true) }},
							CheckBox{AssignTo: &u.autoCB, Text: "Auto-update this DCTL file whenever presets change", Checked: u.cfg.AutoSync, OnCheckedChanged: func() {
								u.cfg.AutoSync = u.autoCB.Checked()
								u.persist()
								if u.cfg.AutoSync && u.cfg.DCTLPath == "" {
									u.info.SetText("Export the DCTL once first; after that the file stays in sync.")
								}
							}},
							HSpacer{},
						},
					},
					Label{AssignTo: &u.pathLbl, Text: ""},
				},
			},
			ProgressBar{AssignTo: &u.pb},
			Label{AssignTo: &u.info, Text: "Drop TIFF / PNG files here, or use Add files. Use the same linear scans as on the Film base tab."},
		},
	}
}
