package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"basetool/filmdb"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

func main() {
	migrated := migrateLegacyConfig() // before anything reads settings or writes crash_log
	installCrashLogger()
	defer mainCrash()
	debug.SetGCPercent(20)
	// "NegativeScanStudio.exe --dust light|strong <tiff> [...]" cleans TIFFs without the window
	if len(os.Args) > 3 && os.Args[1] == "--dust" {
		os.Exit(dustCLI(os.Args[2], os.Args[3:]))
	}
	var mw *walk.MainWindow
	// Four app modules; Resolve conversion is provided by the independent OFX plugin.
	tabs := newPager(4)
	pageBar := newNavBar(tabs, false,
		navItem{"Camera & Light", iconLight}, navItem{"HDR Scan", iconHDR}, navItem{"Crop & Merge", iconCrop}, navItem{"Convert", iconConvert})
	camTabs := newPager(2)
	camSeg := newNavBar(camTabs, true, navItem{label: "Camera"}, navItem{label: "Light"})
	scanTabs := newPager(4)
	scanSeg := newNavBar(scanTabs, true, navItem{label: "White light scan"}, navItem{label: "Trichrome"}, navItem{label: "Mono trichrome"}, navItem{label: "Focus check"})
	cropTabs := newPager(4)
	cropSeg := newNavBar(cropTabs, true, navItem{label: "Crop"}, navItem{label: "Merge files"}, navItem{label: "Remove hairs"}, navItem{label: "Batch RAW to TIFF"})
	module := func(title string, seg *navBar, pg *pager, pages ...TabPage) TabPage {
		return TabPage{Title: title, Layout: VBox{Margins: Margins{Left: 4, Top: 2, Right: 4, Bottom: 4}, Spacing: 2},
			Children: []Widget{seg.Widget(), Composite{StretchFactor: 100, Layout: VBox{MarginsZero: true, Spacing: 0},
				Children: append(pg.Stack(pages...), VSpacer{StretchFactor: 1})}}}
	}
	pageBar.right = appVersion
	rawui := &RAWUI{mw: &mw}
	hdrui := &HDRUI{mw: &mw}
	cropui := &CropUI{mw: &mw}
	convui := &ConvertUI{mw: &mw}
	lightui := &LightUI{mw: &mw, hdr: hdrui}
	hdrui.light = lightui
	triui := &TriUI{hdr: hdrui, light: lightui}
	hdrui.tri = triui
	monoui := &MonoUI{hdr: hdrui, light: lightui}
	focusui := &FocusUI{hdr: hdrui, light: lightui, mono: monoui}
	hdrui.mono = monoui
	hdrui.focus = focusui
	hdrui.subTabs = cropTabs // Merge files = sub-page 1, Remove hairs = 2
	hp := hdrui.Pages(rawui.Page())
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
					entries, _ := os.ReadDir(f)
					for _, entry := range entries {
						all = append(all, filepath.Join(f, entry.Name()))
					}
				} else {
					all = append(all, f)
				}
			}
			switch tabs.CurrentIndex() {
			case 3:
				convui.Open(files)
			case 2:
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
							Label{Text: "Camera and light control, tethered HDR scanning, RAW to linear TIFF, crop and Convert (RAW to film negative)", TextColor: walk.RGB(128, 128, 136)},
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
				module("  1. Camera & Light  ", camSeg, camTabs, hp.Camera, lightui.Page()),
				module("  2. HDR Scan  ", scanSeg, scanTabs, hp.White, triui.Page(), monoui.Page(), focusui.Page()),
				module("  3. Crop & Merge  ", cropSeg, cropTabs, cropui.Page(), hp.Merge, hp.Hairs, hp.Batch),
				convui.Page(),
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
	// One camera session and live-view loop; stop polling outside pages with previews.
	updateLivePage := func(_ int) {
		k := -1
		switch tabs.CurrentIndex() {
		case 0:
			if camTabs.CurrentIndex() == 1 {
				if lightui.usesMonoCam() {
					k = -1 // Hikrobot / sim preview is injected into the Light live view
					lightui.startHikPreview()
				} else {
					lightui.stopHikPreview()
					k = 3
				}
			} else {
				lightui.stopHikPreview()
				k = 0
			}
		case 1:
			lightui.stopHikPreview()
			switch scanTabs.CurrentIndex() {
			case 1:
				k = 2
			case 2, 3:
				k = -1 // Mono trichrome and Focus check run their own preview
			default:
				k = 1
			}
			if scanTabs.CurrentIndex() == 3 {
				focusui.enter()
			} else if focusui.active {
				focusui.leave()
			}
		default:
			lightui.stopHikPreview()
		}
		if tabs.CurrentIndex() != 1 && focusui.active {
			focusui.leave()
		}
		hdrui.setLivePage(k)
	}
	hdrui.liveRoute = func() { updateLivePage(0) }
	tabs.onChange, camTabs.onChange, scanTabs.onChange = updateLivePage, updateLivePage, updateLivePage
	updateLivePage(0)
	lightui.cast.toDevelop = func(c filmdb.ColourCast) {
		walk.MsgBox(mw, "500T colour cast", "Convert models a digital-to-negative exposure and does not use the Develop Density In cast controls. Colour cast stays on the 500T calculator page.", walk.MsgBoxIconInformation)
	}
	hdrui.SendBase = func(b SentBase) {
		walk.MsgBox(mw, "Film base", "Convert does not apply a scanned film-base reference. Use the OFX plugin for negative-to-positive grading of scanned film.", walk.MsgBoxIconInformation)
	}
	hdrui.OpenInDevelop = func(path string) {
		tabs.SetCurrentIndex(3)
		convui.Open([]string{path})
	}
	hdrui.AfterCreate()
	lightui.AfterCreate()
	triui.AfterCreate()
	monoui.AfterCreate()
	focusui.AfterCreate()
	convui.init()
	applyDark(mw)
	fitToWorkArea(mw)
	// "NegativeScanStudio.exe --crop <files or folders>" opens them on the Crop page
	if len(os.Args) > 2 && os.Args[1] == "--crop" {
		tabs.SetCurrentIndex(2)
		cropTabs.SetCurrentIndex(0)
		cropui.AddFiles(os.Args[2:])
	}
	// "NegativeScanStudio.exe --develop / --convert <files or folder>" opens them on the Convert page
	if len(os.Args) > 2 && (os.Args[1] == "--develop" || os.Args[1] == "--convert") {
		tabs.SetCurrentIndex(3)
		convui.Open(os.Args[2:])
	}
	// "NegativeScanStudio.exe --light" opens the Light page
	if len(os.Args) > 1 && os.Args[1] == "--light" {
		tabs.SetCurrentIndex(0)
		camTabs.SetCurrentIndex(1)
	}
	// "--tri" opens the Trichrome page (v0.82)
	if len(os.Args) > 1 && os.Args[1] == "--tri" {
		tabs.SetCurrentIndex(1)
		scanTabs.SetCurrentIndex(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "--mono" {
		tabs.SetCurrentIndex(1)
		scanTabs.SetCurrentIndex(2)
	}
	if len(os.Args) > 1 && os.Args[1] == "--focus" {
		tabs.SetCurrentIndex(1)
		scanTabs.SetCurrentIndex(3)
	}
	// "--camera" / "--white" / "--merge" (v0.83) open those sub-pages
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--camera":
			tabs.SetCurrentIndex(0)
		case "--white":
			tabs.SetCurrentIndex(1)
		case "--merge":
			tabs.SetCurrentIndex(2)
			cropTabs.SetCurrentIndex(1)
		case "--hairs":
			tabs.SetCurrentIndex(2)
			cropTabs.SetCurrentIndex(2)
		case "--batch":
			tabs.SetCurrentIndex(2)
			cropTabs.SetCurrentIndex(3)
		}
	}
	if len(migrated) > 0 {
		hdrui.say("Settings and film base DB copied from Film Base Meter (old folder left unchanged): " + strings.Join(migrated, ", "))
	}
	mw.Run()
}
