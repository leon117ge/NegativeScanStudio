// Package appstate is the toolkit-free part of the macOS UI: the page model
// (the first three modules and their sub-pages of the Windows walk UI in
// ../main.go; the Windows Convert module is not part of the macOS app, by
// the user's decision),
// the HDR scan settings file and the button rules. Nothing here imports Gio,
// so it builds and tests on every platform.
package appstate

// Module is one entry of the bottom page bar.
type Module struct {
	Title string   // label under the icon
	Tab   string   // page title as the Windows TabPage has it
	Icon  string   // icon name (see the icon functions in the Gio app)
	Subs  []string // segmented sub-pages (nil = a single page)
}

// Modules mirrors main.go: newNavBar(tabs, ...) and the camSeg / scanSeg /
// cropSeg segment bars, in the same order, without Windows' fourth module
// (Convert), which the macOS app does not have.
var Modules = []Module{
	{Title: "Camera & Light", Tab: "  1. Camera & Light  ", Icon: "light", Subs: []string{"Camera", "Light"}},
	{Title: "HDR Scan", Tab: "  2. HDR Scan  ", Icon: "hdr", Subs: []string{"White light scan", "Trichrome", "Mono trichrome", "Focus check"}},
	{Title: "Crop & Merge", Tab: "  3. Crop & Merge  ", Icon: "crop", Subs: []string{"Crop", "Merge files", "Remove hairs", "Batch RAW to TIFF"}},
}

// Page identifies a leaf page.
type Page string

const (
	PageCamera Page = "camera"
	PageLight  Page = "light"
	PageWhite  Page = "white"
	PageTri    Page = "tri"
	PageMono   Page = "mono"
	PageFocus  Page = "focus"
	PageCrop   Page = "crop"
	PageMerge  Page = "merge"
	PageHairs  Page = "hairs"
	PageBatch  Page = "batch"
)

var pageIDs = [][]Page{
	{PageCamera, PageLight},
	{PageWhite, PageTri, PageMono, PageFocus},
	{PageCrop, PageMerge, PageHairs, PageBatch},
}

// Ported lists the pages that are real in the Gio UI; any other page would
// show a placeholder (all pages are ported).
var Ported = map[Page]bool{PageCamera: true, PageLight: true, PageWhite: true, PageTri: true, PageMono: true, PageFocus: true, PageCrop: true, PageMerge: true, PageHairs: true, PageBatch: true}

// AllPages is every leaf page in Windows tab order.
func AllPages() []Page {
	var ps []Page
	for _, m := range pageIDs {
		ps = append(ps, m...)
	}
	return ps
}

// Nav is the current module and the current sub-page of every module.
type Nav struct {
	Module int
	Sub    [3]int
}

// SetModule selects a module; false if out of range or unchanged.
func (n *Nav) SetModule(m int) bool {
	if m < 0 || m >= len(Modules) || m == n.Module {
		return false
	}
	n.Module = m
	return true
}

// SetSub selects sub-page s of the current module.
func (n *Nav) SetSub(s int) bool {
	subs := len(Modules[n.Module].Subs)
	if s < 0 || s >= subs || s == n.Sub[n.Module] {
		return false
	}
	n.Sub[n.Module] = s
	return true
}

// Page is the leaf page on screen.
func (n Nav) Page() Page {
	ids := pageIDs[n.Module]
	s := n.Sub[n.Module]
	if s < 0 || s >= len(ids) {
		s = 0
	}
	return ids[s]
}

// LiveView is the live-view slot the visible page shows, as main.go's
// updateLivePage computes it: 0 Camera, 1 White light scan, 2 Trichrome,
// 3 Light (Fuji preview), -1 no camera live view (Mono trichrome and Focus
// check run their own Hikrobot preview, the other pages have none).
func (n Nav) LiveView() int {
	switch n.Module {
	case 0:
		if n.Sub[0] == 1 {
			return 3
		}
		return 0
	case 1:
		switch n.Sub[1] {
		case 1:
			return 2
		case 2, 3:
			return -1
		}
		return 1
	}
	return -1
}

// FromArgs applies the Windows command-line page switches (--camera,
// --light, --white, --tri, --mono, --focus, --crop, --merge, --hairs,
// --batch). files are the paths after --crop. Windows' --develop and
// --convert open its Convert module, which the macOS app does not have, so
// they are ignored here (the app opens on Camera & Light > Camera).
func FromArgs(args []string) (n Nav, files []string) {
	if len(args) == 0 {
		return
	}
	set := func(m, s int) { n.Module = m; n.Sub[m] = s }
	switch args[0] {
	case "--camera":
		set(0, 0)
	case "--light":
		set(0, 1)
	case "--white":
		set(1, 0)
	case "--tri":
		set(1, 1)
	case "--mono":
		set(1, 2)
	case "--focus":
		set(1, 3)
	case "--crop":
		set(2, 0)
		files = args[1:]
	case "--merge":
		set(2, 1)
	case "--hairs":
		set(2, 2)
	case "--batch":
		set(2, 3)
	}
	return
}
